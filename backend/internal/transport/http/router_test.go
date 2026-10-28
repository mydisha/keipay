package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func newTestRouter(t *testing.T, frontendDir string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return NewRouter(Deps{Log: zap.NewNop(), FrontendDir: frontendDir})
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
	// Protected admin and tenant APIs still reach their auth middleware.
	if w := get(r, "/admin/tenants"); w.Code != http.StatusUnauthorized {
		t.Fatalf("/admin/tenants: got %d, want 401", w.Code)
	}
	if w := get(r, "/v1/payments/1"); w.Code != http.StatusUnauthorized {
		t.Fatalf("/v1/payments/1: got %d, want 401", w.Code)
	}
	if w := get(r, "/tenants"); w.Code != http.StatusOK || w.Body.String() != "<html>console</html>" {
		t.Fatalf("/tenants: got %d %q", w.Code, w.Body.String())
	}
}

func TestRouter_NoFrontendDirKeepsPlain404(t *testing.T) {
	if w := get(newTestRouter(t, ""), "/tenants"); w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}
