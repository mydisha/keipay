package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
)

// These tests need a migrated Postgres. They are skipped unless
// KEI_TEST_POSTGRES_DSN is set, e.g.
//
//	KEI_TEST_POSTGRES_DSN=postgres://keipay:keipay@localhost:5432/keipay?sslmode=disable \
//	  go test ./internal/repository/postgres/ -run Integration

type fixture struct {
	pool     *pgxpool.Pool
	payments *postgres.PaymentRepo
	ledger   *postgres.LedgerRepo
	webhooks *postgres.WebhookRepo
	tx       *postgres.TxManager
	payment  *domain.Payment
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

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	app := newTestApp(t, pool, domain.EnvLive)
	f := &fixture{
		pool:     pool,
		payments: postgres.NewPaymentRepo(pool),
		ledger:   postgres.NewLedgerRepo(pool),
		webhooks: postgres.NewWebhookRepo(pool),
		tx:       postgres.NewTxManager(pool),
	}
	f.payment = &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: "ord", ExternalID: app.Slug + ":ord",
		XenditInvoiceID: "inv_" + suffix, Amount: 5000, Currency: "IDR", Status: domain.PaymentStatusPending}
	if err := f.payments.Create(ctx, f.payment); err != nil {
		t.Fatalf("payment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM webhook_events WHERE xendit_id=$1`, f.payment.XenditInvoiceID)
	})
	return f
}

func (f *fixture) counts(t *testing.T) (events, ledger, deliveries int) {
	t.Helper()
	ctx := context.Background()
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_events WHERE xendit_id=$1`, f.payment.XenditInvoiceID).Scan(&events)
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE payment_id=$1`, f.payment.ID).Scan(&ledger)
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE payment_id=$1`, f.payment.ID).Scan(&deliveries)
	return
}

func (f *fixture) callback() service.XenditInvoiceCallback {
	return service.XenditInvoiceCallback{ID: f.payment.XenditInvoiceID, ExternalID: f.payment.ExternalID,
		Status: "PAID", PaidAmount: 5000, FeesPaid: 100}
}

// noCache never reports a key as seen, so only the DB guard dedups.
type noCache struct{}

func (noCache) Seen(context.Context, string) bool { return false }
func (noCache) Mark(context.Context, string)      {}

type stubEnqueuer struct {
	mu  sync.Mutex
	ids []int64
	err error
}

func (e *stubEnqueuer) EnqueueWebhookForward(_ context.Context, id int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.ids = append(e.ids, id)
	return nil
}

func TestIntegrationWithinTxRollsBackAllWrites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	boom := errors.New("boom")

	err := f.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := f.webhooks.CreateEventIfNew(ctx, f.payment.XenditInvoiceID, "PAID"); err != nil {
			return err
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
	if ev, led, _ := f.counts(t); ev != 0 || led != 0 {
		t.Fatalf("rollback left rows: events=%d ledger=%d", ev, led)
	}
	p, err := f.payments.GetByID(ctx, f.payment.AppID, f.payment.ID)
	if err != nil || p.Status != domain.PaymentStatusPending {
		t.Fatalf("status changed despite rollback: %+v %v", p, err)
	}
}

// A failed attempt followed by Xendit's retry ends with exactly one of each row.
func TestIntegrationProcessRetryAfterFailure(t *testing.T) {
	f := newFixture(t)
	enq := &stubEnqueuer{}
	svc := service.NewWebhookService(f.payments, f.ledger, &failingDelivery{WebhookRepo: f.webhooks}, f.tx, noCache{}, enq)

	if err := svc.Process(context.Background(), f.callback(), []byte(`{}`)); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	if ev, led, del := f.counts(t); ev+led+del != 0 {
		t.Fatalf("failed attempt left rows: events=%d ledger=%d deliveries=%d", ev, led, del)
	}

	svc = service.NewWebhookService(f.payments, f.ledger, f.webhooks, f.tx, noCache{}, enq)
	if err := svc.Process(context.Background(), f.callback(), []byte(`{}`)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ev, led, del := f.counts(t); ev != 1 || led != 1 || del != 1 || len(enq.ids) != 1 {
		t.Fatalf("retry: events=%d ledger=%d deliveries=%d enqueued=%d", ev, led, del, len(enq.ids))
	}
}

// failingDelivery fails CreateDelivery, the last write in the transaction.
type failingDelivery struct{ *postgres.WebhookRepo }

func (failingDelivery) CreateDelivery(context.Context, *domain.WebhookDelivery) error {
	return errors.New("insert delivery failed")
}

// Concurrent duplicates with no Redis: the unique event insert inside the
// transaction makes exactly one of them do the work.
func TestIntegrationProcessConcurrentDuplicates(t *testing.T) {
	f := newFixture(t)
	enq := &stubEnqueuer{}
	svc := service.NewWebhookService(f.payments, f.ledger, f.webhooks, f.tx, noCache{}, enq)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.Process(context.Background(), f.callback(), []byte(`{}`))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	if ev, led, del := f.counts(t); ev != 1 || led != 1 || del != 1 || len(enq.ids) != 1 {
		t.Fatalf("duplicates: events=%d ledger=%d deliveries=%d enqueued=%d", ev, led, del, len(enq.ids))
	}
}

// When enqueue fails after commit, the delivery is left for the sweeper.
func TestIntegrationEnqueueFailureLeavesUnsentDelivery(t *testing.T) {
	f := newFixture(t)
	enq := &stubEnqueuer{err: errors.New("redis down")}
	svc := service.NewWebhookService(f.payments, f.ledger, f.webhooks, f.tx, noCache{}, enq)

	if err := svc.Process(context.Background(), f.callback(), []byte(`{}`)); !errors.Is(err, service.ErrForwardNotQueued) {
		t.Fatalf("expected ErrForwardNotQueued, got %v", err)
	}
	unsent, err := f.webhooks.ListUnsentDeliveries(context.Background(), time.Now().Add(time.Minute), 1000)
	if err != nil {
		t.Fatalf("list unsent: %v", err)
	}
	for _, d := range unsent {
		if d.PaymentID == f.payment.ID {
			return
		}
	}
	t.Fatalf("delivery for payment %d not listed as unsent", f.payment.ID)
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
	svc := service.NewWebhookService(f.payments, f.ledger, f.webhooks, f.tx, noCache{}, &stubEnqueuer{})
	ctx := context.Background()

	paid := f.callback()
	settled := f.callback()
	settled.Status = "SETTLED"
	settled.SettlementDate = "2026-01-02T03:04:05Z"
	for _, cb := range []service.XenditInvoiceCallback{paid, settled} {
		if err := svc.Process(ctx, cb, []byte(`{}`)); err != nil {
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
// whichever transaction commits last.
func TestIntegrationConcurrentPaidAndSettledEndSettled(t *testing.T) {
	for i := 0; i < 20; i++ {
		f := newFixture(t)
		svc := service.NewWebhookService(f.payments, f.ledger, f.webhooks, f.tx, noCache{}, &stubEnqueuer{})
		paid := f.callback()
		settled := f.callback()
		settled.Status = "SETTLED"

		var wg sync.WaitGroup
		for _, cb := range []service.XenditInvoiceCallback{settled, paid} {
			wg.Add(1)
			go func(cb service.XenditInvoiceCallback) {
				defer wg.Done()
				if err := svc.Process(context.Background(), cb, []byte(`{}`)); err != nil {
					t.Errorf("process %s: %v", cb.Status, err)
				}
			}(cb)
		}
		wg.Wait()
		if got := f.status(t); got != domain.PaymentStatusSettled {
			t.Fatalf("iteration %d: final status %s, want SETTLED", i, got)
		}
	}
}
