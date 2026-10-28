package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// AccessLog records one structured line per HTTP request. Fields are stable and
// machine-friendly (request_id, method, path, status, latency_ms, client_ip,
// bytes) so logs are easy to search, sort (e.g. by latency_ms or status), and
// correlate via request_id. Log level scales with status: 5xx error, 4xx warn,
// otherwise info.
func AccessLog(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path // unmatched routes still get logged
		}

		fields := []zap.Field{
			zap.String(logger.FieldLogType, logger.LogTypeAccess),
			zap.String(logger.FieldRequestID, c.GetString(ctxRequestID)),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Int64(logger.FieldLatencyMS, latency.Milliseconds()),
			zap.String(logger.FieldClientIP, c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Int("bytes", c.Writer.Size()),
			zap.String("query", c.Request.URL.RawQuery),
		}

		// Attach tenant id when the request was authenticated as a tenant.
		if t, ok := TenantFromContext(c); ok {
			fields = append(fields, zap.Int64(logger.FieldTenantID, tenantID(t)))
		}

		// Surface any errors gin collected during handling.
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		msg := "http request"
		switch {
		case status >= 500:
			log.Error(msg, fields...)
		case status >= 400:
			log.Warn(msg, fields...)
		default:
			log.Info(msg, fields...)
		}
	}
}

func tenantID(t *domain.Tenant) int64 {
	if t == nil {
		return 0
	}
	return t.ID
}
