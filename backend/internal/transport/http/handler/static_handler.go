package handler

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// SPA serves the built admin console from dir. Existing files are served as-is;
// any other GET/HEAD path falls back to index.html so client-side routes work.
// Paths under apiPrefixes keep the JSON 404 so API clients never get HTML.
func SPA(dir string, apiPrefixes ...string) gin.HandlerFunc {
	fsys := http.Dir(dir)
	index := filepath.Join(dir, "index.html")

	return func(c *gin.Context) {
		p := c.Request.URL.Path
		method := c.Request.Method
		if (method != http.MethodGet && method != http.MethodHead) || underAny(p, apiPrefixes) {
			response.ErrorWithMessage(c, http.StatusNotFound, "not found")
			return
		}

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
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}
