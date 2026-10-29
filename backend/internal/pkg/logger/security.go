package logger

import "go.uber.org/zap"

// Security event names. Keep these stable and namespaced (area.reason) so they
// are easy to search, alert on, and aggregate.
const (
	EventAuthMissingHeaders    = "auth.missing_headers"
	EventAuthInvalidTimestamp  = "auth.invalid_timestamp"
	EventAuthTimestampSkew     = "auth.timestamp_skew"
	EventAuthUnknownAPIKey     = "auth.unknown_api_key"
	EventAuthAppDisabled       = "auth.app_disabled"
	EventAuthAPIKeyRevoked     = "auth.api_key_revoked"
	EventAuthAPIKeyExpired     = "auth.api_key_expired"
	EventAuthAPIKeyEnvMismatch = "auth.api_key_env_mismatch"
	EventAPIKeyTouchFailed     = "api_key.touch_failed"
	EventAuthBadSignature      = "auth.bad_signature"
	EventAuthReplay            = "auth.replay"
	EventAuthLookupFailed      = "auth.lookup_failed"
	// EventAuthRateLimited is a /v1 caller refused because too many of its
	// recent requests failed authentication.
	EventAuthRateLimited = "auth.rate_limited"
	// EventAuthStoreUnavailable is a /v1 request refused because the store
	// behind replay protection or the failure limits did not answer.
	EventAuthStoreUnavailable = "auth.store_unavailable"
	// EventCallbackRateLimited is a rejected callback dropped because the
	// connection or the sender already had too many rejected ones.
	EventCallbackRateLimited  = "callback.rate_limited"
	EventCallbackInvalidToken = "callback.invalid_token"
	EventCallbackMismatch     = "callback.mismatch"
	// EventCallbackUnknownConnection is a callback to a URL naming no
	// connection of that provider: usually a stale dashboard setting.
	EventCallbackUnknownConnection = "callback.unknown_connection"
	EventAdminSessionMissing       = "admin.session_missing"
	EventAdminSessionInvalid       = "admin.session_invalid"
	EventAdminCSRFFailed           = "admin.csrf_failed"
	EventAdminForbidden            = "admin.forbidden"
	EventLoginFailed               = "auth.login_failed"
	EventLoginRateLimited          = "auth.login_rate_limited"
	EventSetupRateLimited          = "auth.setup_rate_limited"
	EventSetupTokenInvalid         = "auth.setup_token_invalid"
)

// Severity levels for security events.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Security emits a structured security log line. It only records safe metadata
// (event, severity, client ip, request id, reason) and never request bodies or
// secrets, so logs cannot leak sensitive data.
//
// Caller passes any extra fields (e.g. api key prefix, path) as zap fields.
func Security(log *zap.Logger, event, severity, clientIP, requestID, reason string, extra ...zap.Field) {
	if log == nil {
		return
	}
	fields := make([]zap.Field, 0, 6+len(extra))
	fields = append(fields,
		zap.String(FieldLogType, LogTypeSecurity),
		zap.String(FieldEvent, event),
		zap.String(FieldSeverity, severity),
		zap.String(FieldClientIP, clientIP),
		zap.String(FieldRequestID, requestID),
		zap.String("reason", reason),
	)
	fields = append(fields, extra...)

	msg := "security event: " + event
	switch severity {
	case SeverityCritical, SeverityHigh:
		log.Error(msg, fields...)
	default:
		log.Warn(msg, fields...)
	}
}
