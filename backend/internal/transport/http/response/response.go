// Package response writes HTTP responses. Error is the only place that maps
// errors to status codes and the error envelope:
//
//	{"error":{"code":"not_found","message":"not found","details":{}}}
//
// Usage in handlers:
//
//	if err := svc.DoSomething(ctx); err != nil {
//	    response.Error(c, err)
//	    return
//	}
//	response.JSON(c, http.StatusOK, data)
package response

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Envelope is the body of every error response.
type Envelope struct {
	Error Body `json:"error"`
}

// Body carries the stable code, a human-readable message and details, which
// is always an object.
type Body struct {
	Code    domain.ErrorCode `json:"code"`
	Message string           `json:"message"`
	Details map[string]any   `json:"details"`
}

const internalMessage = "internal server error"

var statusByCode = map[domain.ErrorCode]int{
	domain.CodeInvalidRequest:          http.StatusBadRequest,
	domain.CodeValidationFailed:        http.StatusBadRequest,
	domain.CodeInvalidCursor:           http.StatusBadRequest,
	domain.CodeUnauthorized:            http.StatusUnauthorized,
	domain.CodeInvalidCredentials:      http.StatusUnauthorized,
	domain.CodeMissingToken:            http.StatusUnauthorized,
	domain.CodeInvalidToken:            http.StatusUnauthorized,
	domain.CodeMissingSignatureHeaders: http.StatusUnauthorized,
	domain.CodeInvalidTimestamp:        http.StatusUnauthorized,
	domain.CodeTimestampOutOfRange:     http.StatusUnauthorized,
	domain.CodeUnknownAPIKey:           http.StatusUnauthorized,
	domain.CodeInvalidSignature:        http.StatusUnauthorized,
	domain.CodeReplayedRequest:         http.StatusUnauthorized,
	domain.CodeInvalidCallbackToken:    http.StatusUnauthorized,
	domain.CodeTenantInactive:          http.StatusForbidden,
	domain.CodeNotFound:                http.StatusNotFound,
	domain.CodeConflict:                http.StatusConflict,
	domain.CodeInternal:                http.StatusInternalServerError,
	domain.CodeSessionRequired:         http.StatusUnauthorized,
	domain.CodeSessionExpired:          http.StatusUnauthorized,
	domain.CodeForbidden:               http.StatusForbidden,
	domain.CodePasswordChangeRequired:  http.StatusForbidden,
	domain.CodeInvalidCSRFToken:        http.StatusForbidden,
	domain.CodeInvalidSetupToken:       http.StatusForbidden,
	domain.CodeSetupCompleted:          http.StatusConflict,
	domain.CodeLastOwner:               http.StatusConflict,
	domain.CodeCannotModifySelf:        http.StatusConflict,
	domain.CodeRateLimited:             http.StatusTooManyRequests,
	domain.CodeAPIKeyRevoked:           http.StatusUnauthorized,
	domain.CodeAPIKeyExpired:           http.StatusUnauthorized,
	domain.CodeIdempotencyKeyReused:    http.StatusConflict,
}

// Error writes the envelope for err and aborts the request. Unrecognised
// errors become 500 internal_error with a generic message; the real error is
// attached to the gin context so the access log records it with the request id.
func Error(c *gin.Context, err error) {
	status, body := resolve(err)
	if status >= http.StatusInternalServerError {
		_ = c.Error(err)
	}
	if body.Code == domain.CodeRateLimited {
		if secs, ok := body.Details["retry_after_seconds"].(int); ok {
			c.Header("Retry-After", strconv.Itoa(secs))
		}
	}
	c.AbortWithStatusJSON(status, Envelope{Error: body})
}

func resolve(err error) (int, Body) {
	var coded *domain.Error
	switch {
	case errors.As(err, &coded):
		status, ok := statusByCode[coded.Code]
		if !ok || status >= http.StatusInternalServerError {
			return internal()
		}
		return status, body(coded.Code, coded.Message, coded.Details)
	case errors.Is(err, domain.ErrInvalidInput):
		// Validation messages are written by the service for the caller.
		return http.StatusBadRequest, body(domain.CodeValidationFailed, err.Error(), nil)
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, body(domain.CodeNotFound, "not found", nil)
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, body(domain.CodeConflict, "conflict", nil)
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, body(domain.CodeUnauthorized, "unauthorized", nil)
	default:
		return internal()
	}
}

func internal() (int, Body) {
	return http.StatusInternalServerError, body(domain.CodeInternal, internalMessage, nil)
}

func body(code domain.ErrorCode, message string, details map[string]any) Body {
	if details == nil {
		details = map[string]any{}
	}
	return Body{Code: code, Message: message, Details: details}
}

// JSON sends a JSON response with the given status code.
func JSON(c *gin.Context, status int, data any) {
	c.JSON(status, data)
}

// Success sends a 200 OK response with data.
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, data)
}

// Created sends a 201 Created response with data.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, data)
}

// Accepted sends a 202 Accepted response with data.
func Accepted(c *gin.Context, data any) {
	c.JSON(http.StatusAccepted, data)
}
