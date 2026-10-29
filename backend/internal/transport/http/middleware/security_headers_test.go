package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders_Defaults(t *testing.T) {
	w := serve(newEngine(SecurityHeaders(SecurityHeadersConfig{})), httptest.NewRequest(http.MethodGet, "/x", nil))
	want := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "no-referrer",
		"Permissions-Policy":         "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Content-Security-Policy":    APIContentSecurityPolicy,
		"Cache-Control":              "no-store",
	}
	for name, value := range want {
		if got := w.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS sent while disabled: %q", got)
	}
}

func TestSecurityHeaders_HSTSOnlyWhenEnabled(t *testing.T) {
	w := serve(newEngine(SecurityHeaders(SecurityHeadersConfig{HSTS: true})), httptest.NewRequest(http.MethodGet, "/x", nil))
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Fatalf("HSTS = %q", got)
	}
}

// A handler that serves something cacheable or renderable replaces the
// defaults for its own response only.
func TestSecurityHeaders_HandlerOverrides(t *testing.T) {
	r := newEngine(SecurityHeaders(SecurityHeadersConfig{}), func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=60")
		c.Header("Content-Security-Policy", ConsoleContentSecurityPolicy)
	})
	w := serve(r, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Header().Get("Cache-Control") != "public, max-age=60" || w.Header().Get("Content-Security-Policy") != ConsoleContentSecurityPolicy {
		t.Fatalf("headers = %v", w.Header())
	}
	if len(w.Header().Values("Content-Security-Policy")) != 1 {
		t.Fatalf("policies = %v, want one", w.Header().Values("Content-Security-Policy"))
	}
}
