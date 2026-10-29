package middleware

import "github.com/gin-gonic/gin"

// Content security policies. The API one forbids everything: its responses
// are JSON and must never be rendered. The console one is what the built
// admin console needs and nothing more: scripts and fonts come from the
// build under /assets, small images and fonts are inlined by the bundler as
// data: URLs, and the UI libraries insert their own <style> elements at
// runtime, which is why inline styles (and only styles) are allowed.
const (
	APIContentSecurityPolicy     = "default-src 'none'; frame-ancestors 'none'"
	ConsoleContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; " +
		"base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
	// hstsValue asks browsers to use HTTPS for a year, subdomains included.
	hstsValue = "max-age=31536000; includeSubDomains"
)

// SecurityHeadersConfig says which optional headers to send.
type SecurityHeadersConfig struct {
	// HSTS sends Strict-Transport-Security. Only useful when browsers reach
	// the server over TLS; they ignore the header over plain HTTP.
	HSTS bool
}

// SecurityHeaders sets the response headers every route shares: no content
// sniffing, no framing, no referrer, no device permissions, no caching and
// the API content security policy. They are set before the handler runs, so
// a handler that serves something else (the console, a CSV export) can
// replace what it needs.
func SecurityHeaders(cfg SecurityHeadersConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy", APIContentSecurityPolicy)
		h.Set("Cache-Control", "no-store")
		if cfg.HSTS {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		c.Next()
	}
}
