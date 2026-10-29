package http

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
)

var (
	runKeys         = []string{"id", "connection", "from", "to", "status", "run_count", "summary", "error", "created_by", "created_at", "started_at", "finished_at"}
	itemKeys        = []string{"id", "run_id", "reference", "result", "keipay", "gateway", "resolution", "created_at", "updated_at"}
	keipaySideKeys  = []string{"payment_id", "app", "status", "amount", "currency", "fee", "paid_at"}
	gatewaySideKeys = []string{"provider_payment_id", "provider_status", "status", "amount", "currency", "fee", "occurred_at",
		"transaction_count"}
	reportKeys = []string{"payment_id", "reference", "order_id", "app", "connection", "provider", "currency", "gross", "fee", "net",
		"status", "paid_at", "settled_at"}
)

// reconApp is an app paying through the fixture's fake test connection.
func (f *apiFixture) reconApp(t *testing.T) *domain.App {
	t.Helper()
	a := &domain.App{PublicID: id.New(id.App), Slug: "acme-" + strings.ToLower(id.New(id.App)[20:]), Name: "Acme",
		Environment: domain.EnvTest, Status: domain.AppActive}
	if err := f.apps.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

// paidPayment stores a paid payment of app through the fake test connection
// with its report.
func (f *apiFixture) paidPayment(t *testing.T, app *domain.App, ref string, amount int64, paidAt time.Time) *domain.Payment {
	t.Helper()
	ctx := context.Background()
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: "order-" + ref, ExternalID: ref,
		ConnectionID: &f.testConn.ID, Provider: "fake", Amount: amount, Currency: "IDR", Status: domain.PaymentStatusPaid, PaidAt: &paidAt}
	if err := f.payments.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	conn := f.testConn.Summary()
	if err := f.reports.Upsert(ctx, &domain.PaymentReport{PaymentID: p.ID, PaymentPublicID: p.PublicID, Reference: ref,
		OrderID: p.OrderID, AppID: app.ID, App: app.Summary(), ConnectionID: &f.testConn.ID, Connection: &conn,
		Currency: "IDR", Gross: amount, Status: domain.PaymentStatusPaid, PaidAt: paidAt}); err != nil {
		t.Fatal(err)
	}
	return p
}

func gatewayTx(ref, txID string, amount int64, at time.Time) domain.GatewayTransaction {
	return domain.GatewayTransaction{Reference: ref, ProviderPaymentID: txID, ProviderStatus: "SUCCESS",
		Status: domain.PaymentStatusPaid, Amount: amount, Currency: "IDR", OccurredAt: at}
}

// runWindow is yesterday in WIB, as the console sends it.
func runWindow() (from, to time.Time, body func(conn string) string) {
	wib := time.FixedZone("WIB", 7*3600)
	now := time.Now().In(wib)
	to = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wib)
	from = to.AddDate(0, 0, -1)
	return from, to, func(conn string) string {
		return fmt.Sprintf(`{"connection_id":%q,"from":%q,"to":%q}`, conn, from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
}

// executeRun performs the run's current run count as the worker would.
func (f *apiFixture) executeRun(t *testing.T, publicID string) {
	t.Helper()
	run, err := f.recon.GetRun(context.Background(), publicID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.reconSvc.Execute(context.Background(), run.ID, run.RunCount); err != nil {
		t.Fatal(err)
	}
}

// succeededRun creates a run over yesterday with one missing_in_keipay, one
// missing_in_gateway and one matched item, and executes it.
func (f *apiFixture) succeededRun(t *testing.T, owner *browser) string {
	t.Helper()
	from, _, body := runWindow()
	at := from.Add(3 * time.Hour)
	app := f.reconApp(t)
	f.paidPayment(t, app, "kp_pay_match_"+app.Slug, 5000, at)
	f.paidPayment(t, app, "kp_pay_lost_"+app.Slug, 7000, at)
	f.fake.Pages = []provider.TransactionPage{{Transactions: []domain.GatewayTransaction{
		gatewayTx("kp_pay_match_"+app.Slug, "tx_1", 5000, at), gatewayTx("=cmd|stranger", "tx_2", 1000, at),
	}}}
	run := decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs", body(f.testConn.PublicID)), http.StatusCreated)
	runID := run["run"].(map[string]any)["id"].(string)
	f.executeRun(t, runID)
	return runID
}

func (f *apiFixture) itemsOf(t *testing.T, b *browser, runID, query string) []any {
	t.Helper()
	m := decode(t, f.send(b, http.MethodGet, "/admin/reconciliation-runs/"+runID+"/items"+query, ""), http.StatusOK)
	return m["data"].([]any)
}

// Viewers read reports, runs, items and exports; only owner and admin start,
// re-run and resolve.
func TestReconciliationPermissions(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	runID := f.succeededRun(t, owner)
	items := f.itemsOf(t, owner, runID, "?result=missing_in_gateway")
	itemID := items[0].(map[string]any)["id"].(string)
	_, _, body := runWindow()

	routes := []struct {
		method, path, body string
		write              bool
	}{
		{http.MethodGet, "/admin/payment-reports", "", false},
		{http.MethodGet, "/admin/payment-reports/summary?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z", "", false},
		{http.MethodGet, "/admin/payment-reports/export", "", false},
		{http.MethodGet, "/admin/reconciliation-runs", "", false},
		{http.MethodPost, "/admin/reconciliation-runs", body(f.testConn.PublicID), true},
		{http.MethodGet, "/admin/reconciliation-runs/" + runID, "", false},
		{http.MethodPost, "/admin/reconciliation-runs/" + runID + "/rerun", "", true},
		{http.MethodGet, "/admin/reconciliation-runs/" + runID + "/items", "", false},
		{http.MethodGet, "/admin/reconciliation-runs/" + runID + "/items/export", "", false},
		{http.MethodGet, "/admin/reconciliation-items/" + itemID, "", false},
		{http.MethodPost, "/admin/reconciliation-items/" + itemID + "/resolve", `{"note":"checked"}`, true},
	}
	// The viewer goes first, so its refused writes change nothing the others
	// then do.
	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleAdmin, domain.RoleOwner} {
		_, b := f.as(t, role)
		for _, rt := range routes {
			w := f.send(b, rt.method, rt.path, rt.body)
			if rt.write && role == domain.RoleViewer {
				expectError(t, w, http.StatusForbidden, domain.CodeForbidden)
				continue
			}
			if w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized || w.Code >= 500 {
				t.Fatalf("%s %s %s: %d %s", role, rt.method, rt.path, w.Code, w.Body.String())
			}
		}
	}
	for _, rt := range routes {
		expectError(t, f.send(newBrowser(), rt.method, rt.path, rt.body), http.StatusUnauthorized, domain.CodeSessionRequired)
	}
}

// The run lifecycle over HTTP with the contract's shapes.
func TestReconciliationRunOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	from, to, body := runWindow()
	f.audit.logs = nil

	created := decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs", body(f.testConn.PublicID)), http.StatusCreated)
	run := keysOf(t, "run", created["run"], runKeys...)
	if run["status"] != "queued" || run["run_count"] != float64(1) || run["summary"] != nil || run["error"] != nil ||
		run["from"] != from.UTC().Format(time.RFC3339) || run["to"] != to.UTC().Format(time.RFC3339) || run["started_at"] != nil {
		t.Fatalf("run = %v", run)
	}
	keysOf(t, "created_by", run["created_by"], "id", "name")
	keysOf(t, "connection", run["connection"], "id", "name", "provider", "environment", "status")
	runID := run["id"].(string)
	if len(f.queue.runs) != 1 || f.queue.runs[0].attempt != 1 {
		t.Fatalf("queued = %+v", f.queue.runs)
	}
	if acts := f.audit.actions(); len(acts) != 1 || acts[0] != "reconciliation_run.create" {
		t.Fatalf("audit = %v", acts)
	}
	expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs/"+runID+"/rerun", ""), http.StatusConflict, domain.CodeRunInProgress)

	at := from.Add(time.Hour)
	app := f.reconApp(t)
	f.paidPayment(t, app, "kp_pay_lost", 7000, at)
	f.fake.Pages = []provider.TransactionPage{{Transactions: []domain.GatewayTransaction{gatewayTx("kp_pay_stranger", "tx_9", 1000, at)}}}
	f.executeRun(t, runID)

	got := keysOf(t, "run", decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+strings.ToLower(runID), ""), http.StatusOK)["run"], runKeys...)
	summary := keysOf(t, "summary", got["summary"], "total", "unresolved", "results")
	results := keysOf(t, "results", summary["results"], "matched", "missing_in_keipay", "missing_in_gateway", "amount_mismatch",
		"status_mismatch", "fee_mismatch")
	if got["status"] != "succeeded" || summary["total"] != float64(2) || summary["unresolved"] != float64(2) ||
		results["missing_in_gateway"] != float64(1) || results["missing_in_keipay"] != float64(1) || got["finished_at"] == nil {
		t.Fatalf("run = %v", got)
	}

	items := f.itemsOf(t, owner, runID, "")
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	var lost, stranger map[string]any
	for _, raw := range items {
		it := keysOf(t, "item", raw, itemKeys...)
		if it["reference"] == "kp_pay_lost" {
			lost = it
		} else {
			stranger = it
		}
	}
	keysOf(t, "keipay", lost["keipay"], keipaySideKeys...)
	keysOf(t, "app", lost["keipay"].(map[string]any)["app"], "id", "slug", "name", "environment")
	if lost["gateway"] != nil || lost["resolution"] != nil || lost["result"] != "missing_in_gateway" || lost["run_id"] != runID {
		t.Fatalf("lost = %v", lost)
	}
	keysOf(t, "gateway", stranger["gateway"], gatewaySideKeys...)
	if stranger["keipay"] != nil || stranger["gateway"].(map[string]any)["status"] != "PAID" {
		t.Fatalf("stranger = %v", stranger)
	}

	f.audit.logs = nil
	resolveURL := "/admin/reconciliation-items/" + lost["id"].(string) + "/resolve"
	resolved := decode(t, f.send(owner, http.MethodPost, resolveURL, `{"note":"  Paid by transfer  "}`), http.StatusOK)
	item := keysOf(t, "item", resolved["item"], itemKeys...)
	res := keysOf(t, "resolution", item["resolution"], "note", "resolved_by", "resolved_at")
	if res["note"] != "Paid by transfer" || res["resolved_by"].(map[string]any)["id"] != f.owner.PublicID {
		t.Fatalf("resolution = %v", res)
	}
	if acts := f.audit.actions(); len(acts) != 1 || acts[0] != "reconciliation_item.resolve" {
		t.Fatalf("audit = %v", acts)
	}
	expectError(t, f.send(owner, http.MethodPost, resolveURL, `{"note":"again"}`), http.StatusConflict, domain.CodeConflict)
	if n := len(f.itemsOf(t, owner, runID, "?resolved=true")); n != 1 {
		t.Fatalf("resolved items = %d", n)
	}
	if n := len(f.itemsOf(t, owner, runID, "?resolved=false&result=missing_in_keipay")); n != 1 {
		t.Fatalf("unresolved missing_in_keipay = %d", n)
	}
	got = decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID, ""), http.StatusOK)["run"].(map[string]any)
	if got["summary"].(map[string]any)["unresolved"] != float64(1) {
		t.Fatalf("unresolved after resolve = %v", got["summary"])
	}

	rerun := decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs/"+runID+"/rerun", ""), http.StatusAccepted)
	if r := rerun["run"].(map[string]any); r["status"] != "queued" || r["run_count"] != float64(2) || r["summary"] != nil {
		t.Fatalf("rerun = %v", r)
	}
	expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs/"+runID+"/rerun", ""), http.StatusConflict, domain.CodeRunInProgress)
	expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+stranger["id"].(string)+"/resolve", `{"note":"x"}`),
		http.StatusConflict, domain.CodeRunInProgress)

	list := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs?status=queued&environment=test&connection_id="+f.testConn.PublicID, ""), http.StatusOK)
	if data := list["data"].([]any); len(data) != 1 || list["next_cursor"] != nil {
		t.Fatalf("list = %v", list)
	}
}

// When the queue is down the run comes back failed with not_queued.
func TestReconciliationRunNotQueuedOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	f.queue.err = errors.New("redis down")
	_, _, body := runWindow()
	run := decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs", body(f.testConn.PublicID)), http.StatusCreated)["run"].(map[string]any)
	e := keysOf(t, "error", run["error"], "code", "message")
	if run["status"] != "failed" || e["code"] != "not_queued" || strings.Contains(e["message"].(string), "redis") {
		t.Fatalf("run = %v", run)
	}
}

func TestReconciliationRequestErrors(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	runID := f.succeededRun(t, owner)
	matched := f.itemsOf(t, owner, runID, "?result=matched")[0].(map[string]any)["id"].(string)
	open := f.itemsOf(t, owner, runID, "?result=missing_in_gateway")[0].(map[string]any)["id"].(string)

	env := expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs", `{}`), http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "connection_id", Rule: "required"}, domain.FieldError{Field: "from", Rule: "required"},
		domain.FieldError{Field: "to", Rule: "required"})
	env = expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs", `{"connection_id":5}`), http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "connection_id", Rule: "type"})
	env = expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs",
		`{"connection_id":"`+id.New(id.Connection)+`","from":"2026-10-02T00:00:00+07:00","to":"2026-10-01T00:00:00+07:00"}`),
		http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "connection_id", Rule: "exists"}, domain.FieldError{Field: "to", Rule: "after"})
	expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs", `not json`), http.StatusBadRequest, domain.CodeInvalidRequest)

	for path, field := range map[string]string{
		"/admin/reconciliation-runs/rec_1":                       "id",
		"/admin/reconciliation-runs/" + id.New(id.App):           "id",
		"/admin/reconciliation-runs/12/items":                    "id",
		"/admin/reconciliation-runs?connection_id=con_x":         "connection_id",
		"/admin/payment-reports?app_id=1":                        "app_id",
		"/admin/payment-reports?connection_id=" + id.New(id.App): "connection_id",
	} {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, path, ""), http.StatusBadRequest, domain.CodeInvalidRequest),
			domain.FieldError{Field: field, Rule: "public_id"})
	}
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/rci_1/resolve", `{"note":"x"}`),
		http.StatusBadRequest, domain.CodeInvalidRequest), domain.FieldError{Field: "id", Rule: "public_id"})

	queryCases := map[string]domain.FieldError{
		"/admin/reconciliation-runs?status=done":                           {Field: "status", Rule: "oneof"},
		"/admin/reconciliation-runs?environment=prod":                      {Field: "environment", Rule: "oneof"},
		"/admin/reconciliation-runs/" + runID + "/items?result=lost":       {Field: "result", Rule: "oneof"},
		"/admin/reconciliation-runs/" + runID + "/items?resolved=yes":      {Field: "resolved", Rule: "bool"},
		"/admin/reconciliation-runs/" + runID + "/items?resolved=1":        {Field: "resolved", Rule: "bool"},
		"/admin/reconciliation-runs/" + runID + "/items/export?resolved=x": {Field: "resolved", Rule: "bool"},
		"/admin/reconciliation-runs?limit=0":                               {Field: "limit", Rule: "range"},
	}
	for path, want := range queryCases {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, path, ""), http.StatusBadRequest, domain.CodeInvalidRequest), want)
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs?cursor=nope", ""), http.StatusBadRequest, domain.CodeInvalidCursor)
	expectError(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID+"/items?cursor=nope", ""), http.StatusBadRequest, domain.CodeInvalidCursor)

	unknownRun := id.New(id.Reconciliation)
	for _, path := range []string{"/admin/reconciliation-runs/" + unknownRun, "/admin/reconciliation-runs/" + unknownRun + "/items",
		"/admin/reconciliation-runs/" + unknownRun + "/items/export"} {
		expectError(t, f.send(owner, http.MethodGet, path, ""), http.StatusNotFound, domain.CodeNotFound)
	}
	expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs/"+unknownRun+"/rerun", ""), http.StatusNotFound, domain.CodeNotFound)
	expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+id.New(id.ReconciliationItem)+"/resolve", `{"note":"x"}`),
		http.StatusNotFound, domain.CodeNotFound)

	expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+matched+"/resolve", `{"note":"x"}`),
		http.StatusBadRequest, domain.CodeValidationFailed)
	for body, rule := range map[string]string{`{"note":"   "}`: "required", `{}`: "required",
		`{"note":"` + strings.Repeat("ä", 1001) + `"}`: "max"} {
		expectFields(t, expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+open+"/resolve", body),
			http.StatusBadRequest, domain.CodeInvalidRequest), domain.FieldError{Field: "note", Rule: rule})
	}
	decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+open+"/resolve",
		`{"note":"`+strings.Repeat("ä", 1000)+`"}`), http.StatusOK)
}

// Report list shape and query errors.
func TestPaymentReportsOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.reconApp(t)
	p := f.paidPayment(t, app, "kp_pay_report", 5000, time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC))
	list := decode(t, f.send(owner, http.MethodGet, "/admin/payment-reports", ""), http.StatusOK)
	keysOf(t, "list", list, "data", "next_cursor")
	data := list["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("data = %v", data)
	}
	r := keysOf(t, "report", data[0], reportKeys...)
	if r["payment_id"] != p.PublicID || r["reference"] != "kp_pay_report" || r["provider"] != "fake" || r["fee"] != nil ||
		r["net"] != nil || r["gross"] != float64(5000) || r["status"] != "PAID" || r["paid_at"] != "2026-10-01T03:00:00Z" {
		t.Fatalf("report = %v", r)
	}
	keysOf(t, "connection", r["connection"], "id", "name", "provider", "environment", "status")

	summary := decode(t, f.send(owner, http.MethodGet,
		"/admin/payment-reports/summary?from=2026-10-01T00:00:00%2B07:00&to=2026-10-02T00:00:00%2B07:00&interval=day", ""), http.StatusOK)
	keysOf(t, "summary", summary, "interval", "from", "to", "data")
	if summary["interval"] != "day" || summary["from"] != "2026-09-30T17:00:00Z" || summary["to"] != "2026-10-01T17:00:00Z" {
		t.Fatalf("summary = %v", summary)
	}
	if def := decode(t, f.send(owner, http.MethodGet, "/admin/payment-reports/summary?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z", ""),
		http.StatusOK); def["interval"] != "total" || len(def["data"].([]any)) != 0 {
		t.Fatalf("default summary = %v", def)
	}

	cases := map[string][]domain.FieldError{
		"/admin/payment-reports?environment=prod":                                                        {{Field: "environment", Rule: "oneof"}},
		"/admin/payment-reports?currency=idr":                                                            {{Field: "currency", Rule: "format"}},
		"/admin/payment-reports?currency=RUPIAH":                                                         {{Field: "currency", Rule: "format"}},
		"/admin/payment-reports?from=yesterday":                                                          {{Field: "from", Rule: "datetime"}},
		"/admin/payment-reports?to=2026-10-01":                                                           {{Field: "to", Rule: "datetime"}},
		"/admin/payment-reports?from=2026-10-01T00:00:00Z&to=2026-10-01T00:00:00Z":                       {{Field: "to", Rule: "after"}},
		"/admin/payment-reports/export?from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z":                {{Field: "to", Rule: "after"}},
		"/admin/payment-reports?limit=201":                                                               {{Field: "limit", Rule: "range"}},
		"/admin/payment-reports/summary":                                                                 {{Field: "from", Rule: "required"}, {Field: "to", Rule: "required"}},
		"/admin/payment-reports/summary?from=2026-10-01T00:00:00Z":                                       {{Field: "to", Rule: "required"}},
		"/admin/payment-reports/summary?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&interval=week": {{Field: "interval", Rule: "oneof"}},
		"/admin/payment-reports/summary?from=2026-01-01T00:00:00Z&to=2026-04-04T00:00:00Z&interval=day":  {{Field: "to", Rule: "max_range"}},
		"/admin/payment-reports/summary?from=2025-01-01T00:00:00Z&to=2026-01-03T00:00:00Z":               {{Field: "to", Rule: "max_range"}},
		"/admin/payment-reports/summary?from=2026-01-01T00:00:00Z&to=2026-01-01T00:00:00Z":               {{Field: "to", Rule: "after"}},
	}
	for path, want := range cases {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, path, ""), http.StatusBadRequest, domain.CodeInvalidRequest), want...)
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/payment-reports?cursor=nope", ""), http.StatusBadRequest, domain.CodeInvalidCursor)
	decode(t, f.send(owner, http.MethodGet, "/admin/payment-reports/summary?from=2026-01-01T00:00:00Z&to=2026-04-03T00:00:00Z&interval=day", ""), http.StatusOK)
}

// flushRecorder counts flushes and the body size at each.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushedAt []int
}

func (r *flushRecorder) Flush() {
	r.flushedAt = append(r.flushedAt, r.Body.Len())
	r.ResponseRecorder.Flush()
}

func (f *apiFixture) sendTo(w http.ResponseWriter, b *browser, method, target string) {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range b.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	req.RemoteAddr = b.remote
	f.router.ServeHTTP(w, req)
}

func readCSV(t *testing.T, body []byte) [][]string {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v\n%s", err, body)
	}
	return rows
}

// The report export's headers, columns, WIB times, empty unknowns and formula
// guard.
func TestPaymentReportExport(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	paidAt := time.Date(2026, 10, 1, 3, 15, 0, 0, time.UTC)
	settledAt := paidAt.Add(24 * time.Hour)
	conn := f.testConn.Summary()
	conn.Name = "-x"
	for i, r := range []domain.PaymentReport{
		{PaymentID: 1, PaymentPublicID: "pay_A", Reference: "=cmd", OrderID: "+1", App: domain.AppSummary{PublicID: "app_A", Slug: "@a",
			Environment: domain.EnvTest}, Connection: &conn, Currency: "IDR", Gross: 150000, Fee: i64p(4995), Net: i64p(145005),
			Status: domain.PaymentStatusSettled, PaidAt: paidAt, SettledAt: &settledAt},
		{PaymentID: 2, PaymentPublicID: "pay_B", Reference: "kp_pay_B", OrderID: "\to2", App: domain.AppSummary{PublicID: "app_A", Slug: "acme",
			Environment: domain.EnvLive}, Currency: "IDR", Gross: 1000, Status: domain.PaymentStatusPaid, PaidAt: paidAt},
	} {
		if err := f.reports.Upsert(context.Background(), &r); err != nil {
			t.Fatal(i, err)
		}
	}
	w := f.send(owner, http.MethodGet, "/admin/payment-reports/export?from=2026-10-01T00:00:00%2B07:00", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	h := w.Header()
	if h.Get("Content-Type") != "text/csv; charset=utf-8" || h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Disposition") != `attachment; filename="keipay-payment-reports-20261001-all.csv"` {
		t.Fatalf("headers = %v", h)
	}
	rows := readCSV(t, w.Body.Bytes())
	want := [][]string{
		{"payment_id", "reference", "order_id", "app_id", "app_slug", "app_environment", "connection_id", "connection_name", "provider",
			"currency", "gross", "fee", "net", "status", "paid_at", "settled_at"},
		{"pay_B", "kp_pay_B", "'\to2", "app_A", "acme", "live", "", "", "", "IDR", "1000", "", "", "PAID", "2026-10-01T10:15:00+07:00", ""},
		{"pay_A", "'=cmd", "'+1", "app_A", "'@a", "test", conn.PublicID, "'-x", "fake", "IDR", "150000", "4995", "145005", "SETTLED",
			"2026-10-01T10:15:00+07:00", "2026-10-02T10:15:00+07:00"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q", rows)
	}
	for i := range want {
		if strings.Join(rows[i], "|") != strings.Join(want[i], "|") {
			t.Fatalf("row %d = %q\nwant    %q", i, rows[i], want[i])
		}
	}

	// An empty export still has its header.
	f2 := newAPI(t, "")
	w = f2.send(f2.signIn(t, f2.owner), http.MethodGet, "/admin/payment-reports/export", "")
	if rows := readCSV(t, w.Body.Bytes()); w.Code != http.StatusOK || len(rows) != 1 || rows[0][0] != "payment_id" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "keipay-payment-reports-all-all.csv") {
		t.Fatalf("empty export: %d %q", w.Code, rows)
	}
}

func i64p(v int64) *int64 { return &v }

// A large export is flushed while it streams, not at the end.
func TestPaymentReportExportStreams(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	paidAt := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	const n = 10001
	for i := range n {
		if err := f.reports.Upsert(context.Background(), &domain.PaymentReport{PaymentID: int64(i + 1), PaymentPublicID: fmt.Sprintf("pay_%05d", i),
			Reference: fmt.Sprintf("kp_%05d", i), Currency: "IDR", Gross: int64(i), Status: domain.PaymentStatusPaid, PaidAt: paidAt}); err != nil {
			t.Fatal(err)
		}
	}
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	f.sendTo(w, owner, http.MethodGet, "/admin/payment-reports/export")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if rows := readCSV(t, w.Body.Bytes()); len(rows) != n+1 {
		t.Fatalf("rows = %d", len(rows))
	}
	total := w.Body.Len()
	early := 0
	for _, at := range w.flushedAt {
		if at < total {
			early++
		}
	}
	if len(w.flushedAt) < n/500 || early < n/500 {
		t.Fatalf("flushes at %v (body %d bytes)", w.flushedAt, total)
	}
}

// Invalid parameters get a JSON error before any CSV byte; a failure after
// the first row cuts the response off instead of a 500.
func TestExportErrors(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	w := f.send(owner, http.MethodGet, "/admin/payment-reports/export?currency=idr", "")
	expectError(t, w, http.StatusBadRequest, domain.CodeInvalidRequest)
	if strings.Contains(w.Body.String(), "payment_id") || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("csv written before validation: %s", w.Body.String())
	}

	// More rows than one flush: the failure comes after bytes were sent.
	f.reports.StreamErr = errors.New("connection reset by database")
	for i := range 600 {
		if err := f.reports.Upsert(context.Background(), &domain.PaymentReport{PaymentID: int64(i + 1), PaymentPublicID: fmt.Sprintf("pay_%d", i),
			Currency: "IDR", Gross: 1, Status: domain.PaymentStatusPaid, PaidAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if got := recover(); got != http.ErrAbortHandler {
				t.Fatalf("recovered %v, want http.ErrAbortHandler", got)
			}
		}()
		f.sendTo(rec, owner, http.MethodGet, "/admin/payment-reports/export")
		t.Fatal("the export completed")
	}()
	if body := rec.Body.String(); !strings.HasPrefix(body, "payment_id,") || strings.Contains(body, "internal_error") {
		t.Fatalf("aborted body = %.80q", body)
	}

	// While nothing reached the client, a failure is still a JSON error,
	// with no rows and none of the CSV headers.
	f2 := newAPI(t, "")
	f2.reports.StreamErr = errors.New("query failed")
	if err := f2.reports.Upsert(context.Background(), &domain.PaymentReport{PaymentID: 1, PaymentPublicID: "pay_A", Currency: "IDR",
		Gross: 1, Status: domain.PaymentStatusPaid, PaidAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w = f2.send(f2.signIn(t, f2.owner), http.MethodGet, "/admin/payment-reports/export", "")
	expectError(t, w, http.StatusInternalServerError, domain.CodeInternal)
	if w.Header().Get("Content-Disposition") != "" || strings.Contains(w.Body.String(), "pay_A") {
		t.Fatalf("csv leaked into the error: %v %s", w.Header(), w.Body.String())
	}
}

// The item export's columns, filters and formula guard.
func TestReconciliationItemExport(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	runID := f.succeededRun(t, owner)
	open := f.itemsOf(t, owner, runID, "?result=missing_in_gateway")[0].(map[string]any)["id"].(string)
	decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+open+"/resolve", `{"note":"=HYPERLINK(\"x\")"}`), http.StatusOK)

	w := f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID+"/items/export", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Disposition") != `attachment; filename="keipay-reconciliation-`+runID+`.csv"` {
		t.Fatalf("export: %d %v", w.Code, w.Header())
	}
	rows := readCSV(t, w.Body.Bytes())
	header := []string{"item_id", "reference", "result", "resolved", "resolution_note", "resolved_at", "payment_id", "app_slug",
		"keipay_status", "keipay_amount", "keipay_currency", "keipay_fee", "keipay_paid_at", "gateway_payment_id", "gateway_status",
		"gateway_provider_status", "gateway_amount", "gateway_currency", "gateway_fee", "gateway_occurred_at"}
	if len(rows) != 4 || strings.Join(rows[0], ",") != strings.Join(header, ",") {
		t.Fatalf("rows = %q", rows)
	}
	byResult := map[string][]string{}
	for _, r := range rows[1:] {
		if len(r) != len(header) {
			t.Fatalf("row width %d: %q", len(r), r)
		}
		byResult[r[2]] = r
	}
	lost, stranger, matched := byResult["missing_in_gateway"], byResult["missing_in_keipay"], byResult["matched"]
	if lost[3] != "true" || lost[4] != `'=HYPERLINK("x")` || !strings.HasSuffix(lost[5], "+07:00") || lost[9] != "7000" ||
		lost[13] != "" || lost[16] != "" {
		t.Fatalf("lost = %q", lost)
	}
	if stranger[1] != "'=cmd|stranger" || stranger[3] != "false" || stranger[6] != "" || stranger[14] != "PAID" ||
		stranger[15] != "SUCCESS" || stranger[16] != "1000" || !strings.HasSuffix(stranger[19], "+07:00") {
		t.Fatalf("stranger = %q", stranger)
	}
	if matched[3] != "false" || matched[9] != "5000" || matched[16] != "5000" || matched[11] != "" {
		t.Fatalf("matched = %q", matched)
	}

	w = f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID+"/items/export?resolved=false&result=matched", "")
	if rows := readCSV(t, w.Body.Bytes()); len(rows) != 2 || rows[1][2] != "matched" {
		t.Fatalf("filtered = %q", rows)
	}
}
