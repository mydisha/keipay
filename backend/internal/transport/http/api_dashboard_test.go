package http

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

var dashboardKeys = []string{"environment", "range", "from", "to", "previous_from", "payments", "deliveries", "inbound",
	"connections", "reconciliation", "recent_payments"}

func TestDashboardPermissions(t *testing.T) {
	f := newAPI(t, "")
	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleAdmin, domain.RoleOwner} {
		_, b := f.as(t, role)
		if w := f.send(b, http.MethodGet, "/admin/dashboard", ""); w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", role, w.Code, w.Body.String())
		}
	}
	expectError(t, f.send(nil, http.MethodGet, "/admin/dashboard", ""), http.StatusUnauthorized, domain.CodeSessionRequired)
}

func TestDashboardQueryErrors(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	cases := map[string]domain.FieldError{
		"/admin/dashboard?range=3d":         {Field: "range", Rule: "oneof"},
		"/admin/dashboard?range=7D":         {Field: "range", Rule: "oneof"},
		"/admin/dashboard?environment=prod": {Field: "environment", Rule: "oneof"},
	}
	for path, want := range cases {
		env := expectError(t, f.send(owner, http.MethodGet, path, ""), http.StatusBadRequest, domain.CodeInvalidRequest)
		expectFields(t, env, want)
	}
}

// The response shape: fixed window, zero-filled series, environment-scoped
// connections, public ids only and [] for empty lists.
func TestDashboardOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	f.dashNow = func() time.Time { return time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC) }
	owner := f.signIn(t, f.owner)

	raw := decodeRaw(t, f.send(owner, http.MethodGet, "/admin/dashboard", ""), http.StatusOK)
	for _, want := range []string{`"by_currency":[]`, `"recent_payments":[]`, `"latest_run":null`, `"success_rate":null`, `"gross":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("body %s does not contain %s", raw, want)
		}
	}
	body := decode(t, f.send(owner, http.MethodGet, "/admin/dashboard", ""), http.StatusOK)
	keysOf(t, "dashboard", body, dashboardKeys...)
	if body["environment"] != nil || body["range"] != "7d" || body["from"] != "2026-10-22T17:00:00Z" ||
		body["to"] != "2026-10-29T17:00:00Z" || body["previous_from"] != "2026-10-15T17:00:00Z" {
		t.Fatalf("window = %v %v %v %v %v", body["environment"], body["range"], body["from"], body["to"], body["previous_from"])
	}
	payments := keysOf(t, "payments", body["payments"], "totals", "by_currency", "series")
	periods := keysOf(t, "totals", payments["totals"], "current", "previous")
	counts := keysOf(t, "totals.current", periods["current"], "created", "pending", "paid", "settled", "expired", "failed",
		"successful", "finished", "success_rate")
	if counts["created"] != float64(0) || counts["success_rate"] != nil {
		t.Fatalf("empty totals = %v", counts)
	}
	series := payments["series"].([]any)
	if len(series) != 7 {
		t.Fatalf("series has %d points", len(series))
	}
	point := keysOf(t, "point", series[0], "period_start", "period_end", "created", "successful", "gross")
	if point["period_start"] != "2026-10-22T17:00:00Z" || point["period_end"] != "2026-10-23T17:00:00Z" {
		t.Fatalf("point = %v", point)
	}
	keysOf(t, "deliveries", body["deliveries"], "total", "pending", "delivered", "failed", "finished", "success_rate")
	inbound := keysOf(t, "inbound", body["inbound"], "total", "applied", "problems", "by_result")
	keysOf(t, "by_result", inbound["by_result"], "applied", "stale", "duplicate", "mismatch", "unmapped", "invalid_signature",
		"invalid_payload", "error")
	keysOf(t, "reconciliation", body["reconciliation"], "unresolved_items", "latest_run")
	// The fixture has one fake connection per environment.
	if conns := body["connections"].([]any); len(conns) != 2 {
		t.Fatalf("connections = %v", conns)
	}

	body = decode(t, f.send(owner, http.MethodGet, "/admin/dashboard?range=30d&environment=test", ""), http.StatusOK)
	if body["environment"] != "test" || body["range"] != "30d" || len(body["payments"].(map[string]any)["series"].([]any)) != 30 {
		t.Fatalf("30d test = %v", body)
	}
	conns := body["connections"].([]any)
	if len(conns) != 1 {
		t.Fatalf("test connections = %v", conns)
	}
	conn := keysOf(t, "connection", conns[0], "id", "name", "provider", "environment", "status", "legacy_webhook_alias",
		"last_tested_at", "last_test_status", "created_at", "updated_at")
	if conn["id"] != f.testConn.PublicID || conn["environment"] != "test" {
		t.Fatalf("connection = %v", conn)
	}
}

// Canned counts, a recent payment and the latest run come through with the
// documented keys and only public ids.
func TestDashboardData(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	ctx := context.Background()
	app := f.reconApp(t)
	p := f.paidPayment(t, app, "kp_pay_dash", 5000, time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC))
	f.dash.Totals = []domain.DashboardPaymentTotalsRow{
		{Current: true, Currency: "IDR", Totals: domain.DashboardPaymentTotals{Created: 3, Paid: 2, Failed: 1, Gross: 5000}},
	}
	f.dash.Deliveries = domain.DashboardDeliveryCounts{Total: 2, Delivered: 1, Failed: 1}
	f.dash.Inbound = map[domain.InboundResult]int64{domain.InboundApplied: 2, domain.InboundError: 1}
	f.dash.Unresolved = 3

	body := decode(t, f.send(owner, http.MethodGet, "/admin/dashboard?environment=test", ""), http.StatusOK)
	by := body["payments"].(map[string]any)["by_currency"].([]any)
	if len(by) != 1 {
		t.Fatalf("by_currency = %v", by)
	}
	cur := keysOf(t, "currency", by[0], "currency", "current", "previous")
	totals := keysOf(t, "current", cur["current"], "created", "pending", "paid", "settled", "expired", "failed", "successful",
		"finished", "gross", "success_rate")
	if cur["currency"] != "IDR" || totals["successful"] != float64(2) || totals["gross"] != float64(5000) || totals["success_rate"] != float64(2)/3 {
		t.Fatalf("current = %v", totals)
	}
	if prev := cur["previous"].(map[string]any); prev["created"] != float64(0) || prev["success_rate"] != nil {
		t.Fatalf("previous = %v", prev)
	}
	all := body["payments"].(map[string]any)["totals"].(map[string]any)["current"].(map[string]any)
	if all["successful"] != totals["successful"] || all["finished"] != totals["finished"] || all["success_rate"] != totals["success_rate"] {
		t.Fatalf("totals.current = %v, IDR current = %v", all, totals)
	}
	if d := body["deliveries"].(map[string]any); d["success_rate"] != 0.5 || d["total"] != float64(2) || d["finished"] != float64(2) {
		t.Fatalf("deliveries = %v", d)
	}
	in := body["inbound"].(map[string]any)
	if in["total"] != float64(3) || in["applied"] != float64(2) || in["problems"] != float64(1) ||
		in["by_result"].(map[string]any)["error"] != float64(1) {
		t.Fatalf("inbound = %v", in)
	}
	recent := body["recent_payments"].([]any)
	if len(recent) != 1 {
		t.Fatalf("recent = %v", recent)
	}
	if r := recent[0].(map[string]any); r["id"] != p.PublicID || !strings.HasPrefix(r["id"].(string), "pay_") {
		t.Fatalf("recent[0] = %v", r)
	}
	recon := body["reconciliation"].(map[string]any)
	if recon["unresolved_items"] != float64(3) || recon["latest_run"] != nil {
		t.Fatalf("reconciliation = %v", recon)
	}

	run := &domain.ReconciliationRun{PublicID: id.New(id.Reconciliation), ConnectionID: f.testConn.ID,
		From: time.Date(2026, 10, 27, 17, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 28, 17, 0, 0, 0, time.UTC),
		Status: domain.RunQueued, RunCount: 1}
	if err := f.recon.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	body = decode(t, f.send(owner, http.MethodGet, "/admin/dashboard?environment=test", ""), http.StatusOK)
	latest := keysOf(t, "latest_run", body["reconciliation"].(map[string]any)["latest_run"], runKeys...)
	if latest["id"] != run.PublicID || latest["status"] != "queued" {
		t.Fatalf("latest_run = %v", latest)
	}
	if live := decode(t, f.send(owner, http.MethodGet, "/admin/dashboard?environment=live", ""), http.StatusOK); live["reconciliation"].(map[string]any)["latest_run"] != nil ||
		len(live["recent_payments"].([]any)) != 0 {
		t.Fatalf("live = %v", live)
	}
	raw := decodeRaw(t, f.send(owner, http.MethodGet, "/admin/dashboard", ""), http.StatusOK)
	if strings.Contains(string(raw), `"payment_id"`) || strings.Contains(string(raw), `"created_by_id"`) {
		t.Fatalf("internal fields exposed: %s", raw)
	}
}
