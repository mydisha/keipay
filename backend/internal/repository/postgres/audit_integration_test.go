package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("KEI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("KEI_TEST_POSTGRES_DSN not set")
	}
	pool, err := postgres.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type plainCipher struct{}

func (plainCipher) Encrypt(s string) (string, error) { return "enc:" + s, nil }
func (plainCipher) Decrypt(s string) (string, error) { return s[len("enc:"):], nil }

// failAfter records the entry for real and then fails, so the transaction
// holding both the mutation and the entry must roll back.
type failAfter struct{ inner service.Auditor }

var errAfterAudit = errors.New("fail after audit")

func (f failAfter) Record(ctx context.Context, action string, target domain.AuditTarget, before, after any) error {
	if err := f.inner.Record(ctx, action, target, before, after); err != nil {
		return err
	}
	return errAfterAudit
}

type auditFixture struct {
	pool  *pgxpool.Pool
	apps  *postgres.AppRepo
	keys  *postgres.APIKeyRepo
	tx    *postgres.TxManager
	audit *service.AuditService
	slug  string
}

func newAuditFixture(t *testing.T) *auditFixture {
	t.Helper()
	pool := testPool(t)
	f := &auditFixture{
		pool:  pool,
		apps:  postgres.NewAppRepo(pool),
		keys:  postgres.NewAPIKeyRepo(pool),
		tx:    postgres.NewTxManager(pool),
		audit: service.NewAuditService(postgres.NewAuditRepo(pool)),
		slug:  fmt.Sprintf("it-audit-%d", time.Now().UnixNano()),
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, env := range []domain.Environment{domain.EnvTest, domain.EnvLive} {
			if a, err := f.apps.GetBySlug(ctx, env, f.slug); err == nil {
				rows, _ := pool.Query(ctx, `SELECT public_id FROM app_api_keys WHERE app_id=$1`, a.ID)
				var keyIDs []string
				for rows.Next() {
					var k string
					_ = rows.Scan(&k)
					keyIDs = append(keyIDs, k)
				}
				rows.Close()
				_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE target_id = ANY($1)`, keyIDs)
				deleteApp(pool, a)
			}
		}
	})
	return f
}

func (f *auditFixture) service(audit service.Auditor) *service.AppService {
	return service.NewAppService(f.apps, f.keys, postgres.NewConnectionRepo(f.pool), plainCipher{}, f.tx, audit)
}

func actorCtx() context.Context {
	return domain.WithActor(context.Background(), domain.Actor{
		Type: domain.ActorAdmin, ID: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", IP: "203.0.113.7", UserAgent: "it-agent", RequestID: "req-it",
	})
}

type auditRow struct {
	actorType, actorID, action, targetType, targetID, ip, ua, rid string
	before, after                                                 []byte
}

// rows returns the entries of the app and its keys, oldest first.
func (f *auditFixture) rows(t *testing.T, app *domain.App) []auditRow {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT actor_type, actor_id, action, target_type, target_id, ip, user_agent,
		request_id, before, after FROM audit_logs
		WHERE target_id = $1 OR target_id IN (SELECT public_id FROM app_api_keys WHERE app_id = $2)
		ORDER BY occurred_at, public_id`, app.PublicID, app.ID)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.actorType, &r.actorID, &r.action, &r.targetType, &r.targetID, &r.ip, &r.ua, &r.rid, &r.before, &r.after); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func jsonEqual(t *testing.T, label string, got []byte, want any) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: %v (%s)", label, err, got)
	}
	b, _ := json.Marshal(want)
	_ = json.Unmarshal(b, &w)
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s = %s, want %s", label, got, b)
	}
}

// AC-12: every app and key mutation writes its entries in the same
// transaction, with no secret material.
func TestIntegrationAppMutationsWriteTheirEntries(t *testing.T) {
	f := newAuditFixture(t)
	svc := f.service(f.audit)
	ctx := actorCtx()

	created, err := svc.Create(ctx, service.CreateAppInput{Name: "IT", Slug: f.slug, Environment: domain.EnvTest})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	name := "IT Ünïcode"
	if _, err := svc.Update(ctx, created.App.PublicID, service.UpdateAppInput{Name: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	rotated, err := svc.RotateKey(ctx, created.App.PublicID, created.Key.PublicID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, _, err := svc.RevokeKey(ctx, created.App.PublicID, created.Key.PublicID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	rows := f.rows(t, created.App)
	want := []string{"app.create", "api_key.create", "app.update", "api_key.rotate", "api_key.revoke"}
	if len(rows) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(rows), len(want), rows)
	}
	for i, r := range rows {
		if r.action != want[i] || r.actorType != "admin" || r.ip != "203.0.113.7" || r.ua != "it-agent" || r.rid != "req-it" {
			t.Fatalf("entry %d = %+v", i, r)
		}
		for _, secret := range []string{created.APIKey, created.APISecret, rotated.APIKey, rotated.APISecret,
			created.Key.Hash, rotated.Key.Hash, "enc:"} {
			if contains(r.before, secret) || contains(r.after, secret) {
				t.Fatalf("entry %d stores secret material", i)
			}
		}
	}
	if rows[0].before != nil || rows[0].targetType != "app" || rows[1].targetType != "api_key" || rows[3].targetID != rotated.Key.PublicID {
		t.Fatalf("entries = %+v", rows)
	}
	jsonEqual(t, "update before", rows[2].before, map[string]any{"name": "IT"})
	jsonEqual(t, "update after", rows[2].after, map[string]any{"name": "IT Ünïcode"})
	jsonEqual(t, "revoke before", rows[4].before, map[string]any{"revoked_at": nil})
}

// T-30 / AC-12: a failing audit insert rolls the update back.
func TestIntegrationFailedAuditRollsBackUpdate(t *testing.T) {
	f := newAuditFixture(t)
	created, err := f.service(f.audit).Create(actorCtx(), service.CreateAppInput{Name: "Before", Slug: f.slug, Environment: domain.EnvLive})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	name := "After"
	if _, err := f.service(failAfter{f.audit}).Update(actorCtx(), created.App.PublicID, service.UpdateAppInput{Name: &name}); !errors.Is(err, errAfterAudit) {
		t.Fatalf("err = %v, want errAfterAudit", err)
	}
	stored, err := f.apps.GetByPublicID(context.Background(), created.App.PublicID)
	if err != nil || stored.Name != "Before" {
		t.Fatalf("app = %+v, %v; update should have rolled back", stored, err)
	}
	if rows := f.rows(t, created.App); len(rows) != 2 {
		t.Fatalf("entries = %+v, want only the create pair", rows)
	}

	// Without an actor the audit insert never happens and the update rolls back.
	if _, err := f.service(f.audit).Update(context.Background(), created.App.PublicID, service.UpdateAppInput{Name: &name}); !errors.Is(err, service.ErrNoActor) {
		t.Fatalf("err = %v, want ErrNoActor", err)
	}
	if stored, _ := f.apps.GetByPublicID(context.Background(), created.App.PublicID); stored.Name != "Before" {
		t.Fatalf("app updated without an audit entry: %+v", stored)
	}
}

func TestIntegrationFailedCreateLeavesNoApp(t *testing.T) {
	f := newAuditFixture(t)
	if _, err := f.service(failAfter{f.audit}).Create(actorCtx(), service.CreateAppInput{Name: "X", Slug: f.slug, Environment: domain.EnvLive}); !errors.Is(err, errAfterAudit) {
		t.Fatalf("err = %v, want errAfterAudit", err)
	}
	if _, err := f.apps.GetBySlug(context.Background(), domain.EnvLive, f.slug); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("app persisted despite rollback: %v", err)
	}
}

// AC-9: a duplicate slug inside the audit transaction is a conflict; the same
// slug in the other environment is fine.
func TestIntegrationDuplicateSlugInsideTxIsConflict(t *testing.T) {
	f := newAuditFixture(t)
	svc := f.service(f.audit)
	created, err := svc.Create(actorCtx(), service.CreateAppInput{Name: "One", Slug: f.slug, Environment: domain.EnvLive})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Create(actorCtx(), service.CreateAppInput{Name: "Two", Slug: f.slug, Environment: domain.EnvLive}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if rows := f.rows(t, created.App); len(rows) != 2 {
		t.Fatalf("got %d entries, want 2", len(rows))
	}
	if _, err := svc.Create(actorCtx(), service.CreateAppInput{Name: "Test", Slug: f.slug, Environment: domain.EnvTest}); err != nil {
		t.Fatalf("other environment: %v", err)
	}
}

func contains(b []byte, s string) bool {
	return s != "" && strings.Contains(string(b), s)
}
