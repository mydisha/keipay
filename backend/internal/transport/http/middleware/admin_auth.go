package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

const ctxAdmin = "admin"

// JWTVerifier validates an admin session token and returns the subject.
type JWTVerifier interface {
	Verify(token string) (string, error)
}

// AdminAuth protects admin routes using a Bearer JWT issued at login. Rejected
// requests are logged as security events to surface unauthorized access
// attempts against the admin surface. Accepted requests carry the admin as the
// audit actor in the request context.
func AdminAuth(verifier JWTVerifier, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rid := c.GetString(ctxRequestID)

		h := c.GetHeader("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
		if token == "" || token == h {
			logger.Security(log, logger.EventAdminMissingToken, logger.SeverityMedium, ip, rid, "missing bearer token")
			response.Error(c, domain.NewError(domain.CodeMissingToken, "missing bearer token"))
			return
		}
		sub, err := verifier.Verify(token)
		if err != nil {
			logger.Security(log, logger.EventAdminInvalidToken, logger.SeverityHigh, ip, rid, "invalid token")
			response.Error(c, domain.NewError(domain.CodeInvalidToken, "invalid or expired token"))
			return
		}
		c.Set(ctxAdmin, sub)
		c.Request = c.Request.WithContext(domain.WithActor(c.Request.Context(), domain.Actor{
			Type:      domain.ActorAdmin,
			ID:        sub,
			IP:        ip,
			UserAgent: c.Request.UserAgent(),
			RequestID: rid,
		}))
		c.Next()
	}
}

// AdminFromContext returns the admin username set by AdminAuth.
func AdminFromContext(c *gin.Context) (string, bool) {
	sub := c.GetString(ctxAdmin)
	return sub, sub != ""
}
