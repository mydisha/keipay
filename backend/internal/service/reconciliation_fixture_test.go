package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

// runQueue records the reconciliation runs queued; set err to make the
// queue unavailable.
type runQueue struct {
	mu    sync.Mutex
	calls [][2]int64
	err   error
}

func (q *runQueue) EnqueueReconciliation(_ context.Context, runID int64, runCount int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.calls = append(q.calls, [2]int64{runID, int64(runCount)})
	return nil
}

// reconFixture wires the reconciliation service to in-memory stores and the
// fake provider, which lists the transactions a test sets. conn is a fake
// test connection; app pays through it.
type reconFixture struct {
	*connFixture
	users   *memstore.Users
	reports *memstore.Reports
	runs    *memstore.Reconciliations
	queue   *runQueue
	source  *ProviderSource
	sleeps  []time.Duration
	svc     *ReconciliationService
	conn    *domain.Connection
	app     *domain.App
	admin   *domain.User
}

func newReconFixture(t *testing.T) *reconFixture {
	t.Helper()
	f := &reconFixture{connFixture: newConnFixture(t), reports: memstore.NewReports(), queue: &runQueue{}}
	f.users = memstore.NewUsers(f.clock.Now)
	f.runs = memstore.NewReconciliations(f.conns, f.payments, f.reports, f.users, f.clock.Now)
	tx := memstore.NewTx(f.apps, f.conns, f.payments, f.audit, f.reports, f.runs)
	auditSvc := NewAuditService(f.audit)
	auditSvc.now = f.clock.Now
	f.source = NewProviderSource(f.registry, f.cipher)
	f.source.sleep = func(_ context.Context, d time.Duration) error {
		f.sleeps = append(f.sleeps, d)
		return nil
	}
	f.svc = NewReconciliationService(ReconciliationDeps{
		Runs: f.runs, Connections: f.conns, Registry: f.registry, Source: f.source, Tx: tx, Enqueuer: f.queue, Audit: auditSvc,
	})
	f.svc.now = f.clock.Now
	f.conn = f.addConnection(t, "fake-test", domain.EnvTest)
	f.app = &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "Acme", Environment: domain.EnvTest, Status: domain.AppActive}
	if err := f.apps.Create(context.Background(), f.app); err != nil {
		t.Fatal(err)
	}
	f.admin = &domain.User{PublicID: id.New(id.User), Email: "ops@example.com", Name: "Ops", Role: domain.RoleAdmin}
	f.users.Add(f.admin)
	f.audit.Reset()
	return f
}

// window is a range that ended an hour before the fixture's clock.
func (f *reconFixture) window() (from, to time.Time) {
	to = f.clock.Now().Add(-time.Hour).Truncate(time.Hour)
	return to.Add(-24 * time.Hour), to
}

func (f *reconFixture) createRun(t *testing.T) *domain.ReconciliationRun {
	t.Helper()
	from, to := f.window()
	run, err := f.svc.CreateRun(adminCtx(), CreateRunInput{ConnectionID: f.conn.PublicID,
		From: from.Format(time.RFC3339), To: to.Format(time.RFC3339), CreatedByID: &f.admin.ID})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run
}

// paidPayment stores a payment of the fixture's app through conn, paid at
// paidAt with a report, and returns it.
func (f *reconFixture) paidPayment(t *testing.T, ref string, amount int64, fee *int64, paidAt time.Time) *domain.Payment {
	t.Helper()
	p := f.payment(t, ref, amount)
	if err := f.payments.UpdateStatus(context.Background(), p.ID, domain.PaymentStatusPaid, &paidAt, nil, nil); err != nil {
		t.Fatal(err)
	}
	r := &domain.PaymentReport{PaymentID: p.ID, AppID: p.AppID, ConnectionID: p.ConnectionID, Currency: "IDR", Gross: amount,
		Fee: fee, Status: domain.PaymentStatusPaid, PaidAt: paidAt}
	if fee != nil {
		net := amount - *fee
		r.Net = &net
	}
	if err := f.reports.Upsert(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *reconFixture) payment(t *testing.T, ref string, amount int64) *domain.Payment {
	t.Helper()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: f.app.ID, OrderID: "order-" + ref, ExternalID: ref,
		ConnectionID: &f.conn.ID, Provider: f.conn.Provider, Amount: amount, Currency: "IDR", Status: domain.PaymentStatusPending}
	if err := f.payments.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

// gateway makes the fake provider list txs as one page.
func (f *reconFixture) gateway(txs ...domain.GatewayTransaction) {
	f.fake.Pages = []provider.TransactionPage{{Transactions: txs}}
}

func (f *reconFixture) run(t *testing.T, publicID string) *domain.ReconciliationRun {
	t.Helper()
	r, err := f.runs.GetRun(context.Background(), publicID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// execute performs the run's current run count as the worker would.
func (f *reconFixture) execute(t *testing.T, run *domain.ReconciliationRun) error {
	t.Helper()
	cur := f.run(t, run.PublicID)
	return f.svc.Execute(context.Background(), cur.ID, cur.RunCount)
}
