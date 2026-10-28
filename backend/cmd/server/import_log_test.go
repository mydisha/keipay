package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

// AC-23: the start-up log says what the XENDIT_* import did, at the level
// the requirements ask for, and the server keeps running on every outcome.
func TestLogLegacyImport(t *testing.T) {
	conn := &domain.Connection{PublicID: "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0D", Name: "xendit-imported", Environment: domain.EnvTest}
	other := domain.AppSummary{PublicID: "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0E", Slug: "globex", Environment: domain.EnvLive}
	cases := []struct {
		name   string
		res    *service.LegacyImport
		err    error
		levels []zapcore.Level
		want   []string
	}{
		{"not set", nil, nil, nil, nil},
		{"unknown prefix", nil, fmt.Errorf("import xendit settings: %w", service.ErrUnknownKeyPrefix),
			[]zapcore.Level{zapcore.ErrorLevel}, []string{"were not imported"}},
		{"schema error", nil, errors.New("import xendit settings: invalid credentials.callback_token (min_length)"),
			[]zapcore.Level{zapcore.ErrorLevel}, []string{"were not imported"}},
		{"imported before", &service.LegacyImport{Connection: conn}, nil,
			[]zapcore.Level{zapcore.WarnLevel}, []string{"deprecated and ignored"}},
		{"imported now", &service.LegacyImport{Connection: conn, Created: true, PaymentsBackfilled: 3}, nil,
			[]zapcore.Level{zapcore.InfoLevel, zapcore.WarnLevel}, []string{"imported XENDIT_*", "deprecated"}},
		{"imported with other-environment apps", &service.LegacyImport{Connection: conn, Created: true,
			OtherEnvironment: []domain.AppSummary{other}}, nil,
			[]zapcore.Level{zapcore.InfoLevel, zapcore.WarnLevel, zapcore.WarnLevel},
			[]string{"imported XENDIT_*", "other environment", "deprecated"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			logLegacyImport(zap.New(core), tc.res, tc.err)
			entries := logs.All()
			if len(entries) != len(tc.levels) {
				t.Fatalf("entries = %v", entries)
			}
			for i, e := range entries {
				if e.Level != tc.levels[i] || !strings.Contains(e.Message, tc.want[i]) {
					t.Fatalf("entry %d = %s %q, want %s containing %q", i, e.Level, e.Message, tc.levels[i], tc.want[i])
				}
				if tc.res != nil && e.ContextMap()["connection_id"] != nil && e.ContextMap()["connection_id"] != conn.PublicID {
					t.Fatalf("connection_id = %v", e.ContextMap()["connection_id"])
				}
			}
			if tc.name == "imported before" && entries[0].ContextMap()["connection_id"] != conn.PublicID {
				t.Fatalf("warning does not name the connection: %v", entries[0].ContextMap())
			}
			if tc.name == "imported with other-environment apps" {
				apps := fmt.Sprint(entries[1].ContextMap()["apps"])
				if !strings.Contains(apps, other.PublicID) || !strings.Contains(apps, "globex") {
					t.Fatalf("other-environment apps = %s", apps)
				}
			}
		})
	}
}
