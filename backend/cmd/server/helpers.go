package main

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/service"
)

// logLegacyImport reports what importing the deprecated XENDIT_* settings
// did. A nil result means the settings are not set.
func logLegacyImport(log *zap.Logger, res *service.LegacyImport, err error) {
	switch {
	case err != nil:
		log.Error("XENDIT_SECRET_KEY and XENDIT_CALLBACK_TOKEN were not imported; add a connection in the console instead",
			zap.Error(err))
	case res == nil:
	case !res.Created:
		log.Warn("XENDIT_SECRET_KEY and XENDIT_CALLBACK_TOKEN are deprecated and ignored: they were imported before. Remove them from the environment.",
			zap.String("connection_id", res.Connection.PublicID))
	default:
		log.Info("imported XENDIT_* settings into a connection",
			zap.String("connection_id", res.Connection.PublicID),
			zap.String("name", res.Connection.Name),
			zap.String("environment", string(res.Connection.Environment)),
			zap.Int("apps_defaulted", len(res.Defaulted)),
			zap.Int64("payments_backfilled", res.PaymentsBackfilled))
		if len(res.OtherEnvironment) > 0 {
			apps := make([]string, 0, len(res.OtherEnvironment))
			for _, a := range res.OtherEnvironment {
				apps = append(apps, a.PublicID+" ("+a.Slug+")")
			}
			log.Warn("apps of the other environment have no default connection; payments for them fail until one is set",
				zap.String("connection_environment", string(res.Connection.Environment)),
				zap.Strings("apps", apps))
		}
		log.Warn("XENDIT_SECRET_KEY and XENDIT_CALLBACK_TOKEN are deprecated; remove them from the environment now that they are imported")
	}
}

// resolveFrontendDir returns the admin console build to serve, or "" if none
// exists. A configured FRONTEND_DIR is used as-is; otherwise the build next to
// backend/ (repository layout) is picked up.
func resolveFrontendDir(configured string) string {
	candidates := []string{"../frontend/dist", "frontend/dist"}
	if configured != "" {
		candidates = []string{configured}
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !info.IsDir() {
			return dir
		}
	}
	return ""
}

// asynqLogger adapts zap to asynq.Logger.
type asynqLogger struct{ log *zap.Logger }

func newAsynqLogger(log *zap.Logger) *asynqLogger { return &asynqLogger{log: log} }

func (l *asynqLogger) Debug(args ...any) { l.log.Sugar().Debug(args...) }
func (l *asynqLogger) Info(args ...any)  { l.log.Sugar().Info(args...) }
func (l *asynqLogger) Warn(args ...any)  { l.log.Sugar().Warn(args...) }
func (l *asynqLogger) Error(args ...any) { l.log.Sugar().Error(args...) }
func (l *asynqLogger) Fatal(args ...any) { l.log.Sugar().Fatal(args...) }
