package worker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
)

type deliveryRepo struct {
	domain.WebhookRepository
	d         domain.WebhookDelivery
	delivered bool
	failed    string
}

func (r *deliveryRepo) GetDelivery(context.Context, int64) (*domain.WebhookDelivery, error) {
	d := r.d
	return &d, nil
}
func (r *deliveryRepo) MarkDelivered(context.Context, int64, int) error {
	r.delivered = true
	return nil
}
func (r *deliveryRepo) MarkFailed(_ context.Context, _ int64, _ int, msg string) error {
	r.failed = msg
	return nil
}

type appsByID map[int64]*domain.App

func (a appsByID) GetByID(_ context.Context, id int64) (*domain.App, error) {
	if app, ok := a[id]; ok {
		return app, nil
	}
	return nil, domain.ErrNotFound
}

type prefixCipher struct{}

func (prefixCipher) Decrypt(s string) (string, error) { return strings.TrimPrefix(s, "enc:"), nil }

type received struct {
	signature, event string
	body             []byte
}

func receiver(t *testing.T) (*httptest.Server, *[]received) {
	t.Helper()
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, received{signature: r.Header.Get("X-Kei-Signature"), event: r.Header.Get("X-Kei-Event"), body: b})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func forwardTask(t *testing.T) *asynq.Task {
	t.Helper()
	task, err := NewWebhookForwardTask(1)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// T-15: forwards are signed with the app's webhook secret. For a migrated app
// that secret is its old api_secret, so the signature is what it always was.
func TestForwardSignsWithAppWebhookSecret(t *testing.T) {
	srv, got := receiver(t)
	payload := []byte(`{"event":"payment.paid","payment_public_id":"pay_X","metadata":{"note":"ü"}}`)
	cases := map[string]struct{ secretEnc, secret string }{
		"new app":      {"enc:kpwh_" + strings.Repeat("a", 48), "kpwh_" + strings.Repeat("a", 48)},
		"migrated app": {"enc:sk_legacy_secret", "sk_legacy_secret"},
	}
	for name, tc := range cases {
		*got = nil
		repo := &deliveryRepo{d: domain.WebhookDelivery{ID: 1, AppID: 7, EventType: "payment.paid", Payload: payload}}
		apps := appsByID{7: {ID: 7, PublicID: "app_X", WebhookURL: srv.URL, WebhookSecretEnc: tc.secretEnc}}
		f := NewForwarder(apps, repo, prefixCipher{}, zap.NewNop(), 5)
		if err := f.HandleWebhookForward(context.Background(), forwardTask(t)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(*got) != 1 || !repo.delivered {
			t.Fatalf("%s: requests=%d delivered=%v", name, len(*got), repo.delivered)
		}
		r := (*got)[0]
		if r.signature != hmac.Sign(tc.secret, payload) || string(r.body) != string(payload) || r.event != "payment.paid" {
			t.Fatalf("%s: signature %s body %s", name, r.signature, r.body)
		}
	}
}

// API keys play no part in signing: whatever happens to them, the same app
// secret signs the same payload the same way.
func TestForwardSignatureIgnoresAPIKeys(t *testing.T) {
	srv, got := receiver(t)
	payload := []byte(`{"event":"payment.paid"}`)
	app := &domain.App{ID: 7, PublicID: "app_X", WebhookURL: srv.URL, WebhookSecretEnc: "enc:kpwh_secret"}
	for range 2 {
		repo := &deliveryRepo{d: domain.WebhookDelivery{ID: 1, AppID: 7, EventType: "payment.paid", Payload: payload}}
		if err := NewForwarder(appsByID{7: app}, repo, prefixCipher{}, zap.NewNop(), 5).HandleWebhookForward(context.Background(), forwardTask(t)); err != nil {
			t.Fatal(err)
		}
	}
	if len(*got) != 2 || (*got)[0].signature != (*got)[1].signature || (*got)[0].signature != hmac.Sign("kpwh_secret", payload) {
		t.Fatalf("signatures = %+v", *got)
	}
}

func TestForwardWithoutWebhookURLFailsWithoutRetry(t *testing.T) {
	repo := &deliveryRepo{d: domain.WebhookDelivery{ID: 1, AppID: 7, Payload: []byte(`{}`)}}
	f := NewForwarder(appsByID{7: {ID: 7, PublicID: "app_X"}}, repo, prefixCipher{}, zap.NewNop(), 5)
	err := f.HandleWebhookForward(context.Background(), forwardTask(t))
	if err == nil || !strings.Contains(err.Error(), asynq.SkipRetry.Error()) || repo.failed != "app has no webhook_url" {
		t.Fatalf("err = %v failed = %q", err, repo.failed)
	}
}

func TestForwardUnknownAppRetries(t *testing.T) {
	repo := &deliveryRepo{d: domain.WebhookDelivery{ID: 1, AppID: 9, Payload: []byte(`{}`)}}
	err := NewForwarder(appsByID{}, repo, prefixCipher{}, zap.NewNop(), 5).HandleWebhookForward(context.Background(), forwardTask(t))
	if err == nil || strings.Contains(err.Error(), asynq.SkipRetry.Error()) {
		t.Fatalf("err = %v", err)
	}
}
