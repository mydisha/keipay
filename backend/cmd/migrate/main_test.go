package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

// The seeded tenant is recorded as tenant.create by system:seed, and a second
// seed run skips it without another entry.
func TestIntegrationSeedTenantIsAuditedAsSystem(t *testing.T) {
	dsn := os.Getenv("KEI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("KEI_TEST_POSTGRES_DSN not set")
	}
	ctx := domain.WithActor(context.Background(), domain.SystemActor("seed"))
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	slug := fmt.Sprintf("seed-it-%d", time.Now().UnixNano())
	tenant, creds, err := seedTenant(ctx, pool, cipher, "Seed IT", slug)
	if err != nil || tenant == nil || creds == nil {
		t.Fatalf("seed = %+v, %v", tenant, err)
	}
	target := strconv.FormatInt(tenant.ID, 10)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM audit_logs WHERE target_type='tenant' AND target_id=$1`, target)
		_, _ = pool.Exec(bg, `DELETE FROM tenants WHERE id=$1`, tenant.ID)
	})

	again, _, err := seedTenant(ctx, pool, cipher, "Seed IT", slug)
	if err != nil || again != nil {
		t.Fatalf("second seed = %+v, %v; want skip", again, err)
	}

	var n int
	var actorType, actorID, action, ip string
	err = pool.QueryRow(context.Background(), `SELECT count(*), min(actor_type), min(actor_id), min(action), min(ip)
		FROM audit_logs WHERE target_type='tenant' AND target_id=$1`, target).Scan(&n, &actorType, &actorID, &action, &ip)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 || actorType != "system" || actorID != "seed" || action != "tenant.create" || ip != "" {
		t.Fatalf("entries=%d actor=%s:%s action=%s ip=%q", n, actorType, actorID, action, ip)
	}
}

func freshCLIPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := pgtest.New(t, 0)
	pool, err := postgres.NewPool(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// T-22 / AC-4: seeding creates the demo tenant but never a user or password.
func TestIntegrationSeedCreatesNoUser(t *testing.T) {
	pool := freshCLIPool(t)
	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var out bytes.Buffer
		if err := runSeed(context.Background(), pool, cipher, &out); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if strings.Contains(out.String(), "password") || !strings.Contains(out.String(), "-reset-admin -email <email>") {
			t.Fatalf("seed output = %q", out.String())
		}
	}
	if n, _ := postgres.NewUserRepo(pool).Count(context.Background()); n != 0 {
		t.Fatalf("seed created %d users", n)
	}
}

// T-22 / AC-4: -reset-admin creates the first owner, resets by email or the
// oldest owner, and refuses unknown emails; every change is system:cli.
func TestIntegrationResetAdmin(t *testing.T) {
	pool := freshCLIPool(t)
	ctx := context.Background()
	hasher := service.NewBcryptHasher(bcrypt.MinCost)
	repo := postgres.NewUserRepo(pool)
	run := func(email, password string) (string, error) {
		var out bytes.Buffer
		err := runResetAdmin(ctx, pool, hasher, email, password, &out)
		return out.String(), err
	}

	if _, err := run("", ""); err == nil {
		t.Fatal("no users and no email should fail")
	}
	out, err := run("Boss@Example.com", "")
	if err != nil || !strings.Contains(out, "created owner") || !strings.Contains(out, "email=boss@example.com") || !strings.Contains(out, "password=") {
		t.Fatalf("create owner: %q, %v", out, err)
	}
	boss, err := repo.GetByEmail(ctx, "boss@example.com")
	if err != nil || boss.Role != domain.RoleOwner || !boss.MustChangePassword || boss.Name != "boss" {
		t.Fatalf("owner = %+v, %v", boss, err)
	}

	if _, err := run("stranger@example.com", ""); err == nil {
		t.Fatal("unknown email with users present should fail")
	}
	if n, _ := repo.Count(ctx); n != 1 {
		t.Fatalf("unknown email created a user: %d", n)
	}
	if _, err := run("boss@example.com", "too-short"); err == nil || !strings.Contains(describe(err), "password (min)") {
		t.Fatalf("short password: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	boss.DisabledAt = &at
	_ = repo.Update(ctx, boss)
	out, err = run("BOSS@example.com", "chosen-password-1")
	if err != nil || strings.Contains(out, "password=") {
		t.Fatalf("reset with a password: %q, %v", out, err)
	}
	boss, _ = repo.GetByEmail(ctx, "boss@example.com")
	if boss.DisabledAt != nil || boss.MustChangePassword || boss.SessionVersion != 2 || !hasher.Matches(boss.PasswordHash, "chosen-password-1") {
		t.Fatalf("after reset = %+v", boss)
	}
	out, err = run("", "")
	if err != nil || !strings.Contains(out, "email=boss@example.com") || !strings.Contains(out, "password=") {
		t.Fatalf("oldest owner: %q, %v", out, err)
	}

	rows, err := pool.Query(ctx, `SELECT action, actor_type, actor_id FROM audit_logs ORDER BY occurred_at, public_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var action, actorType, actorID string
		_ = rows.Scan(&action, &actorType, &actorID)
		got = append(got, action+" "+actorType+":"+actorID)
	}
	want := []string{"user.setup system:cli", "user.reset_password system:cli", "user.reset_password system:cli"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("audit = %v, want %v", got, want)
	}
}
