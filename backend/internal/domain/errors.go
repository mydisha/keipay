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
)

// ErrorCodes returns every defined error code.
func ErrorCodes() []ErrorCode {
	return []ErrorCode{
		CodeInvalidRequest, CodeValidationFailed, CodeInvalidCursor,
		CodeUnauthorized, CodeInvalidCredentials, CodeMissingToken, CodeInvalidToken,
		CodeMissingSignatureHeaders, CodeInvalidTimestamp, CodeTimestampOutOfRange,
		CodeUnknownAPIKey, CodeInvalidSignature, CodeReplayedRequest,
		CodeInvalidCallbackToken, CodeTenantInactive, CodeNotFound, CodeConflict,
		CodeInternal,
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

var (
	// ErrInvalidCredentials is a wrong username, password or current password.
	ErrInvalidCredentials = &Error{Code: CodeInvalidCredentials, Message: "invalid credentials", Err: ErrUnauthorized}
	// ErrInvalidCursor is a pagination cursor that cannot be decoded.
	ErrInvalidCursor = &Error{Code: CodeInvalidCursor, Message: "invalid cursor", Err: ErrInvalidInput}
)
