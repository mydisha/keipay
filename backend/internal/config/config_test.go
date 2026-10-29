package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envBody(dsn string) string {
	return strings.Join([]string{
		"POSTGRES_DSN=" + dsn,
		"XENDIT_SECRET_KEY=xnd_test",
		"XENDIT_CALLBACK_TOKEN=cb_test",
		"ENCRYPTION_KEY=" + strings.Repeat("ab", 32),
	}, "\n") + "\n"
}

// chdirBackend makes root/backend the working directory, as `make backend` does,
// and blanks env vars a developer shell may export (empty counts as unset).
func chdirBackend(t *testing.T) string {
	t.Helper()
	for _, key := range []string{
		"POSTGRES_DSN", "XENDIT_SECRET_KEY", "XENDIT_CALLBACK_TOKEN", "JWT_SECRET", "ENCRYPTION_KEY", "FRONTEND_DIR",
		"APP_ENV", "COOKIE_SECURE", "HSTS_ENABLED", "SESSION_IDLE_TIMEOUT", "SESSION_ABSOLUTE_TIMEOUT",
		"LOGIN_MAX_FAILURES_PER_IP", "LOGIN_MAX_FAILURES_PER_EMAIL", "LOGIN_FAILURE_WINDOW",
		"TRUSTED_PROXIES", "SETUP_TOKEN", "WEBHOOK_DISABLE_AFTER_FAILURES",
		"V1_AUTH_MAX_FAILURES_PER_IP", "V1_AUTH_MAX_FAILURES_PER_KEY", "V1_AUTH_FAILURE_WINDOW",
		"CALLBACK_MAX_REJECTED_PER_IP", "CALLBACK_MAX_REJECTED_PER_CONNECTION", "CALLBACK_REJECTED_WINDOW",
	} {
		t.Setenv(key, "")
	}
	root := t.TempDir()
	backend := filepath.Join(root, "backend")
	if err := os.Mkdir(backend, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(backend)
	return root
}

func TestLoad_ReadsRepositoryRootEnv(t *testing.T) {
	root := chdirBackend(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envBody("postgres://root")), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://root" {
		t.Fatalf("PostgresDSN = %q", cfg.PostgresDSN)
	}
}

func TestLoad_LocalEnvWinsOverRootEnv(t *testing.T) {
	root := chdirBackend(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envBody("postgres://root")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".env", []byte(envBody("postgres://local")), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://local" {
		t.Fatalf("PostgresDSN = %q, want the backend/.env value", cfg.PostgresDSN)
	}
}

func TestLoad_ProcessEnvWinsOverFile(t *testing.T) {
	root := chdirBackend(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envBody("postgres://root")), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTGRES_DSN", "postgres://process")
	t.Setenv("FRONTEND_DIR", "/srv/console")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://process" || cfg.FrontendDir != "/srv/console" {
		t.Fatalf("got dsn=%q frontend=%q", cfg.PostgresDSN, cfg.FrontendDir)
	}
}

func TestLoad_NoEnvFileReportsMissingKeys(t *testing.T) {
	chdirBackend(t)
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("ab", 32))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_DSN") {
		t.Fatalf("err = %v, want missing POSTGRES_DSN", err)
	}
}

// The deprecated Xendit settings are optional, but only as a pair.
func TestLoad_XenditSettingsBothOrNeither(t *testing.T) {
	cases := []struct {
		name, key, token string
		wantErr          bool
	}{
		{"neither", "", "", false},
		{"both", "xnd_development_abc", "token-123", false},
		{"key only", "xnd_development_abc", "", true},
		{"token only", "", "token-123", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chdirBackend(t)
			t.Setenv("POSTGRES_DSN", "postgres://x")
			t.Setenv("ENCRYPTION_KEY", strings.Repeat("ab", 32))
			t.Setenv("XENDIT_SECRET_KEY", tc.key)
			t.Setenv("XENDIT_CALLBACK_TOKEN", tc.token)
			cfg, err := Load()
			if tc.wantErr {
				if err == nil || err.Error() != "XENDIT_SECRET_KEY and XENDIT_CALLBACK_TOKEN must be set together" {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.XenditSecretKey != tc.key || cfg.XenditCallbackToken != tc.token {
				t.Fatalf("cfg = %q / %q", cfg.XenditSecretKey, cfg.XenditCallbackToken)
			}
		})
	}
}

// Endpoints are disabled after 5 exhausted deliveries by
// default; 0 turns it off; a negative value is a configuration error.
func TestLoad_WebhookDisableAfterFailures(t *testing.T) {
	root := chdirBackend(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envBody("postgres://root")), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || cfg.WebhookDisableAfterFailures != 5 {
		t.Fatalf("default = %v, %v", cfg, err)
	}
	t.Setenv("WEBHOOK_DISABLE_AFTER_FAILURES", "0")
	if cfg, err := Load(); err != nil || cfg.WebhookDisableAfterFailures != 0 {
		t.Fatalf("0 = %v, %v", cfg, err)
	}
	t.Setenv("WEBHOOK_DISABLE_AFTER_FAILURES", "12")
	if cfg, err := Load(); err != nil || cfg.WebhookDisableAfterFailures != 12 {
		t.Fatalf("12 = %v, %v", cfg, err)
	}
	t.Setenv("WEBHOOK_DISABLE_AFTER_FAILURES", "-1")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "WEBHOOK_DISABLE_AFTER_FAILURES") {
		t.Fatalf("-1: %v", err)
	}
}
