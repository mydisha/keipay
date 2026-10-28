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
