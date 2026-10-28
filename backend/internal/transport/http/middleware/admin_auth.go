package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// JWTVerifier validates an admin session token and returns the subject.
type JWTVerifier interface {
	Verify(token string) (string, error)
}

// AdminAuth protects admin routes using a Bearer JWT issued at login. Rejected
// requests are logged as security events to surface unauthorized access
// attempts against the admin surface.
func AdminAuth(verifier JWTVerifier, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rid := c.GetString(ctxRequestID)

		h := c.GetHeader("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
		if token == "" || token == h {
			logger.Security(log, logger.EventAdminMissingToken, logger.SeverityMedium, ip, rid, "missing bearer token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		sub, err := verifier.Verify(token)
		if err != nil {
			logger.Security(log, logger.EventAdminInvalidToken, logger.SeverityHigh, ip, rid, "invalid token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Set("admin", sub)
		c.Next()
	}
}
