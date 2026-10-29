package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// A note with U+0000 sent as a JSON escape is refused on the note field,
// and nothing about the item or the run changes.
func TestResolveRefusesNULNoteOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	runID := f.succeededRun(t, owner)
	open := f.itemsOf(t, owner, runID, "?result=missing_in_gateway")[0].(map[string]any)["id"].(string)
	before := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID, ""), http.StatusOK)["run"].(map[string]any)

	env := expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+open+"/resolve", `{"note":"ok\u0000then"}`),
		http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "note", Rule: "invalid_character"})

	item := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-items/"+open, ""), http.StatusOK)["item"].(map[string]any)
	if item["resolution"] != nil {
		t.Fatalf("item resolved by a refused note: %v", item)
	}
	after := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID, ""), http.StatusOK)["run"].(map[string]any)
	if fmt.Sprint(after["summary"]) != fmt.Sprint(before["summary"]) {
		t.Fatalf("summary changed: %v -> %v", before["summary"], after["summary"])
	}
}

// Resolving lowers summary.unresolved on the run as the API shows it, and a
// non-matched item of every unresolved result can be resolved.
func TestResolveLowersRunSummaryOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	runID := f.succeededRun(t, owner)
	summary := func() map[string]any {
		t.Helper()
		run := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID, ""), http.StatusOK)["run"].(map[string]any)
		return run["summary"].(map[string]any)
	}
	if s := summary(); s["unresolved"] != float64(2) || s["total"] != float64(3) {
		t.Fatalf("summary = %v", s)
	}
	for i, result := range []string{"missing_in_gateway", "missing_in_keipay"} {
		itemID := f.itemsOf(t, owner, runID, "?result="+result)[0].(map[string]any)["id"].(string)
		decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+itemID+"/resolve", `{"note":"checked"}`), http.StatusOK)
		if s := summary(); s["unresolved"] != float64(1-i) || s["total"] != float64(3) {
			t.Fatalf("after resolving %s: summary = %v", result, s)
		}
	}
	if got := f.itemsOf(t, owner, runID, "?resolved=false"); len(got) != 1 || got[0].(map[string]any)["result"] != "matched" {
		t.Fatalf("unresolved items = %v", got)
	}
}

// The run range limits at their edges: exactly 31 days is accepted, one
// second more is max_range, and a start that is not in the past is past.
func TestCreateRunRangeEdgesOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	_, to, _ := runWindow()
	send := func(from, to time.Time) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"connection_id":%q,"from":%q,"to":%q}`, f.testConn.PublicID, from.Format(time.RFC3339), to.Format(time.RFC3339))
		return f.send(owner, http.MethodPost, "/admin/reconciliation-runs", body)
	}

	created := decode(t, send(to.AddDate(0, 0, -31), to), http.StatusCreated)["run"].(map[string]any)
	if created["status"] != "queued" || created["run_count"] != float64(1) {
		t.Fatalf("31-day run = %v", created)
	}
	expectFields(t, expectError(t, send(to.AddDate(0, 0, -31).Add(-time.Second), to), http.StatusBadRequest, domain.CodeInvalidRequest),
		domain.FieldError{Field: "to", Rule: "max_range"})

	future := time.Now().Add(time.Hour)
	expectFields(t, expectError(t, send(future, future.Add(time.Hour)), http.StatusBadRequest, domain.CodeInvalidRequest),
		domain.FieldError{Field: "from", Rule: "past"})
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs",
		fmt.Sprintf(`{"connection_id":%q,"from":"yesterday","to":"2026-10-01"}`, f.testConn.PublicID)),
		http.StatusBadRequest, domain.CodeInvalidRequest),
		domain.FieldError{Field: "from", Rule: "datetime"}, domain.FieldError{Field: "to", Rule: "datetime"})
}

// A failed run is not in progress, so it can be re-run right away; the
// re-run clears the error, and the next attempt can succeed.
func TestRerunFailedRunRightAwayOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	_, _, body := runWindow()
	f.fake.ListErrs = []error{provider.NewStatusError(provider.ErrRejected, 403, "maintenance: key xnd_development_qa lacks scope")}
	runID := decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs", body(f.testConn.PublicID)), http.StatusCreated)["run"].(map[string]any)["id"].(string)
	f.executeRun(t, runID)

	failed := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID, ""), http.StatusOK)["run"].(map[string]any)
	errObj, _ := failed["error"].(map[string]any)
	if failed["status"] != "failed" || errObj == nil || errObj["code"] != "provider_rejected" || failed["summary"] != nil {
		t.Fatalf("failed run = %v", failed)
	}
	if msg, _ := errObj["message"].(string); msg == "" || strings.Contains(msg, "maintenance") || strings.Contains(msg, "xnd_") {
		t.Fatalf("message leaks gateway text or is empty: %q", msg)
	}

	rerun := decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-runs/"+runID+"/rerun", ""), http.StatusAccepted)["run"].(map[string]any)
	if rerun["status"] != "queued" || rerun["run_count"] != float64(2) || rerun["error"] != nil || rerun["finished_at"] != nil {
		t.Fatalf("rerun = %v", rerun)
	}
	f.executeRun(t, runID)
	done := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-runs/"+runID, ""), http.StatusOK)["run"].(map[string]any)
	if done["status"] != "succeeded" || done["error"] != nil || done["summary"] == nil {
		t.Fatalf("second attempt = %v", done)
	}
}
