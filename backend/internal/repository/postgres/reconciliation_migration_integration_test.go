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

type reportRow struct {
	connectionID     *int64
	currency, status string
	gross            int64
	fee, net         *int64
	paidAt           time.Time
	settledAt        *time.Time
}

func reportRows(t *testing.T, pool *pgxpool.Pool) map[int64]reportRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT payment_id, connection_id, currency, status, gross, fee, net,
		paid_at, settled_at FROM payment_reports`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]reportRow{}
	for rows.Next() {
		var pid int64
		var r reportRow
		if err := rows.Scan(&pid, &r.connectionID, &r.currency, &r.status, &r.gross, &r.fee, &r.net, &r.paidAt, &r.settledAt); err != nil {
			t.Fatal(err)
		}
		out[pid] = r
	}
	return out
}

type ledgerRow struct {
	amount, fee, net int64
	settledAt        *time.Time
	createdAt        time.Time
}

func ledgerRows(t *testing.T, pool *pgxpool.Pool) map[int64]ledgerRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT payment_id, amount, xendit_fee, net_amount, settled_at, created_at
		FROM ledger_entries WHERE type = 'PAYMENT'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]ledgerRow{}
	for rows.Next() {
		var pid int64
		var l ledgerRow
		if err := rows.Scan(&pid, &l.amount, &l.fee, &l.net, &l.settledAt, &l.createdAt); err != nil {
			t.Fatal(err)
		}
		out[pid] = l
	}
	return out
}

func i64(v int64) *int64 { return &v }

// Reports are backfilled from the ledger (every legacy fee is unknown), down
// copies reports made since back into the ledger, and up works again on the
// data.
func TestIntegrationMigration10UpDownUp(t *testing.T) {
	db := pgtest.New(t, 9)
	pool := openPool(t, db.DSN)
	ctx := context.Background()

	app := &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "Acme", Environment: domain.EnvLive, Status: domain.AppActive, WebhookSecretEnc: "x"}
	if err := postgres.NewAppRepo(pool).Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	conn := newTestConnection(t, pool, domain.EnvLive)
	payments := postgres.NewPaymentRepo(pool)
	pay := func(ref, status string, amount int64, paidAt, settledAt *time.Time) int64 {
		p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: ref, ExternalID: "kp_" + ref, Amount: amount,
			Currency: "IDR", Status: domain.PaymentStatus(status), ConnectionID: &conn.ID, Provider: "xendit"}
		if err := payments.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		exec(t, pool, `UPDATE payments SET paid_at = $2, settled_at = $3 WHERE id = $1`, p.ID, paidAt, settledAt)
		return p.ID
	}
	t1 := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	s2 := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	ledgerAt := time.Date(2026, 9, 1, 3, 0, 5, 0, time.UTC)
	zeroFee := pay("a", "PAID", 10000, &t1, nil)
	withFee := pay("b", "SETTLED", 20000, &t1, &s2)
	noPaidAt := pay("c", "PAID", 30000, nil, nil)
	later := pay("d", "PAID", 40000, &t1, nil)
	exec(t, pool, `INSERT INTO ledger_entries (app_id, payment_id, amount, xendit_fee, net_amount, type, settled_at, created_at) VALUES
		($1, $2, 10000, 0, 10000, 'PAYMENT', NULL, $5),
		($1, $3, 20000, 150, 19850, 'PAYMENT', NULL, $5),
		($1, $4, 30000, 300, 29700, 'PAYMENT', $6, $5),
		($1, $4, 30000, 0, 30000, 'REFUND', NULL, $5)`, app.ID, zeroFee, withFee, noPaidAt, ledgerAt, s2)
	ledgerBefore := ledgerRows(t, pool)

	if err := db.Migrate.Migrate(10); err != nil {
		t.Fatalf("up: %v", err)
	}
	got := reportRows(t, pool)
	if len(got) != 3 {
		t.Fatalf("reports = %+v", got)
	}
	want := map[int64]reportRow{
		zeroFee: {connectionID: &conn.ID, currency: "IDR", status: "PAID", gross: 10000, paidAt: t1},
		// The legacy xendit_fee is the customer's fee, never Xendit's.
		withFee:  {connectionID: &conn.ID, currency: "IDR", status: "SETTLED", gross: 20000, paidAt: t1, settledAt: &s2},
		noPaidAt: {connectionID: &conn.ID, currency: "IDR", status: "SETTLED", gross: 30000, paidAt: ledgerAt, settledAt: &s2},
	}
	for pid, w := range want {
		g, ok := got[pid]
		if !ok || g.status != w.status || g.gross != w.gross || g.currency != w.currency || *g.connectionID != *w.connectionID ||
			!g.paidAt.Equal(w.paidAt) || !eqInt(g.fee, w.fee) || !eqInt(g.net, w.net) || !eqTime(g.settledAt, w.settledAt) {
			t.Fatalf("report of %d = %+v, want %+v", pid, g, w)
		}
	}

	// The constraints hold: unknown fee goes with unknown net, net = gross - fee.
	for _, bad := range []string{
		`INSERT INTO payment_reports (payment_id, app_id, currency, gross, fee, net, status, paid_at) VALUES ($1, $2, 'IDR', 100, 10, NULL, 'PAID', now())`,
		`INSERT INTO payment_reports (payment_id, app_id, currency, gross, fee, net, status, paid_at) VALUES ($1, $2, 'IDR', 100, 10, 80, 'PAID', now())`,
		`INSERT INTO payment_reports (payment_id, app_id, currency, gross, fee, net, status, paid_at) VALUES ($1, $2, 'IDR', 100, -1, 101, 'PAID', now())`,
		`INSERT INTO payment_reports (payment_id, app_id, currency, gross, status, paid_at) VALUES ($1, $2, 'IDR', 100, 'PENDING', now())`,
		`INSERT INTO payment_reports (payment_id, app_id, currency, gross, status, paid_at) VALUES ($1, $2, 'IDR', 100, 'PAID', now()),
			($1, $2, 'IDR', 100, 'PAID', now())`,
	} {
		if _, err := pool.Exec(ctx, bad, later, app.ID); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}

	// A report written by the new code after the upgrade.
	if err := postgres.NewPaymentReportRepo(pool).Upsert(ctx, &domain.PaymentReport{PaymentID: later, AppID: app.ID,
		ConnectionID: &conn.ID, Currency: "IDR", Gross: 40000, Fee: i64(400), Net: i64(39600), Status: domain.PaymentStatusPaid,
		PaidAt: t1}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate.Migrate(9); err != nil {
		t.Fatalf("down: %v", err)
	}
	if tableExists(t, pool, "payment_reports") || tableExists(t, pool, "reconciliation_runs") || tableExists(t, pool, "reconciliation_items") {
		t.Fatal("down left tables behind")
	}
	ledgerAfter := ledgerRows(t, pool)
	for pid, l := range ledgerBefore {
		if a := ledgerAfter[pid]; a.amount != l.amount || a.fee != l.fee || a.net != l.net || !eqTime(a.settledAt, l.settledAt) {
			t.Fatalf("ledger of %d changed: %+v → %+v", pid, l, a)
		}
	}
	if l, ok := ledgerAfter[later]; !ok || l.amount != 40000 || l.fee != 400 || l.net != 39600 {
		t.Fatalf("ledger of the new report = %+v", l)
	}

	if err := db.Migrate.Migrate(10); err != nil {
		t.Fatalf("up again: %v", err)
	}
	// A ledger row cannot tell a fee the new code learned from one the old
	// code copied from fees_paid_amount, so after a rollback every fee is
	// unknown again: a missing fee is never reported as a fee_mismatch.
	if got := reportRows(t, pool); len(got) != 4 || got[later].gross != 40000 || got[later].fee != nil ||
		got[later].net != nil || got[zeroFee].fee != nil {
		t.Fatalf("reports after up again = %+v", got)
	}
}

// Up and down on an empty database.
func TestIntegrationMigration10Empty(t *testing.T) {
	db := pgtest.New(t, 9)
	if err := db.Migrate.Migrate(10); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := db.Migrate.Migrate(9); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := db.Migrate.Migrate(10); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

func eqInt(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func eqTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
