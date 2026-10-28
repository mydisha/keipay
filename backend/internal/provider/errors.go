package provider

import (
	"errors"
	"fmt"
)

// Error classes. Adapters wrap every gateway failure in exactly one of the
// first four with %w; callers decide with errors.Is.
var (
	// ErrRejected: the gateway refused the request (validation, limits,
	// credentials). Retrying the same request will not help.
	ErrRejected = errors.New("provider rejected the request")
	// ErrRetryable: nothing happened at the gateway (unreachable, rate
	// limited); the same request may be retried.
	ErrRetryable = errors.New("provider unavailable")
	// ErrUnknownOutcome: the request may or may not have taken effect
	// (timeout or server error after sending).
	ErrUnknownOutcome = errors.New("provider outcome unknown")
	// ErrNotFound: the gateway does not know the referenced object.
	ErrNotFound = errors.New("provider object not found")
	// ErrInvalidSignature: a callback failed verification.
	ErrInvalidSignature = errors.New("invalid callback signature")
	// ErrUnknownProvider: no adapter is registered under the name.
	ErrUnknownProvider = errors.New("unknown provider")
)

// StatusError is a gateway answer with an HTTP status. It matches its class
// with errors.Is and keeps the status for messages.
type StatusError struct {
	Class  error
	Status int
	// Message is the gateway's own text, for logs only.
	Message string
}

// NewStatusError returns a StatusError of class for an HTTP answer.
func NewStatusError(class error, status int, message string) *StatusError {
	return &StatusError{Class: class, Status: status, Message: message}
}

func (e *StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%v (HTTP %d)", e.Class, e.Status)
	}
	return fmt.Sprintf("%v (HTTP %d): %s", e.Class, e.Status, e.Message)
}

func (e *StatusError) Unwrap() error { return e.Class }

// StatusCode returns the HTTP status carried by err, if any.
func StatusCode(err error) (int, bool) {
	var se *StatusError
	if errors.As(err, &se) && se.Status > 0 {
		return se.Status, true
	}
	return 0, false
}
