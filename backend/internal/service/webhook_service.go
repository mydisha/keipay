package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Enqueuer schedules a webhook delivery for async forwarding (asynq).
type Enqueuer interface {
	EnqueueWebhookForward(ctx context.Context, deliveryID int64) error
}

// Idempotencer is the fast first-line dedup cache (Redis) in front of the
// durable webhook_events guard. Keys are marked only after an event commits.
type Idempotencer interface {
	Seen(ctx context.Context, key string) bool
	Mark(ctx context.Context, key string)
}

// ErrForwardNotQueued means the event was committed but its forward could not be
// enqueued. The delivery row is durable and the sweeper re-enqueues it, so the
// callback should still be acknowledged.
var ErrForwardNotQueued = errors.New("forward not queued")

// ErrCallbackMismatch means a callback does not belong to the payment it
// routes to: its amount or currency differs, or the payment was created
// through another connection. It is not applied and needs a manual look; the
// caller acknowledges it so the gateway stops retrying. An amount or currency
// mismatch is recorded as processed; a connection mismatch is not (see
// ConnectionMismatchError).
var ErrCallbackMismatch = errors.New("callback does not match payment")

// ConnectionMismatchError is the ErrCallbackMismatch of a callback that
// arrived at another connection than the payment's. Its event key is left
// unrecorded, so it cannot mark the payment's real callback as a duplicate.
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

// WebhookService applies verified gateway callbacks and queues app forwards.
type WebhookService struct {
	payments domain.PaymentRepository
	ledger   domain.LedgerRepository
	webhooks domain.WebhookRepository
	tx       domain.TxManager
	idemp    Idempotencer
	enqueuer Enqueuer
}

// NewWebhookService constructs a WebhookService.
func NewWebhookService(
	payments domain.PaymentRepository,
	ledger domain.LedgerRepository,
	webhooks domain.WebhookRepository,
	tx domain.TxManager,
	idemp Idempotencer,
	enqueuer Enqueuer,
) *WebhookService {
	return &WebhookService{payments: payments, ledger: ledger, webhooks: webhooks, tx: tx, idemp: idemp, enqueuer: enqueuer}
}

// ForwardPayload is the normalized event KeiPay forwards to apps. Fields are
// only ever added, so existing receivers keep working.
type ForwardPayload struct {
	Event           string               `json:"event"`
	PaymentID       int64                `json:"payment_id"`
	PaymentPublicID string               `json:"payment_public_id"`
	OrderID         string               `json:"order_id"`
	ExternalID      string               `json:"external_id"`
	Metadata        domain.Metadata      `json:"metadata"`
	Status          domain.PaymentStatus `json:"status"`
	Amount          int64                `json:"amount"`
	Currency        string               `json:"currency"`
	PaidAt          *time.Time           `json:"paid_at,omitempty"`
	SettledAt       *time.Time           `json:"settled_at,omitempty"`
	Timestamp       time.Time            `json:"timestamp"`
}

// Process applies one verified callback that arrived at conn. It is
// idempotent: a repeated (event key, provider status) is ignored. Returns nil
// when accepted or safely skipped.
//
// The idempotency record, status update, ledger entry and delivery row commit in
// one transaction, so a failure leaves nothing behind and the gateway's retry is
// processed in full. The forward is enqueued after commit; see ErrForwardNotQueued.
func (s *WebhookService) Process(ctx context.Context, conn *domain.Connection, ev provider.InboundEvent, raw []byte) error {
	if ev.Reference == "" || ev.EventKey == "" {
		return fmt.Errorf("%w: missing event key or reference", domain.ErrInvalidInput)
	}

	dedupKey := ev.EventKey + ":" + ev.ProviderStatus
	if s.idemp.Seen(ctx, dedupKey) {
		return nil // already processed (Redis)
	}

	var res applyResult
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		res, err = s.apply(ctx, conn, ev, raw)
		return err
	})
	if err != nil {
		return err
	}
	var foreign *ConnectionMismatchError
	if !errors.As(res.rejected, &foreign) {
		s.idemp.Mark(ctx, dedupKey) // see apply: a foreign callback must not consume the key
	}
	if res.rejected != nil {
		return res.rejected
	}
	if res.delivery == nil {
		return nil // duplicate or stale event
	}

	if err := s.enqueuer.EnqueueWebhookForward(ctx, res.delivery.ID); err != nil {
		return fmt.Errorf("%w: delivery %d: %v", ErrForwardNotQueued, res.delivery.ID, err)
	}
	return nil
}

// applyResult is what one committed callback produced.
type applyResult struct {
	// delivery is the forward to enqueue; nil for a duplicate or stale event.
	delivery *domain.WebhookDelivery
	// rejected is set when the event was recorded but deliberately not applied.
	rejected error
}

// apply writes the effects of one callback inside the caller's transaction.
func (s *WebhookService) apply(ctx context.Context, conn *domain.Connection, ev provider.InboundEvent, raw []byte) (applyResult, error) {
	// The reference is matched as stored, never parsed: it finds kp_pay_…
	// references and legacy {slug}:{order_id} ones alike. Locked until commit,
	// so PAID and SETTLED for one payment apply in turn.
	p, err := s.payments.GetByReferenceForUpdate(ctx, ev.Reference)
	if err != nil {
		return applyResult{}, fmt.Errorf("lookup payment %q: %w", ev.Reference, err)
	}

	// A callback only updates payments of the connection it arrived at: the
	// same reference at another gateway account is not this payment. This is
	// checked before the event is recorded because the dedupe key is not
	// scoped by connection: recording it would make the payment's own
	// gateway callback look like a duplicate and drop it.
	if p.ConnectionID == nil || *p.ConnectionID != conn.ID {
		return applyResult{rejected: &ConnectionMismatchError{PaymentID: p.ID, PaymentConnectionID: p.ConnectionID, ConnectionID: conn.ID}}, nil
	}

	isNew, err := s.webhooks.CreateEventIfNew(ctx, ev.EventKey, ev.ProviderStatus)
	if err != nil {
		return applyResult{}, err
	}
	if !isNew {
		return applyResult{}, nil
	}

	// Invoices have a fixed amount, so a mismatch means the callback is not for
	// this payment (e.g. an invoice made outside KeiPay reused external_id).
	// A missing amount/currency is not treated as a mismatch.
	if (ev.Amount != 0 && ev.Amount != p.Amount) || (ev.Currency != "" && ev.Currency != p.Currency) {
		return applyResult{rejected: fmt.Errorf("%w: payment %d is %d %s, callback has %d %s",
			ErrCallbackMismatch, p.ID, p.Amount, p.Currency, ev.Amount, ev.Currency)}, nil
	}

	status := ev.Status
	if !p.Status.CanTransitionTo(status) {
		return applyResult{}, nil // stale, e.g. PAID arriving after SETTLED
	}

	var paidAt *time.Time
	if status.IsSuccessful() {
		paidAt = ev.PaidAt
		if paidAt == nil && status == domain.PaymentStatusPaid {
			now := time.Now().UTC()
			paidAt = &now
		}
	}

	// Capture settlement timing. On a SETTLED event the funds have settled now;
	// PAID callbacks may carry settlement_date as a forward-looking estimate.
	var settledAt *time.Time
	if status == domain.PaymentStatusSettled {
		settledAt = ev.SettledAt
		if settledAt == nil {
			now := time.Now().UTC()
			settledAt = &now
		}
	}

	if err := s.payments.UpdateStatus(ctx, p.ID, status, paidAt, settledAt, raw); err != nil {
		return applyResult{}, err
	}

	// Record ledger on successful payment (idempotent via unique payment_id,type).
	if status.IsSuccessful() {
		amount := ev.PaidAmount
		if amount == 0 {
			amount = p.Amount
		}
		entry := &domain.LedgerEntry{
			AppID:     p.AppID,
			PaymentID: p.ID,
			Amount:    amount,
			XenditFee: ev.Fee,
			NetAmount: amount - ev.Fee,
			Type:      domain.LedgerEntryTypePayment,
			SettledAt: settledAt,
		}
		if err := s.ledger.Create(ctx, entry); err != nil {
			return applyResult{}, err
		}
		// The entry may already exist from PAID; record when it settled.
		if settledAt != nil {
			if err := s.ledger.MarkSettled(ctx, p.ID, *settledAt); err != nil {
				return applyResult{}, err
			}
		}
	}

	// Build normalized forward payload + durable delivery record.
	fp := ForwardPayload{
		Event:           "payment." + strings.ToLower(string(status)),
		PaymentID:       p.ID,
		PaymentPublicID: p.PublicID,
		OrderID:         p.OrderID,
		ExternalID:      p.Reference(),
		Metadata:        p.Metadata,
		Status:          status,
		Amount:          p.Amount,
		Currency:        p.Currency,
		PaidAt:          paidAt,
		SettledAt:       settledAt,
		Timestamp:       time.Now().UTC(),
	}
	payload, err := json.Marshal(fp)
	if err != nil {
		return applyResult{}, err
	}

	delivery := &domain.WebhookDelivery{
		PaymentID: p.ID,
		AppID:     p.AppID,
		EventType: fp.Event,
		Payload:   payload,
		Status:    domain.WebhookDeliveryPending,
	}
	if err := s.webhooks.CreateDelivery(ctx, delivery); err != nil {
		return applyResult{}, err
	}
	return applyResult{delivery: delivery}, nil
}
