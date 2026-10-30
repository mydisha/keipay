package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

const testMasterKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

type connFixture struct {
	clock     *memstore.Clock
	apps      *memstore.Apps
	conns     *memstore.Connections
	merchants *memstore.Merchants
	payments  *memstore.Payments
	audit     *memstore.Audit
	tx        *memstore.Tx
	fake      *providertest.Fake
	registry  *provider.Registry
	cipher    *crypto.Cipher
	svc       *ConnectionService
	logs      *observer.ObservedLogs
}

// newConnFixture registers the fake provider and the real Xendit adapter,
// pointed at a closed port so no test can reach Xendit.
func newConnFixture(t *testing.T) *connFixture {
	t.Helper()
	f := &connFixture{clock: memstore.NewClock(time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)), audit: &memstore.Audit{},
		fake: providertest.New()}
	f.apps = memstore.NewApps(f.clock.Now)
	f.conns = memstore.NewConnections(f.apps, f.clock.Now)
	f.merchants = f.conns.Merchants()
	f.payments = memstore.NewPayments(f.apps, f.clock.Now)
	f.tx = memstore.NewTx(f.apps, f.conns, f.payments, f.audit)
	var err error
	if f.registry, err = provider.NewRegistry(f.fake, xendit.New(xendit.WithBaseURL("http://127.0.0.1:1"))); err != nil {
		t.Fatal(err)
	}
	if f.cipher, err = crypto.New(testMasterKey); err != nil {
		t.Fatal(err)
	}
	core, logs := observer.New(zap.DebugLevel)
	f.logs = logs
	auditSvc := NewAuditService(f.audit)
	auditSvc.now = f.clock.Now
	f.svc = NewConnectionService(ConnectionDeps{
		Connections: f.conns, Merchants: f.merchants, Apps: f.apps, Payments: f.payments, Registry: f.registry,
		Sealer: f.cipher, Tx: f.tx, Audit: auditSvc, Log: zap.New(core), BaseURL: "https://pay.example.com/",
	})
	f.svc.now = f.clock.Now
	return f
}

// fakeCreds are valid credentials of the fake provider; api_key is long enough
// to show its last four characters.
func fakeCreds(name string) map[string]any {
	return map[string]any{"api_key": "key-" + name + "-0123456789", "token": "tok-" + name + "-123"}
}

func (f *connFixture) addConnection(t *testing.T, name string, env domain.Environment) *domain.Connection {
	t.Helper()
	d, err := f.svc.Create(adminCtx(), CreateConnectionInput{Provider: "fake", Name: name, Environment: env, Credentials: fakeCreds(name)})
	if err != nil {
		t.Fatalf("create connection %s: %v", name, err)
	}
	return d.Connection
}

func (f *connFixture) actions() []string {
	var out []string
	for _, l := range f.audit.Logs() {
		out = append(out, l.Action)
	}
	return out
}

// stored decrypts the stored credentials of a connection.
func (f *connFixture) stored(t *testing.T, publicID string) provider.Credentials {
	t.Helper()
	c, err := f.conns.GetByPublicID(context.Background(), publicID)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := openAccount(f.cipher, c)
	if err != nil {
		t.Fatal(err)
	}
	return acc.Credentials
}

func TestConnectionCreate(t *testing.T) {
	f := newConnFixture(t)
	d, err := f.svc.Create(adminCtx(), CreateConnectionInput{
		Provider: " fake ", Name: "  Fake-Test ", Environment: domain.EnvTest,
		Credentials: map[string]any{"api_key": "  key-abcdefghijkl  ", "token": "tok-12345"},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := d.Connection
	if !id.Valid(c.PublicID, id.Connection) || c.Provider != "fake" || c.Name != "fake-test" || c.Environment != domain.EnvTest ||
		c.Status != domain.ConnectionActive || c.LastTestedAt != nil || c.LastTestStatus != nil || c.MerchantID != memstore.DefaultMerchantID {
		t.Fatalf("connection = %+v", c)
	}
	if d.WebhookURL != "https://pay.example.com/webhooks/fake/"+c.PublicID {
		t.Fatalf("webhook url = %s", d.WebhookURL)
	}
	if !sameJSON(c.Settings, map[string]any{"locale": "en"}) {
		t.Fatalf("settings = %v, want defaults", c.Settings)
	}
	if len(d.Credentials) != 2 || d.Credentials[0].Key != "api_key" || *d.Credentials[0].Last4 != "ijkl" ||
		d.Credentials[1].Key != "token" || d.Credentials[1].Last4 != nil {
		t.Fatalf("hints = %+v", d.Credentials)
	}
	if got := f.stored(t, c.PublicID); got["api_key"] != "key-abcdefghijkl" || got["token"] != "tok-12345" {
		t.Fatalf("stored = %v, want trimmed values", got)
	}
	if strings.Contains(c.CredentialsEnc, "abcdefghijkl") || c.KeyVersion != 1 || c.CredentialsDEKEnc == "" {
		t.Fatalf("credentials not sealed: %+v", c)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "connection.create" || logs[0].Target != (domain.AuditTarget{Type: "connection", ID: c.PublicID}) {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "after", logs[0].After, map[string]any{"id": c.PublicID, "provider": "fake", "name": "fake-test",
		"environment": "test", "status": "active", "settings": map[string]any{"locale": "en"}})
	if b, _ := json.Marshal(logs); strings.Contains(string(b), "abcdefghijkl") || strings.Contains(string(b), "ijkl") ||
		strings.Contains(string(b), "tok-12345") {
		t.Fatalf("audit holds credentials: %s", b)
	}
	if len(d.Channels) != 3 || !d.Channels[0].Enabled || d.Channels[0].Channel != domain.ChannelQRIS {
		t.Fatalf("channels = %+v", d.Channels)
	}
}

func TestConnectionCreateFieldRules(t *testing.T) {
	f := newConnFixture(t)
	cases := []struct {
		name string
		in   CreateConnectionInput
		want []domain.FieldError
	}{
		{"unknown provider skips its schemas", CreateConnectionInput{Provider: "nope", Name: "x", Environment: "prod", Credentials: map[string]any{"a": 1}},
			[]domain.FieldError{{Field: "provider", Rule: "oneof"}, {Field: "name", Rule: "slug"}, {Field: "environment", Rule: "oneof"}}},
		{"blank provider", CreateConnectionInput{Provider: " ", Name: "ok-name", Environment: domain.EnvTest},
			[]domain.FieldError{{Field: "provider", Rule: "required"}}},
		{"everything at once", CreateConnectionInput{Provider: "fake", Name: " ", Credentials: map[string]any{"extra": "x", "token": "short"},
			Settings: map[string]any{"locale": "fr", "x": true, "invoice_duration": json.Number("1.5")}},
			[]domain.FieldError{{Field: "name", Rule: "required"}, {Field: "environment", Rule: "required"},
				{Field: "credentials.api_key", Rule: "required"}, {Field: "credentials.token", Rule: "min_length"},
				{Field: "credentials.extra", Rule: "unknown"}, {Field: "settings.invoice_duration", Rule: "type"},
				{Field: "settings.locale", Rule: "oneof"}, {Field: "settings.x", Rule: "unknown"}}},
		{"non-string credential", CreateConnectionInput{Provider: "fake", Name: "ok-name", Environment: domain.EnvTest,
			Credentials: map[string]any{"api_key": json.Number("12345678"), "token": "tok-12345"}},
			[]domain.FieldError{{Field: "credentials.api_key", Rule: "type"}}},
		{"xendit development key on live", CreateConnectionInput{Provider: "xendit", Name: "xnd-live", Environment: domain.EnvLive,
			Credentials: map[string]any{"secret_key": "xnd_development_abc", "callback_token": "token-123"}},
			[]domain.FieldError{{Field: "credentials.secret_key", Rule: "environment"}}},
		{"xendit key pattern", CreateConnectionInput{Provider: "xendit", Name: "xnd-test", Environment: domain.EnvTest,
			Credentials: map[string]any{"secret_key": "sk_live_abc", "callback_token": "token-123"}},
			[]domain.FieldError{{Field: "credentials.secret_key", Rule: "pattern"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.Create(adminCtx(), tc.in)
			assertFields(t, err, tc.want...)
		})
	}
	for _, name := range []string{"ab", "-abc", "abc-", "a_bc", "ünïcode", strings.Repeat("a", 65)} {
		_, err := f.svc.Create(adminCtx(), CreateConnectionInput{Provider: "fake", Name: name, Environment: domain.EnvTest, Credentials: fakeCreds("x")})
		assertFields(t, err, domain.FieldError{Field: "name", Rule: "slug"})
	}
	if len(f.conns.All()) != 0 || len(f.audit.Logs()) != 0 {
		t.Fatal("invalid creates stored something")
	}
}

func TestConnectionCreateDuplicateNameConflicts(t *testing.T) {
	f := newConnFixture(t)
	f.addConnection(t, "acme-live", domain.EnvLive)
	_, err := f.svc.Create(adminCtx(), CreateConnectionInput{Provider: "fake", Name: "ACME-LIVE", Environment: domain.EnvTest, Credentials: fakeCreds("b")})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
	if len(f.audit.Logs()) != 1 {
		t.Fatalf("audit = %v", f.actions())
	}
}

func TestConnectionUpdate(t *testing.T) {
	f := newConnFixture(t)
	c := f.addConnection(t, "fake-test", domain.EnvTest)
	f.addConnection(t, "taken", domain.EnvTest)
	f.audit.Reset()
	ctx := adminCtx()
	name := "renamed"

	_, err := f.svc.Update(ctx, c.PublicID, UpdateConnectionInput{})
	assertFields(t, err, domain.FieldError{Field: "body", Rule: "required"})
	_, err = f.svc.Update(ctx, c.PublicID, UpdateConnectionInput{Name: &name, Provider: true, Environment: true, Credentials: true,
		Settings: map[string]any{"locale": "xx"}})
	assertFields(t, err, domain.FieldError{Field: "provider", Rule: "immutable"}, domain.FieldError{Field: "environment", Rule: "immutable"},
		domain.FieldError{Field: "credentials", Rule: "immutable"}, domain.FieldError{Field: "settings.locale", Rule: "oneof"})

	// Same values: nothing written or recorded.
	same := " FAKE-TEST "
	if _, err := f.svc.Update(ctx, c.PublicID, UpdateConnectionInput{Name: &same, Settings: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if len(f.audit.Logs()) != 0 || f.conns.Updates != 0 {
		t.Fatalf("no-op wrote: audit=%v updates=%d", f.actions(), f.conns.Updates)
	}

	// Settings replace the whole object; defaults come back.
	d, err := f.svc.Update(ctx, c.PublicID, UpdateConnectionInput{Settings: map[string]any{"invoice_duration": json.Number("3600")}})
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(d.Connection.Settings, map[string]any{"invoice_duration": 3600, "locale": "en"}) {
		t.Fatalf("settings = %v", d.Connection.Settings)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "connection.update" {
		t.Fatalf("audit = %v", f.actions())
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"settings": map[string]any{"locale": "en"}})
	assertJSON(t, "after", logs[0].After, map[string]any{"settings": map[string]any{"invoice_duration": 3600, "locale": "en"}})

	// Only the name changed: only the name is recorded.
	if d, err = f.svc.Update(ctx, c.PublicID, UpdateConnectionInput{Name: &name, Settings: map[string]any{"invoice_duration": json.Number("3600")}}); err != nil {
		t.Fatal(err)
	}
	if d.Connection.Name != "renamed" {
		t.Fatalf("name = %s", d.Connection.Name)
	}
	assertJSON(t, "after", f.audit.Logs()[1].After, map[string]any{"name": "renamed"})

	taken := "taken"
	if _, err := f.svc.Update(ctx, c.PublicID, UpdateConnectionInput{Name: &taken}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("taken name: %v", err)
	}
	if _, err := f.svc.Update(ctx, id.New(id.Connection), UpdateConnectionInput{Name: &name}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestConnectionUpdateCredentials(t *testing.T) {
	f := newConnFixture(t)
	c := f.addConnection(t, "fake-test", domain.EnvTest)
	ctx := adminCtx()
	if _, err := f.svc.Test(ctx, c.PublicID); err != nil {
		t.Fatal(err)
	}
	f.audit.Reset()
	before, _ := f.conns.GetByPublicID(ctx, c.PublicID)

	_, err := f.svc.UpdateCredentials(ctx, c.PublicID, map[string]any{})
	assertFields(t, err, domain.FieldError{Field: "credentials", Rule: "required"})
	_, err = f.svc.UpdateCredentials(ctx, c.PublicID, map[string]any{"api_key": "  ", "nope": "x"})
	assertFields(t, err, domain.FieldError{Field: "credentials.api_key", Rule: "required"}, domain.FieldError{Field: "credentials.nope", Rule: "unknown"})

	d, err := f.svc.UpdateCredentials(ctx, c.PublicID, map[string]any{"api_key": "new-key-0123456789wxyz"})
	if err != nil {
		t.Fatal(err)
	}
	got := f.stored(t, c.PublicID)
	if got["api_key"] != "new-key-0123456789wxyz" || got["token"] != "tok-fake-test-123" {
		t.Fatalf("merged = %v", got)
	}
	after := d.Connection
	if after.LastTestedAt != nil || after.LastTestStatus != nil || after.LastTestError != nil {
		t.Fatalf("test result not reset: %+v", after)
	}
	if after.CredentialsDEKEnc == before.CredentialsDEKEnc || after.CredentialsEnc == before.CredentialsEnc {
		t.Fatal("credentials were not re-sealed with a fresh data key")
	}
	if *d.Credentials[0].Last4 != "wxyz" {
		t.Fatalf("hints = %+v", d.Credentials)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "connection.update_credentials" || logs[0].Before != nil {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "after", logs[0].After, map[string]any{"changed_keys": []string{"api_key"}})
	if strings.Contains(string(logs[0].After), "wxyz") {
		t.Fatal("audit holds a credential")
	}

	// The stored environment drives the Xendit key rule.
	x, err := f.svc.Create(ctx, CreateConnectionInput{Provider: "xendit", Name: "xnd-test", Environment: domain.EnvTest,
		Credentials: map[string]any{"secret_key": "xnd_development_abc", "callback_token": "token-123"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.UpdateCredentials(ctx, x.Connection.PublicID, map[string]any{"secret_key": "xnd_production_abc"})
	assertFields(t, err, domain.FieldError{Field: "credentials.secret_key", Rule: "environment"})
	if f.stored(t, x.Connection.PublicID)["secret_key"] != "xnd_development_abc" {
		t.Fatal("rejected credentials were stored")
	}
}

func TestConnectionReplaceChannels(t *testing.T) {
	f := newConnFixture(t)
	c := f.addConnection(t, "fake-test", domain.EnvTest)
	f.audit.Reset()
	ctx := adminCtx()

	_, err := f.svc.ReplaceChannels(ctx, c.PublicID, []ChannelChoice{
		{Channel: "CARD", Enabled: true}, {Channel: "QRIS", Enabled: false}, {Channel: "QRIS", Enabled: false}, {Channel: "qris"},
	})
	assertFields(t, err, domain.FieldError{Field: "channels.0.channel", Rule: "oneof"}, domain.FieldError{Field: "channels.2.channel", Rule: "duplicate"},
		domain.FieldError{Field: "channels.3.channel", Rule: "oneof"}, domain.FieldError{Field: "channels", Rule: "min_enabled"})

	// Every channel enabled is the same as nothing stored: no write, no entry.
	all := []ChannelChoice{{Channel: "QRIS", Enabled: true}, {Channel: "VA_BCA", Enabled: true}, {Channel: "EWALLET_OVO", Enabled: true}}
	if _, err := f.svc.ReplaceChannels(ctx, c.PublicID, all); err != nil {
		t.Fatal(err)
	}
	if len(f.audit.Logs()) != 0 {
		t.Fatalf("no-op recorded %v", f.actions())
	}

	d, err := f.svc.ReplaceChannels(ctx, c.PublicID, []ChannelChoice{{Channel: "VA_BCA", Enabled: true}, {Channel: "QRIS", Enabled: false}})
	if err != nil {
		t.Fatal(err)
	}
	want := []ChannelState{{domain.ChannelQRIS, false}, {domain.ChannelVABCA, true}, {domain.ChannelEwalletOVO, false}}
	if fmt.Sprint(d.Channels) != fmt.Sprint(want) {
		t.Fatalf("channels = %v, want %v (capability order, unlisted off)", d.Channels, want)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "connection.update_channels" {
		t.Fatalf("audit = %v", f.actions())
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"channels": []map[string]any{
		{"channel": "QRIS", "enabled": true}, {"channel": "VA_BCA", "enabled": true}, {"channel": "EWALLET_OVO", "enabled": true}}})
	assertJSON(t, "after", logs[0].After, map[string]any{"channels": want})

	// An empty list resets to every channel.
	if d, err = f.svc.ReplaceChannels(ctx, c.PublicID, nil); err != nil {
		t.Fatal(err)
	}
	for _, ch := range d.Channels {
		if !ch.Enabled {
			t.Fatalf("reset left %s off", ch.Channel)
		}
	}
	if rows, _ := f.conns.Channels(ctx, c.ID); len(rows) != 0 {
		t.Fatalf("rows after reset = %v", rows)
	}
	if got, _ := f.svc.Get(ctx, c.PublicID); len(got.Channels) != 3 {
		t.Fatalf("detail channels = %v", got.Channels)
	}
}

func TestConnectionTestOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		timeout time.Duration
		result  domain.ConnectionTestStatus
		message string
	}{
		{"ok", nil, 0, "ok", "Credentials accepted."},
		{"rejected with status", fmt.Errorf("x: %w", provider.NewStatusError(provider.ErrRejected, 401, "invalid key xnd_secret")), 0, "failed",
			"The provider rejected the credentials (HTTP 401)."},
		{"rejected without status", fmt.Errorf("x: %w", provider.ErrRejected), 0, "failed", "The provider rejected the credentials."},
		{"retryable", provider.NewStatusError(provider.ErrRetryable, 503, "down"), 0, "failed", "Could not reach the provider. Try again later."},
		{"unknown outcome", fmt.Errorf("x: %w", provider.ErrUnknownOutcome), 0, "failed", "Could not reach the provider. Try again later."},
		{"deadline", nil, time.Nanosecond, "failed", "Could not reach the provider. Try again later."},
		{"other", errors.New("boom"), 0, "failed", "The test failed unexpectedly."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnFixture(t)
			c := f.addConnection(t, "fake-test", domain.EnvTest)
			f.audit.Reset()
			f.fake.TestErr = tc.err
			if tc.timeout > 0 {
				f.svc.testTimeout = tc.timeout
			}
			res, err := f.svc.Test(adminCtx(), c.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if res.Result != tc.result || res.Message != tc.message || !res.TestedAt.Equal(f.clock.Now()) {
				t.Fatalf("result = %+v", res)
			}
			got := res.Detail.Connection
			if got.LastTestStatus == nil || *got.LastTestStatus != tc.result || got.LastTestedAt == nil {
				t.Fatalf("stored = %+v", got)
			}
			if (tc.result == "ok") != (got.LastTestError == nil) || (got.LastTestError != nil && *got.LastTestError != tc.message) {
				t.Fatalf("last_test_error = %v", got.LastTestError)
			}
			if acc := f.fake.Tests[0]; acc.Credentials["api_key"] != "key-fake-test-0123456789" || acc.Environment != domain.EnvTest {
				t.Fatalf("tested with %+v", acc)
			}
			logs := f.audit.Logs()
			if len(logs) != 1 || logs[0].Action != "connection.test" {
				t.Fatalf("audit = %v", f.actions())
			}
			assertJSON(t, "after", logs[0].After, map[string]any{"result": string(tc.result), "tested_at": f.clock.Now(), "warnings": []any{}})
			if len(res.Warnings) != 0 {
				t.Fatalf("warnings = %+v", res.Warnings)
			}
			if lists := f.fake.ListCalls(); lists != map[bool]int{true: 1, false: 0}[tc.result == "ok"] {
				t.Fatalf("transaction probes = %d", lists)
			}
			if unexpected := f.logs.FilterMessage("connection test failed unexpectedly").Len(); unexpected != map[bool]int{true: 1, false: 0}[tc.name == "other"] {
				t.Fatalf("unexpected-failure logs = %d", unexpected)
			}
		})
	}
}

// Regression: credentials that pass but cannot list transactions passed
// silently, and the first reconciliation run then failed. The test now warns
// when the gateway refuses the transaction list, and only then.
func TestConnectionTestWarnsWithoutTransactionAccess(t *testing.T) {
	cases := []struct {
		name     string
		caps     bool
		listErr  error
		warnings []string
		probes   int
	}{
		{"refused", true, provider.NewStatusError(provider.ErrRejected, 403, "REQUEST_FORBIDDEN_ERROR"), []string{"transactions_unavailable"}, 1},
		{"allowed", true, nil, nil, 1},
		{"unreachable is not a warning", true, provider.NewStatusError(provider.ErrRetryable, 503, "down"), nil, 1},
		{"provider without the capability", false, nil, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnFixture(t)
			c := f.addConnection(t, "fake-test", domain.EnvTest)
			f.audit.Reset()
			f.fake.Caps.ListTransactions = tc.caps
			f.fake.ListErrs = []error{tc.listErr}
			res, err := f.svc.Test(adminCtx(), c.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			codes := []string(nil)
			for _, w := range res.Warnings {
				codes = append(codes, w.Code)
				if w.Message == "" || strings.Contains(w.Message, "FORBIDDEN") {
					t.Fatalf("message = %q", w.Message)
				}
			}
			if res.Result != domain.ConnectionTestOK || !slices.Equal(codes, tc.warnings) {
				t.Fatalf("result %s, warnings %v; want ok, %v", res.Result, codes, tc.warnings)
			}
			if got := f.fake.ListCalls(); got != tc.probes {
				t.Fatalf("probes = %d", got)
			}
			if tc.probes == 1 {
				q := f.fake.Lists[0]
				if q.Limit != 1 || !q.To.Equal(f.clock.Now()) || !q.From.Equal(f.clock.Now().Add(-time.Hour)) {
					t.Fatalf("probe = %+v", q)
				}
			}
			want := []any{}
			for _, code := range tc.warnings {
				want = append(want, code)
			}
			assertJSON(t, "after", f.audit.Logs()[0].After, map[string]any{"result": "ok", "tested_at": f.clock.Now(), "warnings": want})
		})
	}
}

func TestConnectionTestDecryptFailureIsAnError(t *testing.T) {
	f := newConnFixture(t)
	c := f.addConnection(t, "fake-test", domain.EnvTest)
	f.audit.Reset()
	stored, _ := f.conns.GetByPublicID(context.Background(), c.PublicID)
	stored.CredentialsEnc = "AAAA" + stored.CredentialsEnc[4:]
	if err := f.conns.Update(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Test(adminCtx(), c.PublicID); err == nil {
		t.Fatal("tampered credentials tested")
	}
	got, _ := f.conns.GetByPublicID(context.Background(), c.PublicID)
	if got.LastTestedAt != nil || len(f.audit.Logs()) != 0 || len(f.fake.Tests) != 0 {
		t.Fatalf("a failed decrypt stored a result: %+v %v", got, f.actions())
	}
}

func TestConnectionDisableEnable(t *testing.T) {
	f := newConnFixture(t)
	ctx := adminCtx()
	c := f.addConnection(t, "fake-test", domain.EnvTest)
	free := f.addConnection(t, "fake-free", domain.EnvTest)
	var apps []*domain.App
	for i := range 12 {
		a := &domain.App{PublicID: id.New(id.App), Slug: fmt.Sprintf("app-%02d", i), Name: "App", Environment: domain.EnvTest, Status: domain.AppActive}
		if i == 0 {
			a.Status = domain.AppDisabled
		}
		if err := f.apps.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		a.DefaultConnectionID = &c.ID
		if err := f.apps.Update(ctx, a); err != nil {
			t.Fatal(err)
		}
		apps = append(apps, a)
		f.clock.Advance(time.Second)
	}
	f.audit.Reset()

	_, err := f.svc.Disable(ctx, c.PublicID)
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeConnectionInUse || coded.Details["app_count"] != 12 {
		t.Fatalf("err = %v", err)
	}
	listed := coded.Details["apps"].([]map[string]any)
	if len(listed) != 10 || listed[0]["id"] != apps[11].PublicID || listed[0]["slug"] != "app-11" {
		t.Fatalf("apps = %v", listed)
	}

	// Only the disabled app left: still in use.
	for _, a := range apps[1:] {
		a.DefaultConnectionID = nil
		if err := f.apps.Update(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.Disable(ctx, c.PublicID); !errors.As(err, &coded) || coded.Details["app_count"] != 1 {
		t.Fatalf("disabled app default: %v", err)
	}

	d, err := f.svc.Disable(ctx, free.PublicID)
	if err != nil || d.Connection.Status != domain.ConnectionDisabled {
		t.Fatalf("disable free: %+v %v", d, err)
	}
	if _, err := f.svc.Disable(ctx, free.PublicID); err != nil {
		t.Fatal(err)
	}
	d, err = f.svc.Enable(ctx, free.PublicID)
	if err != nil || d.Connection.Status != domain.ConnectionActive {
		t.Fatalf("enable: %+v %v", d, err)
	}
	if _, err := f.svc.Enable(ctx, free.PublicID); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.actions(), ","); got != "connection.disable,connection.enable" {
		t.Fatalf("audit = %s", got)
	}
	logs := f.audit.Logs()
	assertJSON(t, "before", logs[0].Before, map[string]any{"status": "active"})
	assertJSON(t, "after", logs[0].After, map[string]any{"status": "disabled"})
	if _, err := f.svc.Enable(ctx, id.New(id.Connection)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestConnectionListFilters(t *testing.T) {
	f := newConnFixture(t)
	ctx := adminCtx()
	for _, n := range []string{"a-test", "b-test"} {
		f.addConnection(t, n, domain.EnvTest)
		f.clock.Advance(time.Second)
	}
	live := f.addConnection(t, "c-live", domain.EnvLive)
	if _, err := f.svc.Create(ctx, CreateConnectionInput{Provider: "xendit", Name: "xnd", Environment: domain.EnvTest,
		Credentials: map[string]any{"secret_key": "xnd_development_abc", "callback_token": "token-123"}}); err != nil {
		t.Fatal(err)
	}
	env := domain.EnvLive
	res, err := f.svc.List(ctx, domain.ConnectionFilter{Environment: &env}, domain.PageRequest{})
	if err != nil || len(res.Items) != 1 || res.Items[0].PublicID != live.PublicID {
		t.Fatalf("live = %+v %v", res, err)
	}
	fake := "fake"
	res, err = f.svc.List(ctx, domain.ConnectionFilter{Provider: &fake}, domain.PageRequest{Limit: 2})
	if err != nil || len(res.Items) != 2 || res.NextCursor == "" || res.Items[0].Name != "c-live" {
		t.Fatalf("page 1 = %+v %v", res, err)
	}
	if !f.svc.KnownProvider("xendit") || f.svc.KnownProvider("midtrans") {
		t.Fatal("KnownProvider")
	}
	if ps := f.svc.Providers(); len(ps) != 2 || ps[0].Descriptor().Name != "fake" || ps[1].Descriptor().Name != "xendit" {
		t.Fatalf("providers = %v", ps)
	}
}

func TestLast4(t *testing.T) {
	cases := map[string]any{
		"12345678901":  nil,
		"123456789012": "9012",
		"ñññññññññññ":  nil,
		"ñññññññññññé": "ñññé",
		"🔑🔑🔑🔑🔑🔑🔑🔑abcd": "abcd",
		"":             nil,
	}
	for in, want := range cases {
		got := last4(in)
		if want == nil {
			if got != nil {
				t.Errorf("last4(%q) = %q, want nil", in, *got)
			}
			continue
		}
		if got == nil || *got != want {
			t.Errorf("last4(%q) = %v, want %q", in, got, want)
		}
	}
}
