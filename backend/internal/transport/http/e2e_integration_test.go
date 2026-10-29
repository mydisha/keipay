package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	redisrepo "github.com/mydisha/keipay/backend/internal/repository/redis"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/worker"
)

const (
	legacyKey    = "kp_0123456789abcdef0123456789abcdef"
	legacySecret = "sk_legacy_0123456789abcdef"
)

// runNonces scopes the shared Redis nonce store to one test run, since
// requests repeat byte for byte across runs.
type runNonces struct {
	inner  *redisrepo.Idempotency
	prefix string
}

func (n runNonces) Acquire(ctx context.Context, key string) (bool, error) {
	return n.inner.Acquire(ctx, n.prefix+key)
}

// e2e is the real router over Postgres and Redis on a database migrated to
// 000006 (before apps replaced tenants) with one legacy tenant that had a
// webhook URL. The deprecated XENDIT_* settings are imported as on an
// upgraded server, so the legacy app pays through the real Xendit adapter,
// pointed at a fake Xendit API.
type e2e struct {
	router     *gin.Engine
	appSvc     *service.AppService
	connSvc    *service.ConnectionService
	payments   *postgres.PaymentRepo
	apps       *postgres.AppRepo
	endpoints  *postgres.WebhookEndpointRepo
	deliveries *postgres.DeliveryRepo
	tx         *postgres.TxManager
	audit      *service.AuditService
	webhook    *service.WebhookService
	cipher     *crypto.Cipher
	xenditAPI  *fakeXenditAPI
	// legacy is the connection imported from the XENDIT_* settings.
	legacy *domain.Connection
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	addr := os.Getenv("KEI_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("KEI_TEST_REDIS_ADDR not set")
	}
	db := pgtest.New(t, 6)
	ctx := context.Background()
	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.NewPool(ctx, db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	secretEnc, _ := cipher.Encrypt(legacySecret)
	for _, q := range []string{
		`INSERT INTO tenants (id, name, slug, api_key, api_secret_enc, active, webhook_url)
			VALUES (1, 'Acme', 'acme', '` + legacyKey + `', '` + secretEnc + `', true, 'https://acme.example/hook')`,
		`SELECT setval(pg_get_serial_sequence('tenants', 'id'), 1)`,
		`INSERT INTO payments (id, tenant_id, order_id, external_id, amount) VALUES (1, 1, 'ord-1', 'acme:ord-1', 1000)`,
		`SELECT setval(pg_get_serial_sequence('payments', 'id'), 1)`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.Migrate.Up(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rdb, err := redisrepo.New(ctx, addr, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	tx := postgres.NewTxManager(pool)
	e := &e2e{
		apps:       postgres.NewAppRepo(pool),
		payments:   postgres.NewPaymentRepo(pool),
		endpoints:  postgres.NewWebhookEndpointRepo(pool),
		deliveries: postgres.NewDeliveryRepo(pool),
		tx:         tx,
		cipher:     cipher,
		xenditAPI:  newFakeXenditAPI(t),
	}
	registry, err := provider.NewRegistry(xendit.New(xendit.WithBaseURL(e.xenditAPI.URL)), providertest.New())
	if err != nil {
		t.Fatal(err)
	}
	conns := postgres.NewConnectionRepo(pool)
	audit := service.NewAuditService(postgres.NewAuditRepo(pool))
	e.audit = audit
	e.appSvc = service.NewAppService(e.apps, postgres.NewAPIKeyRepo(pool), conns, cipher, tx, audit)
	e.connSvc = service.NewConnectionService(service.ConnectionDeps{
		Connections: conns, Merchants: postgres.NewMerchantRepo(pool), Apps: e.apps, Payments: e.payments,
		Registry: registry, Sealer: cipher, Tx: tx, Audit: audit, BaseURL: "https://pay.example.com",
	})
	imported, err := e.connSvc.ImportLegacyXendit(ctx, "xnd_production_e2e", "e2e-callback-token")
	if err != nil || !imported.Created || len(imported.Defaulted) != 1 || imported.PaymentsBackfilled != 1 {
		t.Fatalf("import: %+v %v", imported, err)
	}
	e.legacy = imported.Connection
	e.webhook = service.NewWebhookService(service.WebhookDeps{
		Payments: e.payments, Reports: postgres.NewPaymentReportRepo(pool), Inbound: postgres.NewInboundWebhookRepo(pool),
		Events: postgres.NewEventRepo(pool), Endpoints: e.endpoints, Deliveries: e.deliveries, Tx: tx, Enqueuer: &recordingQueue{},
	})
	gin.SetMode(gin.TestMode)
	e.router = NewRouter(Deps{
		Log:             zap.NewNop(),
		AppResolver:     e.appSvc,
		NonceStore:      runNonces{inner: redisrepo.NewIdempotency(rdb, middleware.NonceTTL), prefix: "it-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ":"},
		SecretDecryptor: cipher,
		Payment:         handler.NewPaymentHandler(service.NewPaymentService(e.payments, conns, registry, cipher, zap.NewNop())),
	})
	return e
}

// fakeConnection creates a fake-provider connection and makes it the default
// of the app.
func (e *e2e) fakeConnection(t *testing.T, name string, app *domain.App) *domain.Connection {
	t.Helper()
	ctx := domain.WithActor(context.Background(), domain.SystemActor("test"))
	d, err := e.connSvc.Create(ctx, service.CreateConnectionInput{Provider: "fake", Name: name, Environment: app.Environment,
		Credentials: map[string]any{"api_key": "fake-api-key-0001", "token": "fake-token-0001"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.appSvc.Update(ctx, app.PublicID, service.UpdateAppInput{DefaultConnectionSet: true, DefaultConnectionID: &d.Connection.PublicID}); err != nil {
		t.Fatal(err)
	}
	return d.Connection
}

// invoicesCreated counts the invoices the fake Xendit API was asked for.
func (e *e2e) invoicesCreated() int {
	e.xenditAPI.mu.Lock()
	defer e.xenditAPI.mu.Unlock()
	n := 0
	for _, c := range e.xenditAPI.calls {
		if c == "POST /v2/invoices/" {
			n++
		}
	}
	return n
}

func (e *e2e) call(method, target, apiKey, secret, body string, headers ...string) *httptest.ResponseRecorder {
	ts := signedTimestamp(secret, body)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", hmac.Sign(secret, []byte(ts+"."+body)))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

// A migrated integration keeps working end to end, and references route
// callbacks to the right app.
func TestIntegrationEndToEndMigratedApp(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()

	// The legacy key and secret still authenticate, and see the legacy payment.
	w := e.call(http.MethodGet, "/v1/payments/1", legacyKey, legacySecret, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"reference":"acme:ord-1"`) {
		t.Fatalf("legacy get: %d %s", w.Code, w.Body.String())
	}
	expectError(t, e.call(http.MethodGet, "/v1/payments/1", legacyKey, "wrong", ""), 401, domain.CodeInvalidSignature)

	// Metadata and Idempotency-Key, with a replay.
	body := `{"order_id":"ord-2","amount":2500,"metadata":{"channel":"mobile","n":12345678901234567890,"note":"ü 🌏"}}`
	w = e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret, body, "Idempotency-Key", "idem-e2e")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID        string `json:"id"`
		Reference string `json:"reference"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	w = e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret, body, "Idempotency-Key", "idem-e2e")
	if w.Code != http.StatusCreated || w.Header().Get("Idempotent-Replayed") != "true" || !strings.Contains(w.Body.String(), created.ID) {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(e.call(http.MethodGet, "/v1/payments/"+created.ID, legacyKey, legacySecret, "").Body.String(), `"n":12345678901234567890`) {
		t.Fatal("metadata number changed in storage")
	}

	// Another app gets its own payment with the same order id.
	other, err := e.appSvc.Create(domain.WithActor(ctx, domain.SystemActor("test")),
		service.CreateAppInput{Name: "Globex", Slug: "globex", Environment: domain.EnvTest})
	if err != nil {
		t.Fatal(err)
	}
	otherConn := e.fakeConnection(t, "globex-test", other.App)
	w = e.call(http.MethodPost, "/v1/payments", other.APIKey, other.APISecret, `{"order_id":"ord-2","amount":10}`, "Idempotency-Key", "idem-e2e")
	if w.Code != http.StatusCreated || strings.Contains(w.Body.String(), created.ID) {
		t.Fatalf("other app: %d %s", w.Code, w.Body.String())
	}
	var otherPay struct {
		Reference string `json:"reference"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &otherPay)

	// Callbacks for a legacy reference and both new references, each at the
	// connection its payment was made through.
	for _, cb := range []struct {
		conn *domain.Connection
		ev   provider.InboundEvent
	}{
		{e.legacy, provider.InboundEvent{EventKey: "inv_legacy", Reference: "acme:ord-1", ProviderStatus: "PAID", Status: domain.PaymentStatusPaid, Amount: 1000}},
		{e.legacy, provider.InboundEvent{EventKey: "inv_new", Reference: created.Reference, ProviderStatus: "PAID", Status: domain.PaymentStatusPaid, Amount: 2500}},
		{otherConn, provider.InboundEvent{EventKey: "inv_other", Reference: otherPay.Reference, ProviderStatus: "PAID", Status: domain.PaymentStatusPaid, Amount: 10}},
	} {
		if err := e.webhook.Process(ctx, cb.conn, cb.ev, []byte(`{}`)); err != nil {
			t.Fatalf("%s: %v", cb.ev.Reference, err)
		}
	}
	// Only acme had a webhook URL, so only it has an endpoint: the one
	// migrated from it.
	page, err := e.deliveries.List(ctx, domain.DeliveryFilter{}, domain.Page{Limit: 50})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("deliveries = %+v %v", page.Items, err)
	}
	byPayment := map[string]domain.WebhookDelivery{}
	for _, d := range page.Items {
		full, err := e.deliveries.GetByPublicID(ctx, d.PublicID)
		if err != nil {
			t.Fatal(err)
		}
		byPayment[d.PaymentPublicID] = *full
	}
	if d := byPayment[created.ID]; d.App.Slug != "acme" || !strings.Contains(string(d.Payload), `"payment_public_id": "`+created.ID+`"`) ||
		!strings.Contains(string(d.Payload), `"note": "ü 🌏"`) || !strings.Contains(string(d.Payload), `"event_id": "evt_`) {
		t.Fatalf("new delivery = %+v %s", d.App, d.Payload)
	}

	// The migrated endpoint sends the v1 body, signed as before with the
	// app's original api_secret.
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case !hmac.Verify(legacySecret, b, r.Header.Get("X-Kei-Signature")):
			got = append(got, "bad legacy signature")
		case !strings.HasPrefix(r.Header.Get("X-KeiPay-Signature"), "t="):
			got = append(got, "no signature")
		case !strings.Contains(string(b), `"event": "payment.paid"`):
			got = append(got, "not a v1 body")
		default:
			got = append(got, "ok")
		}
	}))
	defer srv.Close()
	migrated := byPayment[created.ID].Endpoint
	if migrated == nil {
		t.Fatal("delivery has no endpoint")
	}
	err = e.tx.WithinTx(ctx, func(ctx context.Context) error {
		ep, err := e.endpoints.GetByPublicIDForUpdate(ctx, migrated.PublicID)
		if err != nil {
			return err
		}
		if !ep.Migrated || !ep.LegacySignature || ep.APIVersion != domain.WebhookAPIV1 || ep.SecretLast4 != nil {
			t.Errorf("migrated endpoint = %+v", ep)
		}
		ep.URL = srv.URL
		return e.endpoints.Update(ctx, ep)
	})
	if err != nil {
		t.Fatal(err)
	}
	fwd := worker.NewForwarder(worker.ForwarderDeps{Deliveries: e.deliveries, Endpoints: e.endpoints, Tx: e.tx, Cipher: e.cipher,
		Audit: e.audit, Enqueuer: &recordingQueue{}, DisableAfterFailures: 5})
	for _, d := range byPayment {
		task, _ := worker.NewWebhookForwardTask(d.ID, 1, false)
		if err := fwd.HandleWebhookForward(ctx, task); err != nil {
			t.Fatalf("forward %s: %v", d.PublicID, err)
		}
	}
	if strings.Join(got, ",") != "ok,ok" {
		t.Fatalf("forwards = %v", got)
	}
}

// Concurrent duplicates make one payment, and concurrent key creation never
// passes the limit.
func TestIntegrationConcurrency(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	codes := make([]int, n)
	ids := make([]string, n)
	body := `{"order_id":"race-1","amount":777}`
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := e.call(http.MethodPost, "/v1/payments", legacyKey, legacySecret, body, "Idempotency-Key", "race-key")
			codes[i] = w.Code
			var p struct{ ID string }
			_ = json.Unmarshal(w.Body.Bytes(), &p)
			ids[i] = p.ID
		}()
	}
	wg.Wait()
	var rows int
	res, err := e.payments.List(ctx, domain.PaymentFilter{}, domain.Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Items {
		if p.OrderID == "race-1" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("%d payment rows for one order", rows)
	}
	for i := range n {
		if codes[i] != http.StatusCreated || ids[i] != ids[0] {
			t.Fatalf("request %d: %d %s (first %s)", i, codes[i], ids[i], ids[0])
		}
	}

	// 8 usable keys, then 12 concurrent creates: exactly 2 succeed.
	actor := domain.WithActor(ctx, domain.SystemActor("test"))
	app, err := e.appSvc.Create(actor, service.CreateAppInput{Name: "Keys", Slug: "keys", Environment: domain.EnvLive})
	if err != nil {
		t.Fatal(err)
	}
	for range 7 {
		if _, err := e.appSvc.CreateKey(actor, app.App.PublicID, service.CreateKeyInput{Label: "pre"}); err != nil {
			t.Fatal(err)
		}
	}
	var ok, limited, other int
	var mu sync.Mutex
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.appSvc.CreateKey(actor, app.App.PublicID, service.CreateKeyInput{Label: "race"})
			var coded *domain.Error
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.As(err, &coded) && coded.Code == domain.CodeValidationFailed:
				limited++
			default:
				other++
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 2 || limited != 10 || other != 0 {
		t.Fatalf("ok=%d limited=%d other=%d", ok, limited, other)
	}
}
