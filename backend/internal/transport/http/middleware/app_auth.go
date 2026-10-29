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
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/response"
)

// timestampSkew is the max allowed clock skew for request signatures.
const timestampSkew = 5 * time.Minute

// AppResolver resolves and checks the API key of a /v1 request. It is split
// so the signature can be verified between Lookup and Authorize.
type AppResolver interface {
	// Lookup finds the key and its app by the raw X-Api-Key.
	Lookup(ctx context.Context, rawKey string) (*domain.App, *domain.APIKey, error)
	// Authorize checks the key's and the app's state.
	Authorize(app *domain.App, key *domain.APIKey) error
	// Touch records the key's use; failures never fail the request.
	Touch(ctx context.Context, key *domain.APIKey) error
}

// NonceStore records used (key, signature) pairs to prevent replay.
type NonceStore interface {
	// Acquire returns true if the nonce is new (not seen within TTL).
	Acquire(ctx context.Context, key string) bool
}

// SecretDecryptor recovers the plaintext API secret (HMAC key) from storage.
type SecretDecryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// AppAuth authenticates app API requests via HMAC-SHA256 over the raw body.
// Required headers: X-Api-Key, X-Timestamp (unix seconds), X-Signature.
// Signature = HMAC(api secret, timestamp + "." + rawBody).
//
// The key's and the app's state are reported only after a valid signature,
// so only the secret's holder learns that a key was revoked. Failures are
// recorded as security events with safe metadata only: public ids and a key
// hint, never the key, the secret or the body.
func AppAuth(resolver AppResolver, nonces NonceStore, cipher SecretDecryptor, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rid := c.GetString(ctxRequestID)
		ctx := c.Request.Context()
		security := func(event, severity, reason string, fields ...zap.Field) {
			logger.Security(log, event, severity, ip, rid, reason, fields...)
		}

		apiKey := c.GetHeader("X-Api-Key")
		sig := c.GetHeader("X-Signature")
		tsStr := c.GetHeader("X-Timestamp")
		if apiKey == "" || sig == "" || tsStr == "" {
			security(logger.EventAuthMissingHeaders, logger.SeverityLow, "missing auth headers")
			abort(c, domain.CodeMissingSignatureHeaders, "missing signature headers")
			return
		}
		headerPrefix := zap.String("api_key_prefix", apiKeyPrefix(apiKey))

		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			security(logger.EventAuthInvalidTimestamp, logger.SeverityLow, "invalid timestamp", headerPrefix)
			abort(c, domain.CodeInvalidTimestamp, "invalid timestamp")
			return
		}
		if d := time.Since(time.Unix(ts, 0)); d > timestampSkew || d < -timestampSkew {
			security(logger.EventAuthTimestampSkew, logger.SeverityMedium, "timestamp out of range", headerPrefix)
			abort(c, domain.CodeTimestampOutOfRange, "timestamp out of range")
			return
		}

		app, key, err := resolver.Lookup(ctx, apiKey)
		switch {
		case errors.Is(err, service.ErrKeyEnvironmentMismatch):
			security(logger.EventAuthAPIKeyEnvMismatch, logger.SeverityHigh, "api key environment mismatch",
				append(keyFields(app, key), headerPrefix)...)
			response.Error(c, domain.ErrUnknownAPIKey)
			return
		case errors.Is(err, domain.ErrUnknownAPIKey):
			security(logger.EventAuthUnknownAPIKey, logger.SeverityMedium, "unknown api key", headerPrefix)
			response.Error(c, domain.ErrUnknownAPIKey)
			return
		case err != nil:
			security(logger.EventAuthLookupFailed, logger.SeverityHigh, "auth lookup failed", zap.Error(err))
			response.Error(c, domain.Internal(fmt.Errorf("auth lookup: %w", err)))
			return
		}
		fields := keyFields(app, key)

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			if TooLarge(err) {
				response.Error(c, domain.ErrRequestTooLarge)
				return
			}
			abort(c, domain.CodeInvalidRequest, "cannot read body")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body)) // restore for handler

		secret, err := cipher.Decrypt(key.SecretEnc)
		if err != nil {
			response.Error(c, domain.Internal(fmt.Errorf("decrypt api secret of %s: %w", key.PublicID, err)))
			return
		}
		signingInput := append([]byte(tsStr+"."), body...)
		if !hmac.Verify(secret, signingInput, sig) {
			security(logger.EventAuthBadSignature, logger.SeverityHigh, "bad signature", fields...)
			abort(c, domain.CodeInvalidSignature, "invalid signature")
			return
		}

		if err := resolver.Authorize(app, key); err != nil {
			event, reason := authorizeEvent(err)
			security(event, logger.SeverityMedium, reason, fields...)
			response.Error(c, err)
			return
		}

		// Replay protection: the signature is unique per (body, timestamp).
		if nonces != nil && !nonces.Acquire(ctx, "nonce:"+key.Hash+":"+sig) {
			security(logger.EventAuthReplay, logger.SeverityHigh, "replayed request", fields...)
			abort(c, domain.CodeReplayedRequest, "replayed request")
			return
		}

		if err := resolver.Touch(ctx, key); err != nil {
			log.Warn("api key touch failed", append(fields,
				zap.String(logger.FieldEvent, logger.EventAPIKeyTouchFailed),
				zap.String(logger.FieldRequestID, rid),
				zap.Error(err))...)
		}

		c.Set(ctxApp, app)
		c.Set(ctxAPIKey, key)
		c.Next()
	}
}

func authorizeEvent(err error) (event, reason string) {
	switch {
	case errors.Is(err, domain.ErrAPIKeyRevoked):
		return logger.EventAuthAPIKeyRevoked, "api key revoked"
	case errors.Is(err, domain.ErrAPIKeyExpired):
		return logger.EventAuthAPIKeyExpired, "api key expired"
	default:
		return logger.EventAuthAppDisabled, "app disabled"
	}
}

// keyFields identifies the app and key in logs without revealing the key.
func keyFields(app *domain.App, key *domain.APIKey) []zap.Field {
	var fields []zap.Field
	if app != nil {
		fields = append(fields, zap.String(logger.FieldAppID, app.PublicID))
	}
	if key != nil {
		fields = append(fields, zap.String(logger.FieldAPIKeyID, key.PublicID), zap.String(logger.FieldKeyHint, key.Hint()))
	}
	return fields
}

// apiKeyPrefix returns a short, non-sensitive prefix of an API key for logging.
func apiKeyPrefix(k string) string {
	if len(k) <= 8 {
		return k
	}
	return k[:8] + "..."
}

// AppFromContext returns the authenticated app set by AppAuth.
func AppFromContext(c *gin.Context) (*domain.App, bool) {
	v, ok := c.Get(ctxApp)
	if !ok {
		return nil, false
	}
	a, ok := v.(*domain.App)
	return a, ok
}

// APIKeyFromContext returns the API key that authenticated the request.
func APIKeyFromContext(c *gin.Context) (*domain.APIKey, bool) {
	v, ok := c.Get(ctxAPIKey)
	if !ok {
		return nil, false
	}
	k, ok := v.(*domain.APIKey)
	return k, ok
}

func abort(c *gin.Context, code domain.ErrorCode, msg string) {
	response.Error(c, domain.NewError(code, msg))
}
