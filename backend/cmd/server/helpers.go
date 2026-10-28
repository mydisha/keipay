package main

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/config"
	"github.com/mydisha/keipay/backend/internal/xendit"
)

// xenditClient builds the Xendit SDK client from config.
func xenditClient(cfg *config.Config) xendit.Client {
	return xendit.NewSDKClient(cfg.XenditSecretKey)
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
