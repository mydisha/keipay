package middleware

import (
	"crypto/subtle"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// XenditCallbackToken verifies the x-callback-token header against the account
// token configured in Xendit. This authenticates that callbacks originate from
// Xendit (the token is global per account, not per transaction). Rejected
// callbacks are logged as security events for threat monitoring.
func XenditCallbackToken(expected string, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader("x-callback-token")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			logger.Security(log, logger.EventCallbackInvalidToken, logger.SeverityHigh,
				c.ClientIP(), c.GetString(ctxRequestID), "invalid callback token")
			response.Error(c, domain.NewError(domain.CodeInvalidCallbackToken, "invalid callback token"))
			return
		}
		c.Next()
	}
}
