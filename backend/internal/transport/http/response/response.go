// Package response provides centralized HTTP response handling.
//
// This package standardizes how the API responds to clients, ensuring:
// - Consistent error mapping from domain errors to HTTP status codes
// - Uniform JSON response structure
// - Proper logging context
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

	"github.com/gin-gonic/gin"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// JSON sends a JSON response with the given status code.
func JSON(c *gin.Context, status int, data any) {
	c.JSON(status, data)
}

// Error maps domain errors to appropriate HTTP status codes and sends the response.
// This is the single source of truth for error-to-HTTP mapping.
func Error(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, domain.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "conflict"})
	case errors.Is(err, domain.ErrUnauthorized):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	default:
		// Log unexpected errors for debugging but return generic message
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

// ErrorWithMessage sends an error response with a custom message.
func ErrorWithMessage(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

// Success sends a success response with optional data.
func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, data)
}

// Created sends a 201 Created response with data.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, data)
}

// Accepted sends a 202 Accepted response.
func Accepted(c *gin.Context, data any) {
	c.JSON(http.StatusAccepted, data)
}

// NoContent sends a 204 No Content response.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// BadRequest sends a 400 Bad Request response.
func BadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": message})
}

// Unauthorized sends a 401 Unauthorized response.
func Unauthorized(c *gin.Context, message string) {
	if message == "" {
		message = "unauthorized"
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": message})
}

// Forbidden sends a 403 Forbidden response.
func Forbidden(c *gin.Context, message string) {
	if message == "" {
		message = "forbidden"
	}
	c.JSON(http.StatusForbidden, gin.H{"error": message})
}

// InternalError sends a 500 Internal Server Error response.
func InternalError(c *gin.Context, message string) {
	if message == "" {
		message = "internal server error"
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": message})
}
