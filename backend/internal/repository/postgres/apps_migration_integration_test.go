package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/keygen"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func exec(t *testing.T, pool *pgxpool.Pool, q string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

const (
	acmeKey   = "kp_0123456789abcdef0123456789abcdef"
	globexKey = "kp_fedcba9876543210fedcba9876543210"
)

// seedTenants fills a version-6 database: acme (active) and globex
// (disabled), a payment each, a delivery and a ledger entry for acme.
func seedTenants(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	exec(t, pool, `INSERT INTO tenants (id, name, slug, api_key, api_secret_enc, webhook_url, redirect_success_url, active, created_at, updated_at)
		VALUES (1, 'Acme', 'acme', $1, 'enc-acme', 'https://acme.example/hook', 'https://acme.example/ok', true, '2025-01-02T03:04:05Z', '2025-02-03T04:05:06Z'),
		       (2, 'Globex', 'globex', $2, 'enc-globex', '', '', false, '2025-03-01T00:00:00Z', '2025-03-01T00:00:00Z')`, acmeKey, globexKey)
	exec(t, pool, `SELECT setval(pg_get_serial_sequence('tenants', 'id'), 2)`)
	exec(t, pool, `INSERT INTO payments (id, tenant_id, order_id, external_id, amount, created_at)
		VALUES (1, 1, 'ord-1', 'acme:ord-1', 1000, '2025-04-01T00:00:00Z'), (2, 2, 'ord-1', 'globex:ord-1', 2000, '2025-04-01T00:00:00Z')`)
	exec(t, pool, `SELECT setval(pg_get_serial_sequence('payments', 'id'), 2)`)
	exec(t, pool, `INSERT INTO webhook_deliveries (payment_id, tenant_id, event_type, payload) VALUES (1, 1, 'payment.paid', '{}')`)
	exec(t, pool, `INSERT INTO ledger_entries (tenant_id, payment_id, amount, net_amount) VALUES (1, 1, 1000, 1000)`)
}

type tenantRow struct {
	id                                  int64
	name, slug, apiKey, secretEnc, hook string
	active                              bool
}

func tenantRows(t *testing.T, pool *pgxpool.Pool) []tenantRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT id, name, slug, api_key, api_secret_enc, webhook_url, active FROM tenants ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []tenantRow
	for rows.Next() {
		var r tenantRow
		if err := rows.Scan(&r.id, &r.name, &r.slug, &r.apiKey, &r.secretEnc, &r.hook, &r.active); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// T-25 / AC-1: up keeps every tenant as a live app with the same id and its
// credentials as a migrated key; down puts apps back into tenants; up again
// works.
func TestIntegrationMigration7UpDownUp(t *testing.T) {
	db := pgtest.New(t, 6)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	seedTenants(t, pool)
	before := tenantRows(t, pool)

	// The repositories read the current schema; 000008 only adds to what
	// 000007 builds, so migrating through it checks 000007 just the same.
	if err := db.Migrate.Migrate(8); err != nil {
		t.Fatalf("up: %v", err)
	}
	apps := postgres.NewAppRepo(pool)
	keys := postgres.NewAPIKeyRepo(pool)
	acme, err := apps.GetByID(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	globex, err := apps.GetByID(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []*domain.App{acme, globex} {
		if !id.Valid(a.PublicID, id.App) || a.Environment != domain.EnvLive || len(a.Metadata) != 0 {
			t.Fatalf("app = %+v", a)
		}
	}
	if acme.Slug != "acme" || acme.Name != "Acme" || acme.Status != domain.AppActive || acme.WebhookSecretEnc != "enc-acme" ||
		acme.WebhookURL != "https://acme.example/hook" || acme.RedirectSuccessURL != "https://acme.example/ok" ||
		!acme.CreatedAt.Equal(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)) || globex.Status != domain.AppDisabled {
		t.Fatalf("apps = %+v / %+v", acme, globex)
	}
	for raw, app := range map[string]*domain.App{acmeKey: acme, globexKey: globex} {
		k, a, err := keys.GetByHashWithApp(ctx, keygen.HashAPIKey(raw))
		if err != nil || a.ID != app.ID {
			t.Fatalf("migrated key for %s: %+v %v", app.Slug, a, err)
		}
		if !id.Valid(k.PublicID, id.APIKey) || k.Label != "Migrated key" || k.Prefix != "kp_" || k.Last4 != raw[len(raw)-4:] ||
			k.Environment != domain.EnvLive || k.SecretEnc != app.WebhookSecretEnc || k.RevokedAt != nil || k.ExpiresAt != nil {
			t.Fatalf("migrated key = %+v", k)
		}
	}
	var n, distinct, valid int
	rows, err := pool.Query(ctx, `SELECT public_id FROM payments`)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for rows.Next() {
		var pub string
		_ = rows.Scan(&pub)
		n++
		if id.Valid(pub, id.Payment) {
			valid++
		}
		if !seen[pub] {
			distinct++
		}
		seen[pub] = true
	}
	rows.Close()
	if n != 2 || valid != 2 || distinct != 2 {
		t.Fatalf("payment ids: n=%d valid=%d distinct=%d", n, valid, distinct)
	}
	var mismatched int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM payments WHERE app_id IS DISTINCT FROM tenant_id)
		+ (SELECT count(*) FROM webhook_deliveries WHERE app_id IS DISTINCT FROM tenant_id)
		+ (SELECT count(*) FROM ledger_entries WHERE app_id IS DISTINCT FROM tenant_id)`).Scan(&mismatched)
	if mismatched != 0 {
		t.Fatalf("%d rows got another app_id than their tenant_id", mismatched)
	}
	if p, err := postgres.NewPaymentRepo(pool).GetByReferenceForUpdate(ctx, "acme:ord-1"); err != nil || p.AppID != 1 || p.App.Slug != "acme" {
		t.Fatalf("legacy reference: %+v %v", p, err)
	}
	if after := tenantRows(t, pool); len(after) != len(before) || after[0] != before[0] || after[1] != before[1] {
		t.Fatalf("up changed tenants: %+v", after)
	}

	// New rows after the migration: a test app with a payment, and a renamed acme.
	fresh := &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "Acme Test", Environment: domain.EnvTest, Status: domain.AppActive, WebhookSecretEnc: "enc-new"}
	if err := apps.Create(ctx, fresh); err != nil || fresh.ID != 3 {
		t.Fatalf("new app: id=%d %v", fresh.ID, err)
	}
	payment := &domain.Payment{PublicID: id.New(id.Payment), AppID: fresh.ID, OrderID: "ord-1", Amount: 5, Currency: "IDR", Status: domain.PaymentStatusPending}
	payment.ExternalID = "kp_" + payment.PublicID
	if err := postgres.NewPaymentRepo(pool).Create(ctx, payment); err != nil {
		t.Fatal(err)
	}
	acme.Name = "Acme Renamed"
	acme.Status = domain.AppDisabled
	if err := apps.Update(ctx, acme); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate.Migrate(6); err != nil {
		t.Fatalf("down: %v", err)
	}
	after := tenantRows(t, pool)
	if len(after) != 3 || after[0].name != "Acme Renamed" || after[0].active || after[0].apiKey != acmeKey ||
		after[2].id != 3 || after[2].slug != "acme-test" || after[2].apiKey != "kp_unavailable_"+lowerID(fresh.PublicID) ||
		after[2].secretEnc != "enc-new" {
		t.Fatalf("tenants after down = %+v", after)
	}
	var nullable string
	_ = pool.QueryRow(ctx, `SELECT string_agg(table_name || '.' || is_nullable, ',' ORDER BY table_name) FROM information_schema.columns
		WHERE column_name = 'tenant_id' AND table_name IN ('payments', 'webhook_deliveries', 'ledger_entries')`).Scan(&nullable)
	if nullable != "ledger_entries.NO,payments.NO,webhook_deliveries.NO" {
		t.Fatalf("tenant_id nullability after down = %s", nullable)
	}
	var tenantID int64
	_ = pool.QueryRow(ctx, `SELECT tenant_id FROM payments WHERE order_id = 'ord-1' AND external_id LIKE 'kp_%'`).Scan(&tenantID)
	if tenantID != 3 {
		t.Fatalf("new payment tenant_id = %d", tenantID)
	}
	exec(t, pool, `INSERT INTO tenants (name, slug, api_key, api_secret_enc) VALUES ('Next', 'next', 'kp_next', 'x')`)

	if err := db.Migrate.Migrate(8); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if k, a, err := keys.GetByHashWithApp(ctx, keygen.HashAPIKey(acmeKey)); err != nil || a.Name != "Acme Renamed" || k == nil {
		t.Fatalf("acme after up again: %+v %v", a, err)
	}
}

func lowerID(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// T-25 / AC-1: the migration works on an empty database too.
func TestIntegrationMigration7Empty(t *testing.T) {
	db := pgtest.New(t, 6)
	for _, v := range []uint{7, 6, 7, 8} {
		if err := db.Migrate.Migrate(v); err != nil {
			t.Fatalf("migrate to %d: %v", v, err)
		}
	}
	pool := openPool(t, db.DSN)
	a := &domain.App{PublicID: id.New(id.App), Slug: "first", Name: "First", Environment: domain.EnvLive, Status: domain.AppActive, WebhookSecretEnc: "x"}
	if err := postgres.NewAppRepo(pool).Create(context.Background(), a); err != nil || a.ID != 1 {
		t.Fatalf("first app: id=%d %v", a.ID, err)
	}
}

// T-26 / AC-4, AC-9, AC-15: the database refuses what the service never writes.
func TestIntegrationAppConstraints(t *testing.T) {
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	apps := postgres.NewAppRepo(pool)
	newApp := func(slug string, env domain.Environment) *domain.App {
		a := &domain.App{PublicID: id.New(id.App), Slug: slug, Name: slug, Environment: env, Status: domain.AppActive, WebhookSecretEnc: "x"}
		if err := apps.Create(ctx, a); err != nil {
			t.Fatalf("app %s/%s: %v", env, slug, err)
		}
		return a
	}
	live := newApp("acme", domain.EnvLive)
	test := newApp("acme", domain.EnvTest)
	if err := apps.Create(ctx, &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "x", Environment: domain.EnvLive, Status: domain.AppActive}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate slug: %v", err)
	}

	insertKey := func(appID int64, env, prefix string) error {
		_, err := pool.Exec(ctx, `INSERT INTO app_api_keys (public_id, app_id, environment, label, key_prefix, key_last4, key_hash, secret_enc)
			VALUES ($1, $2, $3, 'k', $4, 'abcd', $5, 'x')`, id.New(id.APIKey), appID, env, prefix, id.New(id.APIKey))
		return err
	}
	if err := insertKey(live.ID, "test", "kp_test_"); err == nil {
		t.Fatal("a test key for a live app was accepted")
	}
	if err := insertKey(live.ID, "live", "kp_test_"); err == nil {
		t.Fatal("a kp_test_ prefix on a live key was accepted")
	}
	for _, ok := range []struct {
		app    int64
		env    string
		prefix string
	}{{live.ID, "live", "kp_live_"}, {live.ID, "live", "kp_"}, {test.ID, "test", "kp_test_"}} {
		if err := insertKey(ok.app, ok.env, ok.prefix); err != nil {
			t.Fatalf("valid key %+v: %v", ok, err)
		}
	}

	payments := postgres.NewPaymentRepo(pool)
	pay := func(app *domain.App, order, key string) error {
		p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: order, Amount: 1, Currency: "IDR",
			Status: domain.PaymentStatusPending, IdempotencyKey: key}
		p.ExternalID = "kp_" + p.PublicID
		if key != "" {
			p.IdempotencyFingerprint = "fp"
		}
		return payments.Create(ctx, p)
	}
	if err := pay(live, "o-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := pay(live, "o-2", ""); err != nil {
		t.Fatalf("a second keyless payment collided: %v", err)
	}
	if err := pay(live, "o-1", ""); !errors.Is(err, domain.ErrDuplicateOrderID) {
		t.Fatalf("duplicate order: %v", err)
	}
	if err := pay(test, "o-1", "K"); err != nil {
		t.Fatalf("same order in another app: %v", err)
	}
	if err := pay(test, "o-3", "K"); !errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		t.Fatalf("duplicate key: %v", err)
	}
	if err := pay(live, "o-3", "K"); err != nil {
		t.Fatalf("same key in another app: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET metadata = '[]' WHERE app_id = $1`, live.ID); err == nil {
		t.Fatal("non-object payment metadata was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE apps SET metadata = '"x"' WHERE id = $1`, live.ID); err == nil {
		t.Fatal("non-object app metadata was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE apps SET environment = 'prod' WHERE id = $1`, live.ID); err == nil {
		t.Fatal("unknown environment was accepted")
	}
}

// T-27: repositories on a database of their own, so lists are exact.
func TestIntegrationAppRepositories(t *testing.T) {
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	apps := postgres.NewAppRepo(pool)
	keys := postgres.NewAPIKeyRepo(pool)

	same := time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)
	var created []*domain.App
	for i, env := range []domain.Environment{domain.EnvTest, domain.EnvTest, domain.EnvLive, domain.EnvTest} {
		a := &domain.App{PublicID: id.New(id.App), Slug: "app-" + string(rune('a'+i)), Name: "App", Environment: env,
			Status: domain.AppActive, Metadata: domain.Metadata{"n": "ü"}, WebhookSecretEnc: "x"}
		if err := apps.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		exec(t, pool, `UPDATE apps SET created_at = $2 WHERE id = $1`, a.ID, same)
		created = append(created, a)
	}
	created[3].Status = domain.AppDisabled
	if err := apps.Update(ctx, created[3]); err != nil {
		t.Fatal(err)
	}
	collect := func(f domain.AppFilter) []string {
		var ids []string
		page := domain.Page{Limit: 1}
		for range 10 {
			res, err := apps.List(ctx, f, page)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range res.Items {
				ids = append(ids, a.PublicID)
				if a.Metadata["n"] != "ü" {
					t.Fatalf("metadata = %v", a.Metadata)
				}
			}
			if res.NextCursor == "" {
				return ids
			}
			c, _ := domain.DecodeCursor(res.NextCursor)
			page.After = &c
		}
		t.Fatal("pagination did not end")
		return nil
	}
	test, active := domain.EnvTest, domain.AppActive
	if got := collect(domain.AppFilter{}); len(got) != 4 || !sortedDesc(got) {
		t.Fatalf("all = %v", got)
	}
	if got := collect(domain.AppFilter{Environment: &test}); len(got) != 3 {
		t.Fatalf("test = %v", got)
	}
	if got := collect(domain.AppFilter{Environment: &test, Status: &active}); len(got) != 2 {
		t.Fatalf("test active = %v", got)
	}

	// Keys: one query returns key and app; keyset over equal created_at.
	app := created[0]
	var keyIDs []string
	for range 3 {
		raw := keygen.APIKey("test")
		k := &domain.APIKey{PublicID: id.New(id.APIKey), AppID: app.ID, Environment: domain.EnvTest, Label: "k",
			Prefix: "kp_test_", Last4: raw[len(raw)-4:], Hash: keygen.HashAPIKey(raw), SecretEnc: "x"}
		if err := keys.Create(ctx, k); err != nil {
			t.Fatal(err)
		}
		exec(t, pool, `UPDATE app_api_keys SET created_at = $2 WHERE id = $1`, k.ID, same)
		keyIDs = append(keyIDs, k.PublicID)
		if got, gotApp, err := keys.GetByHashWithApp(ctx, k.Hash); err != nil || got.ID != k.ID || gotApp.ID != app.ID || gotApp.Metadata["n"] != "ü" {
			t.Fatalf("by hash = %+v %+v %v", got, gotApp, err)
		}
	}
	first, err := keys.List(ctx, app.ID, domain.Page{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("keys page 1 = %+v %v", first, err)
	}
	c, _ := domain.DecodeCursor(first.NextCursor)
	second, err := keys.List(ctx, app.ID, domain.Page{Limit: 2, After: &c})
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].PublicID == first.Items[0].PublicID ||
		second.Items[0].PublicID == first.Items[1].PublicID {
		t.Fatalf("keys page 2 = %+v %v", second, err)
	}

	// TouchLastUsed writes at most once a minute.
	k0, _ := keys.GetByPublicID(ctx, app.ID, keyIDs[0])
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{t0, t0.Add(30 * time.Second), t0.Add(59 * time.Second)} {
		if err := keys.TouchLastUsed(ctx, k0.ID, at); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := keys.GetByPublicID(ctx, app.ID, k0.PublicID); !got.LastUsedAt.Equal(t0) {
		t.Fatalf("last_used_at = %v, want %v", got.LastUsedAt, t0)
	}
	if err := keys.TouchLastUsed(ctx, k0.ID, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := keys.GetByPublicID(ctx, app.ID, k0.PublicID); !got.LastUsedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("last_used_at after a minute = %v", got.LastUsedAt)
	}

	// Usable count and revoke.
	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	k0.RevokedAt = &past
	if err := keys.Revoke(ctx, k0); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `UPDATE app_api_keys SET expires_at = now() - interval '1 second' WHERE public_id = $1`, keyIDs[1])
	if n, err := keys.CountUsable(ctx, app.ID, time.Now()); err != nil || n != 1 {
		t.Fatalf("usable = %d, %v", n, err)
	}
	if _, err := keys.GetByPublicID(ctx, created[1].ID, keyIDs[2]); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("key of another app: %v", err)
	}
}

func sortedDesc(ids []string) bool {
	for i := 1; i < len(ids); i++ {
		if ids[i-1] <= ids[i] {
			return false
		}
	}
	return true
}

// T-27 / AC-17: payment, ledger and delivery lists carry the app summary and
// pay_ ids, and filter by app.
func TestIntegrationPaymentLedgerDeliveryLists(t *testing.T) {
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	apps := postgres.NewAppRepo(pool)
	payments := postgres.NewPaymentRepo(pool)
	ledger := postgres.NewLedgerRepo(pool)
	deliveries := postgres.NewDeliveryRepo(pool)

	var all []*domain.App
	for _, slug := range []string{"acme", "globex"} {
		a := &domain.App{PublicID: id.New(id.App), Slug: slug, Name: slug + " Inc", Environment: domain.EnvLive, Status: domain.AppActive, WebhookSecretEnc: "x"}
		if err := apps.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		all = append(all, a)
	}
	acme, globex := all[0], all[1]
	same := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var acmePayments []*domain.Payment
	for i, app := range []*domain.App{acme, acme, acme, globex} {
		p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: "o-" + string(rune('a'+i)), Amount: 1000,
			Currency: "IDR", Status: domain.PaymentStatusPending, Metadata: domain.Metadata{"big": "x"}}
		p.ExternalID = "kp_" + p.PublicID
		if err := payments.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		exec(t, pool, `UPDATE payments SET created_at = $2 WHERE id = $1`, p.ID, same)
		if app == acme {
			acmePayments = append(acmePayments, p)
		}
		if err := ledger.Create(ctx, &domain.LedgerEntry{AppID: app.ID, PaymentID: p.ID, Amount: 1000, XenditFee: 10, NetAmount: 990, Type: domain.LedgerEntryTypePayment}); err != nil {
			t.Fatal(err)
		}
		if err := deliveries.Create(ctx, &domain.WebhookDelivery{PublicID: id.New(id.Delivery), PaymentID: &p.ID, AppID: app.ID,
			EventType: "payment.paid", Payload: []byte(`{}`), Status: domain.WebhookDeliveryPending, MaxAttempts: 1}); err != nil {
			t.Fatal(err)
		}
	}

	acmeID := acme.PublicID
	var seen []string
	page := domain.Page{Limit: 2}
	for range 5 {
		res, err := payments.List(ctx, domain.PaymentFilter{AppPublicID: &acmeID}, page)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range res.Items {
			if p.App != acme.Summary() || len(p.Metadata) != 1 {
				t.Fatalf("row = %+v", p)
			}
			seen = append(seen, p.PublicID)
		}
		if res.NextCursor == "" {
			break
		}
		c, _ := domain.DecodeCursor(res.NextCursor)
		page.After = &c
	}
	if len(seen) != 3 || !sortedDesc(seen) {
		t.Fatalf("acme payments = %v", seen)
	}
	from := same.Add(time.Second)
	if res, _ := payments.List(ctx, domain.PaymentFilter{From: &from}, domain.Page{Limit: 10}); len(res.Items) != 0 {
		t.Fatalf("from filter = %d rows", len(res.Items))
	}
	unknown := id.New(id.App)
	if res, _ := payments.List(ctx, domain.PaymentFilter{AppPublicID: &unknown}, domain.Page{Limit: 10}); len(res.Items) != 0 {
		t.Fatalf("unknown app = %d rows", len(res.Items))
	}

	entries, err := ledger.List(ctx, domain.LedgerFilter{AppPublicID: &acmeID})
	if err != nil || len(entries) != 3 {
		t.Fatalf("ledger = %+v %v", entries, err)
	}
	for _, e := range entries {
		if e.App != acme.Summary() || !id.Valid(e.PaymentPublicID, id.Payment) {
			t.Fatalf("ledger row = %+v", e)
		}
	}
	summary, err := ledger.Summary(ctx, domain.LedgerFilter{})
	if err != nil || len(summary) != 2 || summary[0].App != acme.Summary() || summary[0].Count != 3 || summary[0].NetAmount != 2970 ||
		summary[1].App != globex.Summary() {
		t.Fatalf("summary = %+v %v", summary, err)
	}
	ds, err := deliveries.List(ctx, domain.DeliveryFilter{AppPublicID: &acmeID}, domain.Page{Limit: 50})
	if err != nil || len(ds.Items) != 3 {
		t.Fatalf("deliveries = %+v %v", ds, err)
	}
	for _, d := range ds.Items {
		if d.App != acme.Summary() || d.AppID != acme.ID || !id.Valid(d.PaymentPublicID, id.Payment) {
			t.Fatalf("delivery row = %+v", d)
		}
	}
	if all, _ := deliveries.List(ctx, domain.DeliveryFilter{}, domain.Page{Limit: 50}); len(all.Items) != 4 {
		t.Fatalf("all deliveries = %d", len(all.Items))
	}

	// Lookups are scoped to the app.
	p := acmePayments[0]
	for name, get := range map[string]func(int64) (*domain.Payment, error){
		"public id": func(app int64) (*domain.Payment, error) { return payments.GetByPublicID(ctx, app, p.PublicID) },
		"id":        func(app int64) (*domain.Payment, error) { return payments.GetByID(ctx, app, p.ID) },
		"order id":  func(app int64) (*domain.Payment, error) { return payments.GetByOrderID(ctx, app, p.OrderID) },
	} {
		if got, err := get(acme.ID); err != nil || got.ID != p.ID {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
		if _, err := get(globex.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s from another app: %v", name, err)
		}
	}
}
