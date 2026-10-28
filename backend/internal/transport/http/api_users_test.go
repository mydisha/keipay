package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

func stringField(key, value string) zap.Field { return zap.String(key, value) }

func disable(t *testing.T, f *apiFixture, u *domain.User) {
	t.Helper()
	owner := f.signIn(t, f.owner)
	if w := f.send(owner, http.MethodPost, "/admin/users/"+u.PublicID+"/disable", ""); w.Code != http.StatusOK {
		t.Fatalf("disable %s: %d %s", u.Email, w.Code, w.Body.String())
	}
}

type userBody struct {
	User              map[string]any `json:"user"`
	TemporaryPassword *string        `json:"temporary_password"`
}

func decodeUser(t *testing.T, raw []byte) userBody {
	t.Helper()
	var b userBody
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("body %q: %v", raw, err)
	}
	return b
}

func TestUsersCreateAndGet(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)

	w := f.send(owner, http.MethodPost, "/admin/users", `{"email":" Dewi@Example.com","name":"Dewi Ü","role":"viewer"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decodeUser(t, w.Body.Bytes())
	if created.TemporaryPassword == nil || len(*created.TemporaryPassword) != 20 {
		t.Fatalf("temporary password = %v", created.TemporaryPassword)
	}
	u := created.User
	if u["email"] != "dewi@example.com" || u["role"] != "viewer" || u["status"] != "active" || u["must_change_password"] != true ||
		u["last_login_at"] != nil || u["disabled_at"] != nil {
		t.Fatalf("user = %v", u)
	}
	uid := u["id"].(string)

	w = f.send(owner, http.MethodGet, "/admin/users/"+strings.ToLower(uid), "")
	if w.Code != http.StatusOK {
		t.Fatalf("get lower-case id: %d %s", w.Code, w.Body.String())
	}
	got := decodeUser(t, w.Body.Bytes())
	if got.User["id"] != uid || got.TemporaryPassword != nil || strings.Contains(w.Body.String(), *created.TemporaryPassword) {
		t.Fatalf("get = %s", w.Body.String())
	}

	expectError(t, f.send(owner, http.MethodPost, "/admin/users", `{"email":"DEWI@example.com","name":"Again","role":"admin"}`),
		http.StatusConflict, domain.CodeConflict)
	env := expectError(t, f.send(owner, http.MethodPost, "/admin/users", `{"email":"bad","name":"X","role":"boss"}`),
		http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "email", Rule: "email"}, domain.FieldError{Field: "role", Rule: "oneof"})

	for _, bad := range []string{"nope", "usr_123", "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0!"} {
		env := expectError(t, f.send(owner, http.MethodGet, "/admin/users/"+bad, ""), http.StatusBadRequest, domain.CodeInvalidRequest)
		expectFields(t, env, domain.FieldError{Field: "id", Rule: "public_id"})
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/users/"+id.New(id.User), ""), http.StatusNotFound, domain.CodeNotFound)
	expectError(t, f.send(owner, http.MethodPost, "/admin/users/"+id.New(id.User)+"/enable", ""), http.StatusNotFound, domain.CodeNotFound)
}

func TestUsersUpdateRules(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	other := f.addUser(t, "other@example.com", domain.RoleAdmin)
	self := "/admin/users/" + f.owner.PublicID

	env := expectError(t, f.send(owner, http.MethodPatch, self, `{}`), http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "body", Rule: "required"})
	env = expectError(t, f.send(owner, http.MethodPatch, self, `{"unknown":1}`), http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "body", Rule: "required"})
	env = expectError(t, f.send(owner, http.MethodPatch, self, `{"role":7}`), http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "role", Rule: "type"})
	expectError(t, f.send(owner, http.MethodPatch, self, `{"role":"admin"}`), http.StatusConflict, domain.CodeLastOwner)
	expectError(t, f.send(owner, http.MethodPatch, "/admin/users/"+other.PublicID, `{"email":"OWNER@example.com"}`), http.StatusConflict, domain.CodeConflict)

	w := f.send(owner, http.MethodPatch, "/admin/users/"+other.PublicID, `{"name":"Renamed","role":"owner"}`)
	if w.Code != http.StatusOK || decodeUser(t, w.Body.Bytes()).User["role"] != "owner" {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	// With a second owner, demoting yourself is allowed.
	if w := f.send(owner, http.MethodPatch, self, `{"role":"admin"}`); w.Code != http.StatusOK {
		t.Fatalf("self demote: %d %s", w.Code, w.Body.String())
	}
}

func TestUsersSelfProtection(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	self := "/admin/users/" + f.owner.PublicID
	expectError(t, f.send(owner, http.MethodPost, self+"/disable", ""), http.StatusConflict, domain.CodeCannotModifySelf)
	expectError(t, f.send(owner, http.MethodPost, strings.ToLower(self)+"/reset-password", ""), http.StatusConflict, domain.CodeCannotModifySelf)

	u := f.addUser(t, "ops@example.com", domain.RoleAdmin)
	w := f.send(owner, http.MethodPost, "/admin/users/"+u.PublicID+"/disable", "")
	if body := decodeUser(t, w.Body.Bytes()); w.Code != http.StatusOK || body.User["status"] != "disabled" || body.User["disabled_at"] == nil {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	w = f.send(owner, http.MethodPost, "/admin/users/"+u.PublicID+"/enable", "")
	if body := decodeUser(t, w.Body.Bytes()); w.Code != http.StatusOK || body.User["status"] != "active" || body.User["disabled_at"] != nil {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	w = f.send(owner, http.MethodPost, "/admin/users/"+u.PublicID+"/reset-password", "")
	if body := decodeUser(t, w.Body.Bytes()); w.Code != http.StatusOK || body.TemporaryPassword == nil || body.User["must_change_password"] != true {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
}

func TestUsersListPaging(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	f.addUser(t, "a@example.com", domain.RoleAdmin)
	f.addUser(t, "b@example.com", domain.RoleViewer)

	var seen []string
	cursor := ""
	for range 5 {
		target := "/admin/users?limit=2"
		if cursor != "" {
			target += "&cursor=" + cursor
		}
		w := f.send(owner, http.MethodGet, target, "")
		var page struct {
			Data       []map[string]any `json:"data"`
			NextCursor *string          `json:"next_cursor"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &page) != nil {
			t.Fatalf("list: %d %s", w.Code, w.Body.String())
		}
		for _, u := range page.Data {
			seen = append(seen, u["id"].(string))
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("listed %v", seen)
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/users?cursor=nope", ""), http.StatusBadRequest, domain.CodeInvalidCursor)
	env := expectError(t, f.send(owner, http.MethodGet, "/admin/users?limit=500", ""), http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "limit", Rule: "range"})
}
