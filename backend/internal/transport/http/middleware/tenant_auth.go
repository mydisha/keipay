package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// timestampSkew is the max allowed clock skew for request signatures.
const timestampSkew = 5 * time.Minute

// TenantResolver loads a tenant by its public API key (cache-backed).
type TenantResolver interface {
	GetByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error)
}

// NonceStore records used (apiKey, signature) pairs to prevent replay.
type NonceStore interface {
	// Acquire returns true if the nonce is new (not seen within TTL).
	Acquire(ctx context.Context, key string) bool
}

// SecretDecryptor recovers the plaintext tenant secret (HMAC key) from storage.
type SecretDecryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// TenantAuth authenticates tenant API requests via HMAC-SHA256 over the raw body.
// Required headers: X-Api-Key, X-Timestamp (unix seconds), X-Signature.
// Signature = HMAC(secret, timestamp + "." + rawBody).
//
// Authentication failures are recorded as structured security events (only safe
// metadata, never secrets or bodies) so threats can be searched and alerted on.
func TenantAuth(resolver TenantResolver, nonces NonceStore, cipher SecretDecryptor, log *zap.Logger) gin.HandlerFunc {

	return func(c *gin.Context) {
		ip := c.ClientIP()
		rid := c.GetString(ctxRequestID)
		// apiKeyField masks the key so logs stay searchable without leaking it.
		apiKeyField := func(k string) zap.Field { return zap.String("api_key_prefix", apiKeyPrefix(k)) }

		apiKey := c.GetHeader("X-Api-Key")
		sig := c.GetHeader("X-Signature")
		tsStr := c.GetHeader("X-Timestamp")
		if apiKey == "" || sig == "" || tsStr == "" {
			logger.Security(log, logger.EventAuthMissingHeaders, logger.SeverityLow, ip, rid, "missing auth headers")
			abort(c, domain.CodeMissingSignatureHeaders, "missing signature headers")
			return
		}

		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			logger.Security(log, logger.EventAuthInvalidTimestamp, logger.SeverityLow, ip, rid, "invalid timestamp", apiKeyField(apiKey))
			abort(c, domain.CodeInvalidTimestamp, "invalid timestamp")
			return
		}
		if d := time.Since(time.Unix(ts, 0)); d > timestampSkew || d < -timestampSkew {
			logger.Security(log, logger.EventAuthTimestampSkew, logger.SeverityMedium, ip, rid, "timestamp out of range", apiKeyField(apiKey))
			abort(c, domain.CodeTimestampOutOfRange, "timestamp out of range")
			return
		}

		tenant, err := resolver.GetByAPIKey(c.Request.Context(), apiKey)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				logger.Security(log, logger.EventAuthUnknownAPIKey, logger.SeverityMedium, ip, rid, "unknown api key", apiKeyField(apiKey))
				abort(c, domain.CodeUnknownAPIKey, "unknown api key")
				return
			}
			logger.Security(log, logger.EventAuthLookupFailed, logger.SeverityHigh, ip, rid, "auth lookup failed", zap.Error(err))
			response.Error(c, domain.Internal(fmt.Errorf("auth lookup: %w", err)))
			return
		}
		if !tenant.Active {
			logger.Security(log, logger.EventAuthTenantInactive, logger.SeverityMedium, ip, rid, "tenant inactive", zap.Int64(logger.FieldTenantID, tenant.ID))
			abort(c, domain.CodeTenantInactive, "tenant inactive")
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			abort(c, domain.CodeUnauthorized, "cannot read body")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body)) // restore for handler

		secret, err := cipher.Decrypt(tenant.APISecretEnc)
		if err != nil {
			response.Error(c, domain.Internal(fmt.Errorf("decrypt tenant secret: %w", err)))
			return
		}

		signingInput := append([]byte(tsStr+"."), body...)
		if !hmac.Verify(secret, signingInput, sig) {
			logger.Security(log, logger.EventAuthBadSignature, logger.SeverityHigh, ip, rid, "bad signature", zap.Int64(logger.FieldTenantID, tenant.ID))
			abort(c, domain.CodeInvalidSignature, "invalid signature")
			return
		}

		// Replay protection: signature is unique per (body, timestamp).
		if nonces != nil && !nonces.Acquire(c.Request.Context(), "nonce:"+apiKey+":"+sig) {
			logger.Security(log, logger.EventAuthReplay, logger.SeverityHigh, ip, rid, "replayed request", zap.Int64(logger.FieldTenantID, tenant.ID))
			abort(c, domain.CodeReplayedRequest, "replayed request")
			return
		}

		c.Set(ctxTenant, tenant)
		c.Next()
	}
}

// apiKeyPrefix returns a short, non-sensitive prefix of an API key for logging.
func apiKeyPrefix(k string) string {
	if len(k) <= 8 {
		return k
	}
	return k[:8] + "..."
}

// TenantFromContext returns the authenticated tenant set by TenantAuth.
func TenantFromContext(c *gin.Context) (*domain.Tenant, bool) {
	v, ok := c.Get(ctxTenant)
	if !ok {
		return nil, false
	}
	t, ok := v.(*domain.Tenant)
	return t, ok
}

func abort(c *gin.Context, code domain.ErrorCode, msg string) {
	response.Error(c, domain.NewError(code, msg))
}
