package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

// dashboardNow is 17:00 WIB on 2026-10-29, so the 7-day window is
// [2026-10-22T17:00Z, 2026-10-29T17:00Z) and the previous one starts at
// 2026-10-15T17:00Z.
var dashboardNow = time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

// dashboardDB is a fresh database with one live and one test app, one
// connection each, and the real dashboard service on top.
type dashboardDB struct {
	pool               *pgxpool.Pool
	svc                *service.DashboardService
	live, test         *domain.App
	liveConn, testConn *domain.Connection
	from, to           time.Time
}

func newDashboardDB(t *testing.T) *dashboardDB {
	t.Helper()
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	d := &dashboardDB{pool: pool}
	d.svc = service.NewDashboardService(service.DashboardDeps{
		Dashboard: postgres.NewDashboardRepo(pool), Connections: postgres.NewConnectionRepo(pool),
		Payments: postgres.NewPaymentRepo(pool), Runs: postgres.NewReconciliationRepo(pool),
		Now: func() time.Time { return dashboardNow },
	})
	w := domain.DashboardRange7d.WindowAt(dashboardNow)
	d.from, d.to = w.From, w.To
	app := func(slug string, env domain.Environment) *domain.App {
		a := &domain.App{PublicID: id.New(id.App), Slug: slug, Name: slug, Environment: env, Status: domain.AppActive, WebhookSecretEnc: "x"}
		if err := postgres.NewAppRepo(pool).Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	d.live, d.test = app("acme", domain.EnvLive), app("initech", domain.EnvTest)
	m, err := postgres.NewMerchantRepo(pool).GetDefault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn := func(name string, env domain.Environment) *domain.Connection {
		c := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: m.ID, Provider: "xendit", Name: name, Environment: env,
			Status: domain.ConnectionActive, CredentialsEnc: "sealed", CredentialsDEKEnc: "wrapped", KeyVersion: 1,
			CredentialsLast4: map[string]*string{}, Settings: map[string]any{}}
		if err := postgres.NewConnectionRepo(pool).Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	d.liveConn, d.testConn = conn("alpha", domain.EnvLive), conn("gamma", domain.EnvTest)
	return d
}

// payment stores a payment of app created at createdAt.
func (d *dashboardDB) payment(t *testing.T, app *domain.App, currency string, amount int64, status domain.PaymentStatus, createdAt time.Time) *domain.Payment {
	t.Helper()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: id.New(id.Payment)[4:], Amount: amount,
		Currency: currency, Status: status}
	p.ExternalID = "kp_" + p.PublicID
	if err := postgres.NewPaymentRepo(d.pool).Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	exec(t, d.pool, `UPDATE payments SET created_at=$2 WHERE id=$1`, p.ID, createdAt)
	return p
}

// delivery stores a delivery of app created at createdAt.
func (d *dashboardDB) delivery(t *testing.T, app *domain.App, status domain.WebhookDeliveryStatus, createdAt time.Time) {
	t.Helper()
	dl := &domain.WebhookDelivery{PublicID: id.New(id.Delivery), AppID: app.ID, EventType: "payment.paid", Payload: []byte(`{}`),
		Status: status, MaxAttempts: 1}
	if err := postgres.NewDeliveryRepo(d.pool).Create(context.Background(), dl); err != nil {
		t.Fatal(err)
	}
	exec(t, d.pool, `UPDATE webhook_deliveries SET created_at=$2 WHERE id=$1`, dl.ID, createdAt)
}

// inbound stores a callback of c received at receivedAt.
func (d *dashboardDB) inbound(t *testing.T, c *domain.Connection, result domain.InboundResult, receivedAt time.Time) {
	t.Helper()
	w := &domain.InboundWebhook{PublicID: id.New(id.Inbound), ConnectionID: c.ID, Provider: c.Provider, Source: domain.InboundSourceWebhook,
		SignatureValid: result != domain.InboundInvalidSignature, Result: result, RawBody: []byte(`{}`)}
	if err := postgres.NewInboundWebhookRepo(d.pool).Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	exec(t, d.pool, `UPDATE inbound_webhooks SET received_at=$2 WHERE id=$1`, w.ID, receivedAt)
}

// run stores a finished run of c with unresolved unresolved items,
// created at createdAt.
func (d *dashboardDB) run(t *testing.T, c *domain.Connection, status domain.ReconciliationRunStatus, unresolved int, createdAt time.Time) *domain.ReconciliationRun {
	t.Helper()
	ctx := context.Background()
	repo := postgres.NewReconciliationRepo(d.pool)
	r := &domain.ReconciliationRun{PublicID: id.New(id.Reconciliation), ConnectionID: c.ID, From: d.from, To: d.from.Add(24 * time.Hour),
		Status: domain.RunQueued, RunCount: 1}
	if err := repo.CreateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Status, r.UnresolvedCount, r.FinishedAt = status, unresolved, &createdAt
	if status == domain.RunSucceeded {
		r.Summary = domain.NewReconciliationSummary(nil)
	}
	if err := repo.UpdateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	exec(t, d.pool, `UPDATE reconciliation_runs SET created_at=$2 WHERE id=$1`, r.ID, createdAt)
	return r
}

func (d *dashboardDB) get(t *testing.T, env *domain.Environment) *domain.Dashboard {
	t.Helper()
	out, err := d.svc.Get(context.Background(), domain.DashboardQuery{Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func envPtr(e domain.Environment) *domain.Environment { return &e }

// Payments are counted by creation time with the window's bounds inclusive
// at the start and exclusive at the end, per currency and per period; the
// series has one zero-filled point per WIB day.
func TestIntegrationDashboardPayments(t *testing.T) {
	d := newDashboardDB(t)
	day := 24 * time.Hour
	prevFrom := d.from.Add(-7 * day)
	// Previous period and outside it.
	d.payment(t, d.live, "IDR", 1000, domain.PaymentStatusPaid, d.from.Add(-time.Second))
	d.payment(t, d.live, "IDR", 9999, domain.PaymentStatusPaid, prevFrom.Add(-time.Second))
	d.payment(t, d.live, "IDR", 9999, domain.PaymentStatusPaid, d.to)
	// Current period, day 0.
	d.payment(t, d.live, "IDR", 2000, domain.PaymentStatusPaid, d.from)
	d.payment(t, d.live, "IDR", 500, domain.PaymentStatusExpired, d.from.Add(2*time.Hour))
	d.payment(t, d.live, "IDR", 500, domain.PaymentStatusFailed, d.from.Add(2*time.Hour))
	d.payment(t, d.live, "IDR", 500, domain.PaymentStatusPending, d.from.Add(2*time.Hour))
	// Day 1 in another currency, day 3 in the test environment, day 6.
	d.payment(t, d.live, "USD", 700, domain.PaymentStatusPaid, d.from.Add(day))
	d.payment(t, d.test, "IDR", 4000, domain.PaymentStatusPaid, d.from.Add(3*day))
	d.payment(t, d.live, "IDR", 3000, domain.PaymentStatusSettled, d.to.Add(-time.Second))

	all := d.get(t, nil)
	by := all.Payments.ByCurrency
	if len(by) != 2 || by[0].Currency != "IDR" || by[1].Currency != "USD" {
		t.Fatalf("by_currency = %+v", by)
	}
	idr := by[0]
	if idr.Current != (domain.DashboardPaymentTotals{Created: 6, Pending: 1, Paid: 2, Settled: 1, Expired: 1, Failed: 1, Gross: 9000}) ||
		idr.Previous != (domain.DashboardPaymentTotals{Created: 1, Paid: 1, Gross: 1000}) {
		t.Fatalf("IDR = %+v", idr)
	}
	if *idr.Current.SuccessRate() != 0.6 {
		t.Fatalf("IDR rate = %v", *idr.Current.SuccessRate())
	}
	usd := by[1]
	if usd.Current != (domain.DashboardPaymentTotals{Created: 1, Paid: 1, Gross: 700}) || usd.Previous != (domain.DashboardPaymentTotals{}) {
		t.Fatalf("USD = %+v", usd)
	}

	s := all.Payments.Series
	if len(s) != 7 {
		t.Fatalf("series has %d points", len(s))
	}
	wantCreated := []int64{4, 1, 0, 1, 0, 0, 1}
	wantSuccessful := []int64{1, 1, 0, 1, 0, 0, 1}
	wantIDR := []int64{2000, 0, 0, 4000, 0, 0, 3000}
	wantUSD := []int64{0, 700, 0, 0, 0, 0, 0}
	for i, p := range s {
		if !p.PeriodStart.Equal(d.from.Add(time.Duration(i)*day)) || !p.PeriodEnd.Equal(d.from.Add(time.Duration(i+1)*day)) {
			t.Fatalf("point %d period = %v..%v", i, p.PeriodStart, p.PeriodEnd)
		}
		if p.Created != wantCreated[i] || p.Successful != wantSuccessful[i] || len(p.Gross) != 2 ||
			p.Gross[0] != (domain.DashboardCurrencyAmount{Currency: "IDR", Amount: wantIDR[i]}) ||
			p.Gross[1] != (domain.DashboardCurrencyAmount{Currency: "USD", Amount: wantUSD[i]}) {
			t.Fatalf("point %d = %+v", i, p)
		}
	}
	if len(all.RecentPayments) != 8 || !all.RecentPayments[0].CreatedAt.Equal(d.to) {
		t.Fatalf("recent: %d payments, newest at %v", len(all.RecentPayments), all.RecentPayments[0].CreatedAt)
	}

	live := d.get(t, envPtr(domain.EnvLive))
	if live.Payments.ByCurrency[0].Current.Created != 5 || live.Payments.ByCurrency[0].Current.Gross != 5000 ||
		live.Payments.Series[3].Created != 0 || len(live.RecentPayments) != 8 {
		t.Fatalf("live = %+v", live.Payments)
	}
	test := d.get(t, envPtr(domain.EnvTest))
	if len(test.Payments.ByCurrency) != 1 || test.Payments.ByCurrency[0].Current.Gross != 4000 || len(test.RecentPayments) != 1 ||
		test.Payments.Series[3].Gross[0] != (domain.DashboardCurrencyAmount{Currency: "IDR", Amount: 4000}) {
		t.Fatalf("test = %+v", test.Payments)
	}
}

// Deliveries and callbacks follow the window and the environment of their
// app or connection.
func TestIntegrationDashboardWebhooks(t *testing.T) {
	d := newDashboardDB(t)
	d.delivery(t, d.live, domain.WebhookDeliveryDelivered, d.from)
	d.delivery(t, d.live, domain.WebhookDeliveryFailed, d.to.Add(-time.Second))
	d.delivery(t, d.live, domain.WebhookDeliveryPending, d.from.Add(time.Hour))
	d.delivery(t, d.live, domain.WebhookDeliveryDelivered, d.to)
	d.delivery(t, d.live, domain.WebhookDeliveryDelivered, d.from.Add(-time.Second))
	d.delivery(t, d.test, domain.WebhookDeliveryDelivered, d.from.Add(time.Hour))

	d.inbound(t, d.liveConn, domain.InboundApplied, d.from)
	d.inbound(t, d.liveConn, domain.InboundStale, d.from.Add(time.Hour))
	d.inbound(t, d.liveConn, domain.InboundMismatch, d.from.Add(time.Hour))
	d.inbound(t, d.liveConn, domain.InboundInvalidSignature, d.to.Add(-time.Second))
	d.inbound(t, d.liveConn, domain.InboundApplied, d.to)
	d.inbound(t, d.liveConn, domain.InboundApplied, d.from.Add(-time.Second))
	d.inbound(t, d.testConn, domain.InboundApplied, d.from.Add(time.Hour))

	all := d.get(t, nil)
	if all.Deliveries != (domain.DashboardDeliveryCounts{Total: 4, Pending: 1, Delivered: 2, Failed: 1}) || *all.Deliveries.SuccessRate() != 2.0/3 {
		t.Fatalf("deliveries = %+v", all.Deliveries)
	}
	if all.Inbound.Total != 5 || all.Inbound.Applied != 2 || all.Inbound.Problems != 2 || all.Inbound.ByResult[domain.InboundStale] != 1 ||
		all.Inbound.ByResult[domain.InboundDuplicate] != 0 {
		t.Fatalf("inbound = %+v", all.Inbound)
	}
	live := d.get(t, envPtr(domain.EnvLive))
	if live.Deliveries != (domain.DashboardDeliveryCounts{Total: 3, Pending: 1, Delivered: 1, Failed: 1}) {
		t.Fatalf("live deliveries = %+v", live.Deliveries)
	}
	if live.Inbound.Total != 4 || live.Inbound.Applied != 1 || live.Inbound.Problems != 2 {
		t.Fatalf("live inbound = %+v", live.Inbound)
	}
	test := d.get(t, envPtr(domain.EnvTest))
	if test.Deliveries != (domain.DashboardDeliveryCounts{Total: 1, Delivered: 1}) || test.Inbound.Total != 1 || test.Inbound.Applied != 1 {
		t.Fatalf("test = %+v / %+v", test.Deliveries, test.Inbound)
	}
}

// Unresolved items come from succeeded runs only, per environment; the
// latest run is the newest whatever its status; connections list active
// ones first.
func TestIntegrationDashboardReconciliationAndConnections(t *testing.T) {
	d := newDashboardDB(t)
	ctx := context.Background()
	d.run(t, d.liveConn, domain.RunSucceeded, 2, d.from.Add(-time.Hour))
	d.run(t, d.liveConn, domain.RunSucceeded, 1, d.from.Add(time.Hour))
	failed := d.run(t, d.liveConn, domain.RunFailed, 5, d.from.Add(2*time.Hour))
	testRun := d.run(t, d.testConn, domain.RunSucceeded, 1, d.from.Add(3*time.Hour))

	all := d.get(t, nil)
	if all.Reconciliation.UnresolvedItems != 4 || all.Reconciliation.LatestRun == nil || all.Reconciliation.LatestRun.PublicID != testRun.PublicID {
		t.Fatalf("all = %+v", all.Reconciliation)
	}
	live := d.get(t, envPtr(domain.EnvLive))
	if live.Reconciliation.UnresolvedItems != 3 || live.Reconciliation.LatestRun.PublicID != failed.PublicID ||
		live.Reconciliation.LatestRun.Connection.PublicID != d.liveConn.PublicID {
		t.Fatalf("live = %+v", live.Reconciliation)
	}
	test := d.get(t, envPtr(domain.EnvTest))
	if test.Reconciliation.UnresolvedItems != 1 || test.Reconciliation.LatestRun.PublicID != testRun.PublicID {
		t.Fatalf("test = %+v", test.Reconciliation)
	}

	d.liveConn.Status = domain.ConnectionDisabled
	if err := postgres.NewConnectionRepo(d.pool).Update(ctx, d.liveConn); err != nil {
		t.Fatal(err)
	}
	beta := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: d.liveConn.MerchantID, Provider: "xendit", Name: "Beta",
		Environment: domain.EnvLive, Status: domain.ConnectionActive, CredentialsEnc: "sealed", CredentialsDEKEnc: "wrapped", KeyVersion: 1,
		CredentialsLast4: map[string]*string{}, Settings: map[string]any{}}
	if err := postgres.NewConnectionRepo(d.pool).Create(ctx, beta); err != nil {
		t.Fatal(err)
	}
	all = d.get(t, nil)
	names := []string{}
	for _, c := range all.Connections {
		names = append(names, c.Name)
	}
	if len(names) != 3 || names[0] != "Beta" || names[1] != "gamma" || names[2] != "alpha" {
		t.Fatalf("connections = %v", names)
	}
	if live := d.get(t, envPtr(domain.EnvLive)); len(live.Connections) != 2 || live.Connections[0].PublicID != beta.PublicID {
		t.Fatalf("live connections = %+v", live.Connections)
	}
}
