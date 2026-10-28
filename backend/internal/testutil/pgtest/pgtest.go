// Package pgtest gives integration tests a throwaway Postgres database, so
// tests that depend on the whole users table (setup, migrations, owner
// counts) never touch the shared database other tests and the e2e run use.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
)

// DSNEnv names the server the throwaway databases are created on.
const DSNEnv = "KEI_TEST_POSTGRES_DSN"

// Database is a fresh database and the migrator bound to it.
type Database struct {
	DSN     string
	Migrate *migrate.Migrate
}

// New creates an empty database, applies migrations up to version (0 means
// all of them) and drops the database when the test ends. It skips the test
// when DSNEnv is unset.
func New(t *testing.T, version uint) *Database {
	t.Helper()
	base := os.Getenv(DSNEnv)
	if base == "" {
		t.Skip(DSNEnv + " not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })

	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "keipay_it_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", DSNEnv, err)
	}
	u.Path = "/" + name
	db := &Database{DSN: u.String()}

	db.Migrate, err = migrate.New("file://"+migrationsDir(), db.DSN)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Migrate.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	if version == 0 {
		err = db.Migrate.Up()
	} else {
		err = db.Migrate.Migrate(version)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate to %d: %v", version, err)
	}
	return db
}

// migrationsDir is backend/migrations, found relative to this file so tests
// in any package can use it.
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}
