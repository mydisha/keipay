package domain

import "errors"

var (
	// ErrNotFound is returned when a requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned on unique-constraint or duplicate violations.
	ErrConflict = errors.New("conflict")
	// ErrInvalidInput is returned when input validation fails.
	ErrInvalidInput = errors.New("invalid input")
	// ErrUnauthorized is returned when authentication or signature checks fail.
	ErrUnauthorized = errors.New("unauthorized")
)

// ErrorCode is a stable, machine-readable error code sent to API clients. The
// set must equal the ErrorCode enum in api/openapi.yaml.
type ErrorCode string

// Error codes. Existing codes keep their meaning; new ones may be added.
const (
	CodeInvalidRequest          ErrorCode = "invalid_request"
	CodeValidationFailed        ErrorCode = "validation_failed"
	CodeInvalidCursor           ErrorCode = "invalid_cursor"
	CodeUnauthorized            ErrorCode = "unauthorized"
	CodeInvalidCredentials      ErrorCode = "invalid_credentials"
	CodeMissingToken            ErrorCode = "missing_token"
	CodeInvalidToken            ErrorCode = "invalid_token"
	CodeMissingSignatureHeaders ErrorCode = "missing_signature_headers"
	CodeInvalidTimestamp        ErrorCode = "invalid_timestamp"
	CodeTimestampOutOfRange     ErrorCode = "timestamp_out_of_range"
	CodeUnknownAPIKey           ErrorCode = "unknown_api_key"
	CodeInvalidSignature        ErrorCode = "invalid_signature"
	CodeReplayedRequest         ErrorCode = "replayed_request"
	CodeInvalidCallbackToken    ErrorCode = "invalid_callback_token"
	CodeTenantInactive          ErrorCode = "tenant_inactive"
	CodeNotFound                ErrorCode = "not_found"
	CodeConflict                ErrorCode = "conflict"
	CodeInternal                ErrorCode = "internal_error"
	CodeSessionRequired         ErrorCode = "session_required"
	CodeSessionExpired          ErrorCode = "session_expired"
	CodeForbidden               ErrorCode = "forbidden"
	CodePasswordChangeRequired  ErrorCode = "password_change_required"
	CodeInvalidCSRFToken        ErrorCode = "invalid_csrf_token"
	CodeInvalidSetupToken       ErrorCode = "invalid_setup_token"
	CodeSetupCompleted          ErrorCode = "setup_completed"
	CodeLastOwner               ErrorCode = "last_owner"
	CodeCannotModifySelf        ErrorCode = "cannot_modify_self"
	CodeRateLimited             ErrorCode = "rate_limited"
	CodeAPIKeyRevoked           ErrorCode = "api_key_revoked"
	CodeAPIKeyExpired           ErrorCode = "api_key_expired"
	CodeIdempotencyKeyReused    ErrorCode = "idempotency_key_reused"
	CodeConnectionInUse         ErrorCode = "connection_in_use"
	CodeDeliveryNotReplayable   ErrorCode = "delivery_not_replayable"
	CodeNoEligibleConnection    ErrorCode = "no_eligible_connection"
	CodeProviderRejected        ErrorCode = "provider_rejected"
	CodeProviderUnavailable     ErrorCode = "provider_unavailable"
)

// ErrorCodes returns every defined error code.
func ErrorCodes() []ErrorCode {
	return []ErrorCode{
		CodeInvalidRequest, CodeValidationFailed, CodeInvalidCursor,
		CodeUnauthorized, CodeInvalidCredentials, CodeMissingToken, CodeInvalidToken,
		CodeMissingSignatureHeaders, CodeInvalidTimestamp, CodeTimestampOutOfRange,
		CodeUnknownAPIKey, CodeInvalidSignature, CodeReplayedRequest,
		CodeInvalidCallbackToken, CodeTenantInactive, CodeNotFound, CodeConflict,
		CodeInternal, CodeSessionRequired, CodeSessionExpired, CodeForbidden,
		CodePasswordChangeRequired, CodeInvalidCSRFToken, CodeInvalidSetupToken,
		CodeSetupCompleted, CodeLastOwner, CodeCannotModifySelf, CodeRateLimited,
		CodeAPIKeyRevoked, CodeAPIKeyExpired, CodeIdempotencyKeyReused,
		CodeConnectionInUse, CodeDeliveryNotReplayable, CodeNoEligibleConnection, CodeProviderRejected, CodeProviderUnavailable,
	}
}

// Error is an error with a stable code for API clients. Message is safe to show
// to the caller. Err, when set, is the underlying sentinel or cause, so
// errors.Is keeps working through it.
type Error struct {
	Code    ErrorCode
	Message string
	Details map[string]any
	Err     error
}

// NewError returns a coded error.
func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// Is matches any *Error with the same code, so errors.Is(err, ErrInvalidCursor)
// holds for every invalid-cursor error regardless of its message.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Internal marks err as an internal failure: it maps to 500 internal_error
// even when it wraps a sentinel that would otherwise be a client error.
func Internal(err error) *Error {
	return &Error{Code: CodeInternal, Message: "internal server error", Err: err}
}

// FieldError names one request field that failed validation.
type FieldError struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
}

// InvalidFields returns an invalid_request error listing the offending
// fields. details.fields is always a list.
func InvalidFields(fields ...FieldError) *Error {
	if fields == nil {
		fields = []FieldError{}
	}
	return &Error{
		Code:    CodeInvalidRequest,
		Message: "invalid request body",
		Details: map[string]any{"fields": fields},
		Err:     ErrInvalidInput,
	}
}

// RateLimited returns a rate_limited error asking the caller to retry after
// the given number of seconds (at least 1).
func RateLimited(retryAfterSeconds int) *Error {
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	return &Error{
		Code:    CodeRateLimited,
		Message: "too many failed attempts",
		Details: map[string]any{"retry_after_seconds": retryAfterSeconds},
	}
}

// Reasons reported by no_eligible_connection in details.reasons.
const (
	ReasonNoDefaultConnection = "no_default_connection"
	ReasonConnectionDisabled  = "connection_disabled"
)

// NoEligibleConnection is a payment no connection can take, with the reasons.
func NoEligibleConnection(reasons ...string) *Error {
	return &Error{
		Code:    CodeNoEligibleConnection,
		Message: "no connection can take this payment",
		Details: map[string]any{"reasons": reasons},
	}
}

// ProviderRejected is a request the gateway refused. cause stays available to
// errors.Is and logs; the message never carries the gateway's text.
func ProviderRejected(provider string, cause error) *Error {
	return &Error{
		Code:    CodeProviderRejected,
		Message: "the payment provider rejected the request",
		Details: map[string]any{"provider": provider},
		Err:     cause,
	}
}

// ProviderUnavailable is a gateway that could not be reached, timed out or
// failed on its side.
func ProviderUnavailable(cause error) *Error {
	return &Error{Code: CodeProviderUnavailable, Message: "the payment provider is unavailable; try again later", Err: cause}
}

// ConnectionInUse refuses disabling a connection that apps use as their
// default. apps holds at most MaxInUseApps of them; count is the total.
func ConnectionInUse(apps []AppSummary, count int) *Error {
	listed := make([]map[string]any, 0, len(apps))
	for _, a := range apps {
		listed = append(listed, map[string]any{"id": a.PublicID, "slug": a.Slug, "name": a.Name, "environment": a.Environment})
	}
	return &Error{
		Code:    CodeConnectionInUse,
		Message: "the connection is the default connection of one or more apps",
		Details: map[string]any{"apps": listed, "app_count": count},
		Err:     ErrConflict,
	}
}

// Reasons reported by delivery_not_replayable in details.reason.
const (
	ReplayReasonInProgress      = "in_progress"
	ReplayReasonNoEndpoint      = "no_endpoint"
	ReplayReasonEndpointDeleted = "endpoint_deleted"
	ReplayReasonDisabled        = "endpoint_disabled"
)

// DeliveryNotReplayable refuses a replay for reason.
func DeliveryNotReplayable(reason string) *Error {
	return &Error{
		Code:    CodeDeliveryNotReplayable,
		Message: "the delivery cannot be replayed now",
		Details: map[string]any{"reason": reason},
		Err:     ErrConflict,
	}
}

var (
	// ErrInvalidCredentials is a wrong email, password or current password.
	ErrInvalidCredentials = &Error{Code: CodeInvalidCredentials, Message: "invalid credentials", Err: ErrUnauthorized}
	// ErrInvalidCursor is a pagination cursor that cannot be decoded.
	ErrInvalidCursor = &Error{Code: CodeInvalidCursor, Message: "invalid cursor", Err: ErrInvalidInput}
	// ErrSessionRequired is an admin request without a session cookie.
	ErrSessionRequired = &Error{Code: CodeSessionRequired, Message: "sign in required", Err: ErrUnauthorized}
	// ErrSessionExpired is a session that is unknown, timed out or revoked.
	ErrSessionExpired = &Error{Code: CodeSessionExpired, Message: "session expired", Err: ErrUnauthorized}
	// ErrForbidden is a signed-in user whose role lacks the permission.
	ErrForbidden = &Error{Code: CodeForbidden, Message: "forbidden"}
	// ErrPasswordChangeRequired blocks a user who must change their password first.
	ErrPasswordChangeRequired = &Error{Code: CodePasswordChangeRequired, Message: "password change required"}
	// ErrInvalidCSRFToken is a missing or mismatched CSRF token.
	ErrInvalidCSRFToken = &Error{Code: CodeInvalidCSRFToken, Message: "invalid csrf token"}
	// ErrInvalidSetupToken is a missing or wrong first-run setup token.
	ErrInvalidSetupToken = &Error{Code: CodeInvalidSetupToken, Message: "invalid setup token"}
	// ErrSetupCompleted means the first owner already exists.
	ErrSetupCompleted = &Error{Code: CodeSetupCompleted, Message: "setup already completed"}
	// ErrLastOwner refuses a change that would leave no active owner.
	ErrLastOwner = &Error{Code: CodeLastOwner, Message: "the change would leave no active owner"}
	// ErrUnknownAPIKey is an X-Api-Key that matches no key of the right environment.
	ErrUnknownAPIKey = &Error{Code: CodeUnknownAPIKey, Message: "unknown api key", Err: ErrUnauthorized}
	// ErrAPIKeyRevoked is a key that was revoked.
	ErrAPIKeyRevoked = &Error{Code: CodeAPIKeyRevoked, Message: "api key revoked", Err: ErrUnauthorized}
	// ErrAPIKeyExpired is a key whose expires_at has passed.
	ErrAPIKeyExpired = &Error{Code: CodeAPIKeyExpired, Message: "api key expired", Err: ErrUnauthorized}
	// ErrAppDisabled is a /v1 request for a disabled app. The code predates
	// apps and keeps its name.
	ErrAppDisabled = &Error{Code: CodeTenantInactive, Message: "app disabled"}
	// ErrIdempotencyKeyReused is an Idempotency-Key sent again with a
	// different request.
	ErrIdempotencyKeyReused = &Error{Code: CodeIdempotencyKeyReused, Message: "idempotency key already used for a different request", Err: ErrConflict}
	// ErrCannotModifySelf refuses disabling or resetting your own account.
	ErrCannotModifySelf = &Error{Code: CodeCannotModifySelf, Message: "you cannot do this to your own account"}
	// ErrInvalidCallbackToken is a gateway callback that fails its
	// connection's verification.
	ErrInvalidCallbackToken = &Error{Code: CodeInvalidCallbackToken, Message: "invalid callback token", Err: ErrUnauthorized}
)
