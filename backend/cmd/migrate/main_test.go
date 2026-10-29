package main

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/keygen"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

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

// The seed creates the demo test app with one key through the service,
// audited as system:seed, prints the one-time api_key and api_secret lines
// (no webhook secret: apps have none since webhook endpoints), and on a rerun
// prints the skip line and creates nothing. It never creates a user or a
// webhook endpoint.
func TestIntegrationSeedCreatesDemoTestApp(t *testing.T) {
	pool := freshCLIPool(t)
	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var out bytes.Buffer
	if err := runSeed(ctx, pool, cipher, &out); err != nil {
		t.Fatalf("seed: %v", err)
	}
	re := regexp.MustCompile(`\Acreated app demo \((app_[0-9A-HJKMNP-TV-Z]{26}), test\)\n` +
		`  api_key=(kp_test_[0-9a-f]{32})\n` +
		`  api_secret=(kps_test_[0-9a-f]{48})\n` +
		`no console user yet: open the console to create the first owner, or run -reset-admin -email <email>\n\z`)
	m := re.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("seed output = %q", out.String())
	}
	app, err := postgres.NewAppRepo(pool).GetBySlug(ctx, domain.EnvTest, "demo")
	if err != nil || app.PublicID != m[1] || app.Name != "Demo App" || app.Status != domain.AppActive {
		t.Fatalf("app = %+v, %v", app, err)
	}
	key, keyApp, err := postgres.NewAPIKeyRepo(pool).GetByHashWithApp(ctx, keygen.HashAPIKey(m[2]))
	if err != nil || keyApp.ID != app.ID || key.Label != "Default" {
		t.Fatalf("key = %+v, %v", key, err)
	}
	if secret, err := cipher.Decrypt(key.SecretEnc); err != nil || secret != m[3] {
		t.Fatalf("stored api secret does not match the printed one: %v", err)
	}
	if app.WebhookSecretEnc != "" {
		t.Fatal("seeded app has a webhook secret")
	}
	var endpoints int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM webhook_endpoints`).Scan(&endpoints); err != nil || endpoints != 0 {
		t.Fatalf("seed created %d webhook endpoints: %v", endpoints, err)
	}

	out.Reset()
	if err := runSeed(ctx, pool, cipher, &out); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if !strings.HasPrefix(out.String(), "app demo (test) already exists, skipping\n") || strings.Contains(out.String(), "api_key=") {
		t.Fatalf("second seed output = %q", out.String())
	}

	rows, err := pool.Query(ctx, `SELECT action, actor_type, actor_id, ip FROM audit_logs ORDER BY occurred_at, public_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var action, actorType, actorID, ip string
		_ = rows.Scan(&action, &actorType, &actorID, &ip)
		got = append(got, action+" "+actorType+":"+actorID+" "+ip)
	}
	want := []string{"app.create system:seed ", "api_key.create system:seed "}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("audit = %q, want %q", got, want)
	}
	var keys, users int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM app_api_keys`).Scan(&keys)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users)
	if keys != 1 || users != 0 {
		t.Fatalf("keys=%d users=%d", keys, users)
	}
}

// -reset-admin creates the first owner, resets by email or the
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
