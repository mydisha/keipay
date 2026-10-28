package logger

import (
	"context"

	"go.uber.org/zap"
)

// ctxKey is an unexported type to avoid context key collisions.
type ctxKey int

const (
	requestIDKey ctxKey = iota
)

// WithRequestID returns a copy of ctx carrying the given request id so it can
// be propagated through service and repository layers for end-to-end tracing.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext extracts the request id previously stored with
// WithRequestID. It returns "" when absent.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// Ctx returns a child logger pre-populated with the request id found in ctx.
// Using it keeps logs correlated across layers without threading the id by hand:
//
//	log := logger.Ctx(ctx, h.log)
//	log.Info("doing work")
func Ctx(ctx context.Context, base *zap.Logger) *zap.Logger {
	if base == nil {
		return zap.NewNop()
	}
	if id := RequestIDFromContext(ctx); id != "" {
		return base.With(zap.String(FieldRequestID, id))
	}
	return base
}
