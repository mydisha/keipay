package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
)

// newTestApp inserts an app with a unique slug in the shared database and
// removes it, with everything that references it, when the test ends.
func newTestApp(t *testing.T, pool *pgxpool.Pool, env domain.Environment) *domain.App {
	t.Helper()
	a := &domain.App{PublicID: id.New(id.App), Slug: fmt.Sprintf("it-%d", time.Now().UnixNano()), Name: "IT",
		Environment: env, Status: domain.AppActive, WebhookSecretEnc: "enc:wh"}
	if err := postgres.NewAppRepo(pool).Create(context.Background(), a); err != nil {
		t.Fatalf("app: %v", err)
	}
	t.Cleanup(func() { deleteApp(pool, a) })
	return a
}

func deleteApp(pool *pgxpool.Pool, a *domain.App) {
	ctx := context.Background()
	for _, q := range []string{
		`DELETE FROM webhook_deliveries WHERE app_id=$1`,
		`DELETE FROM ledger_entries WHERE app_id=$1`,
		`DELETE FROM payments WHERE app_id=$1`,
		`DELETE FROM app_api_keys WHERE app_id=$1`,
	} {
		_, _ = pool.Exec(ctx, q, a.ID)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE target_id=$1`, a.PublicID)
	_, _ = pool.Exec(ctx, `DELETE FROM apps WHERE id=$1`, a.ID)
}

// newTestConnection inserts a fake-provider connection with a unique name in
// the shared database and removes it, with every reference to it, when the
// test ends.
func newTestConnection(t *testing.T, pool *pgxpool.Pool, env domain.Environment) *domain.Connection {
	t.Helper()
	ctx := context.Background()
	m, err := postgres.NewMerchantRepo(pool).GetDefault(ctx)
	if err != nil {
		t.Fatalf("merchant: %v", err)
	}
	c := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: m.ID, Provider: "xendit",
		Name: fmt.Sprintf("it-%d", time.Now().UnixNano()), Environment: env, Status: domain.ConnectionActive,
		CredentialsEnc: "sealed", CredentialsDEKEnc: "wrapped", KeyVersion: 1,
		CredentialsLast4: map[string]*string{"secret_key": nil}, Settings: map[string]any{}}
	if err := postgres.NewConnectionRepo(pool).Create(ctx, c); err != nil {
		t.Fatalf("connection: %v", err)
	}
	t.Cleanup(func() { deleteConnection(pool, c.ID, c.PublicID) })
	return c
}

func deleteConnection(pool *pgxpool.Pool, id int64, publicID string) {
	ctx := context.Background()
	for _, q := range []string{
		`UPDATE apps SET default_connection_id = NULL WHERE default_connection_id = $1`,
		`UPDATE payments SET connection_id = NULL WHERE connection_id = $1`,
		`UPDATE merchants SET legacy_connection_id = NULL WHERE legacy_connection_id = $1`,
		`DELETE FROM connections WHERE id = $1`,
	} {
		_, _ = pool.Exec(ctx, q, id)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE target_id = $1`, publicID)
}
