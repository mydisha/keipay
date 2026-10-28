package logger

import "go.uber.org/zap"

// Security event names. Keep these stable and namespaced (area.reason) so they
// are easy to search, alert on, and aggregate.
const (
	EventAuthMissingHeaders   = "auth.missing_headers"
	EventAuthInvalidTimestamp = "auth.invalid_timestamp"
	EventAuthTimestampSkew    = "auth.timestamp_skew"
	EventAuthUnknownAPIKey    = "auth.unknown_api_key"
	EventAuthTenantInactive   = "auth.tenant_inactive"
	EventAuthBadSignature     = "auth.bad_signature"
	EventAuthReplay           = "auth.replay"
	EventAuthLookupFailed     = "auth.lookup_failed"
	EventCallbackInvalidToken = "callback.invalid_token"
	EventCallbackMismatch     = "callback.mismatch"
	EventAdminMissingToken    = "admin.missing_token"
	EventAdminInvalidToken    = "admin.invalid_token"
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
