package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
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

// NonceTTL is how long a used signature or nonce is remembered: the whole
// window a timestamp is accepted in (twice the skew), plus the same again
// for clock drift between the server's own nodes.
const NonceTTL = 3 * timestampSkew

// Signing versions of /v1 requests, chosen with X-Signature-Version.
const (
	// SignatureV1 signs "{timestamp}.{body}". The signature itself is the
	// replay nonce, so two identical requests in one second collide.
	SignatureV1 = "1"
	// SignatureV2 signs "{timestamp}.{nonce}.{METHOD}.{path}.{body}" with a
	// client nonce from X-Nonce, so a captured signature fits no other
	// route and identical requests never collide.
	SignatureV2 = "2"
)

// Nonce rules of SignatureV2: printable ASCII without spaces, 16 to 64 bytes.
const (
	minNonceLen = 16
	maxNonceLen = 64
)

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

// NonceStore remembers used nonces for NonceTTL to prevent replay.
type NonceStore interface {
	// Acquire returns true if the nonce is new (not seen within TTL). An
	// error means the answer is unknown, and the request must be refused.
	Acquire(ctx context.Context, key string) (bool, error)
}

// SecretDecryptor recovers the plaintext API secret (HMAC key) from storage.
type SecretDecryptor interface {
	Decrypt(ciphertext string) (string, error)
}

// AuthLimits bound the authentication failures a client IP and an API key
// may accumulate in a window before their requests are refused outright.
// Only failures count (unknown key, bad signature, replay), so a busy app
// with a correct integration never meets them.
type AuthLimits struct {
	MaxFailuresPerIP  int
	MaxFailuresPerKey int
	Window            time.Duration
}

// AppAuthDeps are the collaborators of AppAuth. Limiter may be nil, which
// disables the failure limits; Nonces may not: replay protection is not
// optional, and a nil store refuses every request.
type AppAuthDeps struct {
	Resolver AppResolver
	Nonces   NonceStore
	Cipher   SecretDecryptor
	Limiter  domain.FailureLimiter
	Limits   AuthLimits
	Log      *zap.Logger
}

// AppAuth authenticates app API requests via HMAC-SHA256 over the raw body.
// Required headers: X-Api-Key, X-Timestamp (unix seconds), X-Signature, and
// for X-Signature-Version: 2 also X-Nonce. The signed input depends on the
// version; see SignatureV1 and SignatureV2.
//
// The key's and the app's state are reported only after a valid signature,
// so only the secret's holder learns that a key was revoked. Failures are
// recorded as security events with safe metadata only: public ids and a key
// hint, never the key, the secret or the body. When the nonce store or the
// limiter cannot answer, the request is refused with 503 auth_unavailable:
// skipping the replay check would be worse than a retry.
func AppAuth(d AppAuthDeps) gin.HandlerFunc {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return func(c *gin.Context) {
		ip := c.ClientIP()
		rid := c.GetString(ctxRequestID)
		ctx := c.Request.Context()
		security := func(event, severity, reason string, fields ...zap.Field) {
			logger.Security(log, event, severity, ip, rid, reason, fields...)
		}
		// fail counts an authentication failure against the given limiter
		// keys, outliving the request so a dropped connection still counts.
		fail := func(keys ...string) {
			if d.Limiter == nil {
				return
			}
			if err := d.Limiter.Fail(context.WithoutCancel(ctx), d.Limits.Window, keys...); err != nil {
				log.Warn("auth failure not counted", zap.String(logger.FieldEvent, logger.EventAuthStoreUnavailable),
					zap.String(logger.FieldRequestID, rid), zap.Error(err))
			}
		}
		unavailable := func(reason string, err error, fields ...zap.Field) {
			security(logger.EventAuthStoreUnavailable, logger.SeverityHigh, reason, append(fields, zap.Error(err))...)
			response.Error(c, domain.AuthUnavailable(err))
		}

		apiKey := c.GetHeader("X-Api-Key")
		sig := c.GetHeader("X-Signature")
		tsStr := c.GetHeader("X-Timestamp")
		version := c.GetHeader("X-Signature-Version")
		nonce := c.GetHeader("X-Nonce")
		if version == "" {
			version = SignatureV1
		}
		if version != SignatureV1 && version != SignatureV2 {
			response.Error(c, invalidHeader("X-Signature-Version", "oneof"))
			return
		}
		if apiKey == "" || sig == "" || tsStr == "" || (version == SignatureV2 && nonce == "") {
			security(logger.EventAuthMissingHeaders, logger.SeverityLow, "missing auth headers")
			abort(c, domain.CodeMissingSignatureHeaders, "missing signature headers")
			return
		}
		if version == SignatureV2 && !validNonce(nonce) {
			response.Error(c, invalidHeader("X-Nonce", "format"))
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

		ipKey := "ip:" + ip
		app, key, lookupErr := d.Resolver.Lookup(ctx, apiKey)
		fields := keyFields(app, key)

		// Refuse a client or key with too many recent failures before saying
		// anything about the key, so a blocked caller learns nothing new.
		if d.Limiter != nil {
			limits := []domain.FailureLimit{{Key: ipKey, Max: d.Limits.MaxFailuresPerIP}}
			if lookupErr == nil {
				limits = append(limits, domain.FailureLimit{Key: "key:" + key.Hash, Max: d.Limits.MaxFailuresPerKey})
			}
			blocked, wait, err := d.Limiter.Blocked(ctx, limits...)
			if err != nil {
				unavailable("failure limiter unavailable", err, append(fields, headerPrefix)...)
				return
			}
			if blocked {
				security(logger.EventAuthRateLimited, logger.SeverityMedium, "too many failed attempts", append(fields, headerPrefix)...)
				response.Error(c, domain.RateLimited(int(math.Ceil(wait.Seconds()))))
				return
			}
		}

		switch {
		case errors.Is(lookupErr, service.ErrKeyEnvironmentMismatch):
			security(logger.EventAuthAPIKeyEnvMismatch, logger.SeverityHigh, "api key environment mismatch",
				append(fields, headerPrefix)...)
			fail(ipKey)
			response.Error(c, domain.ErrUnknownAPIKey)
			return
		case errors.Is(lookupErr, domain.ErrUnknownAPIKey):
			security(logger.EventAuthUnknownAPIKey, logger.SeverityMedium, "unknown api key", headerPrefix)
			fail(ipKey)
			response.Error(c, domain.ErrUnknownAPIKey)
			return
		case lookupErr != nil:
			security(logger.EventAuthLookupFailed, logger.SeverityHigh, "auth lookup failed", zap.Error(lookupErr))
			response.Error(c, domain.Internal(fmt.Errorf("auth lookup: %w", lookupErr)))
			return
		}
		keyKey := "key:" + key.Hash

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

		secret, err := d.Cipher.Decrypt(key.SecretEnc)
		if err != nil {
			response.Error(c, domain.Internal(fmt.Errorf("decrypt api secret of %s: %w", key.PublicID, err)))
			return
		}
		if !hmac.Verify(secret, SigningInput(version, tsStr, nonce, c.Request.Method, c.Request.URL.EscapedPath(), body), sig) {
			security(logger.EventAuthBadSignature, logger.SeverityHigh, "bad signature", fields...)
			fail(ipKey, keyKey)
			abort(c, domain.CodeInvalidSignature, "invalid signature")
			return
		}

		if err := d.Resolver.Authorize(app, key); err != nil {
			event, reason := authorizeEvent(err)
			security(event, logger.SeverityMedium, reason, fields...)
			response.Error(c, err)
			return
		}

		// Replay protection. In v1 the signature is unique per (body,
		// timestamp); in v2 the client's nonce is.
		nonceKey := "nonce:" + key.Hash + ":" + sig
		if version == SignatureV2 {
			nonceKey = "nonce:" + key.Hash + ":" + nonce
		}
		if d.Nonces == nil {
			unavailable("no nonce store", errors.New("nonce store not configured"), fields...)
			return
		}
		fresh, err := d.Nonces.Acquire(ctx, nonceKey)
		if err != nil {
			unavailable("nonce store unavailable", err, fields...)
			return
		}
		if !fresh {
			security(logger.EventAuthReplay, logger.SeverityHigh, "replayed request", fields...)
			fail(ipKey, keyKey)
			abort(c, domain.CodeReplayedRequest, "replayed request")
			return
		}

		if err := d.Resolver.Touch(ctx, key); err != nil {
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

// SigningInput is the byte string a /v1 request signs: for SignatureV1
// "{timestamp}.{body}", for SignatureV2
// "{timestamp}.{nonce}.{METHOD}.{path}.{body}" where path is the request
// path without the query string.
func SigningInput(version, timestamp, nonce, method, path string, body []byte) []byte {
	var prefix string
	if version == SignatureV2 {
		prefix = timestamp + "." + nonce + "." + method + "." + path + "."
	} else {
		prefix = timestamp + "."
	}
	return append([]byte(prefix), body...)
}

// validNonce reports whether a v2 nonce is 16 to 64 printable ASCII bytes
// without spaces.
func validNonce(nonce string) bool {
	if len(nonce) < minNonceLen || len(nonce) > maxNonceLen {
		return false
	}
	for i := 0; i < len(nonce); i++ {
		if nonce[i] < 0x21 || nonce[i] > 0x7e {
			return false
		}
	}
	return true
}

// invalidHeader is the invalid_request error of a header with a bad value.
func invalidHeader(name, rule string) *domain.Error {
	err := domain.InvalidFields(domain.FieldError{Field: name, Rule: rule})
	err.Message = "invalid request header"
	return err
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
