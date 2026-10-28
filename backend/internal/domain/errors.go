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
	// ErrInactiveTenant is returned when a tenant exists but is disabled.
	ErrInactiveTenant = errors.New("tenant inactive")
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
	// ErrCannotModifySelf refuses disabling or resetting your own account.
	ErrCannotModifySelf = &Error{Code: CodeCannotModifySelf, Message: "you cannot do this to your own account"}
)
