package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

var secretRe = regexp.MustCompile(`^kpwh_[0-9a-f]{48}$`)

// assertNoEndpointSecrets fails when an audit snapshot holds a secret, a
// ciphertext or one of the given header values.
func assertNoEndpointSecrets(t *testing.T, f *webhookFixture, values ...string) {
	t.Helper()
	values = append(values, "enc(", "kpwh_")
	for _, l := range f.audit.Logs() {
		for _, v := range values {
			if strings.Contains(string(l.Before)+string(l.After), v) {
				t.Fatalf("%s audit entry holds secret material %q: %s %s", l.Action, v, l.Before, l.After)
			}
		}
	}
}

// T-B4 / AC-2: create returns the secret once, keeps it and the header values
// encrypted, and audits neither.
func TestEndpointCreate(t *testing.T) {
	f := newWebhookFixture(t)
	created, err := f.endpoints.Create(adminCtx(), f.app.PublicID, CreateWebhookEndpointInput{
		URL: " https://hooks.example.com/kei ", Description: " Orders\tservice\n", Events: []string{" payment.paid ", "payment.*"},
		Headers:     []WebhookHeaderInput{{Name: "Authorization", Value: "Bearer abcdefghijkl1234"}, {Name: "X-Team", Value: "core"}},
		RetryPolicy: &RetryPolicyInput{MaxAttempts: ptr(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	e, secret := created.Endpoint, created.Secret
	if !secretRe.MatchString(secret) || *e.SecretLast4 != secret[len(secret)-4:] || e.SecretEnc != "enc("+secret+")" {
		t.Fatalf("secret = %q last4 %v enc %q", secret, e.SecretLast4, e.SecretEnc)
	}
	if !id.Valid(e.PublicID, id.WebhookEndpoint) || e.URL != "https://hooks.example.com/kei" || e.Description != "Orders\tservice" ||
		strings.Join(e.Events, ",") != "payment.paid,payment.*" || e.APIVersion != domain.WebhookAPIV2 || e.LegacySignature ||
		e.Status != domain.WebhookEndpointActive || e.Migrated || e.App.PublicID != f.app.PublicID ||
		e.RetryPolicy != (domain.RetryPolicy{MaxAttempts: 3, Backoff: domain.BackoffExponential, IntervalSeconds: 30}) {
		t.Fatalf("endpoint = %+v", e)
	}
	if len(e.HeadersHint) != 2 || e.HeadersHint[0].Name != "Authorization" || *e.HeadersHint[0].ValueLast4 != "1234" ||
		e.HeadersHint[1].ValueLast4 != nil {
		t.Fatalf("hints = %+v", e.HeadersHint)
	}
	if e.HeadersEnc == nil || !strings.HasPrefix(*e.HeadersEnc, "enc(") {
		t.Fatalf("headers not encrypted: %v", e.HeadersEnc)
	}
	stored := f.hooks.AllEndpoints()[0]
	if stored.SecretEnc == secret || stored.SecretEnc != "enc("+secret+")" {
		t.Fatal("secret stored in clear")
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "webhook_endpoint.create" || logs[0].Target != (domain.AuditTarget{Type: "webhook_endpoint", ID: e.PublicID}) {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "after", logs[0].After, map[string]any{
		"id": e.PublicID, "app_id": f.app.PublicID, "url": "https://hooks.example.com/kei", "description": "Orders\tservice",
		"events": []string{"payment.paid", "payment.*"}, "api_version": "v2", "legacy_signature": false,
		"header_names": []string{"Authorization", "X-Team"}, "status": "active", "disabled_reason": nil,
		"retry_policy": map[string]any{"max_attempts": 3, "backoff": "exponential", "interval_seconds": 30},
	})
	assertNoEndpointSecrets(t, f, secret, "Bearer abcdefghijkl1234", "core", *e.SecretLast4)

	again := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{APIVersion: ptr("v1"), LegacySignature: true})
	if again.Secret == secret || again.Endpoint.HeadersEnc != nil || len(again.Endpoint.HeadersHint) != 0 ||
		again.Endpoint.APIVersion != domain.WebhookAPIV1 || !again.Endpoint.LegacySignature ||
		again.Endpoint.RetryPolicy != domain.DefaultRetryPolicy() {
		t.Fatalf("second endpoint = %+v", again.Endpoint)
	}
}

func TestEndpointCreateUnknownApp(t *testing.T) {
	f := newWebhookFixture(t)
	_, err := f.endpoints.Create(adminCtx(), id.New(id.App), CreateWebhookEndpointInput{URL: "https://x.example", Events: []string{"*"}})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func assertFieldErrors(t *testing.T, err error, want ...domain.FieldError) {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeInvalidRequest {
		t.Fatalf("err = %v, want invalid_request", err)
	}
	got, _ := de.Details["fields"].([]domain.FieldError)
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fields = %v, want %v", got, want)
		}
	}
}

// T-B5 / AC-3: every failing field comes back in one response.
func TestEndpointCreateFieldErrors(t *testing.T) {
	f := newWebhookFixture(t)
	headers := make([]WebhookHeaderInput, 11)
	for i := range headers {
		headers[i] = WebhookHeaderInput{Name: "X-H" + string(rune('a'+i)), Value: "v"}
	}
	cases := []struct {
		name string
		app  *domain.App
		in   CreateWebhookEndpointInput
		want []domain.FieldError
	}{
		{name: "everything", app: f.app, in: CreateWebhookEndpointInput{
			URL: "  ", Description: strings.Repeat("é", 501), Events: []string{"payment.paid", " payment.paid", "foo.*", "pay​ment"},
			APIVersion: ptr("v3"),
			Headers: []WebhookHeaderInput{{Name: "X-KeiPay-Foo", Value: "a"}, {Name: "content-type", Value: "b"}, {Name: "X-A", Value: "c"},
				{Name: "x-a", Value: "d"}, {Name: "Bad Name", Value: "e\r\nInjected: 1"}, {Name: "X-Kei-Event", Value: ""}},
			RetryPolicy: &RetryPolicyInput{MaxAttempts: ptr(0), Backoff: ptr("linear"), IntervalSeconds: ptr(3601)},
		}, want: []domain.FieldError{
			{Field: "url", Rule: "required"}, {Field: "description", Rule: "max"},
			{Field: "events.1", Rule: "duplicate"}, {Field: "events.2", Rule: "oneof"}, {Field: "events.3", Rule: "oneof"},
			{Field: "api_version", Rule: "oneof"},
			{Field: "headers.0.name", Rule: "reserved"}, {Field: "headers.1.name", Rule: "reserved"}, {Field: "headers.3.name", Rule: "duplicate"},
			{Field: "headers.4.name", Rule: "header_name"}, {Field: "headers.4.value", Rule: "header_value"},
			{Field: "headers.5.name", Rule: "reserved"}, {Field: "headers.5.value", Rule: "header_value"},
			{Field: "retry_policy.max_attempts", Rule: "min"}, {Field: "retry_policy.backoff", Rule: "oneof"},
			{Field: "retry_policy.interval_seconds", Rule: "max"},
		}},
		{name: "invalid url, no events, 11 headers", app: f.app, in: CreateWebhookEndpointInput{URL: "ftp://x.example/a", Headers: headers,
			Description: "line\x00break", RetryPolicy: &RetryPolicyInput{MaxAttempts: ptr(21), IntervalSeconds: ptr(9)}},
			want: []domain.FieldError{{Field: "url", Rule: "url"}, {Field: "description", Rule: "name"}, {Field: "events", Rule: "required"},
				{Field: "headers", Rule: "max"}, {Field: "retry_policy.max_attempts", Rule: "max"}, {Field: "retry_policy.interval_seconds", Rule: "min"}}},
		{name: "http on live", app: f.addApp(t, "live", domain.EnvLive), in: CreateWebhookEndpointInput{URL: "http://hooks.example.com", Events: []string{"*"}},
			want: []domain.FieldError{{Field: "url", Rule: "https"}}},
		{name: "21 events", app: f.app, in: CreateWebhookEndpointInput{URL: "https://x.example", Events: make([]string, 21)},
			want: []domain.FieldError{{Field: "events", Rule: "max"}}},
		{name: "header value too long", app: f.app, in: CreateWebhookEndpointInput{URL: "https://x.example", Events: []string{"*"},
			Headers: []WebhookHeaderInput{{Name: "X-Long", Value: strings.Repeat("v", 1025)}, {Name: strings.Repeat("h", 65), Value: "v"}}},
			want: []domain.FieldError{{Field: "headers.0.value", Rule: "header_value"}, {Field: "headers.1.name", Rule: "header_name"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.endpoints.Create(adminCtx(), tc.app.PublicID, tc.in)
			assertFieldErrors(t, err, tc.want...)
		})
	}
	if len(f.hooks.AllEndpoints()) != 0 || len(f.audit.Logs()) != 0 {
		t.Fatal("invalid input wrote something")
	}
	// http stays fine for a test app, and the boundaries are accepted.
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{URL: "http://127.0.0.1:9000/hook", Description: strings.Repeat("é", 500),
		Headers:     []WebhookHeaderInput{{Name: "X-Long", Value: strings.Repeat("ü", 1024)}, {Name: strings.Repeat("h", 64), Value: "v"}},
		RetryPolicy: &RetryPolicyInput{MaxAttempts: ptr(20), IntervalSeconds: ptr(10), Backoff: ptr("fixed")}})
}

// T-B6 / AC-3: at most 10 endpoints per app; deleted ones do not count.
func TestEndpointLimit(t *testing.T) {
	f := newWebhookFixture(t)
	var first *EndpointWithSecret
	for i := range domain.MaxWebhookEndpointsPerApp {
		e := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
		if i == 0 {
			first = e
		}
	}
	_, err := f.endpoints.Create(adminCtx(), f.app.PublicID, CreateWebhookEndpointInput{URL: "https://x.example", Events: []string{"*"}})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeValidationFailed || de.Message != "an app can have at most 10 webhook endpoints" {
		t.Fatalf("11th endpoint: %v", err)
	}
	if err := f.endpoints.Delete(adminCtx(), first.Endpoint.PublicID); err != nil {
		t.Fatal(err)
	}
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	// Another app has its own limit.
	f.addEndpoint(t, f.addApp(t, "globex", domain.EnvTest), CreateWebhookEndpointInput{})
}

// T-B7 / AC-3: PATCH audits only what changed and refuses what cannot change.
func TestEndpointUpdate(t *testing.T) {
	f := newWebhookFixture(t)
	created := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{URL: "https://hooks.example.com/a",
		Headers: []WebhookHeaderInput{{Name: "Authorization", Value: "Bearer secret-value-1"}}})
	pub := created.Endpoint.PublicID
	f.audit.Reset()

	for name, tc := range map[string]struct {
		in   UpdateWebhookEndpointInput
		want []domain.FieldError
	}{
		"empty":     {UpdateWebhookEndpointInput{}, []domain.FieldError{{Field: "body", Rule: "required"}}},
		"immutable": {UpdateWebhookEndpointInput{Status: true, Secret: true, AppID: true, URL: ptr("https://x.example")}, []domain.FieldError{{Field: "status", Rule: "immutable"}, {Field: "secret", Rule: "immutable"}, {Field: "app_id", Rule: "immutable"}}},
		"invalid": {UpdateWebhookEndpointInput{URL: ptr(""), Events: &[]string{}, APIVersion: ptr("v9"),
			RetryPolicy: &RetryPolicyInput{Backoff: ptr("x")}}, []domain.FieldError{{Field: "url", Rule: "required"}, {Field: "events", Rule: "required"},
			{Field: "api_version", Rule: "oneof"}, {Field: "retry_policy.backoff", Rule: "oneof"}}},
	} {
		_, err := f.endpoints.Update(adminCtx(), pub, tc.in)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		assertFieldErrors(t, err, tc.want...)
	}
	before := f.hooks.AllEndpoints()[0]

	// No change: nothing written or audited, updated_at kept.
	f.clock.Advance(time.Minute)
	same := []WebhookHeaderInput{{Name: "Authorization", Value: "Bearer secret-value-1"}}
	got, err := f.endpoints.Update(adminCtx(), pub, UpdateWebhookEndpointInput{URL: ptr(" https://hooks.example.com/a "),
		Events: &[]string{"*"}, Headers: &same, RetryPolicy: &RetryPolicyInput{}})
	if err != nil || len(f.audit.Logs()) != 0 || !f.hooks.AllEndpoints()[0].UpdatedAt.Equal(before.UpdatedAt) || got.URL != before.URL {
		t.Fatalf("no-op: %v audit=%d", err, len(f.audit.Logs()))
	}

	// Omitted headers are kept; a partial retry policy fills defaults.
	got, err = f.endpoints.Update(adminCtx(), pub, UpdateWebhookEndpointInput{Description: ptr("Ünïcode"), APIVersion: ptr("v1"),
		LegacySignature: ptr(true), RetryPolicy: &RetryPolicyInput{IntervalSeconds: ptr(60)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Ünïcode" || got.APIVersion != domain.WebhookAPIV1 || !got.LegacySignature || got.HeadersEnc == nil ||
		got.RetryPolicy != (domain.RetryPolicy{MaxAttempts: 8, Backoff: domain.BackoffExponential, IntervalSeconds: 60}) {
		t.Fatalf("updated = %+v", got)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "webhook_endpoint.update" {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "after", logs[0].After, map[string]any{"description": "Ünïcode", "api_version": "v1", "legacy_signature": true,
		"retry_policy": map[string]any{"max_attempts": 8, "backoff": "exponential", "interval_seconds": 60}})

	// headers [] clears them; the audit names headers, never values.
	got, err = f.endpoints.Update(adminCtx(), pub, UpdateWebhookEndpointInput{Headers: &[]WebhookHeaderInput{}})
	if err != nil || got.HeadersEnc != nil || len(got.HeadersHint) != 0 {
		t.Fatalf("clear headers: %+v %v", got, err)
	}
	logs = f.audit.Logs()
	assertJSON(t, "before", logs[1].Before, map[string]any{"header_names": []string{"Authorization"}})
	assertJSON(t, "after", logs[1].After, map[string]any{"header_names": []string{}})
	assertNoEndpointSecrets(t, f, "Bearer secret-value-1")

	// The https rule follows the app's environment.
	live := f.addEndpoint(t, f.addApp(t, "live", domain.EnvLive), CreateWebhookEndpointInput{})
	_, err = f.endpoints.Update(adminCtx(), live.Endpoint.PublicID, UpdateWebhookEndpointInput{URL: ptr("http://hooks.example.com")})
	assertFieldErrors(t, err, domain.FieldError{Field: "url", Rule: "https"})
	if _, err := f.endpoints.Update(adminCtx(), id.New(id.WebhookEndpoint), UpdateWebhookEndpointInput{URL: ptr("https://x.example")}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown endpoint: %v", err)
	}
}

// T-B8: a deleted endpoint is gone from reads and lists, its deliveries stay.
func TestEndpointDelete(t *testing.T) {
	f := newWebhookFixture(t)
	e := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{}).Endpoint
	keep := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{}).Endpoint
	d, err := f.endpoints.SendTest(adminCtx(), e.PublicID, "payment.paid")
	if err != nil {
		t.Fatal(err)
	}
	f.audit.Reset()
	if err := f.endpoints.Delete(adminCtx(), e.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.endpoints.Get(context.Background(), e.PublicID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
	if err := f.endpoints.Delete(adminCtx(), e.PublicID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	page, err := f.endpoints.List(context.Background(), f.app.PublicID, domain.PageRequest{})
	if err != nil || len(page.Items) != 1 || page.Items[0].PublicID != keep.PublicID {
		t.Fatalf("list = %+v %v", page.Items, err)
	}
	got, _, err := f.deliveries.Get(context.Background(), d.PublicID)
	if err != nil || got.Endpoint == nil || !got.Endpoint.Deleted {
		t.Fatalf("delivery of a deleted endpoint = %+v %v", got, err)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "webhook_endpoint.delete" || logs[0].After != nil {
		t.Fatalf("audit = %+v", logs)
	}
}

// T-B9 / AC-7: rotation keeps the current secret as the previous one for the
// overlap; a second rotation drops the oldest.
func TestEndpointRotateSecret(t *testing.T) {
	f := newWebhookFixture(t)
	created := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	pub := created.Endpoint.PublicID
	f.audit.Reset()

	for _, tc := range []struct {
		hours int
		rule  string
	}{{169, "max"}, {-1, "min"}} {
		_, err := f.endpoints.RotateSecret(adminCtx(), pub, ptr(tc.hours))
		assertFieldErrors(t, err, domain.FieldError{Field: "overlap_hours", Rule: tc.rule})
	}

	first, err := f.endpoints.RotateSecret(adminCtx(), pub, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := first.Endpoint
	wantExpiry := fixtureStart.Add(24 * time.Hour)
	if !secretRe.MatchString(first.Secret) || first.Secret == created.Secret || e.SecretEnc != "enc("+first.Secret+")" ||
		*e.PreviousSecretEnc != "enc("+created.Secret+")" || !e.PreviousSecretExpiresAt.Equal(wantExpiry) || *e.SecretLast4 != first.Secret[len(first.Secret)-4:] {
		t.Fatalf("after rotation = %+v", e)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "webhook_endpoint.rotate_secret" {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"secret_last4": *created.Endpoint.SecretLast4, "previous_secret_expires_at": nil})
	assertJSON(t, "after", logs[0].After, map[string]any{"secret_last4": *e.SecretLast4, "previous_secret_expires_at": "2026-10-30T09:00:00Z"})
	assertNoEndpointSecrets(t, f, created.Secret, first.Secret)

	second, err := f.endpoints.RotateSecret(adminCtx(), pub, ptr(168))
	if err != nil {
		t.Fatal(err)
	}
	if *second.Endpoint.PreviousSecretEnc != "enc("+first.Secret+")" || !second.Endpoint.PreviousSecretExpiresAt.Equal(fixtureStart.Add(168*time.Hour)) {
		t.Fatalf("second rotation kept the oldest secret: %+v", second.Endpoint)
	}
	third, err := f.endpoints.RotateSecret(adminCtx(), pub, ptr(0))
	if err != nil {
		t.Fatal(err)
	}
	if third.Endpoint.PreviousSecretEnc != nil || third.Endpoint.PreviousSecretExpiresAt != nil {
		t.Fatalf("overlap 0 kept a previous secret: %+v", third.Endpoint)
	}

	// Disabled endpoints rotate too; deleted ones do not exist.
	if _, err := f.endpoints.Disable(adminCtx(), pub); err != nil {
		t.Fatal(err)
	}
	if _, err := f.endpoints.RotateSecret(adminCtx(), pub, nil); err != nil {
		t.Fatalf("rotate disabled: %v", err)
	}
	if _, err := f.endpoints.RotateSecret(adminCtx(), id.New(id.WebhookEndpoint), nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

// T-B10 / AC-10: disable and enable are idempotent and audited once; enable
// resets the failure counter.
func TestEndpointDisableEnable(t *testing.T) {
	f := newWebhookFixture(t)
	pub := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{}).Endpoint.PublicID
	f.audit.Reset()
	for range 2 {
		e, err := f.endpoints.Disable(adminCtx(), pub)
		if err != nil || e.Status != domain.WebhookEndpointDisabled || *e.DisabledReason != domain.WebhookDisabledManual || !e.DisabledAt.Equal(fixtureStart) {
			t.Fatalf("disable = %+v %v", e, err)
		}
	}
	stored := f.hooks.AllEndpoints()[0]
	stored.ConsecutiveFailures = 4
	if err := f.hooks.Endpoints().UpdateHealth(context.Background(), &stored); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		e, err := f.endpoints.Enable(adminCtx(), pub)
		if err != nil || e.Status != domain.WebhookEndpointActive || e.DisabledReason != nil || e.DisabledAt != nil || e.ConsecutiveFailures != 0 {
			t.Fatalf("enable = %+v %v", e, err)
		}
	}
	logs := f.audit.Logs()
	if len(logs) != 2 || logs[0].Action != "webhook_endpoint.disable" || logs[1].Action != "webhook_endpoint.enable" {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "disable after", logs[0].After, map[string]any{"status": "disabled", "disabled_reason": "manual", "consecutive_failures": 0})
	assertJSON(t, "enable before", logs[1].Before, map[string]any{"status": "disabled", "disabled_reason": "manual", "consecutive_failures": 4})
	assertJSON(t, "enable after", logs[1].After, map[string]any{"status": "active", "disabled_reason": nil, "consecutive_failures": 0})
}

// T-B26 / AC-12: a test send is one test event with the catalog example and
// one single-attempt delivery to this endpoint, whatever its state.
func TestEndpointSendTest(t *testing.T) {
	f := newWebhookFixture(t)
	e := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{Events: []string{"payment.paid"}}).Endpoint
	f.addEndpoint(t, f.app, CreateWebhookEndpointInput{})
	if _, err := f.endpoints.Disable(adminCtx(), e.PublicID); err != nil {
		t.Fatal(err)
	}
	f.audit.Reset()
	for _, bad := range []struct{ in, rule string }{{"payment.*", "oneof"}, {"*", "oneof"}, {"payment.refunded", "oneof"}, {"", "required"}} {
		_, err := f.endpoints.SendTest(adminCtx(), e.PublicID, bad.in)
		assertFieldErrors(t, err, domain.FieldError{Field: "event_type", Rule: bad.rule})
	}

	d, err := f.endpoints.SendTest(adminCtx(), e.PublicID, "payment.settled")
	if err != nil {
		t.Fatal(err)
	}
	events := f.hooks.AllEvents()
	if len(events) != 1 || !events[0].IsTest || events[0].PaymentID != nil || events[0].Type != domain.EventPaymentSettled ||
		events[0].AppID != f.app.ID {
		t.Fatalf("events = %+v", events)
	}
	wantData, _ := marshalData(domain.ExampleEventData(domain.EventPaymentSettled))
	if string(events[0].Data) != string(wantData) {
		t.Fatalf("data = %s, want %s", events[0].Data, wantData)
	}
	ds := f.hooks.AllDeliveries()
	if len(ds) != 1 || ds[0].ID != d.ID || *ds[0].EndpointID != e.ID || ds[0].MaxAttempts != 1 || ds[0].PaymentID != nil ||
		ds[0].Status != domain.WebhookDeliveryPending || !ds[0].IsTest() || d.Endpoint == nil || d.Event == nil || !d.Event.IsTest {
		t.Fatalf("delivery = %+v", ds)
	}
	if len(f.enq.calls) != 1 || f.enq.calls[0] != (enqueued{id: d.ID, attempt: 1, at: fixtureStart}) {
		t.Fatalf("enqueued = %+v", f.enq.calls)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "webhook_endpoint.test" {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "after", logs[0].After, map[string]any{"event_type": "payment.settled", "delivery_id": d.PublicID})

	// A queue failure still answers; the sweeper sends the row.
	f.enq.err = errors.New("redis down")
	if _, err := f.endpoints.SendTest(adminCtx(), e.PublicID, "payment.paid"); err != nil {
		t.Fatalf("enqueue failure: %v", err)
	}
	if _, err := f.endpoints.SendTest(adminCtx(), id.New(id.WebhookEndpoint), "payment.paid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown endpoint: %v", err)
	}
}

// A v1 test body has payment_id 0 and the example data.
func TestEndpointSendTestV1Body(t *testing.T) {
	f := newWebhookFixture(t)
	e := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{APIVersion: ptr("v1")}).Endpoint
	d, err := f.endpoints.SendTest(adminCtx(), e.PublicID, "payment.paid")
	if err != nil {
		t.Fatal(err)
	}
	ev := f.hooks.AllEvents()[0]
	assertBody(t, f.hooks.AllDeliveries()[0].Payload, map[string]any{
		"event": "payment.paid", "event_id": ev.PublicID, "payment_id": jsonNumber("0"), "payment_public_id": "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
		"order_id": "order_1001", "external_id": "kp_pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", "metadata": map[string]any{}, "status": "PAID",
		"amount": jsonNumber("150000"), "currency": "IDR", "paid_at": "2026-01-01T09:59:30Z", "timestamp": "2026-10-29T09:00:00Z",
	})
	if d.PaymentPublicID != "" {
		t.Fatal("test delivery names a payment")
	}
}

func TestEndpointListAndGet(t *testing.T) {
	f := newWebhookFixture(t)
	var ids []string
	for range 3 {
		ids = append(ids, f.addEndpoint(t, f.app, CreateWebhookEndpointInput{}).Endpoint.PublicID)
		f.clock.Advance(time.Second)
	}
	page, err := f.endpoints.List(context.Background(), f.app.PublicID, domain.PageRequest{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Items[0].PublicID != ids[2] || page.NextCursor == "" {
		t.Fatalf("page 1 = %+v %v", page, err)
	}
	page, err = f.endpoints.List(context.Background(), f.app.PublicID, domain.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(page.Items) != 1 || page.Items[0].PublicID != ids[0] || page.NextCursor != "" {
		t.Fatalf("page 2 = %+v %v", page, err)
	}
	if _, err := f.endpoints.List(context.Background(), id.New(id.App), domain.PageRequest{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown app: %v", err)
	}
	if _, err := f.endpoints.List(context.Background(), f.app.PublicID, domain.PageRequest{Cursor: "x"}); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
	got, err := f.endpoints.Get(context.Background(), ids[1])
	if err != nil || got.PublicID != ids[1] || got.App.PublicID != f.app.PublicID {
		t.Fatalf("get = %+v %v", got, err)
	}
	types, wildcards := f.endpoints.EventTypes()
	if len(types) != 5 || len(wildcards) != 2 {
		t.Fatalf("catalog = %d types, %d wildcards", len(types), len(wildcards))
	}
}

func TestEndpointMutationsNeedAnActor(t *testing.T) {
	f := newWebhookFixture(t)
	_, err := f.endpoints.Create(context.Background(), f.app.PublicID, CreateWebhookEndpointInput{URL: "https://x.example", Events: []string{"*"}})
	if !errors.Is(err, ErrNoActor) || len(f.hooks.AllEndpoints()) != 0 {
		t.Fatalf("err = %v endpoints=%d", err, len(f.hooks.AllEndpoints()))
	}
}
