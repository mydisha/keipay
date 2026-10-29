package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// Body limits per route group. A create-payment body is a few KB (metadata
// alone is capped at 8 KiB), so 64 KiB leaves room without letting an
// unauthenticated caller stream megabytes into memory before its signature
// is checked. Admin bodies carry at most a webhook endpoint definition or
// provider credentials. Gateway callbacks keep their own cap in the handler.
const (
	MaxV1BodyBytes    int64 = 64 << 10
	MaxAdminBodyBytes int64 = 1 << 20
)

// MaxBody refuses a request whose declared Content-Length is over limit
// before anything is read, and wraps the body of every other request so a
// read past limit fails with *http.MaxBytesError, which readers answer as
// 413 request_too_large (see TooLarge).
func MaxBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			// net/http would close the connection itself on a refused body,
			// but gin's writer hides that hint, so say it explicitly: the
			// client must not keep sending the body on this connection.
			c.Header("Connection", "close")
			response.Error(c, domain.ErrRequestTooLarge)
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

// TooLarge reports whether a body read failed because the body passed the
// limit set by MaxBody or http.MaxBytesReader.
func TooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}
