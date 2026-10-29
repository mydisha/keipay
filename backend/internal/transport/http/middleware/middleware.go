// Package middleware holds Gin middleware for auth, request id, and recovery.
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

const (
	ctxRequestID = "request_id"
	ctxApp       = "app"
	ctxAPIKey    = "api_key"
)

// RequestID attaches a unique id to each request, propagates it into the
// request context (so service/repository layers can correlate logs), and
// echoes it back in the X-Request-ID response header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(ctxRequestID, id)
		c.Header("X-Request-ID", id)
		// Propagate down the call chain for end-to-end tracing.
		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

// RequestIDFromContext returns the request id stored by the RequestID
// middleware in the gin context, or "" if absent.
func RequestIDFromContext(c *gin.Context) string {
	return c.GetString(ctxRequestID)
}

// Recover logs panics and returns 500 instead of crashing the server. A
// handler that panics with http.ErrAbortHandler wants the connection cut
// (a stream failed after its first byte), so that panic is passed on to the
// server, which closes the connection without logging.
func Recover(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				if r == http.ErrAbortHandler {
					panic(r)
				}
				log.Error("panic recovered",
					zap.String(logger.FieldLogType, logger.LogTypeApp),
					zap.Any("error", r),
					zap.String("method", c.Request.Method),
					zap.String("path", c.FullPath()),
					zap.String(logger.FieldRequestID, c.GetString(ctxRequestID)),
					zap.Stack("stack"),
				)
				response.Error(c, domain.NewError(domain.CodeInternal, "internal server error"))
			}
		}()
		c.Next()
	}
}
