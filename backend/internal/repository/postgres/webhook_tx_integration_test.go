package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
)

// These tests need a migrated Postgres. They are skipped unless
// KEI_TEST_POSTGRES_DSN is set, e.g.
//
//	KEI_TEST_POSTGRES_DSN=postgres://keipay:keipay@localhost:5432/keipay?sslmode=disable \
//	  go test ./internal/repository/postgres/ -run Integration

type fixture struct {
	pool       *pgxpool.Pool
	payments   *postgres.PaymentRepo
	ledger     *postgres.LedgerRepo
	inbound    *postgres.InboundWebhookRepo
	events     *postgres.EventRepo
	endpoints  *postgres.WebhookEndpointRepo
	deliveries *postgres.DeliveryRepo
	tx         *postgres.TxManager
	app        *domain.App
	payment    *domain.Payment
	conn       *domain.Connection
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("KEI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("KEI_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	conn := newTestConnection(t, pool, domain.EnvLive)
	f := &fixture{
		pool:       pool,
		payments:   postgres.NewPaymentRepo(pool),
		ledger:     postgres.NewLedgerRepo(pool),
		inbound:    postgres.NewInboundWebhookRepo(pool),
		events:     postgres.NewEventRepo(pool),
		endpoints:  postgres.NewWebhookEndpointRepo(pool),
		deliveries: postgres.NewDeliveryRepo(pool),
		tx:         postgres.NewTxManager(pool),
		conn:       conn,
		app:        newTestApp(t, pool, domain.EnvLive),
	}
	f.payment = f.addPayment(t, conn, 5000)
	return f
}

func (f *fixture) addPayment(t *testing.T, conn *domain.Connection, amount int64) *domain.Payment {
	t.Helper()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: f.app.ID, OrderID: "ord-" + id.New(id.Payment)[20:],
		XenditInvoiceID: fmt.Sprintf("inv_%d", time.Now().UnixNano()), ConnectionID: &conn.ID, Provider: conn.Provider,
		Amount: amount, Currency: "IDR", Status: domain.PaymentStatusPending}
	p.ExternalID = "kp_" + p.PublicID
	if err := f.payments.Create(context.Background(), p); err != nil {
		t.Fatalf("payment: %v", err)
	}
	return p
}

func (f *fixture) svc(enq service.Enqueuer) *service.WebhookService {
	return f.svcWith(f.deliveries, enq)
}

func (f *fixture) svcWith(deliveries domain.DeliveryRepository, enq service.Enqueuer) *service.WebhookService {
	return service.NewWebhookService(service.WebhookDeps{
		Payments: f.payments, Ledger: f.ledger, Inbound: f.inbound, Events: f.events, Endpoints: f.endpoints,
		Deliveries: deliveries, Tx: f.tx, Enqueuer: enq,
	})
}

// addEndpoint stores an active endpoint of the fixture's app.
func (f *fixture) addEndpoint(t *testing.T, events ...string) *domain.WebhookEndpoint {
	t.Helper()
	e := &domain.WebhookEndpoint{PublicID: id.New(id.WebhookEndpoint), AppID: f.app.ID, URL: "https://hooks.example.com/" + id.New(id.Event)[20:],
		Events: events, APIVersion: domain.WebhookAPIV2, SecretEnc: "enc:secret", Status: domain.WebhookEndpointActive,
		RetryPolicy: domain.DefaultRetryPolicy()}
	if err := f.endpoints.Create(context.Background(), e); err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	return e
}

// counts returns the claimed inbound rows of the fixture's connection and the
// payment's ledger entries, events and deliveries.
func (f *fixture) counts(t *testing.T, p *domain.Payment) (claims, ledger, events, deliveries int) {
	t.Helper()
	ctx := context.Background()
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM inbound_webhooks WHERE reference=$1
		AND result IN ('applied','stale','mismatch','unmapped')`, p.ExternalID).Scan(&claims)
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE payment_id=$1`, p.ID).Scan(&ledger)
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE payment_id=$1`, p.ID).Scan(&events)
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE payment_id=$1`, p.ID).Scan(&deliveries)
	return
}

func (f *fixture) results(t *testing.T, ref string) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT result FROM inbound_webhooks WHERE reference=$1 ORDER BY id`, ref)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		_ = rows.Scan(&r)
		out = append(out, r)
	}
	return out
}

func callback(p *domain.Payment, status string) xenditCallback {
	return xenditCallback{ID: p.XenditInvoiceID, ExternalID: p.ExternalID, Status: status, PaidAmount: p.Amount, FeesPaid: 100}
}

// xenditCallback is a Xendit invoice callback; event parses it with the
// adapter, as the webhook handler does.
type xenditCallback struct {
	ID, ExternalID, Status string
	PaidAmount, FeesPaid   int64
	SettlementDate         string
}

func (cb xenditCallback) raw() []byte {
	raw, _ := json.Marshal(map[string]any{"id": cb.ID, "external_id": cb.ExternalID, "status": cb.Status,
		"paid_amount": cb.PaidAmount, "fees_paid_amount": cb.FeesPaid, "settlement_date": cb.SettlementDate})
	return raw
}

func (cb xenditCallback) event() provider.InboundEvent {
	ev, err := xendit.New().ParseWebhook(cb.raw())
	if err != nil {
		panic(err)
	}
	return *ev
}

func (f *fixture) process(svc *service.WebhookService, conn *domain.Connection, cb xenditCallback) error {
	return svc.Process(context.Background(), conn, cb.event(), cb.raw())
}

type stubEnqueuer struct {
	mu    sync.Mutex
	calls []int64
	err   error
}

func (e *stubEnqueuer) EnqueueDelivery(_ context.Context, id int64, _ int, _ bool, _ time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.calls = append(e.calls, id)
	return nil
}

func TestIntegrationWithinTxRollsBackAllWrites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	boom := errors.New("boom")
	key, status, ref := "inv_rollback", "PAID", f.payment.ExternalID
	err := f.tx.WithinTx(ctx, func(ctx context.Context) error {
		w := &domain.InboundWebhook{PublicID: id.New(id.Inbound), ConnectionID: f.conn.ID, Provider: "xendit", Source: "webhook",
			SignatureValid: true, Result: domain.InboundApplied, EventKey: &key, ProviderStatus: &status, Reference: &ref, RawBody: []byte(`{}`)}
		if ok, err := f.inbound.Claim(ctx, w); err != nil || !ok {
			return fmt.Errorf("claim: %v %w", ok, err)
		}
		if err := f.payments.UpdateStatus(ctx, f.payment.ID, domain.PaymentStatusPaid, nil, nil, nil); err != nil {
			return err
		}
		if err := f.ledger.Create(ctx, &domain.LedgerEntry{AppID: f.payment.AppID, PaymentID: f.payment.ID,
			Amount: 5000, NetAmount: 5000, Type: domain.LedgerEntryTypePayment}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if claims, led, _, _ := f.counts(t, f.payment); claims != 0 || led != 0 {
		t.Fatalf("rollback left rows: claims=%d ledger=%d", claims, led)
	}
	p, err := f.payments.GetByID(ctx, f.payment.AppID, f.payment.ID)
	if err != nil || p.Status != domain.PaymentStatusPending {
		t.Fatalf("status changed despite rollback: %+v %v", p, err)
	}
}

// failingDeliveries fails Create, the last write of an applied callback.
type failingDeliveries struct{ *postgres.DeliveryRepo }

func (failingDeliveries) Create(context.Context, *domain.WebhookDelivery) error {
	return errors.New("insert delivery failed")
}

// T-B16 / AC-15: a failure after the claim rolls the claim back; one error row
// is written; the gateway's retry is applied in full.
func TestIntegrationProcessRollbackReleasesClaim(t *testing.T) {
	f := newFixture(t)
	f.addEndpoint(t, "*")
	enq := &stubEnqueuer{}
	if err := f.process(f.svcWith(failingDeliveries{f.deliveries}, enq), f.conn, callback(f.payment, "PAID")); err == nil {
		t.Fatal("expected the first attempt to fail")
	}
	if claims, led, ev, del := f.counts(t, f.payment); claims+led+ev+del != 0 {
		t.Fatalf("failed attempt left rows: claims=%d ledger=%d events=%d deliveries=%d", claims, led, ev, del)
	}
	if err := f.process(f.svc(enq), f.conn, callback(f.payment, "PAID")); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if claims, led, ev, del := f.counts(t, f.payment); claims != 1 || led != 1 || ev != 1 || del != 1 || len(enq.calls) != 1 {
		t.Fatalf("retry: claims=%d ledger=%d events=%d deliveries=%d enqueued=%d", claims, led, ev, del, len(enq.calls))
	}
	var detail string
	if err := f.pool.QueryRow(context.Background(), `SELECT result_detail FROM inbound_webhooks WHERE reference=$1 AND result='error'`,
		f.payment.ExternalID).Scan(&detail); err != nil || detail != "processing failed" {
		t.Fatalf("error row: %q %v", detail, err)
	}
	if got := f.results(t, f.payment.ExternalID); fmt.Sprint(got) != "[error applied]" {
		t.Fatalf("results = %v", got)
	}
}

// AC-15: concurrent duplicates without Redis: the claim index makes exactly
// one of them do the work; the others are recorded as duplicates.
func TestIntegrationProcessConcurrentDuplicates(t *testing.T) {
	f := newFixture(t)
	f.addEndpoint(t, "*")
	enq := &stubEnqueuer{}
	svc := f.svc(enq)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- f.process(svc, f.conn, callback(f.payment, "PAID"))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	if claims, led, ev, del := f.counts(t, f.payment); claims != 1 || led != 1 || ev != 1 || del != 1 || len(enq.calls) != 1 {
		t.Fatalf("duplicates: claims=%d ledger=%d events=%d deliveries=%d enqueued=%d", claims, led, ev, del, len(enq.calls))
	}
	var dups int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM inbound_webhooks WHERE reference=$1 AND result='duplicate'`,
		f.payment.ExternalID).Scan(&dups)
	if dups != 7 {
		t.Fatalf("duplicates recorded = %d", dups)
	}
}

// T-B15 / AC-15: dedupe is per connection. The same key at another connection
// for this payment is a mismatch there and does not block the real callback.
func TestIntegrationDedupePerConnection(t *testing.T) {
	f := newFixture(t)
	f.addEndpoint(t, "*")
	enq := &stubEnqueuer{}
	svc := f.svc(enq)
	other := newTestConnection(t, f.pool, domain.EnvLive)
	if err := f.process(svc, other, callback(f.payment, "PAID")); !errors.Is(err, service.ErrCallbackMismatch) {
		t.Fatalf("foreign callback: %v", err)
	}
	if _, led, ev, del := f.counts(t, f.payment); led+ev+del != 0 {
		t.Fatalf("foreign callback applied: ledger=%d events=%d deliveries=%d", led, ev, del)
	}
	for range 2 {
		if err := f.process(svc, f.conn, callback(f.payment, "PAID")); err != nil {
			t.Fatalf("own callback: %v", err)
		}
	}
	if err := f.process(svc, other, callback(f.payment, "PAID")); err != nil {
		t.Fatalf("foreign retry: %v", err)
	}
	if got := f.results(t, f.payment.ExternalID); fmt.Sprint(got) != "[mismatch applied duplicate duplicate]" {
		t.Fatalf("results = %v", got)
	}
	if _, led, ev, del := f.counts(t, f.payment); led != 1 || ev != 1 || del != 1 || len(enq.calls) != 1 {
		t.Fatalf("own callback: ledger=%d events=%d deliveries=%d enqueued=%d", led, ev, del, len(enq.calls))
	}
	var paymentID *int64
	if err := f.pool.QueryRow(context.Background(), `SELECT payment_id FROM inbound_webhooks WHERE connection_id=$1 AND result='mismatch'`,
		other.ID).Scan(&paymentID); err != nil || paymentID == nil || *paymentID != f.payment.ID {
		t.Fatalf("mismatch row payment = %v %v", paymentID, err)
	}
}

// T-B11 over Postgres: one event, one delivery per matching active endpoint,
// bodies stored as JSONB.
func TestIntegrationProcessFansOut(t *testing.T) {
	f := newFixture(t)
	all := f.addEndpoint(t, "*")
	f.addEndpoint(t, "payment.settled")
	disabled := f.addEndpoint(t, "*")
	reason, now := domain.WebhookDisabledManual, time.Now()
	disabled.Status, disabled.DisabledReason, disabled.DisabledAt = domain.WebhookEndpointDisabled, &reason, &now
	deleted := f.addEndpoint(t, "payment.*")
	deleted.DeletedAt = &now
	for _, e := range []*domain.WebhookEndpoint{disabled, deleted} {
		if err := f.endpoints.Update(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.process(f.svc(&stubEnqueuer{}), f.conn, callback(f.payment, "PAID")); err != nil {
		t.Fatal(err)
	}
	page, err := f.deliveries.List(context.Background(), domain.DeliveryFilter{AppPublicID: &f.app.PublicID}, domain.Page{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Endpoint.PublicID != all.PublicID || page.Items[0].Event == nil ||
		page.Items[0].PaymentPublicID != f.payment.PublicID {
		t.Fatalf("deliveries = %+v %v", page.Items, err)
	}
	d, err := f.deliveries.GetByPublicID(context.Background(), page.Items[0].PublicID)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		ID   string `json:"id"`
		Data struct {
			ID           string `json:"id"`
			ConnectionID string `json:"connection_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(d.Payload, &body); err != nil || body.ID != d.Event.PublicID || body.Data.ID != f.payment.PublicID ||
		body.Data.ConnectionID != f.conn.PublicID {
		t.Fatalf("payload = %s %v", d.Payload, err)
	}
}

// When enqueue fails after commit the delivery is due for the sweeper.
func TestIntegrationEnqueueFailureLeavesDueDelivery(t *testing.T) {
	f := newFixture(t)
	f.addEndpoint(t, "*")
	err := f.process(f.svc(&stubEnqueuer{err: errors.New("redis down")}), f.conn, callback(f.payment, "PAID"))
	if !errors.Is(err, service.ErrForwardNotQueued) {
		t.Fatalf("expected ErrForwardNotQueued, got %v", err)
	}
	due, err := f.deliveries.ListDue(context.Background(), time.Now().Add(time.Minute), 1000)
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	for _, d := range due {
		if full, _ := f.deliveries.GetByPublicID(context.Background(), d.PublicID); full != nil && full.PaymentID != nil && *full.PaymentID == f.payment.ID {
			return
		}
	}
	t.Fatalf("delivery for payment %d not due", f.payment.ID)
}

func (f *fixture) status(t *testing.T) domain.PaymentStatus {
	t.Helper()
	p, err := f.payments.GetByID(context.Background(), f.payment.AppID, f.payment.ID)
	if err != nil {
		t.Fatalf("get payment: %v", err)
	}
	return p.Status
}

// Regression: SETTLED after PAID must record the settlement time on the ledger
// entry created by PAID, not leave it empty.
func TestIntegrationSettledAfterPaidSetsLedgerSettledAt(t *testing.T) {
	f := newFixture(t)
	svc := f.svc(&stubEnqueuer{})
	ctx := context.Background()
	settled := callback(f.payment, "SETTLED")
	settled.SettlementDate = "2026-01-02T03:04:05Z"
	for _, cb := range []xenditCallback{callback(f.payment, "PAID"), settled} {
		if err := f.process(svc, f.conn, cb); err != nil {
			t.Fatalf("process %s: %v", cb.Status, err)
		}
	}
	var settledAt *time.Time
	if err := f.pool.QueryRow(ctx, `SELECT settled_at FROM ledger_entries WHERE payment_id=$1`, f.payment.ID).Scan(&settledAt); err != nil {
		t.Fatalf("ledger: %v", err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if settledAt == nil || !settledAt.Equal(want) {
		t.Fatalf("ledger settled_at = %v, want %v", settledAt, want)
	}
}

// Regression: PAID and SETTLED racing for the same payment always end SETTLED,
// whichever transaction commits last; the loser is recorded stale.
func TestIntegrationConcurrentPaidAndSettledEndSettled(t *testing.T) {
	f := newFixture(t)
	svc := f.svc(&stubEnqueuer{})
	for i := range 20 {
		p := f.addPayment(t, f.conn, 5000)
		var wg sync.WaitGroup
		for _, cb := range []xenditCallback{callback(p, "SETTLED"), callback(p, "PAID")} {
			wg.Add(1)
			go func(cb xenditCallback) {
				defer wg.Done()
				if err := f.process(svc, f.conn, cb); err != nil {
					t.Errorf("process %s: %v", cb.Status, err)
				}
			}(cb)
		}
		wg.Wait()
		got, err := f.payments.GetByID(context.Background(), p.AppID, p.ID)
		if err != nil || got.Status != domain.PaymentStatusSettled {
			t.Fatalf("iteration %d: final status %v %v, want SETTLED", i, got, err)
		}
	}
}
