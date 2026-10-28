// Package logger provides a configured zap logger with consistent,
// human-readable, and searchable structured output.
//
// Every log line carries a stable set of fields (ts, level, msg, caller) plus
// the service name and environment so logs can be filtered and correlated
// across services. Use the LogType* constants in the "log_type" field to make
// logs easy to filter by category (access, security, webhook, app).
package logger

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Log type categories. Put these in the "log_type" field so logs are easy to
// filter and sort by purpose (e.g. log_type=security).
const (
	LogTypeApp      = "app"
	LogTypeAccess   = "access"
	LogTypeSecurity = "security"
	LogTypeWebhook  = "webhook"
)

// Field keys used consistently across the codebase. Centralizing them keeps log
// queries predictable (search/sort always hits the same key names).
const (
	FieldLogType   = "log_type"
	FieldRequestID = "request_id"
	FieldEvent     = "event"
	FieldSeverity  = "severity"
	FieldClientIP  = "client_ip"
	FieldAppID     = "app_id"
	FieldAPIKeyID  = "api_key_id"
	FieldKeyHint   = "api_key_hint"
	FieldLatencyMS = "latency_ms"
)

// Options configures the logger.
type Options struct {
	// Service is the logical service name attached to every log line.
	Service string
	// Env is the deployment environment (e.g. development, production).
	Env string
	// Level is the minimum level to emit: debug, info, warn, error.
	Level string
	// Format is "json" or "console". Defaults to json in production-like envs
	// and console otherwise when empty.
	Format string
	// Production toggles json default + sampling when Format is empty.
	Production bool
}

// New returns a configured *zap.Logger.
//
// Production-style output is JSON (easy to ingest/search in Loki/ELK), while
// development uses a colored console encoder for readability. Timestamps are
// ISO8601 with timezone so logs sort lexically and stay human-readable.
func New(opts Options) (*zap.Logger, error) {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		if opts.Production {
			format = "json"
		} else {
			format = "console"
		}
	}

	encCfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	if format == "console" {
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(parseLevel(opts.Level)),
		Development:      !opts.Production,
		Encoding:         format,
		EncoderConfig:    encCfg,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	if opts.Production {
		// Sample high-volume duplicate logs to control cost while keeping signal.
		cfg.Sampling = &zap.SamplingConfig{Initial: 100, Thereafter: 100}
	}

	base, err := cfg.Build(zap.AddCallerSkip(0))
	if err != nil {
		return nil, err
	}

	fields := []zap.Field{}
	if opts.Service != "" {
		fields = append(fields, zap.String("service", opts.Service))
	}
	if opts.Env != "" {
		fields = append(fields, zap.String("env", opts.Env))
	}
	if len(fields) > 0 {
		base = base.With(fields...)
	}
	return base, nil
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "info", "":
		return zapcore.InfoLevel
	default:
		return zapcore.InfoLevel
	}
}
