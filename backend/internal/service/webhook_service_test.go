package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// --- mocks ---

type mockPaymentRepo struct {
	byExternal map[string]*domain.Payment
	updated    []domain.PaymentStatus
	// failUpdates makes the next N UpdateStatus calls fail.
	failUpdates int
}

func (m *mockPaymentRepo) Create(context.Context, *domain.Payment) error { return nil }
func (m *mockPaymentRepo) UpdateStatus(ctx context.Context, id int64, s domain.PaymentStatus, _, _ *time.Time, _ []byte) error {
	if m.failUpdates > 0 {
		m.failUpdates--
		return errors.New("db down")
	}
	m.updated = append(m.updated, s)
	onRollback(ctx, func() { m.updated = m.updated[:len(m.updated)-1] })
	for _, p := range m.byExternal {
		if p.ID == id {
			prev := p.Status
			p.Status = s
			onRollback(ctx, func() { p.Status = prev })
		}
	}
	return nil
}
func (m *mockPaymentRepo) GetByID(context.Context, int64) (*domain.Payment, error) {
	return nil, domain.ErrNotFound
}
func (m *mockPaymentRepo) GetByExternalID(_ context.Context, ext string) (*domain.Payment, error) {
	if p, ok := m.byExternal[ext]; ok {
		return p, nil
	}
	return nil, domain.ErrNotFound
}
func (m *mockPaymentRepo) GetByExternalIDForUpdate(ctx context.Context, ext string) (*domain.Payment, error) {
	return m.GetByExternalID(ctx, ext)
}
func (m *mockPaymentRepo) List(context.Context, domain.PaymentFilter) ([]domain.Payment, int64, error) {
	return nil, 0, nil
}

type mockLedgerRepo struct {
	created int
	settled []time.Time
}

func (m *mockLedgerRepo) MarkSettled(ctx context.Context, _ int64, at time.Time) error {
	m.settled = append(m.settled, at)
	onRollback(ctx, func() { m.settled = m.settled[:len(m.settled)-1] })
	return nil
}

func (m *mockLedgerRepo) Create(ctx context.Context, _ *domain.LedgerEntry) error {
	m.created++
	onRollback(ctx, func() { m.created-- })
	return nil
}
func (m *mockLedgerRepo) List(context.Context, domain.LedgerFilter) ([]domain.LedgerEntry, error) {
	return nil, nil
}
func (m *mockLedgerRepo) Summary(context.Context, domain.LedgerFilter) ([]domain.LedgerSummary, error) {
	return nil, nil
}

type mockWebhookRepo struct {
	events     map[string]bool
	deliveries []domain.WebhookDelivery
	nextID     int64
}

func (m *mockWebhookRepo) CreateEventIfNew(ctx context.Context, id, status string) (bool, error) {
	key := id + ":" + status
	if m.events[key] {
		return false, nil
	}
	if m.events == nil {
		m.events = map[string]bool{}
	}
	m.events[key] = true
	onRollback(ctx, func() { delete(m.events, key) })
	return true, nil
}
func (m *mockWebhookRepo) CreateDelivery(ctx context.Context, d *domain.WebhookDelivery) error {
	m.nextID++
	d.ID = m.nextID
	m.deliveries = append(m.deliveries, *d)
	onRollback(ctx, func() { m.deliveries = m.deliveries[:len(m.deliveries)-1] })
	return nil
}
func (m *mockWebhookRepo) GetDelivery(context.Context, int64) (*domain.WebhookDelivery, error) {
	return nil, domain.ErrNotFound
}
func (m *mockWebhookRepo) MarkDelivered(context.Context, int64, int) error { return nil }
func (m *mockWebhookRepo) MarkFailed(context.Context, int64, int, string) error {
	return nil
}
func (m *mockWebhookRepo) ListDeliveries(context.Context, *int64, *domain.WebhookDeliveryStatus, int, int) ([]domain.WebhookDelivery, error) {
	return nil, nil
}
func (m *mockWebhookRepo) ListUnsentDeliveries(context.Context, time.Time, int) ([]domain.WebhookDelivery, error) {
	return nil, nil
}

// fakeTx mimics a DB transaction for in-memory mocks: writes register an undo
// via onRollback, and the undos run in reverse when fn fails.
type fakeTx struct{ undo []func() }

type fakeTxKey struct{}

type fakeTxManager struct{ commits, rollbacks int }

func (m *fakeTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx := &fakeTx{}
	if err := fn(context.WithValue(ctx, fakeTxKey{}, tx)); err != nil {
		for i := len(tx.undo) - 1; i >= 0; i-- {
			tx.undo[i]()
		}
		m.rollbacks++
		return err
	}
	m.commits++
	return nil
}

func onRollback(ctx context.Context, undo func()) {
	if tx, ok := ctx.Value(fakeTxKey{}).(*fakeTx); ok {
		tx.undo = append(tx.undo, undo)
	}
}

type memIdemp struct{ marked map[string]bool }

func (a *memIdemp) Seen(_ context.Context, key string) bool { return a.marked[key] }
func (a *memIdemp) Mark(_ context.Context, key string) {
	if a.marked == nil {
		a.marked = map[string]bool{}
	}
	a.marked[key] = true
}

type mockEnqueuer struct {
	enqueued []int64
	err      error
}

func (m *mockEnqueuer) EnqueueWebhookForward(_ context.Context, id int64) error {
	if m.err != nil {
		return m.err
	}
	m.enqueued = append(m.enqueued, id)
	return nil
}

// --- tests ---

func newSvc() (*WebhookService, *mockPaymentRepo, *mockLedgerRepo, *mockWebhookRepo, *mockEnqueuer) {
	pr := &mockPaymentRepo{byExternal: map[string]*domain.Payment{
		"acme:order-1": {ID: 10, TenantID: 1, OrderID: "order-1", ExternalID: "acme:order-1", Amount: 5000, Currency: "IDR", Status: domain.PaymentStatusPending},
	}}
	lr := &mockLedgerRepo{}
	wr := &mockWebhookRepo{}
	eq := &mockEnqueuer{}
	svc := NewWebhookService(pr, lr, wr, &fakeTxManager{}, &memIdemp{}, eq)
	return svc, pr, lr, wr, eq
}

func TestProcessPaidRoutesToTenantAndLedger(t *testing.T) {
	svc, pr, lr, _, eq := newSvc()
	cb := XenditInvoiceCallback{
		ID:         "inv_1",
		ExternalID: "acme:order-1",
		Status:     "PAID",
		PaidAmount: 5000,
		FeesPaid:   100,
	}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(pr.updated) != 1 || pr.updated[0] != domain.PaymentStatusPaid {
		t.Fatalf("expected payment marked PAID, got %v", pr.updated)
	}
	if lr.created != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", lr.created)
	}
	if len(eq.enqueued) != 1 {
		t.Fatalf("expected 1 enqueued forward, got %d", len(eq.enqueued))
	}
}

func TestProcessIdempotentDuplicate(t *testing.T) {
	svc, _, lr, _, eq := newSvc()
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", PaidAmount: 5000}
	_ = svc.Process(context.Background(), cb, []byte(`{}`))
	_ = svc.Process(context.Background(), cb, []byte(`{}`))
	if lr.created != 1 {
		t.Fatalf("duplicate created extra ledger: %d", lr.created)
	}
	if len(eq.enqueued) != 1 {
		t.Fatalf("duplicate enqueued extra forward: %d", len(eq.enqueued))
	}
}

func TestProcessSettledRecordsSettlement(t *testing.T) {
	svc, pr, lr, wr, _ := newSvc()
	cb := XenditInvoiceCallback{
		ID:             "inv_2",
		ExternalID:     "acme:order-1",
		Status:         "SETTLED",
		PaidAmount:     5000,
		FeesPaid:       100,
		SettlementDate: "2026-01-02T03:04:05Z",
	}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(pr.updated) != 1 || pr.updated[0] != domain.PaymentStatusSettled {
		t.Fatalf("expected payment marked SETTLED, got %v", pr.updated)
	}
	if lr.created != 1 {
		t.Fatalf("expected 1 ledger entry on settle, got %d", lr.created)
	}
	if len(wr.deliveries) != 1 || wr.deliveries[0].EventType != "payment.settled" {
		t.Fatalf("expected payment.settled forward, got %+v", wr.deliveries)
	}
}

// Regression: a callback that fails mid-processing must be processed in full
// when Xendit retries it, not skipped as a duplicate.
func TestProcessRetryAfterFailureIsNotSkipped(t *testing.T) {
	svc, pr, lr, wr, eq := newSvc()
	pr.failUpdates = 1
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", PaidAmount: 5000}

	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(pr.updated) != 1 || pr.updated[0] != domain.PaymentStatusPaid {
		t.Fatalf("retry did not mark payment PAID, got %v", pr.updated)
	}
	if lr.created != 1 {
		t.Fatalf("expected 1 ledger entry after retry, got %d", lr.created)
	}
	if len(wr.deliveries) != 1 || len(eq.enqueued) != 1 {
		t.Fatalf("expected 1 delivery enqueued after retry, got deliveries=%d enqueued=%d", len(wr.deliveries), len(eq.enqueued))
	}
}

// A failed attempt must not mark the event in Redis, or the retry would be
// skipped before reaching the DB.
func TestProcessFailureLeavesNoTrace(t *testing.T) {
	svc, pr, lr, wr, eq := newSvc()
	pr.failUpdates = 1
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", PaidAmount: 5000}

	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err == nil {
		t.Fatal("expected failure")
	}
	if svc.idemp.Seen(context.Background(), "inv_1:PAID") {
		t.Fatal("failed event was marked in redis")
	}
	if len(wr.events) != 0 || lr.created != 0 || len(wr.deliveries) != 0 || len(eq.enqueued) != 0 {
		t.Fatalf("failed attempt left writes: events=%d ledger=%d deliveries=%d enqueued=%d",
			len(wr.events), lr.created, len(wr.deliveries), len(eq.enqueued))
	}
	if got := svc.tx.(*fakeTxManager).rollbacks; got != 1 {
		t.Fatalf("expected 1 rollback, got %d", got)
	}
}

// An unknown external_id rolls back too, so the event is not burned if the
// payment row appears later (e.g. a slow insert on create).
func TestProcessUnknownExternalIDNotRecorded(t *testing.T) {
	svc, _, _, wr, _ := newSvc()
	cb := XenditInvoiceCallback{ID: "inv_x", ExternalID: "ghost:order", Status: "PAID"}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if len(wr.events) != 0 {
		t.Fatalf("unknown external_id recorded an event: %v", wr.events)
	}
}

// When the queue is down after commit, the delivery row stays (for the sweeper)
// and the caller gets ErrForwardNotQueued so it can still ack Xendit.
func TestProcessEnqueueFailureKeepsDelivery(t *testing.T) {
	svc, pr, lr, wr, eq := newSvc()
	eq.err = errors.New("redis down")
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", PaidAmount: 5000}

	err := svc.Process(context.Background(), cb, []byte(`{}`))
	if !errors.Is(err, ErrForwardNotQueued) {
		t.Fatalf("expected ErrForwardNotQueued, got %v", err)
	}
	if len(pr.updated) != 1 || lr.created != 1 || len(wr.deliveries) != 1 {
		t.Fatalf("expected committed writes, got updated=%v ledger=%d deliveries=%d", pr.updated, lr.created, len(wr.deliveries))
	}
	if !svc.idemp.Seen(context.Background(), "inv_1:PAID") {
		t.Fatal("committed event should be marked in redis")
	}

	// The retry Xendit would not send anyway is still a no-op.
	eq.err = nil
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if len(wr.deliveries) != 1 || lr.created != 1 {
		t.Fatalf("duplicate wrote again: deliveries=%d ledger=%d", len(wr.deliveries), lr.created)
	}
}

// Redis being empty (evicted, flushed, down) must not cause double processing:
// the DB event record is the final guard.
func TestProcessDBGuardWithoutRedis(t *testing.T) {
	svc, _, lr, wr, eq := newSvc()
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", PaidAmount: 5000}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("process: %v", err)
	}
	svc.idemp.(*memIdemp).marked = nil
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if lr.created != 1 || len(wr.deliveries) != 1 || len(eq.enqueued) != 1 {
		t.Fatalf("duplicate processed twice: ledger=%d deliveries=%d enqueued=%d", lr.created, len(wr.deliveries), len(eq.enqueued))
	}
}

func TestProcessMissingIDs(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	for _, cb := range []XenditInvoiceCallback{
		{ExternalID: "acme:order-1", Status: "PAID"},
		{ID: "inv_1", Status: "PAID"},
	} {
		if err := svc.Process(context.Background(), cb, nil); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for %+v, got %v", cb, err)
		}
	}
}

// Regression: callbacks can arrive out of order. A late PAID after SETTLED must
// not move the payment back to PAID or send the tenant a stale payment.paid.
func TestProcessLatePaidAfterSettledIsIgnored(t *testing.T) {
	svc, pr, _, wr, _ := newSvc()
	ctx := context.Background()
	settled := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "SETTLED", Amount: 5000, PaidAmount: 5000}
	paid := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", Amount: 5000, PaidAmount: 5000}

	if err := svc.Process(ctx, settled, []byte(`{}`)); err != nil {
		t.Fatalf("settled: %v", err)
	}
	if err := svc.Process(ctx, paid, []byte(`{}`)); err != nil {
		t.Fatalf("late paid: %v", err)
	}
	if got := pr.byExternal["acme:order-1"].Status; got != domain.PaymentStatusSettled {
		t.Fatalf("status regressed to %s", got)
	}
	if len(wr.deliveries) != 1 || wr.deliveries[0].EventType != "payment.settled" {
		t.Fatalf("expected only payment.settled forwarded, got %+v", wr.deliveries)
	}
}

// Regression: a callback whose amount differs from the payment (e.g. an invoice
// made outside KeiPayment that reused the external_id) must not mark it paid.
func TestProcessAmountMismatchIsNotApplied(t *testing.T) {
	svc, pr, lr, wr, _ := newSvc()
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", Amount: 1000, PaidAmount: 1000}

	err := svc.Process(context.Background(), cb, []byte(`{}`))
	if !errors.Is(err, ErrCallbackMismatch) {
		t.Fatalf("expected ErrCallbackMismatch, got %v", err)
	}
	if len(pr.updated) != 0 || lr.created != 0 || len(wr.deliveries) != 0 {
		t.Fatalf("mismatched callback applied: updated=%v ledger=%d deliveries=%d", pr.updated, lr.created, len(wr.deliveries))
	}
	// Recorded, so Xendit's retry of the same callback is a quiet duplicate.
	if !wr.events["inv_1:PAID"] {
		t.Fatal("mismatched event not recorded")
	}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("retry of mismatched callback: %v", err)
	}
}

func TestProcessCurrencyMismatchIsNotApplied(t *testing.T) {
	svc, pr, _, _, _ := newSvc()
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", Amount: 5000, Currency: "USD"}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); !errors.Is(err, ErrCallbackMismatch) {
		t.Fatalf("expected ErrCallbackMismatch, got %v", err)
	}
	if len(pr.updated) != 0 {
		t.Fatalf("currency mismatch applied: %v", pr.updated)
	}
}

// Matching amount and currency, as Xendit sends them, is applied normally.
func TestProcessMatchingAmountAndCurrencyApplied(t *testing.T) {
	svc, pr, _, _, _ := newSvc()
	cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: "PAID", Amount: 5000, Currency: "IDR"}
	if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(pr.updated) != 1 || pr.updated[0] != domain.PaymentStatusPaid {
		t.Fatalf("expected PAID, got %v", pr.updated)
	}
}

// A payment captured after the invoice expired is still recorded and forwarded:
// the money is in the balance.
func TestProcessPaidAfterExpiredIsApplied(t *testing.T) {
	svc, pr, lr, wr, _ := newSvc()
	ctx := context.Background()
	for _, st := range []string{"EXPIRED", "PAID"} {
		cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: st, Amount: 5000}
		if err := svc.Process(ctx, cb, []byte(`{}`)); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
	if got := pr.byExternal["acme:order-1"].Status; got != domain.PaymentStatusPaid {
		t.Fatalf("expected PAID, got %s", got)
	}
	if lr.created != 1 || len(wr.deliveries) != 2 || wr.deliveries[1].EventType != "payment.paid" {
		t.Fatalf("expected ledger + payment.paid forward, got ledger=%d deliveries=%+v", lr.created, wr.deliveries)
	}
}

// An unrecognised Xendit status maps to PENDING; it must not undo a payment.
func TestProcessUnknownStatusAfterPaidIsIgnored(t *testing.T) {
	svc, pr, _, wr, _ := newSvc()
	ctx := context.Background()
	for _, st := range []string{"PAID", "SOMETHING_NEW"} {
		cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: st, Amount: 5000}
		if err := svc.Process(ctx, cb, []byte(`{}`)); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
	if got := pr.byExternal["acme:order-1"].Status; got != domain.PaymentStatusPaid {
		t.Fatalf("status changed to %s", got)
	}
	if len(wr.deliveries) != 1 {
		t.Fatalf("unknown status forwarded: %+v", wr.deliveries)
	}
}

func TestProcessSettledMarksLedgerSettled(t *testing.T) {
	svc, _, lr, _, _ := newSvc()
	ctx := context.Background()
	for _, st := range []string{"PAID", "SETTLED"} {
		cb := XenditInvoiceCallback{ID: "inv_1", ExternalID: "acme:order-1", Status: st, Amount: 5000,
			SettlementDate: "2026-01-02T03:04:05Z"}
		if err := svc.Process(ctx, cb, []byte(`{}`)); err != nil {
			t.Fatalf("%s: %v", st, err)
		}
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if len(lr.settled) != 1 || !lr.settled[0].Equal(want) {
		t.Fatalf("expected ledger settled once at %v, got %v", want, lr.settled)
	}
}
