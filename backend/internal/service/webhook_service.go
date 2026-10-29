package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Enqueuer schedules one attempt of a delivery on the queue. Enqueuing an
// attempt that is already queued is a no-op.
type Enqueuer interface {
	EnqueueDelivery(ctx context.Context, deliveryID int64, attempt int, manual bool, at time.Time) error
}

// ErrForwardNotQueued means the event was committed but a delivery could not
// be enqueued. The delivery row is durable and the sweeper re-enqueues it, so
// the callback should still be acknowledged.
var ErrForwardNotQueued = errors.New("forward not queued")

// ErrCallbackMismatch means a callback does not belong to the payment it
// routes to: its amount or currency differs, or the payment was created
// through another connection. It is recorded as mismatch, not applied, and
// needs a manual look; the caller acknowledges it so the gateway stops
// retrying.
var ErrCallbackMismatch = errors.New("callback does not match payment")

// ConnectionMismatchError is the ErrCallbackMismatch of a callback that
// arrived at another connection than the payment's. It carries the ids the
// security log needs.
type ConnectionMismatchError struct {
	PaymentID int64
	// PaymentConnectionID is nil for a payment without a connection.
	PaymentConnectionID *int64
	ConnectionID        int64
}

func (e *ConnectionMismatchError) Error() string {
	paymentConn := "none"
	if e.PaymentConnectionID != nil {
		paymentConn = fmt.Sprint(*e.PaymentConnectionID)
	}
	return fmt.Sprintf("%v: payment %d belongs to connection %s, callback arrived at connection %d",
		ErrCallbackMismatch, e.PaymentID, paymentConn, e.ConnectionID)
}

func (e *ConnectionMismatchError) Unwrap() error { return ErrCallbackMismatch }

// Result details stored on inbound webhooks. They are fixed strings, so no
// gateway text or internal error reaches the console through them.
const (
	detailOtherConnection  = "payment belongs to another connection"
	detailAmountMismatch   = "amount or currency differs from the payment"
	detailUnknownReference = "unknown payment reference"
	detailProcessingFailed = "processing failed"
	detailMissingKey       = "missing event key or reference"
)

// WebhookDeps are the collaborators of a WebhookService.
type WebhookDeps struct {
	Payments   domain.PaymentRepository
	Ledger     domain.LedgerRepository
	Inbound    domain.InboundWebhookRepository
	Events     domain.EventRepository
	Endpoints  domain.WebhookEndpointRepository
	Deliveries domain.DeliveryRepository
	Tx         domain.TxManager
	Enqueuer   Enqueuer
	Log        *zap.Logger
}

// WebhookService records every callback that reaches a known connection,
// applies the verified ones, and fans the resulting event out to the app's
// webhook endpoints.
type WebhookService struct {
	payments   domain.PaymentRepository
	ledger     domain.LedgerRepository
	inbound    domain.InboundWebhookRepository
	events     domain.EventRepository
	endpoints  domain.WebhookEndpointRepository
	deliveries domain.DeliveryRepository
	tx         domain.TxManager
	enqueuer   Enqueuer
	log        *zap.Logger
	now        func() time.Time
}

// NewWebhookService constructs a WebhookService.
func NewWebhookService(d WebhookDeps) *WebhookService {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &WebhookService{
		payments: d.Payments, ledger: d.Ledger, inbound: d.Inbound, events: d.Events, endpoints: d.Endpoints,
		deliveries: d.Deliveries, tx: d.Tx, enqueuer: d.Enqueuer, log: log, now: time.Now,
	}
}

func (s *WebhookService) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

// processOutcome is what one committed callback produced.
type processOutcome struct {
	duplicate bool
	// rejected is set when the callback was recorded but deliberately not applied.
	rejected   error
	deliveries []domain.WebhookDelivery
}

// Process applies one verified callback that arrived at conn and records it
// in the inbound log. It returns nil when the callback was applied or safely
// skipped (duplicate, stale, unmapped).
//
// The dedupe claim, status update, ledger entry, event and deliveries commit
// in one transaction, so a failure leaves nothing behind (only an error row)
// and the gateway's retry is processed in full. Deliveries are enqueued after
// commit; see ErrForwardNotQueued.
func (s *WebhookService) Process(ctx context.Context, conn *domain.Connection, ev provider.InboundEvent, raw []byte) error {
	if ev.Reference == "" || ev.EventKey == "" {
		s.record(ctx, s.newInbound(conn, raw, domain.InboundInvalidPayload, detailMissingKey))
		return fmt.Errorf("%w: %s", domain.ErrInvalidInput, detailMissingKey)
	}

	var out processOutcome
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		out, err = s.apply(ctx, conn, ev, raw)
		return err
	})
	if err != nil {
		detail := detailProcessingFailed
		if errors.Is(err, domain.ErrNotFound) {
			detail = detailUnknownReference
		}
		s.record(ctx, s.callbackInbound(conn, ev, raw, domain.InboundError, detail))
		return err
	}
	if out.duplicate {
		s.record(ctx, s.callbackInbound(conn, ev, raw, domain.InboundDuplicate, ""))
		return nil
	}
	if out.rejected != nil {
		return out.rejected
	}
	return s.enqueueAll(ctx, out.deliveries)
}

// RecordRejected logs a callback that failed before processing: verification
// (InboundInvalidSignature; the body is cut to MaxRejectedBodyBytes, since
// anyone can send it) or parsing (InboundInvalidPayload). Failing to write
// is logged, not returned.
func (s *WebhookService) RecordRejected(ctx context.Context, conn *domain.Connection, raw []byte, result domain.InboundResult, detail string) {
	s.record(ctx, s.newInbound(conn, raw, result, detail))
}

func (s *WebhookService) newInbound(conn *domain.Connection, raw []byte, result domain.InboundResult, detail string) *domain.InboundWebhook {
	w := &domain.InboundWebhook{
		PublicID: id.New(id.Inbound), ConnectionID: conn.ID, Connection: conn.Summary(), Provider: conn.Provider,
		Source: domain.InboundSourceWebhook, SignatureValid: result != domain.InboundInvalidSignature,
		Result: result, ResultDetail: detail, RawBody: raw,
	}
	if !w.SignatureValid && len(raw) > domain.MaxRejectedBodyBytes {
		w.RawBody, w.BodyTruncated = raw[:domain.MaxRejectedBodyBytes], true
	}
	return w
}

// callbackInbound is the log row of a verified, parsed callback.
func (s *WebhookService) callbackInbound(conn *domain.Connection, ev provider.InboundEvent, raw []byte, result domain.InboundResult, detail string) *domain.InboundWebhook {
	w := s.newInbound(conn, raw, result, detail)
	key, status, ref := ev.EventKey, ev.ProviderStatus, ev.Reference
	w.EventKey, w.ProviderStatus, w.Reference = &key, &status, &ref
	return w
}

// record writes a log row outside any transaction, best effort.
func (s *WebhookService) record(ctx context.Context, w *domain.InboundWebhook) {
	if err := s.inbound.Create(ctx, w); err != nil {
		logger.Ctx(ctx, s.log).Error("inbound webhook not recorded",
			zap.String(logger.FieldLogType, logger.LogTypeWebhook),
			zap.String(logger.FieldEvent, "webhook.inbound.record_failed"),
			zap.String("connection_id", w.Connection.PublicID),
			zap.String("result", string(w.Result)),
			zap.Error(err),
		)
	}
}

// apply writes the effects of one callback inside the caller's transaction.
func (s *WebhookService) apply(ctx context.Context, conn *domain.Connection, ev provider.InboundEvent, raw []byte) (processOutcome, error) {
	row := s.callbackInbound(conn, ev, raw, domain.InboundApplied, "")
	claimed, err := s.inbound.Claim(ctx, row)
	if err != nil {
		return processOutcome{}, err
	}
	if !claimed {
		return processOutcome{duplicate: true}, nil
	}
	settle := func(result domain.InboundResult, detail string, paymentID *int64) error {
		row.Result, row.ResultDetail, row.PaymentID = result, detail, paymentID
		return s.inbound.UpdateOutcome(ctx, row)
	}

	if ev.Status == "" {
		return processOutcome{}, settle(domain.InboundUnmapped, fmt.Sprintf("unknown gateway status %q", ev.ProviderStatus), nil)
	}

	// The reference is matched as stored, never parsed: it finds kp_pay_…
	// references and legacy {slug}:{order_id} ones alike. Locked until commit,
	// so PAID and SETTLED for one payment apply in turn.
	p, err := s.payments.GetByReferenceForUpdate(ctx, ev.Reference)
	if err != nil {
		return processOutcome{}, fmt.Errorf("lookup payment %q: %w", ev.Reference, err)
	}

	// A callback only updates payments of the connection it arrived at: the
	// same reference at another gateway account is not this payment.
	if p.ConnectionID == nil || *p.ConnectionID != conn.ID {
		if err := settle(domain.InboundMismatch, detailOtherConnection, &p.ID); err != nil {
			return processOutcome{}, err
		}
		return processOutcome{rejected: &ConnectionMismatchError{PaymentID: p.ID, PaymentConnectionID: p.ConnectionID, ConnectionID: conn.ID}}, nil
	}

	// Invoices have a fixed amount, so a mismatch means the callback is not for
	// this payment (e.g. an invoice made outside KeiPay reused external_id).
	// A missing amount/currency is not treated as a mismatch.
	if (ev.Amount != 0 && ev.Amount != p.Amount) || (ev.Currency != "" && ev.Currency != p.Currency) {
		if err := settle(domain.InboundMismatch, detailAmountMismatch, &p.ID); err != nil {
			return processOutcome{}, err
		}
		return processOutcome{rejected: fmt.Errorf("%w: payment %d is %d %s, callback has %d %s",
			ErrCallbackMismatch, p.ID, p.Amount, p.Currency, ev.Amount, ev.Currency)}, nil
	}

	if !p.Status.CanTransitionTo(ev.Status) {
		return processOutcome{}, settle(domain.InboundStale, "", &p.ID)
	}

	paidAt, settledAt, err := s.applyStatus(ctx, p, ev, raw)
	if err != nil {
		return processOutcome{}, err
	}
	event, deliveries, err := s.fanOut(ctx, conn, p, ev.Status, paidAt, settledAt)
	if err != nil {
		return processOutcome{}, err
	}
	row.PaymentID, row.EventID = &p.ID, eventIDOf(event)
	if err := s.inbound.UpdateOutcome(ctx, row); err != nil {
		return processOutcome{}, err
	}
	return processOutcome{deliveries: deliveries}, nil
}

func eventIDOf(e *domain.Event) *int64 {
	if e == nil {
		return nil
	}
	return &e.ID
}

// applyStatus moves p to ev.Status and books a successful payment in the
// ledger. It returns the payment's paid and settled times after the change.
func (s *WebhookService) applyStatus(ctx context.Context, p *domain.Payment, ev provider.InboundEvent, raw []byte) (paidAt, settledAt *time.Time, err error) {
	status := ev.Status
	var newPaidAt *time.Time
	if status.IsSuccessful() {
		newPaidAt = ev.PaidAt
		if newPaidAt == nil && status == domain.PaymentStatusPaid {
			now := s.timestamp()
			newPaidAt = &now
		}
	}
	// On a SETTLED event the funds have settled now; PAID callbacks may carry
	// settlement_date only as a forward-looking estimate.
	var newSettledAt *time.Time
	if status == domain.PaymentStatusSettled {
		newSettledAt = ev.SettledAt
		if newSettledAt == nil {
			now := s.timestamp()
			newSettledAt = &now
		}
	}
	if err := s.payments.UpdateStatus(ctx, p.ID, status, newPaidAt, newSettledAt, raw); err != nil {
		return nil, nil, err
	}

	if status.IsSuccessful() {
		amount := ev.PaidAmount
		if amount == 0 {
			amount = p.Amount
		}
		entry := &domain.LedgerEntry{
			AppID: p.AppID, PaymentID: p.ID, Amount: amount, XenditFee: ev.Fee,
			NetAmount: amount - ev.Fee, Type: domain.LedgerEntryTypePayment, SettledAt: newSettledAt,
		}
		// Idempotent via the unique (payment_id, type).
		if err := s.ledger.Create(ctx, entry); err != nil {
			return nil, nil, err
		}
		// The entry may already exist from PAID; record when it settled.
		if newSettledAt != nil {
			if err := s.ledger.MarkSettled(ctx, p.ID, *newSettledAt); err != nil {
				return nil, nil, err
			}
		}
	}
	// UpdateStatus keeps earlier times when the new ones are nil.
	paidAt, settledAt = p.PaidAt, p.SettledAt
	if newPaidAt != nil {
		paidAt = newPaidAt
	}
	if newSettledAt != nil {
		settledAt = newSettledAt
	}
	return paidAt, settledAt, nil
}

// fanOut creates the event of p moving to status and one PENDING delivery per
// active endpoint of the app whose subscriptions match. A status that emits
// no event returns nil.
func (s *WebhookService) fanOut(ctx context.Context, conn *domain.Connection, p *domain.Payment, status domain.PaymentStatus,
	paidAt, settledAt *time.Time) (*domain.Event, []domain.WebhookDelivery, error) {
	eventType, ok := domain.EventTypeForStatus(status)
	if !ok {
		return nil, nil, nil
	}
	data, err := marshalData(paymentEventData(p, conn, status, paidAt, settledAt))
	if err != nil {
		return nil, nil, err
	}
	paymentID := p.ID
	event := &domain.Event{
		PublicID: id.New(id.Event), Type: eventType, APIVersion: domain.EventAPIVersion, AppID: p.AppID,
		PaymentID: &paymentID, Data: data, CreatedAt: s.timestamp(),
	}
	if err := s.events.Create(ctx, event); err != nil {
		return nil, nil, err
	}
	endpoints, err := s.endpoints.ListActiveByApp(ctx, p.AppID)
	if err != nil {
		return nil, nil, err
	}
	var out []domain.WebhookDelivery
	for i := range endpoints {
		ep := &endpoints[i]
		if !domain.MatchSubscription(ep.Events, eventType) {
			continue
		}
		d, err := newDelivery(event, ep, ep.RetryPolicy.MaxAttempts, event.CreatedAt)
		if err != nil {
			return nil, nil, err
		}
		d.PaymentPublicID = p.PublicID
		if err := s.deliveries.Create(ctx, d); err != nil {
			return nil, nil, err
		}
		out = append(out, *d)
	}
	return event, out, nil
}

// newDelivery is an unsaved PENDING delivery of event to ep, due at.
func newDelivery(event *domain.Event, ep *domain.WebhookEndpoint, maxAttempts int, at time.Time) (*domain.WebhookDelivery, error) {
	body, err := renderBody(event, ep.APIVersion)
	if err != nil {
		return nil, err
	}
	ref, endpointRef, endpointID, eventID := domain.EventRef{PublicID: event.PublicID, Type: event.Type, IsTest: event.IsTest},
		ep.Ref(), ep.ID, event.ID
	return &domain.WebhookDelivery{
		PublicID: id.New(id.Delivery), PaymentID: event.PaymentID, AppID: event.AppID, App: ep.App,
		EventID: &eventID, Event: &ref, EndpointID: &endpointID, Endpoint: &endpointRef,
		EventType: string(event.Type), Payload: body, Status: domain.WebhookDeliveryPending,
		MaxAttempts: maxAttempts, NextAttemptAt: &at,
	}, nil
}

// enqueueAll queues the first attempt of each delivery. A failure is
// ErrForwardNotQueued: the rows are committed and the sweeper sends them.
func (s *WebhookService) enqueueAll(ctx context.Context, deliveries []domain.WebhookDelivery) error {
	var failed []error
	for _, d := range deliveries {
		if err := s.enqueuer.EnqueueDelivery(ctx, d.ID, 1, false, *d.NextAttemptAt); err != nil {
			failed = append(failed, fmt.Errorf("delivery %s: %w", d.PublicID, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%w: %w", ErrForwardNotQueued, errors.Join(failed...))
	}
	return nil
}
