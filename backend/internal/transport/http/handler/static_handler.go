package handler

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// SPA serves the built admin console from dir. Existing files are served as-is;
// any other GET/HEAD path falls back to index.html so client-side routes work.
// Paths under apiPrefixes keep the JSON 404 so API clients never get HTML.
// A prefix such as "/v1" reserves the path itself and everything below it; a
// prefix with a trailing slash such as "/webhooks/" reserves only the paths
// below it, so the bare path stays a console route.
func SPA(dir string, apiPrefixes ...string) gin.HandlerFunc {
	fsys := http.Dir(dir)
	index := filepath.Join(dir, "index.html")

	return func(c *gin.Context) {
		p := c.Request.URL.Path
		method := c.Request.Method
		if (method != http.MethodGet && method != http.MethodHead) || underAny(p, apiPrefixes) {
			NotFound(c)
			return
		}

		// The console is the one thing this server serves that a browser
		// renders, so it gets its own content security policy in place of
		// the API's.
		c.Header("Content-Security-Policy", middleware.ConsoleContentSecurityPolicy)

		// http.Dir rejects paths that resolve outside dir.
		name := path.Clean("/" + p)
		if f, err := fsys.Open(name); err == nil {
			info, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !info.IsDir() {
				// Vite fingerprints everything under /assets, so it never changes in place.
				if strings.HasPrefix(name, "/assets/") {
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				}
				c.FileFromFS(name, fsys)
				return
			}
		}

		c.Header("Cache-Control", "no-cache")
		c.File(index)
	}
}

func underAny(p string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasSuffix(prefix, "/") {
			if strings.HasPrefix(p, prefix) {
				return true
			}
			continue
		}
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// NotFound answers unknown routes with the not_found envelope.
func NotFound(c *gin.Context) {
	response.Error(c, domain.ErrNotFound)
}
