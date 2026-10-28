package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/mydisha/keipay/backend/internal/domain"
)

const userID = "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"

// stubAuth knows one valid token per user; "expired" is a revoked session and
// "broken" fails like an unreachable store.
type stubAuth struct {
	users map[string]*domain.User
}

func (s stubAuth) Authenticate(_ context.Context, token string) (*domain.User, *domain.Session, error) {
	switch token {
	case "broken":
		return nil, nil, errors.New("redis down")
	}
	u, ok := s.users[token]
	if !ok {
		return nil, nil, domain.ErrSessionExpired
	}
	return u, &domain.Session{UserID: u.ID, UserPublicID: u.PublicID, CSRFToken: "csrf-of-" + token}, nil
}

type adminHarness struct {
	engine *gin.Engine
	logs   *observer.ObservedLogs
	actors map[string]domain.Actor
}

func newAdminHarness() *adminHarness {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.DebugLevel)
	log := zap.New(core)
	h := &adminHarness{logs: logs, actors: map[string]domain.Actor{}}
	auth := stubAuth{users: map[string]*domain.User{
		"owner-token":   {ID: 1, PublicID: userID, Role: domain.RoleOwner},
		"viewer-token":  {ID: 2, PublicID: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0D", Role: domain.RoleViewer},
		"pending-token": {ID: 3, PublicID: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0E", Role: domain.RoleOwner, MustChangePassword: true},
	}}
	tokens := 0
	newToken := func() (string, error) {
		tokens++
		return "issued-" + strings.Repeat("x", tokens), nil
	}
	r := gin.New()
	r.Use(RequestID())
	g := r.Group("/admin")
	g.Use(AdminContext(CookieConfig{}, newToken), LoadSession(auth, CookieConfig{}), CSRF(log))
	ok := func(c *gin.Context) {
		a, _ := domain.ActorFromContext(c.Request.Context())
		h.actors[c.Request.Method+" "+c.FullPath()] = a
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
	g.Any("/public", ok)
	g.GET("/me", RequireSession(log), ok)
	g.POST("/write", RequireSession(log), RequirePasswordChanged(), RequirePermission(domain.PermTenantsWrite, log), ok)
	h.engine = r
	return h
}

type req struct {
	method, path, session, csrfCookie, csrfHeader string
}

func (h *adminHarness) do(r req) *httptest.ResponseRecorder {
	hr := httptest.NewRequest(r.method, r.path, nil)
	if r.session != "" {
		hr.AddCookie(&http.Cookie{Name: SessionCookie, Value: r.session})
	}
	if r.csrfCookie != "" {
		hr.AddCookie(&http.Cookie{Name: CSRFCookie, Value: r.csrfCookie})
	}
	if r.csrfHeader != "" {
		hr.Header.Set(CSRFHeader, r.csrfHeader)
	}
	hr.Header.Set("User-Agent", "console/1.0")
	hr.Header.Set("X-Request-ID", "req-42")
	hr.RemoteAddr = "198.51.100.9:5555"
	w := httptest.NewRecorder()
	h.engine.ServeHTTP(w, hr)
	return w
}

func TestCSRFCombinations(t *testing.T) {
	h := newAdminHarness()
	cases := []struct {
		name string
		r    req
		want int
	}{
		{"no header", req{http.MethodPost, "/admin/public", "", "c1", ""}, 403},
		{"header differs from cookie", req{http.MethodPost, "/admin/public", "", "c1", "c2"}, 403},
		{"no cookie", req{http.MethodPost, "/admin/public", "", "", "c1"}, 403},
		{"header equals cookie", req{http.MethodPost, "/admin/public", "", "c1", "c1"}, 200},
		{"equals cookie but not the session", req{http.MethodPost, "/admin/public", "owner-token", "c1", "c1"}, 403},
		{"equals cookie and session", req{http.MethodPost, "/admin/public", "owner-token", "csrf-of-owner-token", "csrf-of-owner-token"}, 200},
		{"expired session falls back to the cookie check", req{http.MethodPost, "/admin/public", "expired", "c1", "c1"}, 200},
		{"GET needs no token", req{http.MethodGet, "/admin/public", "", "", ""}, 200},
		{"PUT checked", req{http.MethodPut, "/admin/public", "", "c1", ""}, 403},
		{"PATCH checked", req{http.MethodPatch, "/admin/public", "", "c1", ""}, 403},
		{"DELETE checked", req{http.MethodDelete, "/admin/public", "", "c1", ""}, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := h.do(tc.r)
			if tc.want == 403 {
				assertEnvelope(t, w, http.StatusForbidden, domain.CodeInvalidCSRFToken)
				return
			}
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
	events := h.logs.FilterField(zap.String("event", "admin.csrf_failed")).All()
	if len(events) != 7 {
		t.Fatalf("csrf events = %d, want 7", len(events))
	}
	for _, e := range events {
		for _, v := range e.ContextMap() {
			if s, _ := v.(string); strings.Contains(s, "c1") || strings.Contains(s, "csrf-of") {
				t.Fatalf("csrf token logged: %v", e.ContextMap())
			}
		}
	}
}

func TestCSRFCookieIssuedOnlyWhenAbsent(t *testing.T) {
	h := newAdminHarness()
	w := h.do(req{method: http.MethodGet, path: "/admin/public"})
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CSRFCookie || cookies[0].Value == "" || cookies[0].HttpOnly {
		t.Fatalf("cookies = %+v", cookies)
	}
	if cookies[0].Path != "/" || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Secure {
		t.Fatalf("cookie attributes = %+v", cookies[0])
	}
	if w := h.do(req{method: http.MethodGet, path: "/admin/public", csrfCookie: "kept"}); len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("re-issued: %v", w.Header().Values("Set-Cookie"))
	}
}

func TestRequireSession(t *testing.T) {
	h := newAdminHarness()
	assertEnvelope(t, h.do(req{method: http.MethodGet, path: "/admin/me"}), http.StatusUnauthorized, domain.CodeSessionRequired)

	w := h.do(req{method: http.MethodGet, path: "/admin/me", session: "expired"})
	assertEnvelope(t, w, http.StatusUnauthorized, domain.CodeSessionExpired)
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie && c.MaxAge < 0 && c.HttpOnly {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("expired session cookie not cleared: %v", w.Header().Values("Set-Cookie"))
	}
	if w := h.do(req{method: http.MethodGet, path: "/admin/me", session: "owner-token"}); w.Code != http.StatusOK {
		t.Fatalf("valid session: %d", w.Code)
	}
	assertEnvelope(t, h.do(req{method: http.MethodGet, path: "/admin/me", session: "broken"}), http.StatusInternalServerError, domain.CodeInternal)
	// An expired cookie on a public route is cleared but not rejected.
	if w := h.do(req{method: http.MethodGet, path: "/admin/public", session: "expired"}); w.Code != http.StatusOK {
		t.Fatalf("public with expired cookie: %d", w.Code)
	}

	if h.logs.FilterField(zap.String("event", "admin.session_missing")).Len() != 1 ||
		h.logs.FilterField(zap.String("event", "admin.session_invalid")).Len() != 1 {
		t.Fatalf("session logs = %v", h.logs.All())
	}
}

func TestPasswordGateAndPermission(t *testing.T) {
	h := newAdminHarness()
	write := func(token string) *httptest.ResponseRecorder {
		return h.do(req{http.MethodPost, "/admin/write", token, "csrf-of-" + token, "csrf-of-" + token})
	}
	if w := write("owner-token"); w.Code != http.StatusOK {
		t.Fatalf("owner: %d %s", w.Code, w.Body.String())
	}
	assertEnvelope(t, write("pending-token"), http.StatusForbidden, domain.CodePasswordChangeRequired)
	if w := h.do(req{method: http.MethodGet, path: "/admin/me", session: "pending-token"}); w.Code != http.StatusOK {
		t.Fatalf("pending user on an allowed route: %d", w.Code)
	}
	assertEnvelope(t, write("viewer-token"), http.StatusForbidden, domain.CodeForbidden)

	events := h.logs.FilterField(zap.String("event", "admin.forbidden")).All()
	if len(events) != 1 {
		t.Fatalf("forbidden events = %d", len(events))
	}
	fields := events[0].ContextMap()
	if fields["user_id"] != "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0D" || fields["route"] != "POST /admin/write" ||
		fields["permission"] != "tenants:write" || fields["client_ip"] != "198.51.100.9" || fields["request_id"] != "req-42" {
		t.Fatalf("forbidden fields = %v", fields)
	}
}

func TestActorInContext(t *testing.T) {
	h := newAdminHarness()
	h.do(req{method: http.MethodGet, path: "/admin/public"})
	want := domain.AnonymousActor("198.51.100.9", "console/1.0", "req-42")
	if got := h.actors["GET /admin/public"]; got != want {
		t.Fatalf("anonymous actor = %+v, want %+v", got, want)
	}
	h.do(req{method: http.MethodGet, path: "/admin/me", session: "owner-token"})
	want = domain.UserActor(userID, "198.51.100.9", "console/1.0", "req-42")
	if got := h.actors["GET /admin/me"]; got != want {
		t.Fatalf("user actor = %+v, want %+v", got, want)
	}
}

func TestSetCookieReplacesSameName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Writer.Header().Add("Set-Cookie", "other=1; Path=/")
	SetCSRFCookie(c, CookieConfig{}, "first")
	ClearSessionCookie(c, CookieConfig{Secure: true})
	SetSessionCookies(c, CookieConfig{Secure: true}, "session-token", "second")
	got := w.Header().Values("Set-Cookie")
	if len(got) != 3 {
		t.Fatalf("Set-Cookie = %v", got)
	}
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "first") || !strings.Contains(joined, "keipay_csrf=second") ||
		!strings.Contains(joined, "keipay_session=session-token; Path=/; HttpOnly; Secure; SameSite=Lax") ||
		!strings.Contains(joined, "other=1") {
		t.Fatalf("Set-Cookie = %v", got)
	}
}
