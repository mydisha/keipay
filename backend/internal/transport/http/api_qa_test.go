package http

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// An email that differs only in case or Unicode normalization is the same
// account, for creating, renaming and signing in.
func TestEmailVariantsAreOneAccountOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)

	w := f.send(owner, http.MethodPost, "/admin/users", `{"email":"üser@example.com","name":"Üser","role":"viewer"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	temporary := *decodeUser(t, w.Body.Bytes()).TemporaryPassword

	nfdUpper := norm.NFD.String("ÜSER@EXAMPLE.COM")
	if nfdUpper == "ÜSER@EXAMPLE.COM" {
		t.Fatal("test input is not decomposed")
	}
	for _, variant := range []string{"Üser@Example.com", nfdUpper, "  üser@example.com  "} {
		w = f.send(owner, http.MethodPost, "/admin/users", `{"email":"`+variant+`","name":"Dup","role":"viewer"}`)
		expectError(t, w, http.StatusConflict, domain.CodeConflict)
	}

	other := f.addUser(t, "other@example.com", domain.RoleViewer)
	w = f.send(owner, http.MethodPatch, "/admin/users/"+other.PublicID, `{"email":"`+nfdUpper+`"}`)
	expectError(t, w, http.StatusConflict, domain.CodeConflict)
	if got := f.users.Get(other.PublicID).Email; got != "other@example.com" {
		t.Fatalf("email changed to %q after a conflict", got)
	}

	b := newBrowser()
	w = f.send(b, http.MethodPost, "/admin/login", `{"email":"`+nfdUpper+`","password":"`+temporary+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("sign in with NFD upper-case email: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"email":"üser@example.com"`) {
		t.Fatalf("signed in as %s", w.Body.String())
	}
}

// Logout without a session (or with a dead one) is a quiet 204 that writes no
// audit row and still clears the cookie.
func TestLogoutWithoutSessionWritesNoAudit(t *testing.T) {
	f := newAPI(t, "")
	before := len(f.audit.actions())

	w := f.send(newBrowser(), http.MethodPost, "/admin/logout", "")
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("anonymous logout: %d %q", w.Code, w.Body.String())
	}

	dead := newBrowser()
	dead.cookies["keipay_session"] = "not-a-session"
	w = f.send(dead, http.MethodPost, "/admin/logout", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout with a dead cookie: %d %s", w.Code, w.Body.String())
	}
	if _, ok := dead.cookies["keipay_session"]; ok {
		t.Fatal("dead session cookie was not cleared")
	}
	if got := f.audit.actions()[before:]; len(got) != 0 {
		t.Fatalf("audit rows written: %v", got)
	}

	// A real logout writes exactly one row.
	owner := f.signIn(t, f.owner)
	before = len(f.audit.actions())
	if w := f.send(owner, http.MethodPost, "/admin/logout", ""); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if got := f.audit.actions()[before:]; !slices.Equal(got, []string{"user.logout"}) {
		t.Fatalf("audit rows = %v, want one user.logout", got)
	}
}

// The password gate applies to permission-free routes too; only the
// allow-list passes, and logout ends the pending user's session.
func TestPendingPasswordChangeCanSignOut(t *testing.T) {
	f := newAPI(t, "")
	u := f.addUser(t, "pending@example.com", domain.RoleOwner)
	u.MustChangePassword = true
	if err := f.users.Update(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	b := f.signIn(t, u)
	expectError(t, f.send(b, http.MethodGet, "/admin/users", ""), http.StatusForbidden, domain.CodePasswordChangeRequired)
	session := b.cookies["keipay_session"]

	if w := f.send(b, http.MethodPost, "/admin/logout", ""); w.Code != http.StatusNoContent {
		t.Fatalf("logout while a change is pending: %d %s", w.Code, w.Body.String())
	}
	stale := newBrowser()
	stale.cookies["keipay_session"] = session
	expectError(t, f.send(stale, http.MethodGet, "/admin/me", ""), http.StatusUnauthorized, domain.CodeSessionExpired)
}
