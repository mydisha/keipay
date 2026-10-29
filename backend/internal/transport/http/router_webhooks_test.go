package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Exactly /webhooks is the console's Webhooks page, so a reload or deep link
// must serve index.html when the console is built. Paths below /webhooks/ and
// non-GET methods stay JSON 404 envelopes.
func TestRouter_WebhooksPathIsConsolePage(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>console</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newTestRouter(t, dist)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/webhooks", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s /webhooks: got %d %q, want 200 index.html", method, w.Code, w.Body.String())
		}
		if method == http.MethodGet && w.Body.String() != "<html>console</html>" {
			t.Fatalf("GET /webhooks: body %q, want index.html", w.Body.String())
		}
	}

	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/webhooks/x"},
		{http.MethodGet, "/webhooks/xendit"},
		{http.MethodPost, "/webhooks"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.target, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s: got %d, want 404", tc.method, tc.target, w.Code)
		}
		var env struct {
			Error struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Error.Code != "not_found" || env.Error.Details == nil {
			t.Fatalf("%s %s: body %q is not a not_found envelope", tc.method, tc.target, w.Body.String())
		}
	}
}

// Without a console build, /webhooks is just an unknown API path.
func TestRouter_WebhooksPathWithoutConsoleIsNotFound(t *testing.T) {
	w := get(newTestRouter(t, ""), "/webhooks")
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}
