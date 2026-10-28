// Package logger provides helper functions for structured logging.
// These helpers make it easier to log common patterns consistently.
package logger

import (
	"time"

	"go.uber.org/zap"
)

// ErrorFields returns a set of structured fields for error logging.
// Use this to ensure error logs are consistent and searchable.
//
// Example:
//
//	log.Error("operation failed", logger.ErrorFields("create_payment", err)...)
func ErrorFields(operation string, err error) []zap.Field {
	if err == nil {
		return []zap.Field{
			zap.String(FieldEvent, operation),
			zap.String("error", "nil"),
		}
	}
	return []zap.Field{
		zap.String(FieldEvent, operation),
		zap.Error(err),
		zap.String("error_type", errorType(err)),
	}
}

// errorType extracts the type name of an error for categorization.
func errorType(err error) string {
	if err == nil {
		return "nil"
	}
	// Try to unwrap and get the concrete type
	type causer interface {
		Cause() error
	}
	if c, ok := err.(causer); ok {
		return errorType(c.Cause())
	}
	return "error"
}

// TimingFunc logs the execution time of a function.
// Returns a function that should be deferred to log the duration.
//
// Example:
//
//	defer logger.TimingFunc(log, "process_payment")()
func TimingFunc(log *zap.Logger, operation string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		log.Debug("operation completed",
			zap.String(FieldEvent, operation),
			zap.Duration("duration_ms", duration),
		)
	}
}

// TimingFuncWithResult logs execution time and allows adding custom fields.
// Returns the duration and a function to add extra fields.
//
// Example:
//
//	duration, addFields := logger.TimingFuncWithResult(log, "db_query")
//	defer addFields(zap.Int("rows", count))
func TimingFuncWithResult(log *zap.Logger, operation string) (time.Duration, func(...zap.Field)) {
	start := time.Now()
	return time.Since(start), func(extraFields ...zap.Field) {
		duration := time.Since(start)
		fields := append([]zap.Field{
			zap.String(FieldEvent, operation),
			zap.Duration("duration_ms", duration),
		}, extraFields...)
		log.Debug("operation completed", fields...)
	}
}

// WithRequestContext creates a logger with request context fields.
// Use this to propagate request_id through service layers.
//
// Example:
//
//	reqLog := logger.WithRequestContext(log, requestID, appID)
//	reqLog.Info("processing payment")
func WithRequestContext(log *zap.Logger, requestID string, fields ...zap.Field) *zap.Logger {
	base := []zap.Field{zap.String(FieldRequestID, requestID)}
	base = append(base, fields...)
	return log.With(base...)
}

// CacheHit logs a cache hit event at debug level.
func CacheHit(log *zap.Logger, cacheName, key string) {
	log.Debug("cache hit",
		zap.String(FieldEvent, "cache_hit"),
		zap.String("cache", cacheName),
		zap.String("key", key),
	)
}

// CacheMiss logs a cache miss event at debug level.
func CacheMiss(log *zap.Logger, cacheName, key string) {
	log.Debug("cache miss",
		zap.String(FieldEvent, "cache_miss"),
		zap.String("cache", cacheName),
		zap.String("key", key),
	)
}

// ExternalAPICall logs an external API call.
// Use this for consistent logging of third-party API interactions.
func ExternalAPICall(log *zap.Logger, service, endpoint string, duration time.Duration, err error) {
	fields := []zap.Field{
		zap.String(FieldEvent, "external_api_call"),
		zap.String("external_service", service),
		zap.String("endpoint", endpoint),
		zap.Duration("duration_ms", duration),
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
		log.Error("external API call failed", fields...)
	} else {
		log.Debug("external API call completed", fields...)
	}
}

// DatabaseQuery logs a database query execution.
// Only logs at debug level to avoid log spam.
func DatabaseQuery(log *zap.Logger, operation, table string, duration time.Duration, rowsAffected int64, err error) {
	fields := []zap.Field{
		zap.String(FieldEvent, "db_query"),
		zap.String("db_operation", operation),
		zap.String("db_table", table),
		zap.Duration("duration_ms", duration),
		zap.Int64("rows_affected", rowsAffected),
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
		log.Error("database query failed", fields...)
	} else {
		log.Debug("database query completed", fields...)
	}
}

// WorkerTask logs a worker task execution.
func WorkerTask(log *zap.Logger, taskType, taskID string, duration time.Duration, err error) {
	fields := []zap.Field{
		zap.String(FieldEvent, "worker_task"),
		zap.String("task_type", taskType),
		zap.String("task_id", taskID),
		zap.Duration("duration_ms", duration),
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
		log.Error("worker task failed", fields...)
	} else {
		log.Info("worker task completed", fields...)
	}
}

// SecurityEvent logs a security-related event.
// These logs are tagged with log_type=security for easy filtering.
func SecurityEvent(log *zap.Logger, event string, fields ...zap.Field) {
	base := []zap.Field{
		zap.String(FieldLogType, LogTypeSecurity),
		zap.String(FieldEvent, event),
	}
	log.Warn("security event", append(base, fields...)...)
}

// StartupComponent logs the initialization of a component.
// Use for consistent startup sequence logging.
func StartupComponent(log *zap.Logger, component string, fields ...zap.Field) *zap.Logger {
	base := []zap.Field{zap.String("component", component)}
	componentLog := log.With(append(base, fields...)...)
	componentLog.Debug("initializing component")
	return componentLog
}
