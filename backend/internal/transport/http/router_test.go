package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

func newTestRouter(t *testing.T, frontendDir string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return NewRouter(Deps{Log: zap.NewNop(), FrontendDir: frontendDir, NonceStore: &memNonces{seen: map[string]bool{}}})
}

func get(r http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestRouter_APIRoutesWinOverConsole(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>console</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newTestRouter(t, dist)

	if w := get(r, "/healthz"); w.Code != http.StatusOK || w.Body.String() == "<html>console</html>" {
		t.Fatalf("/healthz: got %d %q", w.Code, w.Body.String())
	}
	// Protected admin and app APIs still reach their auth middleware.
	if w := get(r, "/admin/apps"); w.Code != http.StatusUnauthorized {
		t.Fatalf("/admin/apps: got %d, want 401", w.Code)
	}
	if w := get(r, "/v1/payments/1"); w.Code != http.StatusUnauthorized {
		t.Fatalf("/v1/payments/1: got %d, want 401", w.Code)
	}
	if w := get(r, "/apps"); w.Code != http.StatusOK || w.Body.String() != "<html>console</html>" {
		t.Fatalf("/apps: got %d %q", w.Code, w.Body.String())
	}
}

// Every kind of response carries the security headers: API answers, auth
// refusals, the console and unknown routes, with or without a console build.
func TestRouter_SecurityHeadersOnEveryResponse(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>console</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	nonces := &memNonces{seen: map[string]bool{}}
	withConsole := NewRouter(Deps{Log: zap.NewNop(), FrontendDir: dist, NonceStore: nonces, SecurityHeaders: middleware.SecurityHeadersConfig{HSTS: true}})
	plain := NewRouter(Deps{Log: zap.NewNop(), NonceStore: nonces})
	cases := []struct {
		r      http.Handler
		target string
		policy string
	}{
		{withConsole, "/healthz", middleware.APIContentSecurityPolicy},
		{withConsole, "/v1/payments/1", middleware.APIContentSecurityPolicy},
		{withConsole, "/admin/apps", middleware.APIContentSecurityPolicy},
		{withConsole, "/v1/missing", middleware.APIContentSecurityPolicy},
		{withConsole, "/apps", middleware.ConsoleContentSecurityPolicy},
		{plain, "/apps", middleware.APIContentSecurityPolicy},
	}
	for _, tc := range cases {
		w := get(tc.r, tc.target)
		h := w.Header()
		if h.Get("Content-Security-Policy") != tc.policy || h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: headers %v", tc.target, h)
		}
		if hsts := h.Get("Strict-Transport-Security"); (hsts != "") != (tc.r == withConsole) {
			t.Errorf("%s: HSTS %q", tc.target, hsts)
		}
	}
}

func TestGinMode(t *testing.T) {
	for env, want := range map[string]string{"development": gin.DebugMode, " Development ": gin.DebugMode,
		"production": gin.ReleaseMode, "staging": gin.ReleaseMode, "": gin.ReleaseMode} {
		if got := GinMode(env); got != want {
			t.Errorf("GinMode(%q) = %q, want %q", env, got, want)
		}
	}
}

func TestRouter_NoFrontendDirKeepsPlain404(t *testing.T) {
	if w := get(newTestRouter(t, ""), "/apps"); w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}
