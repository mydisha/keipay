package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// memTenantRepo enforces UNIQUE(slug) and undoes writes on fake rollback.
type memTenantRepo struct {
	byID   map[int64]*domain.Tenant
	nextID int64
}

func newMemTenantRepo() *memTenantRepo { return &memTenantRepo{byID: map[int64]*domain.Tenant{}} }

func (m *memTenantRepo) Create(ctx context.Context, t *domain.Tenant) error {
	for _, existing := range m.byID {
		if existing.Slug == t.Slug {
			return domain.ErrConflict
		}
	}
	m.nextID++
	t.ID = m.nextID
	cp := *t
	m.byID[t.ID] = &cp
	onRollback(ctx, func() { delete(m.byID, cp.ID) })
	return nil
}

func (m *memTenantRepo) Update(ctx context.Context, t *domain.Tenant) error {
	prev, ok := m.byID[t.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cp := *t
	m.byID[t.ID] = &cp
	onRollback(ctx, func() { m.byID[t.ID] = prev })
	return nil
}

func (m *memTenantRepo) GetByID(_ context.Context, id int64) (*domain.Tenant, error) {
	t, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *memTenantRepo) GetByIDForUpdate(ctx context.Context, id int64) (*domain.Tenant, error) {
	return m.GetByID(ctx, id)
}

func (m *memTenantRepo) GetByAPIKey(context.Context, string) (*domain.Tenant, error) {
	return nil, domain.ErrNotFound
}

func (m *memTenantRepo) GetBySlug(context.Context, string) (*domain.Tenant, error) {
	return nil, domain.ErrNotFound
}

func (m *memTenantRepo) List(context.Context) ([]domain.Tenant, error) { return nil, nil }

type recordingCache struct{ invalidated []string }

func (c *recordingCache) Invalidate(_ context.Context, apiKey string) error {
	c.invalidated = append(c.invalidated, apiKey)
	return nil
}

type prefixCipher struct{}

func (prefixCipher) Encrypt(s string) (string, error) { return "enc:" + s, nil }
func (prefixCipher) Decrypt(s string) (string, error) { return strings.TrimPrefix(s, "enc:"), nil }

type tenantFixture struct {
	svc   *TenantService
	repo  *memTenantRepo
	audit *memAuditRepo
	cache *recordingCache
}

func newTenantFixture() *tenantFixture {
	f := &tenantFixture{repo: newMemTenantRepo(), audit: &memAuditRepo{}, cache: &recordingCache{}}
	f.svc = NewTenantService(f.repo, f.cache, prefixCipher{}, &fakeTxManager{}, NewAuditService(f.audit))
	return f
}

func (f *tenantFixture) seed(t *testing.T) (*domain.Tenant, *Credentials) {
	t.Helper()
	tenant, creds, err := f.svc.Create(adminCtx(), CreateTenantInput{
		Name: "Acme", Slug: "acme", WebhookURL: "https://acme.test/hook",
		RedirectSuccessURL: "https://acme.test/ok", RedirectFailureURL: "https://acme.test/fail",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.audit.logs = nil
	return tenant, creds
}

// assertNoSecrets fails if any stored audit snapshot contains a credential.
func (f *tenantFixture) assertNoSecrets(t *testing.T, secrets ...string) {
	t.Helper()
	for _, l := range f.audit.logs {
		for _, raw := range []string{string(l.Before), string(l.After)} {
			for _, s := range secrets {
				if s != "" && strings.Contains(raw, s) {
					t.Fatalf("audit snapshot %s contains a secret", raw)
				}
			}
			if strings.Contains(raw, "enc:") || strings.Contains(raw, "secret") {
				t.Fatalf("audit snapshot %s contains secret material", raw)
			}
		}
	}
}

func TestTenantCreateRecordsOneEntry(t *testing.T) {
	f := newTenantFixture()
	tenant, creds, err := f.svc.Create(adminCtx(), CreateTenantInput{Name: "Acme", Slug: "Acme", WebhookURL: "https://acme.test/hook"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(f.audit.logs) != 1 {
		t.Fatalf("got %d entries, want 1", len(f.audit.logs))
	}
	l := f.audit.logs[0]
	if l.Action != "tenant.create" || l.Target != (domain.AuditTarget{Type: "tenant", ID: "1"}) {
		t.Fatalf("action/target = %s %+v", l.Action, l.Target)
	}
	assertJSON(t, "before", l.Before, nil)
	assertJSON(t, "after", l.After, map[string]any{
		"id": 1, "name": "Acme", "slug": "acme", "api_key_last4": tenant.APIKey[len(tenant.APIKey)-4:],
		"webhook_url": "https://acme.test/hook", "redirect_success_url": "", "redirect_failure_url": "", "active": true,
	})
	f.assertNoSecrets(t, creds.APIKey, creds.APISecret)
}

func TestTenantCreateAuditFailureRollsBack(t *testing.T) {
	f := newTenantFixture()
	f.audit.err = errAuditDown
	if _, _, err := f.svc.Create(adminCtx(), CreateTenantInput{Name: "Acme", Slug: "acme"}); !errors.Is(err, errAuditDown) {
		t.Fatalf("err = %v, want audit failure", err)
	}
	if len(f.repo.byID) != 0 {
		t.Fatalf("tenant persisted despite audit failure: %+v", f.repo.byID)
	}
}

func TestTenantCreateWithoutActorRollsBack(t *testing.T) {
	f := newTenantFixture()
	if _, _, err := f.svc.Create(context.Background(), CreateTenantInput{Name: "Acme", Slug: "acme"}); !errors.Is(err, ErrNoActor) {
		t.Fatalf("err = %v, want ErrNoActor", err)
	}
	if len(f.repo.byID) != 0 {
		t.Fatal("tenant persisted without an actor")
	}
}

func TestTenantCreateDuplicateSlugIsConflictWithoutEntry(t *testing.T) {
	f := newTenantFixture()
	f.seed(t)
	if _, _, err := f.svc.Create(adminCtx(), CreateTenantInput{Name: "Again", Slug: "acme"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if len(f.audit.logs) != 0 {
		t.Fatalf("conflict recorded an entry: %+v", f.audit.logs)
	}
}

func TestTenantCreateValidationRecordsNothing(t *testing.T) {
	f := newTenantFixture()
	for _, in := range []CreateTenantInput{{Name: "x", Slug: "a"}, {Name: " ", Slug: "acme"}, {Name: "x", Slug: "ácme"}} {
		if _, _, err := f.svc.Create(adminCtx(), in); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("%+v: err = %v, want ErrInvalidInput", in, err)
		}
	}
	if len(f.audit.logs) != 0 {
		t.Fatalf("validation failure recorded entries: %+v", f.audit.logs)
	}
}

func TestTenantUpdateRecordsBeforeAndAfter(t *testing.T) {
	f := newTenantFixture()
	tenant, _ := f.seed(t)
	last4 := tenant.APIKey[len(tenant.APIKey)-4:]

	got, err := f.svc.Update(adminCtx(), tenant.ID, UpdateTenantInput{Name: "Acme Ünïcode", WebhookURL: "https://acme.test/v2", Active: false})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Name != "Acme Ünïcode" || got.Active {
		t.Fatalf("tenant not updated: %+v", got)
	}
	if len(f.audit.logs) != 1 {
		t.Fatalf("got %d entries, want 1", len(f.audit.logs))
	}
	l := f.audit.logs[0]
	if l.Action != "tenant.update" || l.Target.ID != "1" {
		t.Fatalf("action/target = %s %+v", l.Action, l.Target)
	}
	assertJSON(t, "before", l.Before, map[string]any{
		"id": 1, "name": "Acme", "slug": "acme", "api_key_last4": last4, "webhook_url": "https://acme.test/hook",
		"redirect_success_url": "https://acme.test/ok", "redirect_failure_url": "https://acme.test/fail", "active": true,
	})
	assertJSON(t, "after", l.After, map[string]any{
		"id": 1, "name": "Acme Ünïcode", "slug": "acme", "api_key_last4": last4, "webhook_url": "https://acme.test/v2",
		"redirect_success_url": "", "redirect_failure_url": "", "active": false,
	})
	if len(f.cache.invalidated) != 1 || f.cache.invalidated[0] != tenant.APIKey {
		t.Fatalf("invalidated = %v, want [%s]", f.cache.invalidated, tenant.APIKey)
	}
}

func TestTenantUpdateAuditFailureRollsBackAndKeepsCache(t *testing.T) {
	f := newTenantFixture()
	tenant, _ := f.seed(t)
	f.audit.err = errAuditDown

	if _, err := f.svc.Update(adminCtx(), tenant.ID, UpdateTenantInput{Name: "Changed", Active: true}); !errors.Is(err, errAuditDown) {
		t.Fatalf("err = %v, want audit failure", err)
	}
	if stored, _ := f.repo.GetByID(context.Background(), tenant.ID); stored.Name != "Acme" {
		t.Fatalf("update not rolled back: %+v", stored)
	}
	if len(f.cache.invalidated) != 0 {
		t.Fatalf("cache invalidated despite rollback: %v", f.cache.invalidated)
	}
}

func TestTenantUpdateMissingIsNotFound(t *testing.T) {
	f := newTenantFixture()
	if _, err := f.svc.Update(adminCtx(), 42, UpdateTenantInput{Name: "x"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(f.audit.logs) != 0 {
		t.Fatal("missing tenant recorded an entry")
	}
}

func TestTenantRotateRecordsOnlyLast4(t *testing.T) {
	f := newTenantFixture()
	tenant, oldCreds := f.seed(t)

	got, creds, err := f.svc.RotateCredentials(adminCtx(), tenant.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if creds.APIKey == oldCreds.APIKey || got.APIKey != creds.APIKey {
		t.Fatalf("key not rotated: old %s new %s tenant %s", oldCreds.APIKey, creds.APIKey, got.APIKey)
	}
	if len(f.audit.logs) != 1 {
		t.Fatalf("got %d entries, want 1", len(f.audit.logs))
	}
	l := f.audit.logs[0]
	if l.Action != "tenant.rotate_credentials" || l.Target != (domain.AuditTarget{Type: "tenant", ID: "1"}) {
		t.Fatalf("action/target = %s %+v", l.Action, l.Target)
	}
	assertJSON(t, "before", l.Before, map[string]any{"api_key_last4": oldCreds.APIKey[len(oldCreds.APIKey)-4:]})
	assertJSON(t, "after", l.After, map[string]any{"api_key_last4": creds.APIKey[len(creds.APIKey)-4:]})
	f.assertNoSecrets(t, oldCreds.APIKey, oldCreds.APISecret, creds.APIKey, creds.APISecret)
	if len(f.cache.invalidated) != 1 || f.cache.invalidated[0] != oldCreds.APIKey {
		t.Fatalf("invalidated = %v, want old key", f.cache.invalidated)
	}
}

func TestTenantRotateAuditFailureKeepsOldCredentials(t *testing.T) {
	f := newTenantFixture()
	tenant, oldCreds := f.seed(t)
	f.audit.err = errAuditDown

	if _, _, err := f.svc.RotateCredentials(adminCtx(), tenant.ID); !errors.Is(err, errAuditDown) {
		t.Fatalf("err = %v, want audit failure", err)
	}
	stored, _ := f.repo.GetByID(context.Background(), tenant.ID)
	if stored.APIKey != oldCreds.APIKey || stored.APISecretEnc != "enc:"+oldCreds.APISecret {
		t.Fatal("rotation not rolled back")
	}
	if len(f.cache.invalidated) != 0 {
		t.Fatalf("cache invalidated despite rollback: %v", f.cache.invalidated)
	}
}

// The seed command creates the demo tenant as a system actor.
func TestTenantCreateBySystemActor(t *testing.T) {
	f := newTenantFixture()
	ctx := domain.WithActor(context.Background(), domain.SystemActor("seed"))
	if _, _, err := f.svc.Create(ctx, CreateTenantInput{Name: "Demo App", Slug: "demo"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := f.audit.logs[0].Actor; got != (domain.Actor{Type: domain.ActorSystem, ID: "seed"}) {
		t.Fatalf("actor = %+v, want system:seed", got)
	}
}

func TestLast4(t *testing.T) {
	for in, want := range map[string]string{"": "", "abc": "abc", "abcd": "abcd", "kp_0123456789": "6789"} {
		if got := last4(in); got != want {
			t.Errorf("last4(%q) = %q, want %q", in, got, want)
		}
	}
}
