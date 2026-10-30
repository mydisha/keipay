package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

// report returns how many reports the payment has, the report with its
// updated_at, and how many ledger rows were written for the payment.
func (f *fixture) report(t *testing.T, p *domain.Payment) (rows int, r reportRow, updated time.Time, ledger int) {
	t.Helper()
	ctx := context.Background()
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM payment_reports WHERE payment_id=$1`, p.ID).Scan(&rows)
	_ = f.pool.QueryRow(ctx, `SELECT connection_id, currency, status, gross, fee, net, paid_at, settled_at, updated_at
		FROM payment_reports WHERE payment_id=$1`, p.ID).
		Scan(&r.connectionID, &r.currency, &r.status, &r.gross, &r.fee, &r.net, &r.paidAt, &r.settledAt, &updated)
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE payment_id=$1`, p.ID).Scan(&ledger)
	return rows, r, updated, ledger
}

// PAID writes one report in the callback's transaction, SETTLED updates it in
// place, a repeated callback changes nothing, and the ledger is never
// written.
func TestIntegrationCallbacksWritePaymentReport(t *testing.T) {
	f := newFixture(t)
	svc := f.svc(&stubEnqueuer{})
	paid := callback(f.payment, "PAID")
	paid.PaidAt = "2026-10-01T03:00:00Z"
	if err := f.process(svc, f.conn, paid); err != nil {
		t.Fatal(err)
	}
	n, r, updated, ledger := f.report(t, f.payment)
	paidAt := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	if n != 1 || ledger != 0 || r.status != "PAID" || r.gross != 5000 || *r.fee != 100 || *r.net != 4900 ||
		*r.connectionID != f.conn.ID || !r.paidAt.Equal(paidAt) || r.settledAt != nil {
		t.Fatalf("after PAID: rows=%d ledger=%d report=%+v", n, ledger, r)
	}

	if err := f.process(svc, f.conn, paid); err != nil {
		t.Fatal(err)
	}
	if n2, r2, updated2, _ := f.report(t, f.payment); n2 != 1 || !updated2.Equal(updated) || r2.status != "PAID" {
		t.Fatalf("repeated PAID changed the report: rows=%d %+v", n2, r2)
	}

	settled := callback(f.payment, "SETTLED")
	settled.SettlementDate = "2026-10-02T00:00:00Z"
	if err := f.process(svc, f.conn, settled); err != nil {
		t.Fatal(err)
	}
	n, r, _, ledger = f.report(t, f.payment)
	if n != 1 || ledger != 0 || r.status != "SETTLED" || r.settledAt == nil ||
		!r.settledAt.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)) || !r.paidAt.Equal(paidAt) {
		t.Fatalf("after SETTLED: rows=%d ledger=%d report=%+v", n, ledger, r)
	}
}

// No fee is NULL until a callback brings one; 0 is a known fee.
func TestIntegrationReportFee(t *testing.T) {
	f := newFixture(t)
	svc := f.svc(&stubEnqueuer{})
	noFee := callback(f.payment, "PAID")
	noFee.Fee = nil
	if err := f.process(svc, f.conn, noFee); err != nil {
		t.Fatal(err)
	}
	if _, r, _, _ := f.report(t, f.payment); r.fee != nil || r.net != nil {
		t.Fatalf("fee = %v net = %v, want NULL", r.fee, r.net)
	}
	if err := f.process(svc, f.conn, callback(f.payment, "SETTLED")); err != nil {
		t.Fatal(err)
	}
	if _, r, _, _ := f.report(t, f.payment); r.fee == nil || *r.fee != 100 || *r.net != 4900 {
		t.Fatalf("fee = %v net = %v, want 100 / 4900", r.fee, r.net)
	}

	zero := f.addPayment(t, f.conn, 8000)
	cb := callback(zero, "PAID")
	cb.Fee = i64(0)
	if err := f.process(svc, f.conn, cb); err != nil {
		t.Fatal(err)
	}
	if _, r, _, _ := f.report(t, zero); r.fee == nil || *r.fee != 0 || *r.net != 8000 {
		t.Fatalf("zero fee = %v net = %v", r.fee, r.net)
	}

	// A negative fee is stored as unknown rather than failing the CHECKs
	// and with them the whole callback.
	negative := f.addPayment(t, f.conn, 9000)
	cb = callback(negative, "PAID")
	cb.Fee = i64(-5)
	if err := f.process(svc, f.conn, cb); err != nil {
		t.Fatalf("negative fee callback refused: %v", err)
	}
	if n, r, _, _ := f.report(t, negative); n != 1 || r.fee != nil || r.net != nil || r.status != "PAID" {
		t.Fatalf("negative fee: rows %d fee = %v net = %v status %s", n, r.fee, r.net, r.status)
	}
}

// A success after EXPIRED is captured money and gets a report.
func TestIntegrationLateSuccessWritesReport(t *testing.T) {
	f := newFixture(t)
	svc := f.svc(&stubEnqueuer{})
	if err := f.process(svc, f.conn, callback(f.payment, "EXPIRED")); err != nil {
		t.Fatal(err)
	}
	if n, _, _, _ := f.report(t, f.payment); n != 0 {
		t.Fatal("expired payment has a report")
	}
	if err := f.process(svc, f.conn, callback(f.payment, "PAID")); err != nil {
		t.Fatal(err)
	}
	if n, r, _, ledger := f.report(t, f.payment); n != 1 || r.status != "PAID" || ledger != 0 {
		t.Fatalf("late success: rows=%d ledger=%d %+v", n, ledger, r)
	}
}

// reportDB is a fresh database with two live apps (acme, globex), one test
// app (initech), and two live connections (alpha, beta).
type reportDB struct {
	pool                  *pgxpool.Pool
	repo                  *postgres.PaymentReportRepo
	acme, globex, initech *domain.App
	alpha, beta, testConn *domain.Connection
	payments              *postgres.PaymentRepo
}

func newReportDB(t *testing.T) *reportDB {
	t.Helper()
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	d := &reportDB{pool: pool, repo: postgres.NewPaymentReportRepo(pool), payments: postgres.NewPaymentRepo(pool)}
	app := func(slug string, env domain.Environment) *domain.App {
		a := &domain.App{PublicID: id.New(id.App), Slug: slug, Name: slug, Environment: env, Status: domain.AppActive, WebhookSecretEnc: "x"}
		if err := postgres.NewAppRepo(pool).Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	d.acme, d.globex, d.initech = app("acme", domain.EnvLive), app("globex", domain.EnvLive), app("initech", domain.EnvTest)
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
	d.alpha, d.beta, d.testConn = conn("alpha", domain.EnvLive), conn("beta", domain.EnvLive), conn("gamma", domain.EnvTest)
	return d
}

// add stores a paid payment of app through c (nil: none) and its report.
func (d *reportDB) add(t *testing.T, app *domain.App, c *domain.Connection, currency string, gross int64, fee *int64, paidAt time.Time) *domain.Payment {
	t.Helper()
	ctx := context.Background()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: id.New(id.Payment)[4:], Amount: gross,
		Currency: currency, Status: domain.PaymentStatusPaid}
	p.ExternalID = "kp_" + p.PublicID
	r := &domain.PaymentReport{AppID: app.ID, Currency: currency, Gross: gross, Fee: fee, Status: domain.PaymentStatusPaid, PaidAt: paidAt}
	if c != nil {
		p.ConnectionID, p.Provider, r.ConnectionID = &c.ID, c.Provider, &c.ID
	}
	if err := d.payments.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	r.PaymentID = p.ID
	if fee != nil {
		r.Net = i64(gross - *fee)
	}
	if err := d.repo.Upsert(ctx, r); err != nil {
		t.Fatal(err)
	}
	return p
}

func (d *reportDB) list(t *testing.T, f domain.PaymentReportFilter) []string {
	t.Helper()
	var out []string
	page := domain.Page{Limit: 2}
	for range 20 {
		res, err := d.repo.List(context.Background(), f, page)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res.Items {
			out = append(out, r.PaymentPublicID)
		}
		if res.NextCursor == "" {
			return out
		}
		c, err := domain.DecodeCursor(res.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		page.After = &c
	}
	t.Fatal("too many pages")
	return nil
}

// Every filter, the environment join, and a keyset that pages through equal
// paid_at values without skipping or repeating.
func TestIntegrationPaymentReportList(t *testing.T) {
	d := newReportDB(t)
	same := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	for range 5 {
		d.add(t, d.acme, d.alpha, "IDR", 1000, nil, same)
	}
	older := d.add(t, d.acme, d.beta, "USD", 1000, i64(10), same.Add(-time.Hour))
	globex := d.add(t, d.globex, nil, "IDR", 1000, nil, same.Add(time.Hour))
	initech := d.add(t, d.initech, d.testConn, "IDR", 1000, nil, same)

	all := d.list(t, domain.PaymentReportFilter{})
	if len(all) != 8 || all[0] != globex.PublicID || all[len(all)-1] != older.PublicID {
		t.Fatalf("all = %v", all)
	}
	seen := map[string]bool{}
	for _, p := range all {
		if seen[p] {
			t.Fatalf("%s listed twice", p)
		}
		seen[p] = true
	}
	// Within one paid_at, payment ids descend.
	if !sortedDesc(all[1:7]) {
		t.Fatalf("ties not ordered by payment id: %v", all[1:7])
	}

	str := func(s string) *string { return &s }
	env := func(e domain.Environment) *domain.Environment { return &e }
	at := func(t time.Time) *time.Time { return &t }
	cases := []struct {
		name string
		f    domain.PaymentReportFilter
		want int
	}{
		{"app", domain.PaymentReportFilter{AppPublicID: &d.acme.PublicID}, 6},
		{"connection", domain.PaymentReportFilter{ConnectionPublicID: &d.beta.PublicID}, 1},
		{"environment live", domain.PaymentReportFilter{Environment: env(domain.EnvLive)}, 7},
		{"environment test", domain.PaymentReportFilter{Environment: env(domain.EnvTest)}, 1},
		{"currency", domain.PaymentReportFilter{Currency: str("USD")}, 1},
		{"from is inclusive", domain.PaymentReportFilter{From: at(same)}, 7},
		{"to is exclusive", domain.PaymentReportFilter{To: at(same)}, 1},
		{"range", domain.PaymentReportFilter{From: at(same), To: at(same.Add(time.Hour))}, 6},
		{"unknown app", domain.PaymentReportFilter{AppPublicID: str(id.New(id.App))}, 0},
		{"combined", domain.PaymentReportFilter{AppPublicID: &d.acme.PublicID, Currency: str("IDR"), Environment: env(domain.EnvLive)}, 5},
	}
	for _, tc := range cases {
		if got := d.list(t, tc.f); len(got) != tc.want {
			t.Errorf("%s: %d rows %v, want %d", tc.name, len(got), got, tc.want)
		}
	}

	res, err := d.repo.List(context.Background(), domain.PaymentReportFilter{AppPublicID: &d.initech.PublicID}, domain.Page{Limit: 10})
	if err != nil || len(res.Items) != 1 {
		t.Fatalf("initech = %+v %v", res, err)
	}
	r := res.Items[0]
	if r.PaymentPublicID != initech.PublicID || r.Reference != initech.ExternalID || r.OrderID != initech.OrderID ||
		r.App != d.initech.Summary() || r.Connection == nil || *r.Connection != d.testConn.Summary() || r.Provider() != "xendit" {
		t.Fatalf("row = %+v", r)
	}
	res, _ = d.repo.List(context.Background(), domain.PaymentReportFilter{AppPublicID: &d.globex.PublicID}, domain.Page{Limit: 10})
	if res.Items[0].Connection != nil || res.Items[0].Provider() != "" {
		t.Fatalf("report without connection = %+v", res.Items[0])
	}

	var streamed []string
	if err := d.repo.Stream(context.Background(), domain.PaymentReportFilter{AppPublicID: &d.acme.PublicID},
		func(r *domain.PaymentReport) error { streamed = append(streamed, r.PaymentPublicID); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(streamed) != 6 || streamed[5] != older.PublicID {
		t.Fatalf("streamed = %v", streamed)
	}
}

// Totals per WIB day and month, clipped to the range, never adding
// currencies, unknown fees counted with their gross.
func TestIntegrationPaymentReportSummary(t *testing.T) {
	d := newReportDB(t)
	beforeMidnight := time.Date(2026, 9, 30, 16, 59, 59, 0, time.UTC) // 23:59:59 WIB, 30 Sep
	midnight := time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)         // 00:00 WIB, 1 Oct
	d.add(t, d.acme, d.alpha, "IDR", 10000, i64(100), beforeMidnight)
	d.add(t, d.acme, d.alpha, "IDR", 20000, nil, midnight)
	d.add(t, d.acme, d.alpha, "IDR", 30000, i64(300), midnight.Add(time.Hour))
	d.add(t, d.acme, d.alpha, "USD", 500, i64(5), midnight)
	d.add(t, d.acme, nil, "IDR", 7000, nil, midnight)
	d.add(t, d.globex, d.beta, "IDR", 1000, i64(10), midnight)
	d.add(t, d.acme, d.alpha, "IDR", 99999, nil, midnight.Add(-48*time.Hour)) // before the range

	from := time.Date(2026, 9, 29, 17, 0, 0, 0, time.UTC) // 30 Sep 00:00 WIB
	to := time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC)   // 2 Oct 00:00 WIB
	f := domain.PaymentReportFilter{From: &from, To: &to}
	ctx := context.Background()

	type key struct {
		start, end      time.Time
		app, conn, curr string
	}
	type sums struct{ count, gross, fee, net, unknown int64 }
	summarize := func(t *testing.T, interval domain.ReportInterval, f domain.PaymentReportFilter) ([]key, map[key]sums) {
		t.Helper()
		rows, err := d.repo.Summary(ctx, f, interval)
		if err != nil {
			t.Fatal(err)
		}
		var order []key
		out := map[key]sums{}
		for _, r := range rows {
			k := key{start: r.PeriodStart.UTC(), end: r.PeriodEnd.UTC(), app: r.App.Slug, curr: r.Currency}
			if r.Connection != nil {
				k.conn = r.Connection.Name
			}
			order = append(order, k)
			out[k] = sums{r.Count, r.Gross, r.Fee, r.Net, r.FeeUnknownCount}
		}
		return order, out
	}

	order, got := summarize(t, domain.ReportIntervalTotal, f)
	want := map[key]sums{
		{from, to, "acme", "alpha", "IDR"}:  {3, 60000, 400, 59600, 1},
		{from, to, "acme", "alpha", "USD"}:  {1, 500, 5, 495, 0},
		{from, to, "acme", "", "IDR"}:       {1, 7000, 0, 7000, 1},
		{from, to, "globex", "beta", "IDR"}: {1, 1000, 10, 990, 0},
	}
	assertSums := func(label string, got, want map[key]sums) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s rows = %v, want %v", label, got, want)
		}
		for k, w := range want {
			if got[k] != w {
				t.Fatalf("%s %+v = %+v, want %+v (all %v)", label, k, got[k], w, got)
			}
		}
	}
	assertSums("total", got, want)
	// Ordered by period, app slug, connection name (none last), currency.
	if order[0].conn != "alpha" || order[0].curr != "IDR" || order[1].curr != "USD" || order[2].conn != "" || order[3].app != "globex" {
		t.Fatalf("total order = %+v", order)
	}

	sep30, oct1 := from, midnight
	_, got = summarize(t, domain.ReportIntervalDay, f)
	assertSums("day", got, map[key]sums{
		{sep30, oct1, "acme", "alpha", "IDR"}: {1, 10000, 100, 9900, 0},
		{oct1, to, "acme", "alpha", "IDR"}:    {2, 50000, 300, 49700, 1},
		{oct1, to, "acme", "alpha", "USD"}:    {1, 500, 5, 495, 0},
		{oct1, to, "acme", "", "IDR"}:         {1, 7000, 0, 7000, 1},
		{oct1, to, "globex", "beta", "IDR"}:   {1, 1000, 10, 990, 0},
	})

	// The range starts and ends mid-month: September is clipped up to from,
	// October down to to.
	_, got = summarize(t, domain.ReportIntervalMonth, f)
	assertSums("month", got, map[key]sums{
		{from, oct1, "acme", "alpha", "IDR"}: {1, 10000, 100, 9900, 0},
		{oct1, to, "acme", "alpha", "IDR"}:   {2, 50000, 300, 49700, 1},
		{oct1, to, "acme", "alpha", "USD"}:   {1, 500, 5, 495, 0},
		{oct1, to, "acme", "", "IDR"}:        {1, 7000, 0, 7000, 1},
		{oct1, to, "globex", "beta", "IDR"}:  {1, 1000, 10, 990, 0},
	})
	// A full month ends at the next month's WIB midnight.
	novFrom, novTo := time.Date(2026, 8, 31, 17, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC)
	order, _ = summarize(t, domain.ReportIntervalMonth, domain.PaymentReportFilter{From: &novFrom, To: &novTo, AppPublicID: &d.globex.PublicID})
	if len(order) != 1 || !order[0].start.Equal(midnight) || !order[0].end.Equal(novTo) {
		t.Fatalf("october = %+v", order)
	}

	live := domain.EnvLive
	usd := "USD"
	_, got = summarize(t, domain.ReportIntervalTotal, domain.PaymentReportFilter{From: &from, To: &to, Currency: &usd, Environment: &live,
		ConnectionPublicID: &d.alpha.PublicID})
	assertSums("filtered", got, map[key]sums{{from, to, "acme", "alpha", "USD"}: {1, 500, 5, 495, 0}})

	empty := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	emptyTo := empty.Add(time.Hour)
	if rows, err := d.repo.Summary(ctx, domain.PaymentReportFilter{From: &empty, To: &emptyTo}, domain.ReportIntervalDay); err != nil || len(rows) != 0 {
		t.Fatalf("empty range = %+v %v", rows, err)
	}
}

// FillFee writes only an unknown fee, with the net the CHECKs require, and
// never overwrites a fee a callback stored.
func TestIntegrationPaymentReportFillFee(t *testing.T) {
	d := newReportDB(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	unknown := d.add(t, d.initech, d.testConn, "IDR", 150000, nil, at)
	known := d.add(t, d.initech, d.testConn, "IDR", 5000, i64(100), at)

	feeOf := func(p *domain.Payment) (fee, net *int64) {
		t.Helper()
		if err := d.pool.QueryRow(ctx, `SELECT fee, net FROM payment_reports WHERE payment_id = $1`, p.ID).Scan(&fee, &net); err != nil {
			t.Fatal(err)
		}
		return fee, net
	}
	if ok, err := d.repo.FillFee(ctx, unknown.ID, 2220); err != nil || !ok {
		t.Fatalf("fill unknown: %v, %v", ok, err)
	}
	if fee, net := feeOf(unknown); fee == nil || *fee != 2220 || net == nil || *net != 147780 {
		t.Fatalf("filled = %v / %v", fee, net)
	}
	if ok, err := d.repo.FillFee(ctx, unknown.ID, 9999); err != nil || ok {
		t.Fatalf("second fill: %v, %v", ok, err)
	}
	if ok, err := d.repo.FillFee(ctx, known.ID, 7); err != nil || ok {
		t.Fatalf("fill known: %v, %v", ok, err)
	}
	if fee, _ := feeOf(known); *fee != 100 {
		t.Fatalf("known fee overwritten: %d", *fee)
	}
	if ok, err := d.repo.FillFee(ctx, 987654321, 1); err != nil || ok {
		t.Fatalf("no report: %v, %v", ok, err)
	}
}
