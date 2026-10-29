package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

// runQueue records the reconciliation runs queued.
type runQueue struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (q *runQueue) EnqueueReconciliation(context.Context, int64, int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls++
	return q.err
}

// reconDB is a fresh database with one live app paying through a fake
// connection whose transactions the test sets, and an admin user.
type reconDB struct {
	pool     *pgxpool.Pool
	repo     *postgres.ReconciliationRepo
	reports  *postgres.PaymentReportRepo
	payments *postgres.PaymentRepo
	fake     *providertest.Fake
	svc      *service.ReconciliationService
	deps     service.ReconciliationDeps
	queue    *runQueue
	app      *domain.App
	conn     *domain.Connection
	admin    *domain.User
	ctx      context.Context
}

func newReconDB(t *testing.T) *reconDB {
	t.Helper()
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	d := &reconDB{pool: pool, repo: postgres.NewReconciliationRepo(pool), reports: postgres.NewPaymentReportRepo(pool),
		payments: postgres.NewPaymentRepo(pool), fake: providertest.New(), queue: &runQueue{}}
	d.admin = &domain.User{PublicID: id.New(id.User), Email: "ops@example.com", Name: "Ops Lead", Role: domain.RoleAdmin,
		PasswordHash: "hash", SessionVersion: 1}
	if err := postgres.NewUserRepo(pool).Create(context.Background(), d.admin); err != nil {
		t.Fatal(err)
	}
	d.ctx = domain.WithActor(context.Background(), domain.UserActor(d.admin.PublicID, "203.0.113.7", "it-agent", "req-it"))
	registry, err := provider.NewRegistry(d.fake)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	tx := postgres.NewTxManager(pool)
	audit := service.NewAuditService(postgres.NewAuditRepo(pool))
	conns := postgres.NewConnectionRepo(pool)
	connSvc := service.NewConnectionService(service.ConnectionDeps{Connections: conns, Merchants: postgres.NewMerchantRepo(pool),
		Apps: postgres.NewAppRepo(pool), Payments: d.payments, Registry: registry, Sealer: cipher, Tx: tx, Audit: audit,
		BaseURL: "https://pay.example.com"})
	created, err := connSvc.Create(d.ctx, service.CreateConnectionInput{Provider: "fake", Name: "fake-live", Environment: domain.EnvLive,
		Credentials: map[string]any{"api_key": "fake-api-key-0001", "token": "fake-token-0001"}})
	if err != nil {
		t.Fatal(err)
	}
	d.conn = created.Connection
	d.app = &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "Acme", Environment: domain.EnvLive, Status: domain.AppActive, WebhookSecretEnc: "x"}
	if err := postgres.NewAppRepo(pool).Create(context.Background(), d.app); err != nil {
		t.Fatal(err)
	}
	d.deps = service.ReconciliationDeps{Runs: d.repo, Connections: conns, Registry: registry,
		Source: service.NewProviderSource(registry, cipher), Tx: tx, Enqueuer: d.queue, Audit: audit}
	d.svc = service.NewReconciliationService(d.deps)
	return d
}

// window is yesterday, whole hours.
func (d *reconDB) window() (from, to time.Time) {
	to = time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	return to.Add(-24 * time.Hour), to
}

func (d *reconDB) paid(t *testing.T, ref string, amount int64, paidAt time.Time) *domain.Payment {
	t.Helper()
	ctx := context.Background()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: d.app.ID, OrderID: "order-" + ref, ExternalID: ref,
		ConnectionID: &d.conn.ID, Provider: "fake", Amount: amount, Currency: "IDR", Status: domain.PaymentStatusPaid}
	if err := d.payments.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	exec(t, d.pool, `UPDATE payments SET paid_at = $2 WHERE id = $1`, p.ID, paidAt)
	if err := d.reports.Upsert(ctx, &domain.PaymentReport{PaymentID: p.ID, AppID: d.app.ID, ConnectionID: &d.conn.ID,
		Currency: "IDR", Gross: amount, Status: domain.PaymentStatusPaid, PaidAt: paidAt}); err != nil {
		t.Fatal(err)
	}
	return p
}

func (d *reconDB) gateway(txs ...domain.GatewayTransaction) {
	d.fake.Pages = []provider.TransactionPage{{Transactions: txs}}
}

func tx(ref, txID string, amount int64, at time.Time) domain.GatewayTransaction {
	return domain.GatewayTransaction{Reference: ref, ProviderPaymentID: txID, ProviderStatus: "SUCCESS", Status: domain.PaymentStatusPaid,
		Amount: amount, Currency: "IDR", OccurredAt: at}
}

func (d *reconDB) create(t *testing.T) *domain.ReconciliationRun {
	t.Helper()
	from, to := d.window()
	run, err := d.svc.CreateRun(d.ctx, service.CreateRunInput{ConnectionID: d.conn.PublicID, From: from.Format(time.RFC3339),
		To: to.Format(time.RFC3339), CreatedByID: &d.admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (d *reconDB) execute(t *testing.T, publicID string) *domain.ReconciliationRun {
	t.Helper()
	run, err := d.repo.GetRun(context.Background(), publicID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.svc.Execute(context.Background(), run.ID, run.RunCount); err != nil {
		t.Fatalf("execute: %v", err)
	}
	run, err = d.repo.GetRun(context.Background(), publicID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (d *reconDB) items(t *testing.T, run *domain.ReconciliationRun) map[string]domain.ReconciliationItem {
	t.Helper()
	out := map[string]domain.ReconciliationItem{}
	err := d.repo.StreamItems(context.Background(), run.ID, domain.ReconciliationItemFilter{}, func(it *domain.ReconciliationItem) error {
		if _, dup := out[it.Reference]; dup {
			t.Fatalf("reference %s twice", it.Reference)
		}
		out[it.Reference] = *it
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// auditEntries returns the action, before and after of the target's entries,
// oldest first.
func (d *reconDB) auditEntries(t *testing.T, target string) [][3]string {
	t.Helper()
	rows, err := d.pool.Query(context.Background(), `SELECT action, COALESCE(before::text, ''), COALESCE(after::text, '')
		FROM audit_logs WHERE target_id = $1 ORDER BY id`, target)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][3]string
	for rows.Next() {
		var e [3]string
		if err := rows.Scan(&e[0], &e[1], &e[2]); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// A run stores its items; re-running it recomputes them in place (no
// duplicates, kept resolutions only for unchanged results, vanished
// references removed); a run in progress cannot be re-run until it stalls;
// every step is audited.
func TestIntegrationReconciliationRunAndRerun(t *testing.T) {
	d := newReconDB(t)
	from, _ := d.window()
	mid := from.Add(3 * time.Hour)
	match := d.paid(t, "kp_pay_match", 5000, mid)
	d.paid(t, "kp_pay_keep", 6000, mid)
	d.paid(t, "kp_pay_change", 7000, mid)
	d.gateway(tx("kp_pay_match", "tx_1", 5000, mid), tx("kp_pay_change", "tx_2", 7500, mid), tx("kp_pay_gone", "tx_3", 100, mid))

	run := d.create(t)
	if run.Status != domain.RunQueued || run.CreatedBy == nil || run.CreatedBy.Name != "Ops Lead" || d.queue.calls != 1 {
		t.Fatalf("created = %+v", run)
	}
	run = d.execute(t, run.PublicID)
	if run.Status != domain.RunSucceeded || run.UnresolvedCount != 3 || run.Summary.Total != 4 || run.StartedAt == nil || run.FinishedAt == nil {
		t.Fatalf("run = %+v summary %+v", run, run.Summary)
	}
	first := d.items(t, run)
	want := map[string]domain.ReconciliationResult{"kp_pay_match": domain.ResultMatched, "kp_pay_keep": domain.ResultMissingInGateway,
		"kp_pay_change": domain.ResultAmountMismatch, "kp_pay_gone": domain.ResultMissingInKeipay}
	for ref, r := range want {
		if first[ref].Result != r {
			t.Fatalf("%s = %s, want %s", ref, first[ref].Result, r)
		}
	}
	m := first["kp_pay_match"]
	if m.Keipay == nil || m.Keipay.PaymentPublicID != match.PublicID || m.Keipay.App != d.app.Summary() || m.Gateway == nil ||
		m.Gateway.ProviderPaymentID != "tx_1" || m.RunPublicID != run.PublicID || !m.AnchorAt.Equal(mid) {
		t.Fatalf("matched item = %+v / %+v / %+v", m, m.Keipay, m.Gateway)
	}

	for _, ref := range []string{"kp_pay_keep", "kp_pay_change"} {
		if _, err := d.svc.ResolveItem(d.ctx, first[ref].PublicID, "looked at "+ref, &d.admin.ID); err != nil {
			t.Fatal(err)
		}
	}
	d.gateway(tx("kp_pay_match", "tx_1", 5000, mid), tx("kp_pay_change", "tx_2", 7000, mid))
	again, err := d.svc.Rerun(d.ctx, run.PublicID)
	if err != nil || again.Status != domain.RunQueued || again.RunCount != 2 || again.Summary != nil || again.FinishedAt != nil {
		t.Fatalf("rerun = %+v, %v", again, err)
	}
	if _, err := d.svc.Rerun(d.ctx, run.PublicID); !errors.Is(err, domain.RunInProgress()) {
		t.Fatalf("rerun while queued = %v", err)
	}
	// A task of the first run count is stale now and writes nothing.
	if err := d.svc.Execute(context.Background(), run.ID, 1); err != nil {
		t.Fatal(err)
	}
	if r, _ := d.repo.GetRun(context.Background(), run.PublicID); r.Status != domain.RunQueued || r.RunCount != 2 {
		t.Fatalf("stale task changed the run: %+v", r)
	}

	run = d.execute(t, run.PublicID)
	second := d.items(t, run)
	if len(second) != 3 {
		t.Fatalf("items after rerun = %v", second)
	}
	if _, ok := second["kp_pay_gone"]; ok {
		t.Fatal("vanished reference kept")
	}
	keep, change := second["kp_pay_keep"], second["kp_pay_change"]
	if keep.PublicID != first["kp_pay_keep"].PublicID || keep.Resolution == nil || keep.Resolution.Note != "looked at kp_pay_keep" ||
		keep.Resolution.ResolvedBy == nil || keep.Resolution.ResolvedBy.PublicID != d.admin.PublicID {
		t.Fatalf("unchanged item lost its resolution: %+v", keep)
	}
	if change.PublicID != first["kp_pay_change"].PublicID || change.Result != domain.ResultMatched || change.Resolution != nil {
		t.Fatalf("changed item = %+v", change)
	}
	if second["kp_pay_match"].PublicID != first["kp_pay_match"].PublicID {
		t.Fatal("matched item got a new id")
	}
	var dupes int
	_ = d.pool.QueryRow(context.Background(), `SELECT count(*) FROM (SELECT reference FROM reconciliation_items
		WHERE run_id = $1 GROUP BY reference HAVING count(*) > 1) x`, run.ID).Scan(&dupes)
	if dupes != 0 || run.UnresolvedCount != 0 || run.Summary.Total != 3 || run.Summary.Results[domain.ResultMatched] != 2 {
		t.Fatalf("run after rerun = %+v %+v, dupes %d", run, run.Summary, dupes)
	}

	// Running for 14 minutes is in progress; untouched for 16 it has stalled.
	exec(t, d.pool, `UPDATE reconciliation_runs SET status = 'running', updated_at = now() - interval '14 minutes' WHERE id = $1`, run.ID)
	if _, err := d.svc.Rerun(d.ctx, run.PublicID); !errors.Is(err, domain.RunInProgress()) {
		t.Fatalf("rerun while running = %v", err)
	}
	exec(t, d.pool, `UPDATE reconciliation_runs SET updated_at = now() - interval '16 minutes' WHERE id = $1`, run.ID)
	stalled, err := d.svc.Rerun(d.ctx, run.PublicID)
	if err != nil || stalled.RunCount != 3 || stalled.Status != domain.RunQueued {
		t.Fatalf("rerun of a stalled run = %+v, %v", stalled, err)
	}

	entries := d.auditEntries(t, run.PublicID)
	if len(entries) != 3 || entries[0][0] != "reconciliation_run.create" || entries[1][0] != "reconciliation_run.rerun" ||
		entries[2][0] != "reconciliation_run.rerun" {
		t.Fatalf("audit = %v", entries)
	}
	jsonEqual(t, "rerun before", []byte(entries[1][1]), map[string]any{"status": "succeeded", "run_count": 1})
	jsonEqual(t, "rerun after", []byte(entries[1][2]), map[string]any{"status": "queued", "run_count": 2})
	jsonEqual(t, "stalled before", []byte(entries[2][1]), map[string]any{"status": "running", "run_count": 2})
}

// A gateway failure fails the run with a generic message.
func TestIntegrationReconciliationRunFails(t *testing.T) {
	d := newReconDB(t)
	d.fake.ListErrs = []error{provider.NewStatusError(provider.ErrRejected, 401, "API key xnd_production_secret is invalid")}
	run := d.execute(t, d.create(t).PublicID)
	if run.Status != domain.RunFailed || run.Error == nil || run.Error.Code != "provider_rejected" ||
		strings.Contains(run.Error.Message, "xnd_") || run.FinishedAt == nil || run.Summary != nil {
		t.Fatalf("run = %+v (error %+v)", run, run.Error)
	}
	if ok, err := d.repo.ClaimRun(context.Background(), run.ID, 1); ok || err != nil {
		t.Fatalf("claimed a failed run: %v %v", ok, err)
	}
	if ok, _ := d.repo.FailRun(context.Background(), run.ID, 1, domain.RunError{Code: "x", Message: "y"}); ok {
		t.Fatal("failed a finished run again")
	}
}

// A heartbeat refreshes updated_at only of a running run with the same run
// count.
func TestIntegrationTouchRun(t *testing.T) {
	d := newReconDB(t)
	ctx := context.Background()
	run := d.create(t)
	stale := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	backdate := func() { exec(t, d.pool, `UPDATE reconciliation_runs SET updated_at = $2 WHERE id = $1`, run.ID, stale) }
	updatedAt := func() time.Time {
		t.Helper()
		got, err := d.repo.GetRunByID(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.UpdatedAt
	}
	touch := func(runCount int) bool {
		t.Helper()
		ok, err := d.repo.TouchRun(ctx, run.ID, runCount)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	backdate()
	if touch(1) || !updatedAt().Equal(stale) {
		t.Fatal("touched a queued run")
	}
	if ok, err := d.repo.ClaimRun(ctx, run.ID, 1); !ok || err != nil {
		t.Fatalf("claim: %v %v", ok, err)
	}
	backdate()
	if touch(2) || !updatedAt().Equal(stale) {
		t.Fatal("touched a run of another run count")
	}
	if ok, err := d.repo.TouchRun(ctx, run.ID+1000, 1); ok || err != nil {
		t.Fatalf("touched an unknown run: %v %v", ok, err)
	}
	if !touch(1) || time.Since(updatedAt()) > time.Minute {
		t.Fatalf("running run not touched: updated_at %v", updatedAt())
	}
	if ok, err := d.repo.FailRun(ctx, run.ID, 1, domain.RunError{Code: "x", Message: "y"}); !ok || err != nil {
		t.Fatalf("fail: %v %v", ok, err)
	}
	backdate()
	if touch(1) || !updatedAt().Equal(stale) {
		t.Fatal("touched a failed run")
	}
}

// Resolving is stored and audited in one transaction, and refused for matched
// items, resolved items and runs in progress.
func TestIntegrationResolveItem(t *testing.T) {
	d := newReconDB(t)
	from, _ := d.window()
	mid := from.Add(time.Hour)
	d.paid(t, "kp_pay_a", 1000, mid)
	d.paid(t, "kp_pay_b", 1000, mid)
	d.paid(t, "kp_pay_m", 1000, mid)
	d.gateway(tx("kp_pay_m", "tx_m", 1000, mid))
	run := d.execute(t, d.create(t).PublicID)
	items := d.items(t, run)

	// The audit insert fails: nothing is resolved.
	failing := d.deps
	failing.Audit = failingAuditor{}
	if _, err := service.NewReconciliationService(failing).ResolveItem(d.ctx, items["kp_pay_a"].PublicID, "note", &d.admin.ID); err == nil {
		t.Fatal("resolved without an audit entry")
	}
	if it, _ := d.repo.GetItem(context.Background(), items["kp_pay_a"].PublicID); it.Resolution != nil {
		t.Fatal("resolution kept after the audit failed")
	}

	note := "Refunded by hand — ticket #42 ✅"
	got, err := d.svc.ResolveItem(d.ctx, items["kp_pay_a"].PublicID, "  "+note+"  ", &d.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resolution == nil || got.Resolution.Note != note || got.Resolution.ResolvedBy.Name != "Ops Lead" {
		t.Fatalf("item = %+v", got)
	}
	if r, _ := d.repo.GetRun(context.Background(), run.PublicID); r.UnresolvedCount != 1 {
		t.Fatalf("unresolved = %d", r.UnresolvedCount)
	}
	entries := d.auditEntries(t, got.PublicID)
	if len(entries) != 1 || entries[0][0] != "reconciliation_item.resolve" {
		t.Fatalf("audit = %v", entries)
	}
	jsonEqual(t, "before", []byte(entries[0][1]), map[string]any{"result": "missing_in_gateway", "resolved": false})
	jsonEqual(t, "after", []byte(entries[0][2]), map[string]any{"result": "missing_in_gateway", "resolved": true, "note": note})

	var coded *domain.Error
	if _, err := d.svc.ResolveItem(d.ctx, items["kp_pay_a"].PublicID, "again", &d.admin.ID); !errors.As(err, &coded) || coded.Code != domain.CodeConflict {
		t.Fatalf("twice = %v", err)
	}
	if _, err := d.svc.ResolveItem(d.ctx, items["kp_pay_m"].PublicID, "fine", &d.admin.ID); !errors.As(err, &coded) || coded.Code != domain.CodeValidationFailed {
		t.Fatalf("matched = %v", err)
	}
	exec(t, d.pool, `UPDATE reconciliation_runs SET status = 'running' WHERE id = $1`, run.ID)
	if _, err := d.svc.ResolveItem(d.ctx, items["kp_pay_b"].PublicID, "now", &d.admin.ID); !errors.Is(err, domain.RunInProgress()) {
		t.Fatalf("running = %v", err)
	}
	if r, _ := d.repo.GetRun(context.Background(), run.PublicID); r.UnresolvedCount != 1 {
		t.Fatalf("refusals changed the count: %d", r.UnresolvedCount)
	}

	// The item filters.
	resolved, unresolved := true, false
	missing := domain.ResultMissingInGateway
	for _, tc := range []struct {
		f    domain.ReconciliationItemFilter
		want int
	}{
		{domain.ReconciliationItemFilter{}, 3},
		{domain.ReconciliationItemFilter{Resolved: &resolved}, 1},
		{domain.ReconciliationItemFilter{Resolved: &unresolved}, 2},
		{domain.ReconciliationItemFilter{Result: &missing}, 2},
		{domain.ReconciliationItemFilter{Result: &missing, Resolved: &unresolved}, 1},
	} {
		res, err := d.repo.ListItems(context.Background(), run.ID, tc.f, domain.Page{Limit: 50})
		if err != nil || len(res.Items) != tc.want {
			t.Fatalf("filter %+v = %d items, %v", tc.f, len(res.Items), err)
		}
	}
}

type failingAuditor struct{}

func (failingAuditor) Record(context.Context, string, domain.AuditTarget, any, any) error {
	return errors.New("audit down")
}

// Items are written in batches; a second save updates in place and deletes
// the rest; list pages walk every item once.
func TestIntegrationSaveItemsInBatches(t *testing.T) {
	d := newReconDB(t)
	run := d.create(t)
	at := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	items := make([]domain.ReconciliationItem, 2500)
	for i := range items {
		items[i] = domain.ReconciliationItem{PublicID: id.New(id.ReconciliationItem), Reference: fmt.Sprintf("ref-%05d", i),
			Result: domain.ResultMissingInKeipay, AnchorAt: at,
			Gateway: &domain.GatewaySide{ProviderPaymentID: fmt.Sprintf("tx_%d", i), Status: domain.PaymentStatusPaid, Amount: int64(i),
				Currency: "IDR", OccurredAt: at, TransactionCount: 1}}
	}
	ctx := context.Background()
	unresolved, err := d.repo.SaveItems(ctx, run.ID, items)
	if err != nil || unresolved != 2500 {
		t.Fatalf("first save = %d, %v", unresolved, err)
	}
	// Keep the first 1200, half of them now matched; drop the rest.
	next := items[:1200]
	for i := range next {
		next[i].PublicID = id.New(id.ReconciliationItem)
		if i%2 == 0 {
			next[i].Result = domain.ResultMatched
		}
	}
	unresolved, err = d.repo.SaveItems(ctx, run.ID, next)
	if err != nil || unresolved != 600 {
		t.Fatalf("second save = %d, %v", unresolved, err)
	}
	seen := map[string]bool{}
	page := domain.Page{Limit: 200}
	for {
		res, err := d.repo.ListItems(ctx, run.ID, domain.ReconciliationItemFilter{}, page)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range res.Items {
			if seen[it.PublicID] {
				t.Fatalf("%s listed twice", it.PublicID)
			}
			seen[it.PublicID] = true
			if it.Gateway == nil || it.Gateway.Currency != "IDR" || it.Keipay != nil {
				t.Fatalf("item = %+v", it)
			}
		}
		if res.NextCursor == "" {
			break
		}
		c, _ := domain.DecodeCursor(res.NextCursor)
		page.After = &c
	}
	if len(seen) != 1200 {
		t.Fatalf("listed %d items, want 1200", len(seen))
	}
	if unresolved, err := d.repo.SaveItems(ctx, run.ID, nil); err != nil || unresolved != 0 {
		t.Fatalf("empty save = %d, %v", unresolved, err)
	}
}

// KeiPay's side is the reports paid within the window plus the gateway's
// references, each payment once.
func TestIntegrationKeipayPayments(t *testing.T) {
	d := newReconDB(t)
	from, to := d.window()
	inside := d.paid(t, "kp_pay_in", 1000, from.Add(time.Hour))
	d.paid(t, "kp_pay_out", 1000, to.Add(time.Hour))
	pending := &domain.Payment{PublicID: id.New(id.Payment), AppID: d.app.ID, OrderID: "o-p", ExternalID: "kp_pay_pending",
		ConnectionID: &d.conn.ID, Provider: "fake", Amount: 3000, Currency: "IDR", Status: domain.PaymentStatusPending}
	if err := d.payments.Create(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	other := newTestConnection(t, d.pool, domain.EnvLive)
	elsewhere := &domain.Payment{PublicID: id.New(id.Payment), AppID: d.app.ID, OrderID: "o-e", ExternalID: "kp_pay_elsewhere",
		ConnectionID: &other.ID, Provider: "xendit", Amount: 3000, Currency: "IDR", Status: domain.PaymentStatusPending}
	if err := d.payments.Create(context.Background(), elsewhere); err != nil {
		t.Fatal(err)
	}
	refs := []string{"kp_pay_in", "kp_pay_pending", "kp_pay_elsewhere", "kp_pay_unknown"}
	got, err := d.repo.KeipayPayments(context.Background(), d.conn.ID, from, to, refs)
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]domain.KeipayPayment{}
	for _, k := range got {
		byRef[k.Reference] = k
	}
	if len(got) != 2 || byRef["kp_pay_in"].PaymentPublicID != inside.PublicID || byRef["kp_pay_in"].PaidAt == nil ||
		byRef["kp_pay_in"].App != d.app.Summary() || byRef["kp_pay_pending"].Status != domain.PaymentStatusPending ||
		byRef["kp_pay_pending"].PaidAt != nil || byRef["kp_pay_pending"].Fee != nil {
		t.Fatalf("keipay payments = %+v", got)
	}
}

// Run lists filter by connection, status and environment and page by
// (created_at, id).
func TestIntegrationListRuns(t *testing.T) {
	d := newReconDB(t)
	var ids []string
	for range 3 {
		ids = append(ids, d.create(t).PublicID)
	}
	d.queue.err = errors.New("queue down")
	failed := d.create(t)
	if failed.Status != domain.RunFailed || failed.Error.Code != "not_queued" {
		t.Fatalf("not queued = %+v", failed)
	}
	ctx := context.Background()
	var seen []string
	page := domain.Page{Limit: 3}
	for {
		res, err := d.repo.ListRuns(ctx, domain.ReconciliationRunFilter{}, page)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res.Items {
			seen = append(seen, r.PublicID)
		}
		if res.NextCursor == "" {
			break
		}
		c, _ := domain.DecodeCursor(res.NextCursor)
		page.After = &c
	}
	if len(seen) != 4 || seen[0] != failed.PublicID || seen[3] != ids[0] {
		t.Fatalf("runs = %v", seen)
	}
	queued, failedStatus := domain.RunQueued, domain.RunFailed
	live, test := domain.EnvLive, domain.EnvTest
	unknown := id.New(id.Connection)
	for _, tc := range []struct {
		f    domain.ReconciliationRunFilter
		want int
	}{
		{domain.ReconciliationRunFilter{Status: &queued}, 3},
		{domain.ReconciliationRunFilter{Status: &failedStatus}, 1},
		{domain.ReconciliationRunFilter{Environment: &live, ConnectionPublicID: &d.conn.PublicID}, 4},
		{domain.ReconciliationRunFilter{Environment: &test}, 0},
		{domain.ReconciliationRunFilter{ConnectionPublicID: &unknown}, 0},
	} {
		res, err := d.repo.ListRuns(ctx, tc.f, domain.Page{Limit: 50})
		if err != nil || len(res.Items) != tc.want {
			t.Fatalf("filter %+v = %d, %v", tc.f, len(res.Items), err)
		}
	}
	if _, err := d.repo.GetRun(ctx, id.New(id.Reconciliation)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown run = %v", err)
	}
	if _, err := d.repo.GetItem(ctx, id.New(id.ReconciliationItem)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown item = %v", err)
	}
	var raw []byte
	_ = d.pool.QueryRow(ctx, `SELECT summary FROM reconciliation_runs WHERE public_id = $1`, failed.PublicID).Scan(&raw)
	if raw != nil {
		t.Fatalf("failed run summary = %s", raw)
	}
}
