package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name=$1 AND column_name=$2)`, table, column).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func tableExists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// T-40 / AC-24: 000008 runs up, down and up on a database with phase 2 data.
func TestIntegrationMigration8UpDownUp(t *testing.T) {
	db := pgtest.New(t, 7)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	exec(t, pool, `INSERT INTO apps (id, public_id, slug, name, environment, webhook_secret_enc)
		VALUES (1, 'app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C', 'acme', 'Acme', 'live', 'enc'),
		       (2, 'app_01JBKQ7Z8M4X2Y6V3N9T5R1W0D', 'demo', 'Demo', 'test', 'enc')`)
	exec(t, pool, `SELECT setval(pg_get_serial_sequence('apps', 'id'), 2)`)
	exec(t, pool, `INSERT INTO payments (public_id, app_id, order_id, external_id, amount)
		VALUES ('pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C', 1, 'o-1', 'kp_pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C', 1000)`)

	for round := range 2 {
		if err := db.Migrate.Migrate(8); err != nil {
			t.Fatalf("round %d up: %v", round, err)
		}
		var merchants, unassigned int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM merchants WHERE is_default`).Scan(&merchants)
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM apps WHERE merchant_id IS DISTINCT FROM (SELECT id FROM merchants WHERE is_default)`).Scan(&unassigned)
		if merchants != 1 || unassigned != 0 {
			t.Fatalf("round %d: default merchants=%d apps without it=%d", round, merchants, unassigned)
		}
		apps := postgres.NewAppRepo(pool)
		a, err := apps.GetByID(ctx, 1)
		if err != nil || a.MerchantID == 0 || a.DefaultConnectionID != nil || a.DefaultConnection != nil {
			t.Fatalf("round %d: app = %+v %v", round, a, err)
		}
		p, err := postgres.NewPaymentRepo(pool).GetByID(ctx, 1, 1)
		if err != nil || p.ConnectionID != nil || p.Provider != "" {
			t.Fatalf("round %d: payment = %+v %v", round, p, err)
		}

		if err := db.Migrate.Migrate(7); err != nil {
			t.Fatalf("round %d down: %v", round, err)
		}
		for _, table := range []string{"merchants", "connections", "connection_channels"} {
			if tableExists(t, pool, table) {
				t.Fatalf("round %d: %s survived down", round, table)
			}
		}
		for _, col := range [][2]string{{"apps", "merchant_id"}, {"apps", "default_connection_id"}, {"payments", "connection_id"}, {"payments", "provider"}} {
			if columnExists(t, pool, col[0], col[1]) {
				t.Fatalf("round %d: %s.%s survived down", round, col[0], col[1])
			}
		}
		var n int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM apps`).Scan(&n)
		if n != 2 {
			t.Fatalf("round %d: apps after down = %d", round, n)
		}
	}
}

// newConnection stores a connection with sealed placeholder credentials.
func newConnection(t *testing.T, repo *postgres.ConnectionRepo, merchantID int64, name string, env domain.Environment) *domain.Connection {
	t.Helper()
	tail := "abcd"
	c := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: merchantID, Provider: "xendit", Name: name,
		Environment: env, Status: domain.ConnectionActive, CredentialsEnc: "sealed", CredentialsDEKEnc: "wrapped", KeyVersion: 1,
		CredentialsLast4: map[string]*string{"secret_key": &tail, "callback_token": nil},
		Settings:         map[string]any{"invoice_duration": int64(3600), "locale": "id"}}
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return c
}

// T-41 / AC-5, AC-10
func TestIntegrationConnectionRepo(t *testing.T) {
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	repo := postgres.NewConnectionRepo(pool)
	m, err := postgres.NewMerchantRepo(pool).GetDefault(ctx)
	if err != nil || !m.IsDefault || m.LegacyConnectionID != nil {
		t.Fatalf("merchant = %+v %v", m, err)
	}

	a := newConnection(t, repo, m.ID, "conn-a", domain.EnvTest)
	time.Sleep(2 * time.Millisecond)
	b := newConnection(t, repo, m.ID, "conn-b", domain.EnvLive)
	time.Sleep(2 * time.Millisecond)
	c := newConnection(t, repo, m.ID, "conn-c", domain.EnvTest)

	got, err := repo.GetByPublicID(ctx, a.PublicID)
	if err != nil || got.ID != a.ID || got.Name != "conn-a" || got.KeyVersion != 1 || got.LegacyWebhookAlias ||
		*got.CredentialsLast4["secret_key"] != "abcd" || got.CredentialsLast4["callback_token"] != nil {
		t.Fatalf("get = %+v %v", got, err)
	}
	if fmt.Sprint(got.Settings) != "map[invoice_duration:3600 locale:id]" {
		t.Fatalf("settings = %v", got.Settings)
	}
	if _, err := repo.GetByPublicID(ctx, id.New(id.Connection)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	dup := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: m.ID, Provider: "xendit", Name: "conn-a",
		Environment: domain.EnvLive, Status: domain.ConnectionActive, CredentialsEnc: "x", CredentialsDEKEnc: "y", KeyVersion: 1}
	if err := repo.Create(ctx, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	b.Name = "conn-a"
	if err := repo.Update(ctx, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("rename to a taken name: %v", err)
	}
	if taken, err := repo.NameTaken(ctx, m.ID, "conn-c"); err != nil || !taken {
		t.Fatalf("NameTaken = %v %v", taken, err)
	}

	// Keyset pages, newest first, with filters.
	p1, err := repo.List(ctx, domain.ConnectionFilter{}, domain.Page{Limit: 2})
	if err != nil || len(p1.Items) != 2 || p1.Items[0].ID != c.ID || p1.Items[1].ID != b.ID || p1.NextCursor == "" {
		t.Fatalf("page 1 = %+v %v", p1, err)
	}
	cur, _ := domain.DecodeCursor(p1.NextCursor)
	p2, err := repo.List(ctx, domain.ConnectionFilter{}, domain.Page{Limit: 2, After: &cur})
	if err != nil || len(p2.Items) != 1 || p2.Items[0].ID != a.ID || p2.NextCursor != "" {
		t.Fatalf("page 2 = %+v %v", p2, err)
	}
	env, status, prov := domain.EnvTest, domain.ConnectionActive, "xendit"
	if res, _ := repo.List(ctx, domain.ConnectionFilter{Environment: &env, Status: &status, Provider: &prov}, domain.Page{Limit: 10}); len(res.Items) != 2 {
		t.Fatalf("filtered = %+v", res.Items)
	}

	// Update writes credentials, hints and the test result.
	now := time.Now().UTC().Truncate(time.Microsecond)
	failed, msg := domain.ConnectionTestFailed, "Could not reach the provider. Try again later."
	a.CredentialsEnc, a.CredentialsLast4 = "resealed", map[string]*string{"secret_key": nil}
	a.LastTestedAt, a.LastTestStatus, a.LastTestError, a.Status = &now, &failed, &msg, domain.ConnectionDisabled
	if err := repo.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.GetByID(ctx, a.ID)
	if got.CredentialsEnc != "resealed" || got.CredentialsLast4["secret_key"] != nil || !got.LastTestedAt.Equal(now) ||
		*got.LastTestStatus != failed || *got.LastTestError != msg || got.Status != domain.ConnectionDisabled {
		t.Fatalf("updated = %+v", got)
	}

	// Channels replace inside a transaction; a rollback keeps the old rows.
	tx := postgres.NewTxManager(pool)
	if err := repo.ReplaceChannels(ctx, a.ID, []domain.ConnectionChannel{{Channel: domain.ChannelQRIS, Enabled: true}, {Channel: domain.ChannelVABCA}}); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err = tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := repo.ReplaceChannels(ctx, a.ID, nil); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	rows, err := repo.Channels(ctx, a.ID)
	if err != nil || fmt.Sprint(rows) != "[{QRIS true} {VA_BCA false}]" {
		t.Fatalf("channels after rollback = %v %v", rows, err)
	}

	// Legacy alias, and the apps that use a connection.
	merchants := postgres.NewMerchantRepo(pool)
	if err := merchants.SetLegacyConnection(ctx, m.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByID(ctx, c.ID); !got.LegacyWebhookAlias {
		t.Fatal("legacy alias not read")
	}
	apps := postgres.NewAppRepo(pool)
	for i := range 3 {
		app := &domain.App{PublicID: id.New(id.App), Slug: fmt.Sprintf("app-%d", i), Name: "A", Environment: domain.EnvTest,
			Status: domain.AppActive, WebhookSecretEnc: "e"}
		if err := apps.Create(ctx, app); err != nil {
			t.Fatal(err)
		}
		app.DefaultConnectionID = &c.ID
		if err := apps.Update(ctx, app); err != nil {
			t.Fatal(err)
		}
	}
	using, n, err := repo.AppsUsing(ctx, c.ID, 2)
	if err != nil || n != 3 || len(using) != 2 || using[0].Slug != "app-2" {
		t.Fatalf("apps using = %+v %d %v", using, n, err)
	}
	if using, n, _ := repo.AppsUsing(ctx, b.ID, 10); n != 0 || len(using) != 0 {
		t.Fatalf("unused = %v %d", using, n)
	}
	// FOR UPDATE OF / FOR SHARE OF work with the joins.
	if err := tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := repo.GetByPublicIDForUpdate(ctx, a.PublicID); err != nil {
			return err
		}
		if _, err := repo.GetByPublicIDForShare(ctx, b.PublicID); err != nil {
			return err
		}
		_, err := apps.ListWithoutDefaultConnection(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// T-42 / AC-14: the database refuses an app default of another environment
// or merchant even without the service.
func TestIntegrationDefaultConnectionForeignKey(t *testing.T) {
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	repo := postgres.NewConnectionRepo(pool)
	m, _ := postgres.NewMerchantRepo(pool).GetDefault(ctx)
	test := newConnection(t, repo, m.ID, "conn-test", domain.EnvTest)
	live := newConnection(t, repo, m.ID, "conn-live", domain.EnvLive)
	var otherMerchant int64
	if err := pool.QueryRow(ctx, `INSERT INTO merchants (name) VALUES ('Other') RETURNING id`).Scan(&otherMerchant); err != nil {
		t.Fatal(err)
	}
	foreign := newConnection(t, repo, otherMerchant, "conn-foreign", domain.EnvTest)

	apps := postgres.NewAppRepo(pool)
	app := &domain.App{PublicID: id.New(id.App), Slug: "fk", Name: "FK", Environment: domain.EnvTest, Status: domain.AppActive, WebhookSecretEnc: "e"}
	if err := apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE apps SET default_connection_id=$1 WHERE id=$2`, test.ID, app.ID); err != nil {
		t.Fatalf("same environment: %v", err)
	}
	for name, connID := range map[string]int64{"other environment": live.ID, "other merchant": foreign.ID} {
		_, err := pool.Exec(ctx, `UPDATE apps SET default_connection_id=$1 WHERE id=$2`, connID, app.ID)
		if err == nil || !strings.Contains(err.Error(), "apps_default_connection_fkey") {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	got, _ := apps.GetByID(ctx, app.ID)
	if got.DefaultConnectionID == nil || *got.DefaultConnectionID != test.ID || got.DefaultConnection.PublicID != test.PublicID {
		t.Fatalf("app = %+v", got)
	}
}

// T-43 / AC-23: two servers importing at the same time import once.
func TestIntegrationConcurrentLegacyImport(t *testing.T) {
	db := pgtest.New(t, 0)
	ctx := context.Background()
	setup := openPool(t, db.DSN)
	apps := postgres.NewAppRepo(setup)
	demo := &domain.App{PublicID: id.New(id.App), Slug: "demo", Name: "Demo", Environment: domain.EnvTest, Status: domain.AppActive, WebhookSecretEnc: "e"}
	if err := apps.Create(ctx, demo); err != nil {
		t.Fatal(err)
	}
	exec(t, setup, `INSERT INTO payments (public_id, app_id, order_id, external_id, amount) VALUES ($1, $2, 'o-1', 'demo:o-1', 1000)`,
		id.New(id.Payment), demo.ID)

	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	newService := func() *service.ConnectionService {
		pool := openPool(t, db.DSN)
		registry, _ := provider.NewRegistry(xendit.New(xendit.WithBaseURL("http://127.0.0.1:1")))
		return service.NewConnectionService(service.ConnectionDeps{
			Connections: postgres.NewConnectionRepo(pool), Merchants: postgres.NewMerchantRepo(pool), Apps: postgres.NewAppRepo(pool),
			Payments: postgres.NewPaymentRepo(pool), Registry: registry, Sealer: cipher, Tx: postgres.NewTxManager(pool),
			Audit: service.NewAuditService(postgres.NewAuditRepo(pool)), BaseURL: "https://pay.example.com",
		})
	}
	servers := []*service.ConnectionService{newService(), newService()}
	var wg sync.WaitGroup
	results := make([]*service.LegacyImport, len(servers))
	errs := make([]error, len(servers))
	for i, s := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = s.ImportLegacyXendit(ctx, "xnd_development_concurrent", "concurrent-token")
		}()
	}
	wg.Wait()
	created := 0
	for i := range servers {
		if errs[i] != nil {
			t.Fatalf("server %d: %v", i, errs[i])
		}
		if results[i].Created {
			created++
		}
	}
	if created != 1 || results[0].Connection.ID != results[1].Connection.ID {
		t.Fatalf("created=%d connections %d/%d", created, results[0].Connection.ID, results[1].Connection.ID)
	}
	var conns, actions int
	var legacy *int64
	_ = setup.QueryRow(ctx, `SELECT count(*) FROM connections`).Scan(&conns)
	_ = setup.QueryRow(ctx, `SELECT legacy_connection_id FROM merchants WHERE is_default`).Scan(&legacy)
	_ = setup.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE actor_type='system' AND actor_id='import'`).Scan(&actions)
	if conns != 1 || legacy == nil || *legacy != results[0].Connection.ID || actions != 2 {
		t.Fatalf("connections=%d legacy=%v audit=%d", conns, legacy, actions)
	}
	var unassigned int
	_ = setup.QueryRow(ctx, `SELECT count(*) FROM payments WHERE connection_id IS DISTINCT FROM $1`, *legacy).Scan(&unassigned)
	if unassigned != 0 {
		t.Fatalf("%d payments not backfilled", unassigned)
	}
	got, _ := apps.GetByID(ctx, demo.ID)
	if got.DefaultConnection == nil || got.DefaultConnection.Name != "xendit-imported" {
		t.Fatalf("demo default = %+v", got.DefaultConnection)
	}
}

// countingTracer counts the queries a pool runs.
type countingTracer struct{ n atomic.Int64 }

func (c *countingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *countingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// T-44 / AC-14: listing apps with their default connections is one query.
func TestIntegrationAppListLoadsDefaultsInOneQuery(t *testing.T) {
	db := pgtest.New(t, 0)
	ctx := context.Background()
	setup := openPool(t, db.DSN)
	repo := postgres.NewConnectionRepo(setup)
	m, _ := postgres.NewMerchantRepo(setup).GetDefault(ctx)
	conn := newConnection(t, repo, m.ID, "conn-test", domain.EnvTest)
	apps := postgres.NewAppRepo(setup)
	for i := range 5 {
		a := &domain.App{PublicID: id.New(id.App), Slug: fmt.Sprintf("app-%d", i), Name: "A", Environment: domain.EnvTest,
			Status: domain.AppActive, WebhookSecretEnc: "e"}
		if err := apps.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			a.DefaultConnectionID = &conn.ID
			if err := apps.Update(ctx, a); err != nil {
				t.Fatal(err)
			}
		}
	}

	cfg, err := pgxpool.ParseConfig(db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	tracer := &countingTracer{}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	tracer.n.Store(0)
	res, err := postgres.NewAppRepo(pool).List(ctx, domain.AppFilter{}, domain.Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got := tracer.n.Load(); got != 1 {
		t.Fatalf("list ran %d queries, want 1", got)
	}
	with := 0
	for _, a := range res.Items {
		if a.DefaultConnection != nil {
			if a.DefaultConnection.PublicID != conn.PublicID || a.DefaultConnection.Provider != "xendit" {
				t.Fatalf("summary = %+v", a.DefaultConnection)
			}
			with++
		}
	}
	if len(res.Items) != 5 || with != 3 {
		t.Fatalf("items=%d with default=%d", len(res.Items), with)
	}
}
