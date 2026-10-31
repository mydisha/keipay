package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

// dashboardNow is the fixed clock of the dashboard tests: 17:00 WIB, so the
// window ends at 2026-10-29T17:00Z.
var dashboardNow = time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

type dashboardFixture struct {
	svc      *DashboardService
	dash     *memstore.Dashboard
	apps     *memstore.Apps
	conns    *memstore.Connections
	payments *memstore.Payments
	runs     *memstore.Reconciliations
	clock    *memstore.Clock
}

func newDashboardFixture(t *testing.T) *dashboardFixture {
	t.Helper()
	f := &dashboardFixture{dash: &memstore.Dashboard{}, clock: memstore.NewClock(dashboardNow)}
	f.apps = memstore.NewApps(f.clock.Now)
	f.conns = memstore.NewConnections(f.apps, f.clock.Now)
	f.payments = memstore.NewPayments(f.apps, f.clock.Now)
	f.runs = memstore.NewReconciliations(f.conns, f.payments, memstore.NewReports(), nil, f.clock.Now)
	f.svc = NewDashboardService(DashboardDeps{Dashboard: f.dash, Connections: f.conns, Payments: f.payments, Runs: f.runs, Now: f.clock.Now})
	return f
}

func (f *dashboardFixture) app(t *testing.T, env domain.Environment) *domain.App {
	t.Helper()
	a := &domain.App{PublicID: id.New(id.App), Slug: "app-" + strings.ToLower(id.New(id.App)[20:]), Name: "App",
		Environment: env, Status: domain.AppActive}
	if err := f.apps.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *dashboardFixture) connection(t *testing.T, name string, env domain.Environment, status domain.ConnectionStatus) *domain.Connection {
	t.Helper()
	c := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: memstore.DefaultMerchantID, Provider: "fake", Name: name,
		Environment: env, Status: status}
	if err := f.conns.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

// payment stores a payment of app created at the clock, then moves the
// clock forward a minute so the next one is newer.
func (f *dashboardFixture) payment(t *testing.T, app *domain.App) *domain.Payment {
	t.Helper()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: id.New(id.Payment), Amount: 1000, Currency: "IDR",
		Status: domain.PaymentStatusPending}
	p.ExternalID = "kp_" + p.PublicID
	if err := f.payments.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Minute)
	return p
}

func (f *dashboardFixture) run(t *testing.T, c *domain.Connection) *domain.ReconciliationRun {
	t.Helper()
	r := &domain.ReconciliationRun{PublicID: id.New(id.Reconciliation), ConnectionID: c.ID, From: dashboardNow.Add(-48 * time.Hour),
		To: dashboardNow.Add(-24 * time.Hour), Status: domain.RunQueued, RunCount: 1}
	if err := f.runs.CreateRun(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Minute)
	return r
}

func envOf(e domain.Environment) *domain.Environment { return &e }

// The default range is 7 days; the window is cut at WIB midnight and
// handed to every count with the environment.
func TestDashboardWindowAndRange(t *testing.T) {
	f := newDashboardFixture(t)
	ctx := context.Background()
	d, err := f.svc.Get(ctx, domain.DashboardQuery{Environment: envOf(domain.EnvLive)})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.DashboardWindow{Environment: envOf(domain.EnvLive),
		PreviousFrom: time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC), From: time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC),
		To: time.Date(2026, 10, 29, 17, 0, 0, 0, time.UTC)}
	if d.Range != domain.DashboardRange7d || len(d.Payments.Series) != 7 || !sameWindow(d.Window, want) {
		t.Fatalf("range %s series %d window %+v", d.Range, len(d.Payments.Series), d.Window)
	}
	if len(f.dash.Windows) == 0 || len(f.dash.Environments) != 1 || *f.dash.Environments[0] != domain.EnvLive {
		t.Fatalf("repo calls: windows %d environments %v", len(f.dash.Windows), f.dash.Environments)
	}
	for i, w := range f.dash.Windows {
		if !sameWindow(w, want) {
			t.Fatalf("window %d = %+v, want %+v", i, w, want)
		}
	}
	for i, p := range d.Payments.Series {
		if !p.PeriodStart.Equal(want.From.AddDate(0, 0, i)) || !p.PeriodEnd.Equal(want.From.AddDate(0, 0, i+1)) {
			t.Fatalf("point %d = %v..%v", i, p.PeriodStart, p.PeriodEnd)
		}
	}

	d, err = f.svc.Get(ctx, domain.DashboardQuery{Range: domain.DashboardRange30d})
	if err != nil {
		t.Fatal(err)
	}
	if d.Range != domain.DashboardRange30d || len(d.Payments.Series) != 30 || !d.Window.From.Equal(time.Date(2026, 9, 29, 17, 0, 0, 0, time.UTC)) ||
		d.Window.Environment != nil {
		t.Fatalf("30d: range %s series %d window %+v", d.Range, len(d.Payments.Series), d.Window)
	}

	for _, r := range []domain.DashboardRange{"3d", "7D", "7 d"} {
		f.dash.Windows = nil
		_, err := f.svc.Get(ctx, domain.DashboardQuery{Range: r})
		if got := fieldsOf(t, err); len(got) != 1 || got[0] != (domain.FieldError{Field: "range", Rule: RuleOneOf}) {
			t.Fatalf("%q: fields = %v", r, got)
		}
		if len(f.dash.Windows) != 0 {
			t.Fatalf("%q: repository was called", r)
		}
	}
}

func sameWindow(a, b domain.DashboardWindow) bool {
	sameEnv := (a.Environment == nil) == (b.Environment == nil) && (a.Environment == nil || *a.Environment == *b.Environment)
	return sameEnv && a.PreviousFrom.Equal(b.PreviousFrom) && a.From.Equal(b.From) && a.To.Equal(b.To)
}

// Nothing stored gives empty lists, zero-filled days, null rates, every
// inbound result at 0 and no latest run.
func TestDashboardEmpty(t *testing.T) {
	f := newDashboardFixture(t)
	d, err := f.svc.Get(context.Background(), domain.DashboardQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Payments.ByCurrency) != 0 || len(d.Connections) != 0 || len(d.RecentPayments) != 0 || d.Reconciliation.LatestRun != nil ||
		d.Reconciliation.UnresolvedItems != 0 || d.Payments.Totals != (domain.DashboardPeriodTotals{}) ||
		d.Payments.Totals.Current.SuccessRate() != nil {
		t.Fatalf("dashboard = %+v", d)
	}
	if d.Deliveries.SuccessRate() != nil || d.Inbound.Total != 0 || len(d.Inbound.ByResult) != len(domain.InboundResults()) {
		t.Fatalf("deliveries %+v inbound %+v", d.Deliveries, d.Inbound)
	}
	for i, p := range d.Payments.Series {
		if p.Created != 0 || p.Successful != 0 || len(p.Gross) != 0 {
			t.Fatalf("point %d = %+v", i, p)
		}
	}
}

// Sparse rows fold into zero-filled days, currencies stay apart, and a
// currency seen only in the previous period has zero current totals.
func TestDashboardFoldsRows(t *testing.T) {
	f := newDashboardFixture(t)
	from := time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)
	f.dash.Totals = []domain.DashboardPaymentTotalsRow{
		{Current: false, Currency: "IDR", Totals: domain.DashboardPaymentTotals{Created: 2, Paid: 1, Expired: 1, Gross: 500}},
		{Current: false, Currency: "USD", Totals: domain.DashboardPaymentTotals{Created: 1, Paid: 1, Gross: 7}},
		{Current: true, Currency: "IDR", Totals: domain.DashboardPaymentTotals{Created: 4, Pending: 1, Paid: 1, Settled: 1, Failed: 1, Gross: 3000}},
		{Current: true, Currency: "SGD", Totals: domain.DashboardPaymentTotals{Created: 1, Pending: 1}},
	}
	f.dash.Series = []domain.DashboardSeriesRow{
		{DayStart: from, Currency: "IDR", Created: 2, Successful: 1, Gross: 1000},
		{DayStart: from.AddDate(0, 0, 2), Currency: "IDR", Created: 1, Successful: 1, Gross: 2000},
		{DayStart: from.AddDate(0, 0, 2), Currency: "SGD", Created: 1},
		{DayStart: from.AddDate(0, 0, 6), Currency: "IDR", Created: 1},
		// Outside the window: ignored rather than crashing.
		{DayStart: from.AddDate(0, 0, 7), Currency: "IDR", Created: 9},
		{DayStart: from.AddDate(0, 0, -1), Currency: "IDR", Created: 9},
	}
	f.dash.Deliveries = domain.DashboardDeliveryCounts{Total: 4, Pending: 1, Delivered: 2, Failed: 1}
	f.dash.Inbound = map[domain.InboundResult]int64{domain.InboundApplied: 3, domain.InboundMismatch: 1}
	f.dash.Unresolved = 4

	d, err := f.svc.Get(context.Background(), domain.DashboardQuery{})
	if err != nil {
		t.Fatal(err)
	}
	by := d.Payments.ByCurrency
	if len(by) != 3 || by[0].Currency != "IDR" || by[1].Currency != "SGD" || by[2].Currency != "USD" {
		t.Fatalf("by_currency = %+v", by)
	}
	if by[0].Current.Gross != 3000 || by[0].Current.Successful() != 2 || *by[0].Current.SuccessRate() != 2.0/3 ||
		by[0].Previous.Gross != 500 || *by[0].Previous.SuccessRate() != 0.5 {
		t.Fatalf("IDR = %+v", by[0])
	}
	if by[1].Current.Created != 1 || by[1].Current.SuccessRate() != nil || by[1].Previous != (domain.DashboardPaymentTotals{}) {
		t.Fatalf("SGD = %+v", by[1])
	}
	if by[2].Current != (domain.DashboardPaymentTotals{}) || by[2].Previous.Gross != 7 {
		t.Fatalf("USD = %+v", by[2])
	}

	// Totals add the counts of every currency and never an amount.
	wantCurrent := domain.DashboardPaymentTotals{Created: 5, Pending: 2, Paid: 1, Settled: 1, Failed: 1}
	wantPrevious := domain.DashboardPaymentTotals{Created: 3, Paid: 2, Expired: 1}
	tot := d.Payments.Totals
	if tot.Current != wantCurrent || tot.Previous != wantPrevious || tot.Current.Finished() != 3 ||
		*tot.Current.SuccessRate() != 2.0/3 || *tot.Previous.SuccessRate() != 2.0/3 {
		t.Fatalf("totals = %+v", tot)
	}

	s := d.Payments.Series
	if len(s) != 7 {
		t.Fatalf("series has %d points", len(s))
	}
	wantCreated := []int64{2, 0, 2, 0, 0, 0, 1}
	wantSuccessful := []int64{1, 0, 1, 0, 0, 0, 0}
	wantIDR := []int64{1000, 0, 2000, 0, 0, 0, 0}
	for i, p := range s {
		if p.Created != wantCreated[i] || p.Successful != wantSuccessful[i] || len(p.Gross) != 2 ||
			p.Gross[0] != (domain.DashboardCurrencyAmount{Currency: "IDR", Amount: wantIDR[i]}) ||
			p.Gross[1] != (domain.DashboardCurrencyAmount{Currency: "SGD", Amount: 0}) {
			t.Fatalf("point %d = %+v", i, p)
		}
	}
	if *d.Deliveries.SuccessRate() != 2.0/3 || d.Deliveries.Finished() != 3 || d.Inbound.Total != 4 || d.Inbound.Applied != 3 || d.Inbound.Problems != 1 ||
		d.Reconciliation.UnresolvedItems != 4 {
		t.Fatalf("deliveries %+v inbound %+v reconciliation %+v", d.Deliveries, d.Inbound, d.Reconciliation)
	}
}

// Connections, recent payments and the latest run follow the environment;
// connections list active ones first, payments the 8 newest.
func TestDashboardLists(t *testing.T) {
	f := newDashboardFixture(t)
	ctx := context.Background()
	live, test := f.app(t, domain.EnvLive), f.app(t, domain.EnvTest)
	zulu := f.connection(t, "zulu", domain.EnvLive, domain.ConnectionActive)
	f.connection(t, "Alpha", domain.EnvLive, domain.ConnectionDisabled)
	bravo := f.connection(t, "bravo", domain.EnvLive, domain.ConnectionActive)
	gamma := f.connection(t, "gamma", domain.EnvTest, domain.ConnectionActive)
	var livePayments []*domain.Payment
	for range 9 {
		livePayments = append(livePayments, f.payment(t, live))
	}
	testPayment := f.payment(t, test)
	f.run(t, zulu)
	newer := f.run(t, bravo)
	testRun := f.run(t, gamma)

	d, err := f.svc.Get(ctx, domain.DashboardQuery{Environment: envOf(domain.EnvLive)})
	if err != nil {
		t.Fatal(err)
	}
	if names := connectionNames(d.Connections); strings.Join(names, ",") != "bravo,zulu,Alpha" {
		t.Fatalf("connections = %v", names)
	}
	if len(d.RecentPayments) != 8 || d.RecentPayments[0].PublicID != livePayments[8].PublicID ||
		d.RecentPayments[7].PublicID != livePayments[1].PublicID {
		t.Fatalf("recent = %v", paymentIDs(d.RecentPayments))
	}
	if d.Reconciliation.LatestRun == nil || d.Reconciliation.LatestRun.PublicID != newer.PublicID ||
		d.Reconciliation.LatestRun.Connection.PublicID != bravo.PublicID {
		t.Fatalf("latest run = %+v, want %s", d.Reconciliation.LatestRun, newer.PublicID)
	}

	d, err = f.svc.Get(ctx, domain.DashboardQuery{Environment: envOf(domain.EnvTest)})
	if err != nil {
		t.Fatal(err)
	}
	if names := connectionNames(d.Connections); strings.Join(names, ",") != "gamma" {
		t.Fatalf("test connections = %v", names)
	}
	if len(d.RecentPayments) != 1 || d.RecentPayments[0].PublicID != testPayment.PublicID || d.RecentPayments[0].App.PublicID != test.PublicID {
		t.Fatalf("test recent = %v", paymentIDs(d.RecentPayments))
	}
	if d.Reconciliation.LatestRun == nil || d.Reconciliation.LatestRun.PublicID != testRun.PublicID {
		t.Fatalf("test latest run = %+v", d.Reconciliation.LatestRun)
	}

	d, err = f.svc.Get(ctx, domain.DashboardQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Connections) != 4 || len(d.RecentPayments) != 8 || d.RecentPayments[0].PublicID != testPayment.PublicID ||
		d.Reconciliation.LatestRun.PublicID != testRun.PublicID {
		t.Fatalf("all: connections %d recent %v latest %s", len(d.Connections), paymentIDs(d.RecentPayments), d.Reconciliation.LatestRun.PublicID)
	}
}

func connectionNames(conns []domain.Connection) []string {
	out := []string{}
	for _, c := range conns {
		out = append(out, c.Name)
	}
	return out
}

func paymentIDs(ps []domain.Payment) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.PublicID)
	}
	return out
}

// A failing port fails the whole dashboard, wrapped, with no partial result.
func TestDashboardRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	f := newDashboardFixture(t)
	f.dash.Err = boom
	d, err := f.svc.Get(ctx, domain.DashboardQuery{})
	if d != nil || !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "dashboard payment totals: ") {
		t.Fatalf("counts: %v / %v", d, err)
	}

	f = newDashboardFixture(t)
	f.conns.Err = boom
	d, err = f.svc.Get(ctx, domain.DashboardQuery{})
	if d != nil || !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "dashboard connections: ") {
		t.Fatalf("connections: %v / %v", d, err)
	}

	f = newDashboardFixture(t)
	f.runs.Err = boom
	d, err = f.svc.Get(ctx, domain.DashboardQuery{})
	if d != nil || !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "dashboard latest run: ") {
		t.Fatalf("runs: %v / %v", d, err)
	}
}
