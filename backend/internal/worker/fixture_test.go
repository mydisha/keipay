package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

// prefixCipher "encrypts" as "enc:" + plaintext; "bad:" values fail.
type prefixCipher struct{}

func (prefixCipher) Decrypt(s string) (string, error) {
	if strings.HasPrefix(s, "bad:") {
		return "", errDecrypt
	}
	return strings.TrimPrefix(s, "enc:"), nil
}

var errDecrypt = &domain.Error{Code: domain.CodeInternal, Message: "cannot decrypt"}

type scheduled struct {
	id      int64
	attempt int
	manual  bool
	at      time.Time
}

type recordingEnqueuer struct {
	mu    sync.Mutex
	calls []scheduled
	err   error
}

func (e *recordingEnqueuer) EnqueueDelivery(_ context.Context, id int64, attempt int, manual bool, at time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.calls = append(e.calls, scheduled{id: id, attempt: attempt, manual: manual, at: at})
	return nil
}

// request is what the receiver saw of one attempt.
type request struct {
	header http.Header
	body   []byte
}

// receiver is an HTTP endpoint whose answer the test sets.
type receiver struct {
	srv    *httptest.Server
	mu     sync.Mutex
	got    []request
	answer http.HandlerFunc
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{answer: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, request{header: req.Header.Clone(), body: b})
		answer := r.answer
		r.mu.Unlock()
		answer(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) requests() []request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]request(nil), r.got...)
}

func (r *receiver) status(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answer = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

// fwdFixture runs a Forwarder against in-memory stores and a live receiver.
type fwdFixture struct {
	clock    *memstore.Clock
	apps     *memstore.Apps
	payments *memstore.Payments
	hooks    *memstore.Webhooks
	audit    *memstore.Audit
	enq      *recordingEnqueuer
	recv     *receiver
	fwd      *Forwarder
	app      *domain.App
}

var start = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

func newFwdFixture(t *testing.T, disableAfter int) *fwdFixture {
	t.Helper()
	x := &fwdFixture{clock: memstore.NewClock(start), audit: &memstore.Audit{}, enq: &recordingEnqueuer{}, recv: newReceiver(t)}
	x.apps = memstore.NewApps(x.clock.Now)
	x.payments = memstore.NewPayments(x.apps, x.clock.Now)
	x.hooks = memstore.NewWebhooks(x.apps, x.payments, nil, x.clock.Now)
	x.app = &domain.App{PublicID: id.New(id.App), Slug: "acme", Name: "Acme", Environment: domain.EnvTest, Status: domain.AppActive}
	if err := x.apps.Create(context.Background(), x.app); err != nil {
		t.Fatal(err)
	}
	x.fwd = x.newForwarder(disableAfter, nil)
	return x
}

func (x *fwdFixture) newForwarder(disableAfter int, client *http.Client) *Forwarder {
	return NewForwarder(ForwarderDeps{
		Deliveries: x.hooks.Deliveries(), Endpoints: x.hooks.Endpoints(), Tx: memstore.NewTx(x.hooks, x.audit),
		Cipher: prefixCipher{}, Audit: service.NewAuditService(x.audit), Enqueuer: x.enq, Log: zap.NewNop(),
		DisableAfterFailures: disableAfter, Client: client, Now: x.clock.Now,
	})
}

const testSecret = "kpwh_" + "0123456789abcdef0123456789abcdef0123456789abcdef"

// addEndpoint stores an active v2 endpoint pointing at the receiver, signed
// with testSecret; mod adjusts it first.
func (x *fwdFixture) addEndpoint(t *testing.T, mod func(e *domain.WebhookEndpoint)) *domain.WebhookEndpoint {
	t.Helper()
	last4 := testSecret[len(testSecret)-4:]
	e := &domain.WebhookEndpoint{PublicID: id.New(id.WebhookEndpoint), AppID: x.app.ID, URL: x.recv.srv.URL + "/hook",
		Events: []string{"*"}, APIVersion: domain.WebhookAPIV2, SecretEnc: "enc:" + testSecret, SecretLast4: &last4,
		Status: domain.WebhookEndpointActive, RetryPolicy: domain.DefaultRetryPolicy()}
	if mod != nil {
		mod(e)
	}
	if err := x.hooks.Endpoints().Create(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return e
}

// addDelivery stores a PENDING delivery of a new event to ep (nil for none).
func (x *fwdFixture) addDelivery(t *testing.T, ep *domain.WebhookEndpoint, isTest bool, maxAttempts int) *domain.WebhookDelivery {
	t.Helper()
	ctx := context.Background()
	ev := &domain.Event{PublicID: id.New(id.Event), Type: domain.EventPaymentPaid, APIVersion: "v2", AppID: x.app.ID,
		Data: json.RawMessage(`{"id":"pay_X","status":"PAID"}`), IsTest: isTest, CreatedAt: x.clock.Now()}
	if err := x.hooks.Events().Create(ctx, ev); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(ev.Envelope())
	now := x.clock.Now()
	d := &domain.WebhookDelivery{PublicID: id.New(id.Delivery), AppID: x.app.ID, EventID: &ev.ID, EventType: "payment.paid",
		Payload: body, Status: domain.WebhookDeliveryPending, MaxAttempts: maxAttempts, NextAttemptAt: &now}
	if ep != nil {
		d.EndpointID = &ep.ID
	}
	if err := x.hooks.Deliveries().Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	return d
}

func task(t *testing.T, payload string) *asynq.Task {
	t.Helper()
	return asynq.NewTask(TypeWebhookForward, []byte(payload))
}

// run handles the task of one attempt.
func (x *fwdFixture) run(t *testing.T, d *domain.WebhookDelivery, attempt int, manual bool) error {
	t.Helper()
	tk, err := NewWebhookForwardTask(d.ID, attempt, manual)
	if err != nil {
		t.Fatal(err)
	}
	return x.fwd.HandleWebhookForward(context.Background(), tk)
}

func (x *fwdFixture) delivery(t *testing.T, d *domain.WebhookDelivery) domain.WebhookDelivery {
	t.Helper()
	got, err := x.hooks.Deliveries().GetByPublicID(context.Background(), d.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	return *got
}

func (x *fwdFixture) endpoint(t *testing.T, e *domain.WebhookEndpoint) domain.WebhookEndpoint {
	t.Helper()
	got, err := x.hooks.Endpoints().GetByIDForUpdate(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	return *got
}

func (x *fwdFixture) attempts(t *testing.T, d *domain.WebhookDelivery) []domain.DeliveryAttempt {
	t.Helper()
	got, err := x.hooks.Deliveries().Attempts(context.Background(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
