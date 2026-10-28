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
		"APP_ENV", "COOKIE_SECURE", "SESSION_IDLE_TIMEOUT", "SESSION_ABSOLUTE_TIMEOUT",
		"LOGIN_MAX_FAILURES_PER_IP", "LOGIN_MAX_FAILURES_PER_EMAIL", "LOGIN_FAILURE_WINDOW",
		"TRUSTED_PROXIES", "SETUP_TOKEN",
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

// T-20: the deprecated Xendit settings are optional, but only as a pair.
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
