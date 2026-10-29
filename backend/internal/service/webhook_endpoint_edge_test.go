package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// AC-3 / T-B8: once deleted, an endpoint takes no more writes. Every mutation
// answers not found and records nothing, and no test event is created.
func TestEndpointMutationsAfterDelete(t *testing.T) {
	f := newWebhookFixture(t)
	e := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{}).Endpoint
	if err := f.endpoints.Delete(adminCtx(), e.PublicID); err != nil {
		t.Fatal(err)
	}
	f.audit.Reset()
	enqueued := len(f.enq.calls)

	desc := "after delete"
	calls := map[string]func() error{
		"update": func() error {
			_, err := f.endpoints.Update(adminCtx(), e.PublicID, UpdateWebhookEndpointInput{Description: &desc})
			return err
		},
		"rotate": func() error {
			_, err := f.endpoints.RotateSecret(adminCtx(), e.PublicID, nil)
			return err
		},
		"disable": func() error {
			_, err := f.endpoints.Disable(adminCtx(), e.PublicID)
			return err
		},
		"enable": func() error {
			_, err := f.endpoints.Enable(adminCtx(), e.PublicID)
			return err
		},
		"test": func() error {
			_, err := f.endpoints.SendTest(adminCtx(), e.PublicID, "payment.paid")
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s on a deleted endpoint: %v, want not found", name, err)
		}
	}
	if logs := f.audit.Logs(); len(logs) != 0 {
		t.Fatalf("audited after delete: %+v", logs)
	}
	if len(f.enq.calls) != enqueued {
		t.Fatalf("a test delivery was queued for a deleted endpoint")
	}
}

// AC-3: length limits count code points, not bytes, at both edges.
func TestEndpointLimitsCountCodePoints(t *testing.T) {
	f := newWebhookFixture(t)
	// 500 two-byte runes (1000 bytes) is within the limit; 501 is not.
	ok := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{
		Description: strings.Repeat("é", 500),
		Headers: []WebhookHeaderInput{
			{Name: strings.Repeat("a", 64), Value: strings.Repeat("ü", 1024)},
		},
	}).Endpoint
	if got := len([]rune(ok.Description)); got != 500 {
		t.Fatalf("description kept %d runes", got)
	}

	_, err := f.endpoints.Create(adminCtx(), f.app.PublicID, CreateWebhookEndpointInput{
		URL: "https://hooks.example.com/x", Events: []string{"*"},
		Description: strings.Repeat("é", 501),
		Headers: []WebhookHeaderInput{
			{Name: strings.Repeat("a", 65), Value: strings.Repeat("ü", 1025)},
		},
	})
	assertFieldErrors(t, err,
		domain.FieldError{Field: "description", Rule: "max"},
		domain.FieldError{Field: "headers.0.name", Rule: "header_name"},
		domain.FieldError{Field: "headers.0.value", Rule: "header_value"},
	)

	// A header name is an ASCII token: a non-ASCII letter is not a tchar.
	_, err = f.endpoints.Create(adminCtx(), f.app.PublicID, CreateWebhookEndpointInput{
		URL: "https://hooks.example.com/x", Events: []string{"*"},
		Headers: []WebhookHeaderInput{{Name: "X-Tëam", Value: "core"}},
	})
	assertFieldErrors(t, err, domain.FieldError{Field: "headers.0.name", Rule: "header_name"})
}

// AC-2: the header hint shows the last 4 code points only for values of at
// least 12 code points, so short values are never half-revealed.
func TestEndpointHeaderHintEdges(t *testing.T) {
	f := newWebhookFixture(t)
	e := f.addEndpoint(t, f.app, CreateWebhookEndpointInput{Headers: []WebhookHeaderInput{
		{Name: "X-Eleven", Value: strings.Repeat("k", 11)},
		{Name: "X-Twelve", Value: strings.Repeat("k", 8) + "wxyz"},
		{Name: "X-Unicode", Value: strings.Repeat("ß", 8) + "日本語!"},
	}}).Endpoint
	h := e.HeadersHint
	if len(h) != 3 || h[0].ValueLast4 != nil || h[1].ValueLast4 == nil || *h[1].ValueLast4 != "wxyz" ||
		h[2].ValueLast4 == nil || *h[2].ValueLast4 != "日本語!" {
		t.Fatalf("hints = %+v", h)
	}
	assertNoEndpointSecrets(t, f, strings.Repeat("k", 11), "wxyz", "日本語!")
}

// AC-3: a PATCH changing the URL of a live app's endpoint to http is refused
// with https, together with other field errors.
func TestEndpointUpdateCollectsErrorsOnLiveApp(t *testing.T) {
	f := newWebhookFixture(t)
	live := f.addApp(t, "globex", domain.EnvLive)
	e := f.addEndpoint(t, live, CreateWebhookEndpointInput{}).Endpoint
	f.audit.Reset()
	url, version := "http://hooks.example.com/plain", "v9"
	events := []string{"payment.paid", "nope"}
	_, err := f.endpoints.Update(adminCtx(), e.PublicID, UpdateWebhookEndpointInput{
		URL: &url, Events: &events, APIVersion: &version,
		RetryPolicy: &RetryPolicyInput{IntervalSeconds: ptr(3601)},
	})
	assertFieldErrors(t, err,
		domain.FieldError{Field: "url", Rule: "https"},
		domain.FieldError{Field: "events.1", Rule: "oneof"},
		domain.FieldError{Field: "api_version", Rule: "oneof"},
		domain.FieldError{Field: "retry_policy.interval_seconds", Rule: "max"},
	)
	got, err := f.endpoints.Get(context.Background(), e.PublicID)
	if err != nil || got.URL != e.URL || len(f.audit.Logs()) != 0 {
		t.Fatalf("refused update changed something: %+v %v audit=%d", got, err, len(f.audit.Logs()))
	}
}
