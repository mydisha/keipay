package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

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
	pool    *pgxpool.Pool
	tenants *postgres.TenantRepo
	tx      *postgres.TxManager
	audit   *service.AuditService
	slug    string
}

func newAuditFixture(t *testing.T) *auditFixture {
	t.Helper()
	pool := testPool(t)
	f := &auditFixture{
		pool:    pool,
		tenants: postgres.NewTenantRepo(pool),
		tx:      postgres.NewTxManager(pool),
		audit:   service.NewAuditService(postgres.NewAuditRepo(pool)),
		slug:    fmt.Sprintf("it-audit-%d", time.Now().UnixNano()),
	}
	t.Cleanup(func() {
		ctx := context.Background()
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM tenants WHERE slug=$1`, f.slug).Scan(&id); err == nil {
			_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE target_type='tenant' AND target_id=$1`, strconv.FormatInt(id, 10))
			_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, id)
		}
	})
	return f
}

func (f *auditFixture) service(audit service.Auditor) *service.TenantService {
	return service.NewTenantService(f.tenants, nil, plainCipher{}, f.tx, audit)
}

func actorCtx() context.Context {
	return domain.WithActor(context.Background(), domain.Actor{
		Type: domain.ActorAdmin, ID: "admin", IP: "203.0.113.7", UserAgent: "it-agent", RequestID: "req-it",
	})
}

type auditRow struct {
	actorType, actorID, action, ip, ua, rid string
	before, after                           []byte
}

func (f *auditFixture) rows(t *testing.T, tenantID int64) []auditRow {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT actor_type, actor_id, action, ip, user_agent, request_id, before, after
		FROM audit_logs WHERE target_type='tenant' AND target_id=$1 ORDER BY occurred_at, public_id`, strconv.FormatInt(tenantID, 10))
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.actorType, &r.actorID, &r.action, &r.ip, &r.ua, &r.rid, &r.before, &r.after); err != nil {
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

func TestIntegrationTenantMutationsWriteOneEntryEach(t *testing.T) {
	f := newAuditFixture(t)
	svc := f.service(f.audit)
	ctx := actorCtx()

	tenant, creds, err := svc.Create(ctx, service.CreateTenantInput{Name: "IT", Slug: f.slug})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Update(ctx, tenant.ID, service.UpdateTenantInput{Name: "IT Ünïcode", Active: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	_, rotated, err := svc.RotateCredentials(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	rows := f.rows(t, tenant.ID)
	if len(rows) != 3 {
		t.Fatalf("got %d entries, want 3", len(rows))
	}
	for i, action := range []string{"tenant.create", "tenant.update", "tenant.rotate_credentials"} {
		r := rows[i]
		if r.action != action || r.actorType != "admin" || r.actorID != "admin" || r.ip != "203.0.113.7" || r.ua != "it-agent" || r.rid != "req-it" {
			t.Fatalf("entry %d = %+v", i, r)
		}
		for _, secret := range []string{creds.APISecret, rotated.APISecret, creds.APIKey, rotated.APIKey, "enc:"} {
			if contains(r.before, secret) || contains(r.after, secret) {
				t.Fatalf("entry %d stores secret material", i)
			}
		}
	}
	if rows[0].before != nil {
		t.Fatalf("create before = %s, want NULL", rows[0].before)
	}
	jsonEqual(t, "update after name", pick(t, rows[1].after, "name"), "IT Ünïcode")
	jsonEqual(t, "rotate before", rows[2].before, map[string]any{"api_key_last4": creds.APIKey[len(creds.APIKey)-4:]})
	jsonEqual(t, "rotate after", rows[2].after, map[string]any{"api_key_last4": rotated.APIKey[len(rotated.APIKey)-4:]})
}

func TestIntegrationFailedTxRollsBackUpdateAndEntry(t *testing.T) {
	f := newAuditFixture(t)
	tenant, _, err := f.service(f.audit).Create(actorCtx(), service.CreateTenantInput{Name: "Before", Slug: f.slug})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = f.service(failAfter{f.audit}).Update(actorCtx(), tenant.ID, service.UpdateTenantInput{Name: "After", Active: true})
	if !errors.Is(err, errAfterAudit) {
		t.Fatalf("err = %v, want errAfterAudit", err)
	}
	stored, err := f.tenants.GetByID(context.Background(), tenant.ID)
	if err != nil || stored.Name != "Before" {
		t.Fatalf("tenant = %+v, %v; update should have rolled back", stored, err)
	}
	if rows := f.rows(t, tenant.ID); len(rows) != 1 || rows[0].action != "tenant.create" {
		t.Fatalf("entries = %+v, want only tenant.create", rows)
	}

	// Without an actor the audit insert never happens and the update rolls back.
	if _, err := f.service(f.audit).Update(context.Background(), tenant.ID, service.UpdateTenantInput{Name: "After"}); !errors.Is(err, service.ErrNoActor) {
		t.Fatalf("err = %v, want ErrNoActor", err)
	}
	if stored, _ := f.tenants.GetByID(context.Background(), tenant.ID); stored.Name != "Before" {
		t.Fatalf("tenant updated without an audit entry: %+v", stored)
	}
}

func TestIntegrationFailedCreateLeavesNoTenant(t *testing.T) {
	f := newAuditFixture(t)
	if _, _, err := f.service(failAfter{f.audit}).Create(actorCtx(), service.CreateTenantInput{Name: "X", Slug: f.slug}); !errors.Is(err, errAfterAudit) {
		t.Fatalf("err = %v, want errAfterAudit", err)
	}
	if _, err := f.tenants.GetBySlug(context.Background(), f.slug); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("tenant persisted despite rollback: %v", err)
	}
}

func TestIntegrationDuplicateSlugInsideTxIsConflict(t *testing.T) {
	f := newAuditFixture(t)
	svc := f.service(f.audit)
	tenant, _, err := svc.Create(actorCtx(), service.CreateTenantInput{Name: "One", Slug: f.slug})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := svc.Create(actorCtx(), service.CreateTenantInput{Name: "Two", Slug: f.slug}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if rows := f.rows(t, tenant.ID); len(rows) != 1 {
		t.Fatalf("got %d entries, want 1", len(rows))
	}
}

func TestIntegrationAdminPasswordChangeJoinsTx(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	username := fmt.Sprintf("it-admin-%d", time.Now().UnixNano())
	hash, _ := bcrypt.GenerateFromPassword([]byte("current-pass"), bcrypt.MinCost)
	admins := postgres.NewAdminRepo(pool)
	user := &domain.AdminUser{Username: username, PasswordHash: string(hash), MustChangePassword: true}
	if err := admins.Create(ctx, user); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE target_type='admin_user' AND target_id=$1`, username)
		_, _ = pool.Exec(ctx, `DELETE FROM admin_users WHERE id=$1`, user.ID)
	})

	audit := service.NewAuditService(postgres.NewAuditRepo(pool))
	failing := service.NewAuthService(admins, "it-secret", postgres.NewTxManager(pool), failAfter{audit})
	actor := domain.WithActor(ctx, domain.Actor{Type: domain.ActorAdmin, ID: username})
	if err := failing.ChangePassword(actor, username, "current-pass", "new-password-1"); !errors.Is(err, errAfterAudit) {
		t.Fatalf("err = %v, want errAfterAudit", err)
	}
	stored, _ := admins.GetByUsername(ctx, username)
	if stored.PasswordHash != string(hash) || !stored.MustChangePassword {
		t.Fatal("password changed despite rollback")
	}

	ok := service.NewAuthService(admins, "it-secret", postgres.NewTxManager(pool), audit)
	if err := ok.ChangePassword(actor, username, "current-pass", "new-password-1"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	var n int
	var before, after []byte
	if err := pool.QueryRow(ctx, `SELECT count(*), min(before::text)::jsonb, min(after::text)::jsonb FROM audit_logs
		WHERE target_type='admin_user' AND target_id=$1 AND action='admin_user.change_password'`, username).Scan(&n, &before, &after); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("got %d entries, want 1", n)
	}
	jsonEqual(t, "before", before, map[string]any{"must_change_password": true})
	jsonEqual(t, "after", after, map[string]any{"must_change_password": false})
}

func contains(b []byte, s string) bool {
	return s != "" && strings.Contains(string(b), s)
}

func pick(t *testing.T, raw []byte, key string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return m[key]
}
