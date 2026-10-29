package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

func (f *connFixture) addApp(t *testing.T, slug string, env domain.Environment) *domain.App {
	t.Helper()
	a := &domain.App{PublicID: id.New(id.App), Slug: slug, Name: slug, Environment: env, Status: domain.AppActive}
	if err := f.apps.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestImportLegacyXendit(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()
	demo := f.addApp(t, "demo", domain.EnvTest)
	legacy := f.addApp(t, "legacy", domain.EnvLive)
	keep := f.addApp(t, "keep", domain.EnvTest)
	own := f.addConnection(t, "own", domain.EnvTest)
	keep.DefaultConnectionID = &own.ID
	if err := f.apps.Update(ctx, keep); err != nil {
		t.Fatal(err)
	}
	old := &domain.Payment{PublicID: id.New(id.Payment), AppID: legacy.ID, OrderID: "o-1", ExternalID: "legacy:o-1", Amount: 1, Currency: "IDR"}
	if err := f.payments.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	f.audit.Reset()

	res, err := f.svc.ImportLegacyXendit(ctx, "xnd_development_abc123", "ci-callback-token")
	if err != nil {
		t.Fatal(err)
	}
	c := res.Connection
	if !res.Created || c.Name != "xendit-imported" || c.Provider != "xendit" || c.Environment != domain.EnvTest ||
		c.Status != domain.ConnectionActive || !c.LegacyWebhookAlias {
		t.Fatalf("import = %+v / %+v", res, c)
	}
	if got := f.stored(t, c.PublicID); got["secret_key"] != "xnd_development_abc123" || got["callback_token"] != "ci-callback-token" {
		t.Fatalf("stored = %v", got)
	}
	m, _ := f.merchants.GetDefault(ctx)
	if m.LegacyConnectionID == nil || *m.LegacyConnectionID != c.ID {
		t.Fatalf("merchant = %+v", m)
	}
	if len(res.Defaulted) != 1 || res.Defaulted[0].PublicID != demo.PublicID ||
		len(res.OtherEnvironment) != 1 || res.OtherEnvironment[0].PublicID != legacy.PublicID || res.PaymentsBackfilled != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got, _ := f.apps.GetByID(ctx, demo.ID); got.DefaultConnection == nil || got.DefaultConnection.PublicID != c.PublicID {
		t.Fatalf("demo default = %+v", got.DefaultConnection)
	}
	if got, _ := f.apps.GetByID(ctx, keep.ID); *got.DefaultConnectionID != own.ID {
		t.Fatal("an app with a default lost it")
	}
	if got, _ := f.apps.GetByID(ctx, legacy.ID); got.DefaultConnectionID != nil {
		t.Fatal("an app of the other environment got a default")
	}
	if p := f.payments.All()[0]; p.ConnectionID == nil || *p.ConnectionID != c.ID || p.Provider != "xendit" {
		t.Fatalf("payment = %+v", p)
	}
	logs := f.audit.Logs()
	if len(logs) != 2 || logs[0].Action != "connection.create" || logs[1].Action != "app.update" || logs[1].Target.ID != demo.PublicID {
		t.Fatalf("audit = %v", f.actions())
	}
	for _, l := range logs {
		if l.Actor.Type != "system" || l.Actor.ID != "import" {
			t.Fatalf("actor = %+v", l.Actor)
		}
	}
	assertJSON(t, "after", logs[1].After, map[string]any{"default_connection_id": c.PublicID})
	assertJSON(t, "before", logs[1].Before, map[string]any{"default_connection_id": nil})

	// A second start changes nothing, even with other values.
	again, err := f.svc.ImportLegacyXendit(ctx, "xnd_production_other", "another-token")
	if err != nil || again.Created || again.Connection.ID != c.ID {
		t.Fatalf("second import: %+v %v", again, err)
	}
	if len(f.conns.All()) != 2 || len(f.audit.Logs()) != 2 {
		t.Fatalf("second import wrote: conns=%d audit=%v", len(f.conns.All()), f.actions())
	}
}

func TestImportLegacyXenditRefusals(t *testing.T) {
	cases := []struct {
		name, key, token string
		want             string
	}{
		{"unknown prefix", "sk_test_abc", "ci-callback-token", "starts with neither"},
		{"schema", "xnd_development_abc", "short", "credentials.callback_token (min_length)"},
		{"pattern", "xnd_development_a-b", "ci-callback-token", "credentials.secret_key (pattern)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnFixture(t)
			f.addApp(t, "demo", domain.EnvTest)
			res, err := f.svc.ImportLegacyXendit(context.Background(), tc.key, tc.token)
			if err == nil || res != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			if (tc.name == "unknown prefix") != errors.Is(err, ErrUnknownKeyPrefix) {
				t.Fatalf("err = %v, ErrUnknownKeyPrefix only for an unknown prefix", err)
			}
			if strings.Contains(err.Error(), tc.key) || strings.Contains(err.Error(), tc.token) {
				t.Fatalf("error repeats a value: %v", err)
			}
			if len(f.conns.All()) != 0 || len(f.audit.Logs()) != 0 {
				t.Fatal("a refused import stored something")
			}
		})
	}
}

func TestImportLegacyXenditNeedsBothValues(t *testing.T) {
	f := newConnFixture(t)
	for _, pair := range [][2]string{{"", ""}, {"xnd_development_abc", ""}, {"", "ci-callback-token"}} {
		res, err := f.svc.ImportLegacyXendit(context.Background(), pair[0], pair[1])
		if res != nil || err != nil || f.merchants.ForUpdate != 0 {
			t.Fatalf("%v: %+v %v", pair, res, err)
		}
	}
}

func TestImportLegacyXenditTakenNameGetsASuffix(t *testing.T) {
	f := newConnFixture(t)
	f.addConnection(t, "xendit-imported", domain.EnvLive)
	res, err := f.svc.ImportLegacyXendit(context.Background(), "xnd_production_abc", "ci-callback-token")
	if err != nil {
		t.Fatal(err)
	}
	c := res.Connection
	want := "xendit-imported-" + strings.ToLower(c.PublicID[len(c.PublicID)-6:])
	if c.Name != want || c.Environment != domain.EnvLive {
		t.Fatalf("connection = %s %s, want %s live", c.Name, c.Environment, want)
	}
}
