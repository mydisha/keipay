package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newSPAEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	writeFile(t, filepath.Join(dist, "index.html"), "<html>console</html>")
	writeFile(t, filepath.Join(dist, "assets", "app-1a2b.js"), "console.log(1)")
	writeFile(t, filepath.Join(root, "secret.txt"), "outside")

	r := gin.New()
	r.GET("/healthz", Health)
	r.NoRoute(SPA(dist, "/v1", "/admin", "/webhooks/", "/healthz"))
	return r
}

func writeFile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func serve(r http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func TestSPA_ServesBuiltAssetWithLongCache(t *testing.T) {
	w := serve(newSPAEngine(t), http.MethodGet, "/assets/app-1a2b.js")
	if w.Code != http.StatusOK || w.Body.String() != "console.log(1)" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable", cc)
	}
}

func TestSPA_ClientRoutesFallBackToIndex(t *testing.T) {
	r := newSPAEngine(t)
	for _, target := range []string{"/", "/apps", "/payments?page=2", "/login", "/assets/", "/assets/missing.js"} {
		w := serve(r, http.MethodGet, target)
		if w.Code != http.StatusOK || w.Body.String() != "<html>console</html>" {
			t.Fatalf("%s: got %d %q", target, w.Code, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("%s: Cache-Control = %q, want no-cache", target, cc)
		}
	}
}

func TestSPA_HeadIsServed(t *testing.T) {
	w := serve(newSPAEngine(t), http.MethodHead, "/apps")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
}

func TestSPA_APIPathsStayJSON404(t *testing.T) {
	r := newSPAEngine(t)
	for _, target := range []string{"/v1", "/v1/unknown", "/admin/nope", "/webhooks/midtrans", "/healthz/extra"} {
		w := serve(r, http.MethodGet, target)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d, want 404", target, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("%s: Content-Type = %q, want JSON", target, ct)
		}
	}
}

func TestSPA_SubtreeOnlyPrefixKeepsBarePathForConsole(t *testing.T) {
	r := newSPAEngine(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := serve(r, method, "/webhooks")
		if w.Code != http.StatusOK {
			t.Fatalf("%s /webhooks: got %d, want 200", method, w.Code)
		}
		if method == http.MethodGet && w.Body.String() != "<html>console</html>" {
			t.Fatalf("GET /webhooks: body %q, want index.html", w.Body.String())
		}
	}
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/webhooks/"},
		{http.MethodGet, "/webhooks/xendit"},
		{http.MethodHead, "/webhooks/x"},
		{http.MethodPost, "/webhooks"},
	} {
		if w := serve(r, tc.method, tc.target); w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: got %d, want 404", tc.method, tc.target, w.Code)
		}
	}
	// "/webhooksx" shares the bytes but not the segment.
	if w := serve(r, http.MethodGet, "/webhooksx"); w.Code != http.StatusOK {
		t.Fatalf("/webhooksx: got %d, want 200", w.Code)
	}
}

func TestSPA_PrefixMatchIsSegmentAware(t *testing.T) {
	// "/administrator" is not under "/admin", so it is a client route.
	w := serve(newSPAEngine(t), http.MethodGet, "/administrator")
	if w.Code != http.StatusOK || w.Body.String() != "<html>console</html>" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestSPA_NonGetIsNotFound(t *testing.T) {
	r := newSPAEngine(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if w := serve(r, method, "/apps"); w.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d, want 404", method, w.Code)
		}
	}
}

func TestSPA_DoesNotEscapeDistDir(t *testing.T) {
	r := newSPAEngine(t)
	for _, target := range []string{"/../secret.txt", "/assets/../../secret.txt", "/%2e%2e/secret.txt"} {
		w := serve(r, http.MethodGet, target)
		if strings.Contains(w.Body.String(), "outside") {
			t.Fatalf("%s leaked a file outside the dist dir", target)
		}
	}
}
