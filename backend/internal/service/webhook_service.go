package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/xendit"
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

// ErrCallbackMismatch means a callback's amount or currency does not match the
// payment it routes to. The event is recorded (so it is not retried) but not
// applied; it needs a manual look.
var ErrCallbackMismatch = errors.New("callback does not match payment")

// XenditInvoiceCallback is the subset of Xendit's invoice webhook we consume.
type XenditInvoiceCallback struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
	Status     string `json:"status"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	PaidAmount int64  `json:"paid_amount"`
	FeesPaid   int64  `json:"fees_paid_amount"`
	PaidAt     string `json:"paid_at"`
	// SettlementDate is the date Xendit settles funds for this invoice. Xendit
	// sends it as "settlement_date" on PAID callbacks and as the event time on
	// SETTLED callbacks; we use whichever is present.
	SettlementDate string `json:"settlement_date"`
	Updated        string `json:"updated"`
}

// WebhookService processes inbound Xendit callbacks and queues tenant forwards.
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

// ForwardPayload is the normalized event KeiPay forwards to tenants.
type ForwardPayload struct {
	Event      string               `json:"event"`
	PaymentID  int64                `json:"payment_id"`
	OrderID    string               `json:"order_id"`
	ExternalID string               `json:"external_id"`
	Status     domain.PaymentStatus `json:"status"`
	Amount     int64                `json:"amount"`
	Currency   string               `json:"currency"`
	PaidAt     *time.Time           `json:"paid_at,omitempty"`
	SettledAt  *time.Time           `json:"settled_at,omitempty"`
	Timestamp  time.Time            `json:"timestamp"`
}

// parseTime parses an RFC3339 timestamp, returning nil on empty/invalid input.
func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}

// Process handles one inbound Xendit invoice callback. It is idempotent: repeated
// (id,status) events are ignored. Returns nil when accepted or safely skipped.
//
// The idempotency record, status update, ledger entry and delivery row commit in
// one transaction, so a failure leaves nothing behind and Xendit's retry is
// processed in full. The forward is enqueued after commit; see ErrForwardNotQueued.
func (s *WebhookService) Process(ctx context.Context, cb XenditInvoiceCallback, raw []byte) error {
	if cb.ExternalID == "" || cb.ID == "" {
		return fmt.Errorf("%w: missing id/external_id", domain.ErrInvalidInput)
	}

	dedupKey := cb.ID + ":" + cb.Status
	if s.idemp.Seen(ctx, dedupKey) {
		return nil // already processed (Redis)
	}

	var res applyResult
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		res, err = s.apply(ctx, cb, raw)
		return err
	})
	if err != nil {
		return err
	}
	s.idemp.Mark(ctx, dedupKey)
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
func (s *WebhookService) apply(ctx context.Context, cb XenditInvoiceCallback, raw []byte) (applyResult, error) {
	isNew, err := s.webhooks.CreateEventIfNew(ctx, cb.ID, cb.Status)
	if err != nil {
		return applyResult{}, err
	}
	if !isNew {
		return applyResult{}, nil
	}

	// Locked until commit, so PAID and SETTLED for one payment apply in turn.
	p, err := s.payments.GetByExternalIDForUpdate(ctx, cb.ExternalID)
	if err != nil {
		return applyResult{}, fmt.Errorf("lookup payment %q: %w", cb.ExternalID, err)
	}

	// Invoices have a fixed amount, so a mismatch means the callback is not for
	// this payment (e.g. an invoice made outside KeiPay reused external_id).
	// A missing amount/currency is not treated as a mismatch.
	if (cb.Amount != 0 && cb.Amount != p.Amount) || (cb.Currency != "" && cb.Currency != p.Currency) {
		return applyResult{rejected: fmt.Errorf("%w: payment %d is %d %s, callback has %d %s",
			ErrCallbackMismatch, p.ID, p.Amount, p.Currency, cb.Amount, cb.Currency)}, nil
	}

	status := xendit.MapStatus(cb.Status)
	if !p.Status.CanTransitionTo(status) {
		return applyResult{}, nil // stale, e.g. PAID arriving after SETTLED
	}

	var paidAt *time.Time
	if status.IsSuccessful() {
		paidAt = parseTime(cb.PaidAt)
		if paidAt == nil && status == domain.PaymentStatusPaid {
			now := time.Now().UTC()
			paidAt = &now
		}
	}

	// Capture settlement timing. On a SETTLED event the funds have settled now;
	// PAID callbacks may carry settlement_date as a forward-looking estimate.
	var settledAt *time.Time
	if status == domain.PaymentStatusSettled {
		settledAt = parseTime(cb.SettlementDate)
		if settledAt == nil {
			settledAt = parseTime(cb.Updated)
		}
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
		amount := cb.PaidAmount
		if amount == 0 {
			amount = p.Amount
		}
		entry := &domain.LedgerEntry{
			TenantID:  p.TenantID,
			PaymentID: p.ID,
			Amount:    amount,
			XenditFee: cb.FeesPaid,
			NetAmount: amount - cb.FeesPaid,
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
		Event:      "payment." + strings.ToLower(string(status)),
		PaymentID:  p.ID,
		OrderID:    p.OrderID,
		ExternalID: p.ExternalID,
		Status:     status,
		Amount:     p.Amount,
		Currency:   p.Currency,
		PaidAt:     paidAt,
		SettledAt:  settledAt,
		Timestamp:  time.Now().UTC(),
	}
	payload, err := json.Marshal(fp)
	if err != nil {
		return applyResult{}, err
	}

	delivery := &domain.WebhookDelivery{
		PaymentID: p.ID,
		TenantID:  p.TenantID,
		EventType: fp.Event,
		Payload:   payload,
		Status:    domain.WebhookDeliveryPending,
	}
	if err := s.webhooks.CreateDelivery(ctx, delivery); err != nil {
		return applyResult{}, err
	}
	return applyResult{delivery: delivery}, nil
}
