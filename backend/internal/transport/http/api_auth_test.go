package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

func withSetupToken(token string) apiOption {
	return func(cfg *service.AuthConfig, _ *Deps) { cfg.SetupToken = token }
}

func withRequireSetupToken() apiOption {
	return func(cfg *service.AuthConfig, _ *Deps) { cfg.RequireSetupToken = true }
}

func withSecureCookies() apiOption {
	return func(_ *service.AuthConfig, d *Deps) { d.Cookies = middleware.CookieConfig{Secure: true} }
}

func withTrustedProxies(p ...string) apiOption {
	return func(_ *service.AuthConfig, d *Deps) { d.TrustedProxies = p }
}

// setCookies returns the raw Set-Cookie values by cookie name.
func setCookies(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, v := range w.Header().Values("Set-Cookie") {
		name, _, _ := strings.Cut(v, "=")
		if _, dup := out[name]; dup {
			t.Fatalf("cookie %s set twice: %v", name, w.Header().Values("Set-Cookie"))
		}
		out[name] = v
	}
	return out
}

func meBody(t *testing.T, w *httptest.ResponseRecorder) (map[string]any, []string) {
	t.Helper()
	var body struct {
		User        map[string]any `json:"user"`
		Permissions []string       `json:"permissions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return body.User, body.Permissions
}

func TestSetupOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	f.users.Remove(f.owner.PublicID)
	b := &browser{cookies: map[string]string{}, remote: "192.0.2.10:5000"}

	w := f.send(b, http.MethodGet, "/admin/setup", "")
	if w.Code != http.StatusOK || w.Body.String() != `{"setup_required":true,"setup_token_required":false}` {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if b.cookies[middleware.CSRFCookie] == "" {
		t.Fatal("GET /admin/setup did not issue a csrf cookie")
	}

	w = f.send(b, http.MethodPost, "/admin/setup", `{"email":"Owner@Example.com","name":"Ops Lead","password":"`+testPassword+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	user, perms := meBody(t, w)
	if user["email"] != "owner@example.com" || user["role"] != "owner" || user["status"] != "active" || len(perms) != 10 {
		t.Fatalf("me = %v %v", user, perms)
	}
	cookies := setCookies(t, w)
	if cookies[middleware.SessionCookie] == "" || cookies[middleware.CSRFCookie] == "" {
		t.Fatalf("setup cookies = %v", cookies)
	}
	if w := f.send(b, http.MethodGet, "/admin/me", ""); w.Code != http.StatusOK {
		t.Fatalf("me after setup: %d", w.Code)
	}
	if w := f.send(b, http.MethodGet, "/admin/setup", ""); w.Body.String() != `{"setup_required":false,"setup_token_required":false}` {
		t.Fatalf("status after setup: %s", w.Body.String())
	}
	expectError(t, f.send(newBrowser(), http.MethodPost, "/admin/setup", `{"email":"x@example.com","name":"X","password":"`+testPassword+`"}`),
		http.StatusConflict, domain.CodeSetupCompleted)
	if got := f.audit.actions(); !slices.Equal(got, []string{"user.setup"}) {
		t.Fatalf("entries = %v", got)
	}
	if f.audit.logs[0].Actor.Type != domain.ActorSystem || f.audit.logs[0].Actor.ID != "setup" {
		t.Fatalf("setup actor = %+v", f.audit.logs[0].Actor)
	}
}

func TestSetupValidationAndToken(t *testing.T) {
	f := newAPI(t, "", withSetupToken("let-me-in"))
	f.users.Remove(f.owner.PublicID)

	w := f.send(newBrowser(), http.MethodGet, "/admin/setup", "")
	if w.Body.String() != `{"setup_required":true,"setup_token_required":true}` {
		t.Fatalf("status: %s", w.Body.String())
	}
	env := expectError(t, f.send(newBrowser(), http.MethodPost, "/admin/setup", `{"email":"bad","name":" ","password":"short"}`),
		http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env,
		domain.FieldError{Field: "email", Rule: "email"},
		domain.FieldError{Field: "name", Rule: "required"},
		domain.FieldError{Field: "password", Rule: "min"})
	env = expectError(t, f.send(newBrowser(), http.MethodPost, "/admin/setup", `{}`), http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env,
		domain.FieldError{Field: "email", Rule: "required"},
		domain.FieldError{Field: "name", Rule: "required"},
		domain.FieldError{Field: "password", Rule: "required"})

	body := `{"email":"owner@example.com","name":"Owner","password":"` + testPassword + `","setup_token":"nope"}`
	expectError(t, f.send(newBrowser(), http.MethodPost, "/admin/setup", body), http.StatusForbidden, domain.CodeInvalidSetupToken)
	if f.failures.Value("ip:192.0.2.10") != 1 {
		t.Fatal("wrong setup token not counted for the IP")
	}
	body = strings.Replace(body, `"nope"`, `"let-me-in"`, 1)
	if w := f.send(newBrowser(), http.MethodPost, "/admin/setup", body); w.Code != http.StatusCreated {
		t.Fatalf("right token: %d %s", w.Code, w.Body.String())
	}
}

// A production server without SETUP_TOKEN answers setup with a 503 that
// says what to configure, and creates nobody.
func TestSetupWithoutConfiguredTokenOverHTTP(t *testing.T) {
	f := newAPI(t, "", withRequireSetupToken())
	f.users.Remove(f.owner.PublicID)
	body := `{"email":"owner@example.com","name":"Owner","password":"` + testPassword + `"}`
	env := expectError(t, f.send(newBrowser(), http.MethodPost, "/admin/setup", body), http.StatusServiceUnavailable, domain.CodeSetupTokenNotConfigured)
	if !strings.Contains(env.Error.Message, "SETUP_TOKEN") {
		t.Fatalf("message = %q", env.Error.Message)
	}
	if n, _ := f.users.Count(context.Background()); n != 0 {
		t.Fatal("a user was created")
	}
}

func TestLoginOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	u := f.addUser(t, "üser@example.com", domain.RoleViewer)
	b := newBrowser()
	w := f.send(b, http.MethodPost, "/admin/login", `{"email":"  ÜSER@Example.COM ","password":"`+testPassword+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	user, perms := meBody(t, w)
	if user["id"] != u.PublicID || !slices.Equal(perms, []string{"apps:read", "connections:read", "deliveries:read", "payments:read"}) {
		t.Fatalf("me = %v %v", user, perms)
	}
	if user["last_login_at"] == nil {
		t.Fatal("last_login_at not set")
	}
	if got := f.audit.actions(); !slices.Equal(got, []string{"user.login"}) || f.audit.logs[0].Actor.ID != u.PublicID {
		t.Fatalf("entries = %v %+v", got, f.audit.logs)
	}
}

func TestLoginFailuresShareOneBody(t *testing.T) {
	f := newAPI(t, "")
	gone := f.addUser(t, "gone@example.com", domain.RoleAdmin)
	f.signIn(t, f.owner)
	disable(t, f, gone)

	var bodies []string
	for _, body := range []string{
		`{"email":"owner@example.com","password":"wrong-password"}`,
		`{"email":"nobody@example.com","password":"` + testPassword + `"}`,
		`{"email":"gone@example.com","password":"` + testPassword + `"}`,
	} {
		w := f.send(newBrowser(), http.MethodPost, "/admin/login", body)
		expectError(t, w, http.StatusUnauthorized, domain.CodeInvalidCredentials)
		bodies = append(bodies, w.Body.String())
	}
	if bodies[0] != bodies[1] || bodies[1] != bodies[2] {
		t.Fatalf("bodies differ: %q", bodies)
	}
	env := expectError(t, f.send(newBrowser(), http.MethodPost, "/admin/login", `{"email":"owner@example.com"}`), 400, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "password", Rule: "required"})
}

func TestLoginRateLimitOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	for range 5 {
		f.send(newBrowser(), http.MethodPost, "/admin/login", `{"email":"owner@example.com","password":"wrong-password"}`)
	}
	f.audit.logs = nil
	w := f.send(newBrowser(), http.MethodPost, "/admin/login", `{"email":"owner@example.com","password":"`+testPassword+`"}`)
	env := expectError(t, w, http.StatusTooManyRequests, domain.CodeRateLimited)
	secs, ok := env.Error.Details["retry_after_seconds"].(float64)
	if !ok || secs < 1 || secs > 900 {
		t.Fatalf("retry_after_seconds = %v", env.Error.Details["retry_after_seconds"])
	}
	if w.Header().Get("Retry-After") != strconv.Itoa(int(secs)) {
		t.Fatalf("Retry-After = %q, details = %v", w.Header().Get("Retry-After"), secs)
	}
	if len(f.audit.logs) != 0 {
		t.Fatalf("rate-limited attempt audited: %v", f.audit.actions())
	}
}

func TestClientIPAndTrustedProxies(t *testing.T) {
	cases := []struct {
		name    string
		opts    []apiOption
		wantKey string
	}{
		{"no trusted proxy", nil, "ip:192.0.2.10"},
		{"trusted proxy", []apiOption{withTrustedProxies("192.0.2.0/24")}, "ip:198.51.100.77"},
		{"other proxy", []apiOption{withTrustedProxies("10.0.0.1")}, "ip:192.0.2.10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPI(t, "", tc.opts...)
			f.send(newBrowser(), http.MethodPost, "/admin/login", `{"email":"owner@example.com","password":"wrong-password"}`,
				"X-Forwarded-For", "198.51.100.77")
			if f.failures.Value(tc.wantKey) != 1 {
				t.Fatalf("failure not counted under %s", tc.wantKey)
			}
			wantIP := strings.TrimPrefix(tc.wantKey, "ip:")
			if len(f.audit.logs) != 1 || f.audit.logs[0].Actor.IP != wantIP {
				t.Fatalf("audit = %+v, want ip %s", f.audit.logs, wantIP)
			}
		})
	}
}

func TestCookieAttributes(t *testing.T) {
	for _, secure := range []bool{false, true} {
		var opts []apiOption
		if secure {
			opts = append(opts, withSecureCookies())
		}
		f := newAPI(t, "", opts...)
		f.users.Remove(f.owner.PublicID)
		check := func(label string, w *httptest.ResponseRecorder, wantSession, wantCSRF bool) {
			clearing := label == "logout"
			t.Helper()
			cookies := setCookies(t, w)
			for name, want := range map[string]bool{middleware.SessionCookie: wantSession, middleware.CSRFCookie: wantCSRF} {
				v, ok := cookies[name]
				if ok != want {
					t.Fatalf("%s: %s present=%v, want %v (%v)", label, name, ok, want, cookies)
				}
				if !ok {
					continue
				}
				attrs := strings.Split(v, "; ")
				if !slices.Contains(attrs, "Path=/") || !slices.Contains(attrs, "SameSite=Lax") ||
					slices.Contains(attrs, "Secure") != secure || slices.ContainsFunc(attrs, func(a string) bool { return strings.HasPrefix(a, "Expires=") }) {
					t.Fatalf("%s: %s = %q (secure=%v)", label, name, v, secure)
				}
				if slices.Contains(attrs, "HttpOnly") != (name == middleware.SessionCookie) {
					t.Fatalf("%s: %s HttpOnly wrong: %q", label, name, v)
				}
				if name == middleware.SessionCookie && strings.Contains(v, "Max-Age") != clearing {
					t.Fatalf("%s: session cookie has a lifetime: %q", label, v)
				}
			}
		}

		b := &browser{cookies: map[string]string{}, remote: "192.0.2.10:5000"}
		w := f.send(b, http.MethodGet, "/admin/setup", "")
		check("first GET", w, false, true)
		if w := f.send(b, http.MethodGet, "/admin/setup", ""); len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("csrf cookie re-issued: %v", w.Header().Values("Set-Cookie"))
		}
		w = f.send(b, http.MethodPost, "/admin/setup", `{"email":"owner@example.com","name":"Owner","password":"`+testPassword+`"}`)
		check("setup", w, true, true)
		assertNoTokensInBody(t, w, b)

		w = f.send(b, http.MethodPost, "/admin/me/password", `{"current_password":"`+testPassword+`","new_password":"another-pass-12"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("change password: %d %s", w.Code, w.Body.String())
		}
		check("change password", w, true, true)
		assertNoTokensInBody(t, w, b)

		login := newBrowser()
		w = f.send(login, http.MethodPost, "/admin/login", `{"email":"owner@example.com","password":"another-pass-12"}`)
		check("login", w, true, true)
		assertNoTokensInBody(t, w, login)

		oldCSRF := login.cookies[middleware.CSRFCookie]
		w = f.send(login, http.MethodPost, "/admin/logout", "")
		if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
			t.Fatalf("logout: %d %q", w.Code, w.Body.String())
		}
		check("logout", w, true, true)
		if v := setCookies(t, w)[middleware.SessionCookie]; !strings.HasPrefix(v, "keipay_session=;") || !strings.Contains(v, "Max-Age=0") {
			t.Fatalf("logout session cookie = %q", v)
		}
		if login.cookies[middleware.CSRFCookie] == oldCSRF || login.cookies[middleware.CSRFCookie] == "" {
			t.Fatal("logout did not set a fresh csrf cookie")
		}
	}
}

func assertNoTokensInBody(t *testing.T, w *httptest.ResponseRecorder, b *browser) {
	t.Helper()
	for name, v := range b.cookies {
		if v != "" && strings.Contains(w.Body.String(), v) {
			t.Fatalf("response body contains the %s token", name)
		}
	}
}

func TestSessionLifecycleOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	b := f.signIn(t, f.owner)
	oldSession := b.cookies[middleware.SessionCookie]

	if w := f.send(b, http.MethodPost, "/admin/logout", ""); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if f.sessions.Has(oldSession) {
		t.Fatal("session survived logout")
	}
	// The old cookie value no longer works.
	stale := &browser{cookies: map[string]string{middleware.SessionCookie: oldSession}, remote: "192.0.2.10:5000"}
	w := f.send(stale, http.MethodGet, "/admin/me", "")
	expectError(t, w, http.StatusUnauthorized, domain.CodeSessionExpired)
	if v := setCookies(t, w)[middleware.SessionCookie]; !strings.Contains(v, "Max-Age=0") {
		t.Fatalf("expired session cookie not cleared: %q", v)
	}
	// Logout without any session is still 204.
	if w := f.send(newBrowser(), http.MethodPost, "/admin/logout", ""); w.Code != http.StatusNoContent {
		t.Fatalf("anonymous logout: %d", w.Code)
	}
	if got := f.audit.actions(); !slices.Equal(got, []string{"user.login", "user.logout"}) {
		t.Fatalf("entries = %v", got)
	}

	expectError(t, f.send(nil, http.MethodGet, "/admin/me", ""), http.StatusUnauthorized, domain.CodeSessionRequired)
	expectError(t, f.send(nil, http.MethodGet, "/admin/me", "", "Authorization", "Bearer x"), http.StatusUnauthorized, domain.CodeSessionRequired)
	garbage := &browser{cookies: map[string]string{middleware.SessionCookie: "garbage"}, remote: "192.0.2.10:5000"}
	expectError(t, f.send(garbage, http.MethodGet, "/admin/apps", ""), http.StatusUnauthorized, domain.CodeSessionExpired)
}

func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	f := newAPI(t, "")
	a := f.signIn(t, f.owner)
	other := f.signIn(t, f.owner)

	w := f.send(a, http.MethodPost, "/admin/me/password", `{"current_password":"`+testPassword+`","new_password":"brand-new-pass-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if w := f.send(a, http.MethodGet, "/admin/apps", ""); w.Code != http.StatusOK {
		t.Fatalf("the changing browser lost its session: %d", w.Code)
	}
	if w := f.send(a, http.MethodPost, "/admin/apps", `{}`); w.Code == http.StatusForbidden {
		t.Fatalf("new csrf token rejected: %s", w.Body.String())
	}
	expectError(t, f.send(other, http.MethodGet, "/admin/apps", ""), http.StatusUnauthorized, domain.CodeSessionExpired)

	expectError(t, f.send(a, http.MethodPost, "/admin/me/password", `{"current_password":"wrong","new_password":"another-pass-12"}`),
		http.StatusUnauthorized, domain.CodeInvalidCredentials)
	expectError(t, f.send(a, http.MethodPost, "/admin/me/password", `{"current_password":"brand-new-pass-1","new_password":"brand-new-pass-1"}`),
		http.StatusBadRequest, domain.CodeValidationFailed)
	env := expectError(t, f.send(a, http.MethodPost, "/admin/me/password", `{"current_password":"brand-new-pass-1","new_password":"`+strings.Repeat("x", 73)+`"}`),
		http.StatusBadRequest, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "new_password", Rule: "max"})
	if w := f.send(a, http.MethodGet, "/admin/me", ""); w.Code != http.StatusOK {
		t.Fatal("a rejected change ended the session")
	}
}

func TestOwnerActionsEndTargetSessions(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	for _, action := range []string{"disable", "reset-password"} {
		u, b := f.as(t, domain.RoleAdmin)
		if w := f.send(b, http.MethodGet, "/admin/me", ""); w.Code != http.StatusOK {
			t.Fatalf("%s: precondition %d", action, w.Code)
		}
		if w := f.send(owner, http.MethodPost, "/admin/users/"+u.PublicID+"/"+action, ""); w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
		expectError(t, f.send(b, http.MethodGet, "/admin/me", ""), http.StatusUnauthorized, domain.CodeSessionExpired)
	}
}

func TestRoleChangeAppliesOnNextRequest(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	u, b := f.as(t, domain.RoleAdmin)
	if w := f.send(b, http.MethodGet, "/admin/audit-logs", ""); w.Code != http.StatusOK {
		t.Fatalf("admin audit: %d", w.Code)
	}
	if w := f.send(owner, http.MethodPatch, "/admin/users/"+u.PublicID, `{"role":"viewer"}`); w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	expectError(t, f.send(b, http.MethodGet, "/admin/audit-logs", ""), http.StatusForbidden, domain.CodeForbidden)
	_, perms := meBody(t, f.send(b, http.MethodGet, "/admin/me", ""))
	if !slices.Equal(perms, []string{"apps:read", "connections:read", "deliveries:read", "payments:read"}) {
		t.Fatalf("permissions = %v", perms)
	}
}

func TestMustChangePasswordGateLifts(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	w := f.send(owner, http.MethodPost, "/admin/users", `{"email":"new@example.com","name":"New","role":"admin"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		TemporaryPassword string `json:"temporary_password"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	b := newBrowser()
	w = f.send(b, http.MethodPost, "/admin/login", `{"email":"new@example.com","password":"`+created.TemporaryPassword+`"}`)
	if user, _ := meBody(t, w); w.Code != http.StatusOK || user["must_change_password"] != true {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	expectError(t, f.send(b, http.MethodGet, "/admin/apps", ""), http.StatusForbidden, domain.CodePasswordChangeRequired)
	if w := f.send(b, http.MethodGet, "/admin/me", ""); w.Code != http.StatusOK {
		t.Fatalf("me: %d", w.Code)
	}
	w = f.send(b, http.MethodPost, "/admin/me/password", `{"current_password":"`+created.TemporaryPassword+`","new_password":"my-own-password"}`)
	if user, _ := meBody(t, w); w.Code != http.StatusOK || user["must_change_password"] != false {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if w := f.send(b, http.MethodGet, "/admin/apps", ""); w.Code != http.StatusOK {
		t.Fatalf("gate not lifted: %d %s", w.Code, w.Body.String())
	}
}

func TestCSRFOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	create := `{"email":"x@example.com","name":"X","role":"viewer"}`
	session := owner.cookies[middleware.SessionCookie]
	csrf := owner.cookies[middleware.CSRFCookie]

	raw := func(cookieCSRF, header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(create))
		req.AddCookie(&http.Cookie{Name: middleware.SessionCookie, Value: session})
		if cookieCSRF != "" {
			req.AddCookie(&http.Cookie{Name: middleware.CSRFCookie, Value: cookieCSRF})
		}
		if header != "" {
			req.Header.Set(middleware.CSRFHeader, header)
		}
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w
	}
	expectError(t, raw(csrf, ""), http.StatusForbidden, domain.CodeInvalidCSRFToken)
	expectError(t, raw(csrf, "other"), http.StatusForbidden, domain.CodeInvalidCSRFToken)
	expectError(t, raw("", csrf), http.StatusForbidden, domain.CodeInvalidCSRFToken)
	expectError(t, raw("forged", "forged"), http.StatusForbidden, domain.CodeInvalidCSRFToken)
	if n, _ := f.users.Count(t.Context()); n != 1 {
		t.Fatal("a request without a valid csrf token created a user")
	}
	if w := raw(csrf, csrf); w.Code != http.StatusCreated {
		t.Fatalf("valid csrf: %d %s", w.Code, w.Body.String())
	}

	// Safe methods need no token; neither do /v1 and /webhooks.
	req := httptest.NewRequest(http.MethodGet, "/admin/me", nil)
	req.AddCookie(&http.Cookie{Name: middleware.SessionCookie, Value: session})
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET without csrf: %d", w.Code)
	}
	for _, target := range []string{"/v1/payments", "/webhooks/xendit"} {
		w := f.send(nil, http.MethodPost, target, "{}")
		var env errorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env.Error.Code == domain.CodeInvalidCSRFToken || len(w.Header().Values("Set-Cookie")) > 0 {
			t.Fatalf("%s: csrf applied or cookies set: %d %s", target, w.Code, w.Body.String())
		}
	}
	if f.logs.FilterField(stringField("event", "admin.csrf_failed")).Len() != 4 {
		t.Fatalf("csrf security logs = %d", f.logs.FilterField(stringField("event", "admin.csrf_failed")).Len())
	}
}

func TestMeShape(t *testing.T) {
	f := newAPI(t, "")
	b := f.signIn(t, f.owner)
	w := f.send(b, http.MethodGet, "/admin/me", "")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil || len(raw) != 2 {
		t.Fatalf("me = %s", w.Body.String())
	}
	user, perms := meBody(t, w)
	keys := make([]string, 0, len(user))
	for k := range user {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"created_at", "disabled_at", "email", "id", "last_login_at", "must_change_password", "name", "role", "status", "updated_at"}
	if !slices.Equal(keys, want) {
		t.Fatalf("user keys = %v", keys)
	}
	if user["disabled_at"] != nil || user["status"] != "active" || user["id"] != f.owner.PublicID {
		t.Fatalf("user = %v", user)
	}
	if !slices.IsSorted(perms) || len(perms) != 10 {
		t.Fatalf("permissions = %v", perms)
	}
	if strings.Contains(w.Body.String(), "password_hash") || strings.Contains(w.Body.String(), "$2a$") {
		t.Fatal("hash exposed")
	}
}

// withAuthLimits bounds /v1 authentication failures on the fixture's own
// failure counter.
func withAuthLimits(perIP, perKey int) apiOption {
	return func(_ *service.AuthConfig, d *Deps) {
		d.AuthLimits = middleware.AuthLimits{MaxFailuresPerIP: perIP, MaxFailuresPerKey: perKey, Window: 15 * time.Minute}
	}
}

// Over the real router: v2 signing works for both /v1 routes, a 64 KiB+
// body is 413, and failed authentications are limited per client.
func TestV1TransportHardeningOverHTTP(t *testing.T) {
	f := newAPI(t, "", withAuthLimits(3, 100))
	f.deps.AuthLimiter = f.failures
	f.router = NewRouter(f.deps)
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	c := v1Client{f: f, apiKey: app.apiKey, secret: app.apiSecret}

	w := c.doV2(http.MethodPost, "/v1/payments", `{"order_id":"ord-v2","amount":150000}`)
	pub := decode(t, w, http.StatusCreated)["id"].(string)
	if w := c.doV2(http.MethodGet, "/v1/payments/"+pub, ""); w.Code != http.StatusOK {
		t.Fatalf("v2 get: %d %s", w.Code, w.Body.String())
	}
	// The same GET twice in one second is fine in v2 (distinct nonces).
	if w := c.doV2(http.MethodGet, "/v1/payments/"+pub, ""); w.Code != http.StatusOK {
		t.Fatalf("v2 get again: %d %s", w.Code, w.Body.String())
	}

	big := `{"order_id":"ord-big","amount":1,"description":"` + strings.Repeat("x", int(middleware.MaxV1BodyBytes)) + `"}`
	w = c.do(http.MethodPost, "/v1/payments", big)
	expectError(t, w, http.StatusRequestEntityTooLarge, domain.CodeRequestTooLarge)

	for i := 0; i < 3; i++ {
		expectError(t, f.send(nil, http.MethodPost, "/v1/payments", `{}`, "X-Api-Key", "kp_test_"+strings.Repeat("9", 32),
			"X-Timestamp", signedTimestamp("none", strconv.Itoa(i)), "X-Signature", strings.Repeat("0", 64)), 401, domain.CodeUnknownAPIKey)
	}
	w = c.do(http.MethodGet, "/v1/payments/"+pub, "")
	expectError(t, w, http.StatusTooManyRequests, domain.CodeRateLimited)
	if w.Header().Get("Retry-After") == "" {
		t.Fatalf("no Retry-After: %v", w.Header())
	}

	// The limiter failing closes /v1 with a 503 rather than skipping the checks.
	f.failures.Err = errors.New("redis down")
	expectError(t, c.do(http.MethodGet, "/v1/payments/"+pub, ""), http.StatusServiceUnavailable, domain.CodeAuthUnavailable)
}
