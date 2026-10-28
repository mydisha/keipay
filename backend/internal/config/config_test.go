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
		"JWT_SECRET=jwt_test",
		"ENCRYPTION_KEY=" + strings.Repeat("ab", 32),
	}, "\n") + "\n"
}

// chdirBackend makes root/backend the working directory, as `make backend` does,
// and blanks env vars a developer shell may export (empty counts as unset).
func chdirBackend(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"POSTGRES_DSN", "XENDIT_SECRET_KEY", "XENDIT_CALLBACK_TOKEN", "JWT_SECRET", "ENCRYPTION_KEY", "FRONTEND_DIR"} {
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
