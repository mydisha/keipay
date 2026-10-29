package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/keygen"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

// recordingCipher "encrypts" by prefixing and remembers every plaintext, so
// tests can check that secrets were encrypted and never stored in clear.
type recordingCipher struct{ plain []string }

func (c *recordingCipher) Encrypt(s string) (string, error) {
	c.plain = append(c.plain, s)
	return "enc(" + s + ")", nil
}

func (c *recordingCipher) Decrypt(s string) (string, error) {
	return strings.TrimSuffix(strings.TrimPrefix(s, "enc("), ")"), nil
}

type appFixture struct {
	svc    *AppService
	apps   *memstore.Apps
	keys   *memstore.APIKeys
	conns  *memstore.Connections
	audit  *memstore.Audit
	tx     *memstore.Tx
	cipher *recordingCipher
	clock  *memstore.Clock
}

func newAppFixture(t *testing.T) *appFixture {
	t.Helper()
	f := &appFixture{clock: memstore.NewClock(time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)), cipher: &recordingCipher{}, audit: &memstore.Audit{}}
	f.apps = memstore.NewApps(f.clock.Now)
	f.keys = memstore.NewAPIKeys(f.apps, f.clock.Now)
	f.conns = memstore.NewConnections(f.apps, f.clock.Now)
	f.tx = memstore.NewTx(f.apps, f.keys, f.conns, f.audit)
	auditSvc := NewAuditService(f.audit)
	auditSvc.now = f.clock.Now
	f.svc = NewAppService(f.apps, f.keys, f.conns, f.cipher, f.tx, auditSvc)
	f.svc.now = f.clock.Now
	return f
}

func (f *appFixture) create(t *testing.T, slug string, env domain.Environment) *AppCreated {
	t.Helper()
	c, err := f.svc.Create(adminCtx(), CreateAppInput{Name: "App " + slug, Slug: slug, Environment: env})
	if err != nil {
		t.Fatalf("create %s/%s: %v", env, slug, err)
	}
	return c
}

func (f *appFixture) actions() []string {
	var out []string
	for _, l := range f.audit.Logs() {
		out = append(out, l.Action)
	}
	return out
}

func TestAppCreate(t *testing.T) {
	for _, env := range []domain.Environment{domain.EnvTest, domain.EnvLive} {
		t.Run(string(env), func(t *testing.T) {
			f := newAppFixture(t)
			c, err := f.svc.Create(adminCtx(), CreateAppInput{
				Name: " Acme Shop ", Slug: " Acme-Shop ", Environment: env,
				Metadata: json.RawMessage(`{"team":"payments","tier":2}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			envStr := string(env)
			if !regexp.MustCompile(`^kp_`+envStr+`_[0-9a-f]{32}$`).MatchString(c.APIKey) ||
				!regexp.MustCompile(`^kps_`+envStr+`_[0-9a-f]{48}$`).MatchString(c.APISecret) {
				t.Fatalf("secrets = %s %s", c.APIKey, c.APISecret)
			}
			a, k := c.App, c.Key
			if !id.Valid(a.PublicID, id.App) || a.Slug != "acme-shop" || a.Name != "Acme Shop" || a.Environment != env ||
				a.Status != domain.AppActive || a.WebhookURL != "" {
				t.Fatalf("app = %+v", a)
			}
			if !id.Valid(k.PublicID, id.APIKey) || k.Label != "Default" || k.ExpiresAt != nil || k.Environment != env ||
				k.AppID != a.ID || k.Prefix != "kp_"+envStr+"_" || k.Last4 != c.APIKey[len(c.APIKey)-4:] ||
				k.Hash != keygen.HashAPIKey(c.APIKey) {
				t.Fatalf("key = %+v", k)
			}
			// Apps no longer get a webhook secret; endpoints have their own.
			if a.WebhookSecretEnc != "" || k.SecretEnc != "enc("+c.APISecret+")" {
				t.Fatalf("secrets: webhook %q api %q", a.WebhookSecretEnc, k.SecretEnc)
			}

			logs := f.audit.Logs()
			if len(logs) != 2 || logs[0].Action != "app.create" || logs[1].Action != "api_key.create" {
				t.Fatalf("audit = %v", f.actions())
			}
			if logs[0].Target != (domain.AuditTarget{Type: "app", ID: a.PublicID}) || logs[1].Target != (domain.AuditTarget{Type: "api_key", ID: k.PublicID}) {
				t.Fatalf("targets = %+v %+v", logs[0].Target, logs[1].Target)
			}
			assertJSON(t, "app.create before", logs[0].Before, nil)
			assertJSON(t, "app.create after", logs[0].After, map[string]any{
				"id": a.PublicID, "slug": "acme-shop", "name": "Acme Shop", "environment": envStr, "status": "active",
				"redirect_success_url": "", "redirect_failure_url": "",
				"metadata": map[string]any{"team": "payments", "tier": 2},
			})
			assertJSON(t, "api_key.create after", logs[1].After, map[string]any{
				"id": k.PublicID, "app_id": a.PublicID, "label": "Default", "environment": envStr,
				"prefix": "kp_" + envStr + "_", "last4": k.Last4, "expires_at": nil, "revoked_at": nil,
			})
			assertNoSecrets(t, f, c.APIKey, c.APISecret, k.Hash)
		})
	}
}

// assertNoSecrets fails when any audit snapshot holds a secret or ciphertext.
func assertNoSecrets(t *testing.T, f *appFixture, secrets ...string) {
	t.Helper()
	secrets = append(secrets, "enc(")
	for _, l := range f.audit.Logs() {
		for _, s := range secrets {
			if strings.Contains(string(l.Before)+string(l.After), s) {
				t.Fatalf("%s audit entry holds secret material %q", l.Action, s)
			}
		}
	}
}

func TestAppCreateSlugUniquePerEnvironment(t *testing.T) {
	f := newAppFixture(t)
	f.create(t, "acme", domain.EnvTest)
	if _, err := f.svc.Create(adminCtx(), CreateAppInput{Name: "Again", Slug: "ACME", Environment: domain.EnvTest}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if len(f.audit.Logs()) != 2 || len(f.keys.All()) != 1 {
		t.Fatalf("conflict left writes: audit=%v keys=%d", f.actions(), len(f.keys.All()))
	}
	f.create(t, "acme", domain.EnvLive)
}

func TestAppCreateFieldRules(t *testing.T) {
	f := newAppFixture(t)
	_, err := f.svc.Create(adminCtx(), CreateAppInput{
		Name: "", Slug: "a", Environment: "prod", WebhookURL: true, RedirectSuccessURL: "/ok",
		RedirectFailureURL: "https://", Metadata: json.RawMessage(`{"b":[],"a":{}}`),
	})
	assertFields(t, err,
		domain.FieldError{Field: "name", Rule: "required"},
		domain.FieldError{Field: "slug", Rule: "slug"},
		domain.FieldError{Field: "environment", Rule: "oneof"},
		domain.FieldError{Field: "webhook_url", Rule: "moved"},
		domain.FieldError{Field: "redirect_success_url", Rule: "url"},
		domain.FieldError{Field: "redirect_failure_url", Rule: "url"},
		domain.FieldError{Field: "metadata.a", Rule: "type"},
		domain.FieldError{Field: "metadata.b", Rule: "type"},
	)
	// webhook_url alone is refused too.
	_, err = f.svc.Create(adminCtx(), CreateAppInput{Name: "A", Slug: "acme", Environment: domain.EnvTest, WebhookURL: true})
	assertFields(t, err, domain.FieldError{Field: "webhook_url", Rule: "moved"})
	_, err = f.svc.Create(adminCtx(), CreateAppInput{Name: strings.Repeat("n", 101), Slug: "", Environment: ""})
	assertFields(t, err,
		domain.FieldError{Field: "name", Rule: "max"},
		domain.FieldError{Field: "slug", Rule: "required"},
		domain.FieldError{Field: "environment", Rule: "required"},
	)
	if len(f.apps.All()) != 0 || len(f.audit.Logs()) != 0 {
		t.Fatal("invalid input wrote something")
	}
}

// The app, its key and both entries roll back together.
func TestAppCreateAuditFailureRollsBack(t *testing.T) {
	f := newAppFixture(t)
	f.audit.Err = errAuditDown
	if _, err := f.svc.Create(adminCtx(), CreateAppInput{Name: "A", Slug: "acme", Environment: domain.EnvLive}); !errors.Is(err, errAuditDown) {
		t.Fatalf("err = %v", err)
	}
	if len(f.apps.All()) != 0 || len(f.keys.All()) != 0 {
		t.Fatal("app or key survived a failed audit insert")
	}
	if _, err := f.svc.Create(context.Background(), CreateAppInput{Name: "A", Slug: "acme", Environment: domain.EnvLive}); !errors.Is(err, ErrNoActor) {
		t.Fatalf("no actor: %v", err)
	}
}

func TestAppUpdate(t *testing.T) {
	f := newAppFixture(t)
	c := f.create(t, "acme", domain.EnvLive)
	pub := c.App.PublicID
	str := func(s string) *string { return &s }
	f.audit.Reset()

	cases := []struct {
		name string
		in   UpdateAppInput
		want []domain.FieldError
	}{
		{"slug, same value", UpdateAppInput{Slug: true}, []domain.FieldError{fe("slug", "immutable")}},
		{"environment", UpdateAppInput{Environment: true, Name: str("X")}, []domain.FieldError{fe("environment", "immutable")}},
		{"both", UpdateAppInput{Slug: true, Environment: true}, []domain.FieldError{fe("slug", "immutable"), fe("environment", "immutable")}},
		{"empty body", UpdateAppInput{}, []domain.FieldError{fe("body", "required")}},
		{"bad fields", UpdateAppInput{Name: str(" "), WebhookURL: true, Metadata: json.RawMessage(`[]`), Slug: true},
			[]domain.FieldError{fe("name", "required"), fe("slug", "immutable"), fe("webhook_url", "moved"), fe("metadata", "object")}},
		{"webhook_url alone", UpdateAppInput{WebhookURL: true}, []domain.FieldError{fe("webhook_url", "moved")}},
	}
	for _, tc := range cases {
		_, err := f.svc.Update(adminCtx(), pub, tc.in)
		assertFields(t, err, tc.want...)
	}
	if f.apps.Updates != 0 || len(f.audit.Logs()) != 0 {
		t.Fatal("refused updates wrote something")
	}

	// No-op: same values write and record nothing.
	got, err := f.svc.Update(adminCtx(), pub, UpdateAppInput{Name: str(" App acme "), Metadata: json.RawMessage(`null`)})
	if err != nil || got.Name != "App acme" || f.apps.Updates != 0 || len(f.audit.Logs()) != 0 {
		t.Fatalf("no-op: %+v %v updates=%d audit=%v", got, err, f.apps.Updates, f.actions())
	}

	// Metadata is replaced whole; only changed fields are audited.
	if _, err := f.svc.Update(adminCtx(), pub, UpdateAppInput{Metadata: json.RawMessage(`{"a":1,"b":"x"}`)}); err != nil {
		t.Fatal(err)
	}
	got, err = f.svc.Update(adminCtx(), pub, UpdateAppInput{Name: str("Acme Ünïcode"),
		RedirectFailureURL: str("https://acme.example/fail"), Metadata: json.RawMessage(`{"c":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := f.apps.GetByPublicID(context.Background(), pub)
	b, _ := json.Marshal(stored.Metadata)
	if string(b) != `{"c":true}` || stored.Name != "Acme Ünïcode" || stored.RedirectFailureURL != "https://acme.example/fail" || got.Name != stored.Name {
		t.Fatalf("stored = %+v metadata %s", stored, b)
	}
	logs := f.audit.Logs()
	if len(logs) != 2 || logs[1].Action != "app.update" || logs[1].Target.ID != pub {
		t.Fatalf("audit = %v", f.actions())
	}
	assertJSON(t, "before", logs[1].Before, map[string]any{"name": "App acme", "redirect_failure_url": "", "metadata": map[string]any{"a": 1, "b": "x"}})
	assertJSON(t, "after", logs[1].After, map[string]any{"name": "Acme Ünïcode", "redirect_failure_url": "https://acme.example/fail", "metadata": map[string]any{"c": true}})

	if _, err := f.svc.Update(adminCtx(), id.New(id.App), UpdateAppInput{Name: str("x")}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown app: %v", err)
	}
}

func TestAppUpdateAuditFailureRollsBack(t *testing.T) {
	f := newAppFixture(t)
	c := f.create(t, "acme", domain.EnvLive)
	f.audit.Err = errAuditDown
	name := "Changed"
	if _, err := f.svc.Update(adminCtx(), c.App.PublicID, UpdateAppInput{Name: &name}); !errors.Is(err, errAuditDown) {
		t.Fatalf("err = %v", err)
	}
	if stored, _ := f.apps.GetByPublicID(context.Background(), c.App.PublicID); stored.Name != "App acme" {
		t.Fatalf("update survived: %+v", stored)
	}
}

func TestAppDisableEnable(t *testing.T) {
	f := newAppFixture(t)
	pub := f.create(t, "acme", domain.EnvLive).App.PublicID
	f.audit.Reset()
	ctx := adminCtx()

	for i := range 2 {
		a, err := f.svc.Disable(ctx, pub)
		if err != nil || a.Status != domain.AppDisabled {
			t.Fatalf("disable %d: %+v %v", i, a, err)
		}
	}
	for i := range 2 {
		a, err := f.svc.Enable(ctx, pub)
		if err != nil || a.Status != domain.AppActive {
			t.Fatalf("enable %d: %+v %v", i, a, err)
		}
	}
	logs := f.audit.Logs()
	if len(logs) != 2 || logs[0].Action != "app.disable" || logs[1].Action != "app.enable" {
		t.Fatalf("audit = %v", f.actions())
	}
	assertJSON(t, "disable before", logs[0].Before, map[string]any{"status": "active"})
	assertJSON(t, "disable after", logs[0].After, map[string]any{"status": "disabled"})
	if _, err := f.svc.Disable(ctx, id.New(id.App)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestAppListAndGet(t *testing.T) {
	f := newAppFixture(t)
	for _, s := range []string{"a-one", "a-two", "a-three"} {
		f.create(t, s, domain.EnvTest)
		f.clock.Advance(time.Second)
	}
	live := f.create(t, "a-four", domain.EnvLive)
	test := domain.EnvTest
	res, err := f.svc.List(context.Background(), domain.AppFilter{Environment: &test}, domain.PageRequest{Limit: 2})
	if err != nil || len(res.Items) != 2 || res.Items[0].Slug != "a-three" || res.NextCursor == "" {
		t.Fatalf("page = %+v, %v", res, err)
	}
	got, err := f.svc.Get(context.Background(), live.App.PublicID)
	if err != nil || got.ID != live.App.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := f.svc.Get(context.Background(), id.New(id.App)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestCreateKey(t *testing.T) {
	f := newAppFixture(t)
	app := f.create(t, "acme", domain.EnvTest).App
	now := f.clock.Now()
	past, future := now.Add(-time.Second), now.Add(time.Hour)

	for _, at := range []*time.Time{&past, &now} {
		_, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "x", ExpiresAt: at})
		assertFields(t, err, domain.FieldError{Field: "expires_at", Rule: "future"})
	}
	_, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: " ", ExpiresAt: &past})
	assertFields(t, err, domain.FieldError{Field: "label", Rule: "required"}, domain.FieldError{Field: "expires_at", Rule: "future"})

	c, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: " Server ", ExpiresAt: &future})
	if err != nil {
		t.Fatal(err)
	}
	if c.Key.Label != "Server" || c.Key.Environment != domain.EnvTest || !c.Key.ExpiresAt.Equal(future) ||
		!strings.HasPrefix(c.APIKey, "kp_test_") || !strings.HasPrefix(c.APISecret, "kps_test_") {
		t.Fatalf("created = %+v %s", c.Key, c.APIKey)
	}
	if acts := f.actions(); acts[len(acts)-1] != "api_key.create" {
		t.Fatalf("audit = %v", acts)
	}

	// Disabled apps can still get keys.
	if _, err := f.svc.Disable(adminCtx(), app.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "y"}); err != nil {
		t.Fatalf("disabled app: %v", err)
	}
	if _, err := f.svc.CreateKey(adminCtx(), id.New(id.App), CreateKeyInput{Label: "y"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown app: %v", err)
	}
}

func TestKeyLimit(t *testing.T) {
	f := newAppFixture(t)
	app := f.create(t, "acme", domain.EnvLive).App
	soon := f.clock.Now().Add(time.Minute)
	var ids []string
	for i := 1; i < domain.MaxUsableAPIKeys; i++ {
		var exp *time.Time
		if i == 1 {
			exp = &soon
		}
		c, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "k", ExpiresAt: exp})
		if err != nil {
			t.Fatalf("key %d: %v", i+1, err)
		}
		ids = append(ids, c.Key.PublicID)
	}
	isLimit := func(err error) bool {
		var coded *domain.Error
		return errors.As(err, &coded) && coded.Code == domain.CodeValidationFailed && strings.Contains(coded.Message, "at most 10")
	}
	if _, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "11th"}); !isLimit(err) {
		t.Fatalf("11th: %v", err)
	}
	if _, err := f.svc.RotateKey(adminCtx(), app.PublicID, ids[2]); !isLimit(err) {
		t.Fatalf("rotate at the limit: %v", err)
	}
	// A revoked key frees a slot.
	if _, _, err := f.svc.RevokeKey(adminCtx(), app.PublicID, ids[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "after revoke"}); err != nil {
		t.Fatalf("after revoke: %v", err)
	}
	// So does an expired one.
	f.clock.Advance(time.Minute)
	if _, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "after expiry"}); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if _, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "over"}); !isLimit(err) {
		t.Fatalf("over again: %v", err)
	}
}

func TestRotateKey(t *testing.T) {
	f := newAppFixture(t)
	created := f.create(t, "acme", domain.EnvLive)
	app, old := created.App, created.Key
	exp := f.clock.Now().Add(time.Hour)
	labelled, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "Worker", ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	f.audit.Reset()

	c, err := f.svc.RotateKey(adminCtx(), app.PublicID, labelled.Key.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Key.Label != "Worker" || c.Key.ExpiresAt != nil || c.Key.PublicID == labelled.Key.PublicID || c.APIKey == labelled.APIKey {
		t.Fatalf("rotated = %+v", c.Key)
	}
	if stored := f.keys.Get(labelled.Key.PublicID); stored.RevokedAt != nil || !stored.ExpiresAt.Equal(exp) {
		t.Fatalf("old key changed: %+v", stored)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "api_key.rotate" || logs[0].Target.ID != c.Key.PublicID {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "rotate before", logs[0].Before, map[string]any{
		"id": labelled.Key.PublicID, "app_id": app.PublicID, "label": "Worker", "environment": "live",
		"prefix": "kp_live_", "last4": labelled.Key.Last4, "expires_at": exp.UTC().Format(time.RFC3339Nano), "revoked_at": nil,
	})
	assertJSON(t, "rotate after", logs[0].After, map[string]any{
		"id": c.Key.PublicID, "app_id": app.PublicID, "label": "Worker", "environment": "live",
		"prefix": "kp_live_", "last4": c.Key.Last4, "expires_at": nil, "revoked_at": nil,
	})
	assertNoSecrets(t, f, c.APIKey, c.APISecret, c.Key.Hash)

	// Revoked and expired keys cannot be rotated.
	if _, _, err := f.svc.RevokeKey(adminCtx(), app.PublicID, old.PublicID); err != nil {
		t.Fatal(err)
	}
	isRule := func(err error, msg string) bool {
		var coded *domain.Error
		return errors.As(err, &coded) && coded.Code == domain.CodeValidationFailed && strings.Contains(coded.Message, msg)
	}
	if _, err := f.svc.RotateKey(adminCtx(), app.PublicID, old.PublicID); !isRule(err, "revoked") {
		t.Fatalf("rotate revoked: %v", err)
	}
	f.clock.Advance(time.Hour)
	if _, err := f.svc.RotateKey(adminCtx(), app.PublicID, labelled.Key.PublicID); !isRule(err, "expired") {
		t.Fatalf("rotate expired: %v", err)
	}
}

func TestRevokeKey(t *testing.T) {
	f := newAppFixture(t)
	created := f.create(t, "acme", domain.EnvLive)
	app, key := created.App, created.Key
	f.audit.Reset()

	gotApp, got, err := f.svc.RevokeKey(adminCtx(), app.PublicID, key.PublicID)
	if err != nil || got.RevokedAt == nil || !got.RevokedAt.Equal(f.clock.Now()) || gotApp.ID != app.ID {
		t.Fatalf("revoke = %+v %v", got, err)
	}
	f.clock.Advance(time.Minute)
	if _, _, err := f.svc.RevokeKey(adminCtx(), app.PublicID, key.PublicID); err != nil {
		t.Fatal(err)
	}
	if stored := f.keys.Get(key.PublicID); !stored.RevokedAt.Equal(*got.RevokedAt) {
		t.Fatalf("second revoke moved revoked_at: %v", stored.RevokedAt)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "api_key.revoke" {
		t.Fatalf("audit = %v", f.actions())
	}
	assertJSON(t, "revoke before", logs[0].Before, map[string]any{"revoked_at": nil})
	assertJSON(t, "revoke after", logs[0].After, map[string]any{"revoked_at": got.RevokedAt.Format(time.RFC3339Nano)})

	// Expired keys can be revoked too.
	exp := f.clock.Now().Add(time.Second)
	c, err := f.svc.CreateKey(adminCtx(), app.PublicID, CreateKeyInput{Label: "short", ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Hour)
	if _, k, err := f.svc.RevokeKey(adminCtx(), app.PublicID, c.Key.PublicID); err != nil || k.Status(f.clock.Now()) != domain.APIKeyRevoked {
		t.Fatalf("revoke expired: %+v %v", k, err)
	}
}

// A key is only reachable through its own app.
func TestKeysOfAnotherAppAreNotFound(t *testing.T) {
	f := newAppFixture(t)
	acme := f.create(t, "acme", domain.EnvLive)
	globex := f.create(t, "globex", domain.EnvLive)
	if _, err := f.svc.RotateKey(adminCtx(), globex.App.PublicID, acme.Key.PublicID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rotate: %v", err)
	}
	if _, _, err := f.svc.RevokeKey(adminCtx(), globex.App.PublicID, acme.Key.PublicID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("revoke: %v", err)
	}
	if f.keys.Get(acme.Key.PublicID).RevokedAt != nil {
		t.Fatal("another app revoked the key")
	}
	_, page, err := f.svc.ListKeys(context.Background(), globex.App.PublicID, domain.PageRequest{})
	if err != nil || len(page.Items) != 1 || page.Items[0].PublicID != globex.Key.PublicID {
		t.Fatalf("list = %+v, %v", page, err)
	}
	if _, _, err := f.svc.ListKeys(context.Background(), id.New(id.App), domain.PageRequest{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown app list: %v", err)
	}
	for _, k := range f.keys.All() {
		app, _ := f.apps.GetByID(context.Background(), k.AppID)
		if k.Environment != app.Environment {
			t.Fatalf("key %s environment %s, app %s", k.PublicID, k.Environment, app.Environment)
		}
	}
}

func TestLookup(t *testing.T) {
	f := newAppFixture(t)
	live := f.create(t, "acme", domain.EnvLive)
	ctx := context.Background()

	app, key, err := f.svc.Lookup(ctx, live.APIKey)
	if err != nil || app.ID != live.App.ID || key.PublicID != live.Key.PublicID {
		t.Fatalf("lookup = %+v %+v %v", app, key, err)
	}
	if _, _, err := f.svc.Lookup(ctx, "kp_live_"+strings.Repeat("0", 32)); !errors.Is(err, domain.ErrUnknownAPIKey) || errors.Is(err, ErrKeyEnvironmentMismatch) {
		t.Fatalf("unknown: %v", err)
	}

	// A legacy kp_ key acts in its app's environment.
	legacy := "kp_" + strings.Repeat("ab", 16)
	f.keys.Put(domain.APIKey{PublicID: id.New(id.APIKey), AppID: live.App.ID, Environment: domain.EnvLive, Prefix: "kp_",
		Last4: "abab", Hash: keygen.HashAPIKey(legacy), SecretEnc: "enc(s)", Label: "Migrated key"})
	if _, k, err := f.svc.Lookup(ctx, legacy); err != nil || k.Prefix != "kp_" {
		t.Fatalf("legacy: %+v %v", k, err)
	}

	// A row whose environment differs from its app, and a header prefix that
	// differs from the row, both look unknown.
	rowMismatch := "kp_test_" + strings.Repeat("1", 32)
	f.keys.Put(domain.APIKey{PublicID: id.New(id.APIKey), AppID: live.App.ID, Environment: domain.EnvTest, Prefix: "kp_test_",
		Hash: keygen.HashAPIKey(rowMismatch)})
	headerMismatch := "kp_test_" + strings.Repeat("2", 32)
	f.keys.Put(domain.APIKey{PublicID: id.New(id.APIKey), AppID: live.App.ID, Environment: domain.EnvLive, Prefix: "kp_live_",
		Hash: keygen.HashAPIKey(headerMismatch)})
	for _, raw := range []string{rowMismatch, headerMismatch} {
		_, _, err := f.svc.Lookup(ctx, raw)
		var coded *domain.Error
		if !errors.Is(err, ErrKeyEnvironmentMismatch) || !errors.As(err, &coded) || coded.Code != domain.CodeUnknownAPIKey {
			t.Fatalf("%s: err = %v", raw[:8], err)
		}
	}

	f.keys.Err = errors.New("db down")
	if _, _, err := f.svc.Lookup(ctx, live.APIKey); err == nil || errors.Is(err, domain.ErrUnknownAPIKey) {
		t.Fatalf("db error: %v", err)
	}
}

func TestAuthorize(t *testing.T) {
	f := newAppFixture(t)
	now := f.clock.Now()
	past := now.Add(-time.Second)
	active := &domain.App{Status: domain.AppActive}
	disabled := &domain.App{Status: domain.AppDisabled}
	cases := []struct {
		name string
		app  *domain.App
		key  domain.APIKey
		want error
	}{
		{"ok", active, domain.APIKey{}, nil},
		{"revoked", active, domain.APIKey{RevokedAt: &past}, domain.ErrAPIKeyRevoked},
		{"expired at now", active, domain.APIKey{ExpiresAt: &now}, domain.ErrAPIKeyExpired},
		{"revoked beats disabled", disabled, domain.APIKey{RevokedAt: &past}, domain.ErrAPIKeyRevoked},
		{"disabled", disabled, domain.APIKey{}, domain.ErrAppDisabled},
	}
	for _, tc := range cases {
		err := f.svc.Authorize(tc.app, &tc.key)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestTouch(t *testing.T) {
	f := newAppFixture(t)
	c := f.create(t, "acme", domain.EnvLive)
	ctx := context.Background()
	touch := func() {
		t.Helper()
		k := f.keys.Get(c.Key.PublicID)
		if err := f.svc.Touch(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	touch()
	if f.keys.Touches != 1 || !f.keys.Get(c.Key.PublicID).LastUsedAt.Equal(f.clock.Now()) {
		t.Fatalf("first touch: %d", f.keys.Touches)
	}
	f.clock.Advance(59 * time.Second)
	touch()
	if f.keys.Touches != 1 {
		t.Fatalf("touched within a minute: %d", f.keys.Touches)
	}
	f.clock.Advance(time.Second)
	touch()
	if f.keys.Touches != 2 {
		t.Fatalf("not touched after a minute: %d", f.keys.Touches)
	}
	f.clock.Advance(time.Hour)
	f.keys.TouchErr = errors.New("db down")
	if err := f.svc.Touch(ctx, f.keys.Get(c.Key.PublicID)); !errors.Is(err, f.keys.TouchErr) {
		t.Fatalf("touch error: %v", err)
	}
}

// addConnection stores a connection directly; AppService only reads them.
func (f *appFixture) addConnection(t *testing.T, name string, env domain.Environment, status domain.ConnectionStatus) *domain.Connection {
	t.Helper()
	c := &domain.Connection{PublicID: id.New(id.Connection), MerchantID: memstore.DefaultMerchantID, Provider: "fake",
		Name: name, Environment: env, Status: status}
	if err := f.conns.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAppDefaultConnection(t *testing.T) {
	f := newAppFixture(t)
	app := f.create(t, "acme", domain.EnvTest).App
	testConn := f.addConnection(t, "fake-test", domain.EnvTest, domain.ConnectionActive)
	other := f.addConnection(t, "fake-test-2", domain.EnvTest, domain.ConnectionActive)
	liveConn := f.addConnection(t, "fake-live", domain.EnvLive, domain.ConnectionActive)
	disabled := f.addConnection(t, "fake-off", domain.EnvTest, domain.ConnectionDisabled)
	f.audit.Reset()
	ctx := adminCtx()
	set := func(v *string) (*domain.App, error) {
		return f.svc.Update(ctx, app.PublicID, UpdateAppInput{DefaultConnectionSet: true, DefaultConnectionID: v})
	}
	ptr := func(s string) *string { return &s }

	for _, tc := range []struct {
		value *string
		rule  string
	}{
		{ptr("con_nope"), "public_id"},
		{ptr("app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"), "public_id"},
		{ptr(id.New(id.Connection)), "exists"},
		{&liveConn.PublicID, "environment"},
		{&disabled.PublicID, "status"},
	} {
		_, err := set(tc.value)
		assertFields(t, err, domain.FieldError{Field: "default_connection_id", Rule: tc.rule})
	}
	if len(f.audit.Logs()) != 0 {
		t.Fatalf("refused updates recorded %v", f.actions())
	}

	// Lower case is accepted; the app answers with the summary.
	got, err := set(ptr(strings.ToLower(testConn.PublicID)))
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultConnectionID == nil || *got.DefaultConnectionID != testConn.ID || got.DefaultConnection == nil ||
		got.DefaultConnection.PublicID != testConn.PublicID || got.DefaultConnection.Name != "fake-test" {
		t.Fatalf("app = %+v", got)
	}
	stored, _ := f.svc.Get(ctx, app.PublicID)
	if stored.DefaultConnection == nil || stored.DefaultConnection.PublicID != testConn.PublicID {
		t.Fatalf("stored = %+v", stored.DefaultConnection)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "app.update" {
		t.Fatalf("audit = %v", f.actions())
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"default_connection_id": nil})
	assertJSON(t, "after", logs[0].After, map[string]any{"default_connection_id": testConn.PublicID})

	// The same connection again is a no-op; another one is recorded as a change.
	if _, err := set(&testConn.PublicID); err != nil || len(f.audit.Logs()) != 1 {
		t.Fatalf("no-op: %v %v", err, f.actions())
	}
	name := "Renamed"
	if _, err := f.svc.Update(ctx, app.PublicID, UpdateAppInput{Name: &name, DefaultConnectionSet: true, DefaultConnectionID: &other.PublicID}); err != nil {
		t.Fatal(err)
	}
	assertJSON(t, "after", f.audit.Logs()[1].After, map[string]any{"name": "Renamed", "default_connection_id": other.PublicID})

	// null clears it.
	got, err = set(nil)
	if err != nil || got.DefaultConnectionID != nil || got.DefaultConnection != nil {
		t.Fatalf("clear: %+v %v", got, err)
	}
	assertJSON(t, "before", f.audit.Logs()[2].Before, map[string]any{"default_connection_id": other.PublicID})
	assertJSON(t, "after", f.audit.Logs()[2].After, map[string]any{"default_connection_id": nil})
	if _, err := set(nil); err != nil || len(f.audit.Logs()) != 3 {
		t.Fatalf("clearing twice: %v %v", err, f.actions())
	}

	// Apps list rows carry the summary.
	if _, err := set(&testConn.PublicID); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.List(ctx, domain.AppFilter{}, domain.PageRequest{})
	if err != nil || len(res.Items) != 1 || res.Items[0].DefaultConnection == nil || res.Items[0].DefaultConnection.Status != domain.ConnectionActive {
		t.Fatalf("list = %+v %v", res, err)
	}
}
