package http

import (
	"net/http"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

// GET /admin/reconciliation-items/{id} (operationId getReconciliationItem,
// x-keipay-permission payments:read): one item by id whatever the run's
// filters, so the console's ?item=rci_… deep link opens directly.
func TestGetReconciliationItemOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	runID := f.succeededRun(t, owner)
	listed := f.itemsOf(t, owner, runID, "?result=missing_in_gateway")[0].(map[string]any)
	itemID := listed["id"].(string)

	// Every role with payments:read can read it, including a viewer.
	for _, role := range []domain.Role{domain.RoleViewer, domain.RoleAdmin, domain.RoleOwner} {
		_, b := f.as(t, role)
		got := decode(t, f.send(b, http.MethodGet, "/admin/reconciliation-items/"+itemID, ""), http.StatusOK)
		item := keysOf(t, "item", got["item"], itemKeys...)
		if item["id"] != itemID || item["run_id"] != runID || item["result"] != "missing_in_gateway" ||
			item["reference"] != listed["reference"] {
			t.Fatalf("%s: item = %v, listed %v", role, item, listed)
		}
	}

	// A resolution shows up on the single item too.
	decode(t, f.send(owner, http.MethodPost, "/admin/reconciliation-items/"+itemID+"/resolve", `{"note":"checked"}`), http.StatusOK)
	got := decode(t, f.send(owner, http.MethodGet, "/admin/reconciliation-items/"+itemID, ""), http.StatusOK)
	res, ok := got["item"].(map[string]any)["resolution"].(map[string]any)
	if !ok || res["note"] != "checked" {
		t.Fatalf("resolution = %v", got["item"])
	}

	expectError(t, f.send(owner, http.MethodGet, "/admin/reconciliation-items/"+id.New(id.ReconciliationItem), ""),
		http.StatusNotFound, domain.CodeNotFound)
	for _, bad := range []string{"rci_1", id.New(id.Reconciliation)} {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/reconciliation-items/"+bad, ""),
			http.StatusBadRequest, domain.CodeInvalidRequest), domain.FieldError{Field: "id", Rule: "public_id"})
	}
	expectError(t, f.send(newBrowser(), http.MethodGet, "/admin/reconciliation-items/"+itemID, ""),
		http.StatusUnauthorized, domain.CodeSessionRequired)
}
