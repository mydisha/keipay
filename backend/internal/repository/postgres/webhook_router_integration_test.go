package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
	"github.com/mydisha/keipay/backend/internal/worker"
)

const testCipherKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New(testCipherKey)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// queryCounter counts the queries a pool runs.
type queryCounter struct{ n atomic.Int64 }

func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.n.Add(1)
	return ctx
}

func (q *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func countingPool(t *testing.T, dsn string) (*pgxpool.Pool, *queryCounter) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, counter
}

// T-B6 / AC-3: the app row lock keeps concurrent creates within 10 endpoints.
func TestIntegrationEndpointLimitUnderConcurrency(t *testing.T) {
	pool := openPool(t, pgtest.New(t, 0).DSN)
	app := newTestApp(t, pool, domain.EnvTest)
	svc := service.NewWebhookEndpointService(service.WebhookEndpointDeps{
		Apps: postgres.NewAppRepo(pool), Endpoints: postgres.NewWebhookEndpointRepo(pool), Events: postgres.NewEventRepo(pool),
		Deliveries: postgres.NewDeliveryRepo(pool), Cipher: testCipher(t), Tx: postgres.NewTxManager(pool),
		Audit: service.NewAuditService(postgres.NewAuditRepo(pool)), Enqueuer: &stubEnqueuer{},
	})
	ctx := domain.WithActor(context.Background(), domain.SystemActor("test"))
	var wg sync.WaitGroup
	var ok, limited atomic.Int64
	for i := range 15 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Create(ctx, app.PublicID, service.CreateWebhookEndpointInput{
				URL: fmt.Sprintf("https://hooks.example.com/%d", i), Events: []string{"*"},
				Headers: []service.WebhookHeaderInput{{Name: "Authorization", Value: "Bearer header-value-12345"}},
			})
			var de *domain.Error
			switch {
			case err == nil:
				ok.Add(1)
			case errors.As(err, &de) && de.Code == domain.CodeValidationFailed:
				limited.Add(1)
			default:
				t.Errorf("create: %v", err)
			}
		}()
	}
	wg.Wait()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM webhook_endpoints WHERE app_id=$1`, app.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if ok.Load() != 10 || limited.Load() != 5 || n != 10 {
		t.Fatalf("ok=%d limited=%d rows=%d", ok.Load(), limited.Load(), n)
	}

	// The stored row round-trips: secret and headers encrypted, hints readable.
	page, err := svc.List(ctx, app.PublicID, domain.PageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("list = %+v %v", page, err)
	}
	e := page.Items[0]
	if strings.HasPrefix(e.SecretEnc, "kpwh_") || e.HeadersEnc == nil || strings.Contains(*e.HeadersEnc, "Bearer") ||
		len(e.HeadersHint) != 1 || *e.HeadersHint[0].ValueLast4 != "2345" || e.App.PublicID != app.PublicID {
		t.Fatalf("stored endpoint = %+v", e)
	}
	var auditRows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='webhook_endpoint.create' AND after::text LIKE '%Bearer%'`).Scan(&auditRows)
	if auditRows != 0 {
		t.Fatal("audit stores a header value")
	}
}

// Repository round trips the services rely on.
func TestIntegrationEndpointRepository(t *testing.T) {
	pool := openPool(t, pgtest.New(t, 0).DSN)
	ctx := context.Background()
	repo := postgres.NewWebhookEndpointRepo(pool)
	app := newTestApp(t, pool, domain.EnvTest)
	last4 := "abcd"
	e := &domain.WebhookEndpoint{PublicID: id.New(id.WebhookEndpoint), AppID: app.ID, URL: "https://x.example", Description: "Ünïcode\n",
		Events: []string{"payment.paid", "payment.*"}, APIVersion: domain.WebhookAPIV2, SecretEnc: "enc", SecretLast4: &last4,
		HeadersHint: []domain.WebhookHeaderHint{{Name: "A", ValueLast4: &last4}, {Name: "B"}}, Status: domain.WebhookEndpointActive,
		RetryPolicy: domain.RetryPolicy{MaxAttempts: 3, Backoff: domain.BackoffFixed, IntervalSeconds: 60}}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByPublicID(ctx, e.PublicID)
	if err != nil || got.Description != e.Description || !slices.Equal(got.Events, e.Events) || got.RetryPolicy != e.RetryPolicy ||
		len(got.HeadersHint) != 2 || got.HeadersHint[1].ValueLast4 != nil || got.App != app.Summary() || got.DisabledReason != nil {
		t.Fatalf("got = %+v %v", got, err)
	}
	// The table refuses a disabled endpoint without a reason.
	got.Status = domain.WebhookEndpointDisabled
	if err := repo.Update(ctx, got); err == nil {
		t.Fatal("disabled without reason accepted")
	}
	// UpdateHealth keeps updated_at while the status stays.
	before := got.UpdatedAt
	got.Status, got.ConsecutiveFailures = domain.WebhookEndpointActive, 2
	if err := repo.UpdateHealth(ctx, got); err != nil || !got.UpdatedAt.Equal(before) {
		t.Fatalf("health: %v %v -> %v", err, before, got.UpdatedAt)
	}
	reason, now := domain.WebhookDisabledConsecutiveFailures, time.Now()
	got.Status, got.DisabledReason, got.DisabledAt = domain.WebhookEndpointDisabled, &reason, &now
	if err := repo.UpdateHealth(ctx, got); err != nil || got.UpdatedAt.Equal(before) {
		t.Fatalf("status change kept updated_at: %v", err)
	}
	active, err := repo.ListActiveByApp(ctx, app.ID)
	if err != nil || len(active) != 0 {
		t.Fatalf("active = %+v %v", active, err)
	}
	got.DeletedAt = &now
	if err := repo.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByPublicID(ctx, e.PublicID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted endpoint readable: %v", err)
	}
	if deleted, err := repo.GetByIDForUpdate(ctx, e.ID); err != nil || deleted.DeletedAt == nil || deleted.ConsecutiveFailures != 2 {
		t.Fatalf("by id = %+v %v", deleted, err)
	}
	if n, _ := repo.CountByApp(ctx, app.ID); n != 0 {
		t.Fatalf("deleted endpoint counted: %d", n)
	}
}

// deliveryWorld is a fresh database with two apps (test, live), an endpoint
// each, an event each and deliveries at chosen times.
type deliveryWorld struct {
	pool                   *pgxpool.Pool
	deliveries             *postgres.DeliveryRepo
	testApp, liveApp       *domain.App
	testEndpoint, liveEndp *domain.WebhookEndpoint
	testEvent, liveEvent   *domain.Event
}

func newDeliveryWorld(t *testing.T, pool *pgxpool.Pool) *deliveryWorld {
	t.Helper()
	ctx := context.Background()
	w := &deliveryWorld{pool: pool, deliveries: postgres.NewDeliveryRepo(pool)}
	w.testApp, w.liveApp = newTestApp(t, pool, domain.EnvTest), newTestApp(t, pool, domain.EnvLive)
	endpoints, events := postgres.NewWebhookEndpointRepo(pool), postgres.NewEventRepo(pool)
	for _, app := range []*domain.App{w.testApp, w.liveApp} {
		e := &domain.WebhookEndpoint{PublicID: id.New(id.WebhookEndpoint), AppID: app.ID, URL: "https://x.example/" + app.Slug, Events: []string{"*"},
			APIVersion: domain.WebhookAPIV2, SecretEnc: "enc", Status: domain.WebhookEndpointActive, RetryPolicy: domain.DefaultRetryPolicy()}
		if err := endpoints.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
		ev := &domain.Event{PublicID: id.New(id.Event), Type: domain.EventPaymentPaid, APIVersion: "v2", AppID: app.ID,
			Data: json.RawMessage(`{}`), IsTest: true, CreatedAt: time.Now()}
		if err := events.Create(ctx, ev); err != nil {
			t.Fatal(err)
		}
		if app == w.testApp {
			w.testEndpoint, w.testEvent = e, ev
		} else {
			w.liveEndp, w.liveEvent = e, ev
		}
	}
	return w
}

func (w *deliveryWorld) add(t *testing.T, app *domain.App, e *domain.WebhookEndpoint, ev *domain.Event, eventType string,
	status domain.WebhookDeliveryStatus, at time.Time) *domain.WebhookDelivery {
	t.Helper()
	d := &domain.WebhookDelivery{PublicID: id.New(id.Delivery), AppID: app.ID, EventID: &ev.ID, EndpointID: &e.ID, EventType: eventType,
		Payload: []byte(`{"a":1}`), Status: status, MaxAttempts: 1}
	if err := w.deliveries.Create(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	exec(t, w.pool, `UPDATE webhook_deliveries SET created_at=$2 WHERE id=$1`, d.ID, at)
	return d
}

// T-B28 / AC-13: every filter, keyset paging across equal created_at, and one
// query per page.
func TestIntegrationDeliveryList(t *testing.T) {
	db := pgtest.New(t, 0)
	pool, counter := countingPool(t, db.DSN)
	w := newDeliveryWorld(t, pool)
	ctx := context.Background()
	same := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var equal []string
	for range 5 {
		equal = append(equal, w.add(t, w.testApp, w.testEndpoint, w.testEvent, "payment.paid", domain.WebhookDeliveryPending, same).PublicID)
	}
	older := w.add(t, w.liveApp, w.liveEndp, w.liveEvent, "payment.settled", domain.WebhookDeliveryFailed, same.Add(-time.Hour))

	var seen []string
	page := domain.Page{Limit: 2}
	for {
		counter.n.Store(0)
		res, err := w.deliveries.List(ctx, domain.DeliveryFilter{}, page)
		if err != nil {
			t.Fatal(err)
		}
		if n := counter.n.Load(); n != 1 {
			t.Fatalf("a page took %d queries", n)
		}
		for _, d := range res.Items {
			if d.Payload != nil || d.Endpoint == nil || d.Event == nil || d.App.PublicID == "" {
				t.Fatalf("row = %+v", d)
			}
			seen = append(seen, d.PublicID)
		}
		if res.NextCursor == "" {
			break
		}
		c, _ := domain.DecodeCursor(res.NextCursor)
		page.After = &c
	}
	slices.Sort(equal)
	slices.Reverse(equal)
	if want := append(equal, older.PublicID); !slices.Equal(seen, want) {
		t.Fatalf("pages = %v\nwant    %v", seen, want)
	}

	str := func(s string) *string { return &s }
	failed, settled, live := domain.WebhookDeliveryFailed, domain.EventPaymentSettled, domain.EnvLive
	from, to := same.Add(-30*time.Minute), same.Add(-30*time.Minute)
	for name, tc := range map[string]struct {
		f    domain.DeliveryFilter
		want int
	}{
		"app":            {domain.DeliveryFilter{AppPublicID: &w.liveApp.PublicID}, 1},
		"endpoint":       {domain.DeliveryFilter{EndpointPublicID: &w.testEndpoint.PublicID}, 5},
		"event":          {domain.DeliveryFilter{EventPublicID: &w.liveEvent.PublicID}, 1},
		"event type":     {domain.DeliveryFilter{EventType: &settled}, 1},
		"status":         {domain.DeliveryFilter{Status: &failed}, 1},
		"environment":    {domain.DeliveryFilter{Environment: &live}, 1},
		"from":           {domain.DeliveryFilter{From: &from}, 5},
		"to":             {domain.DeliveryFilter{To: &to}, 1},
		"unknown app":    {domain.DeliveryFilter{AppPublicID: str(id.New(id.App))}, 0},
		"unknown event":  {domain.DeliveryFilter{EventPublicID: str(id.New(id.Event))}, 0},
		"combined (AND)": {domain.DeliveryFilter{AppPublicID: &w.testApp.PublicID, Status: &failed}, 0},
	} {
		res, err := w.deliveries.List(ctx, tc.f, domain.Page{Limit: 50})
		if err != nil || len(res.Items) != tc.want {
			t.Fatalf("%s: %d rows, want %d (%v)", name, len(res.Items), tc.want, err)
		}
	}
}

// T-B29 / AC-17: inbound list filters and pages; the detail returns the body
// byte for byte, invalid UTF-8 and NUL included.
func TestIntegrationInboundList(t *testing.T) {
	pool := openPool(t, pgtest.New(t, 0).DSN)
	ctx := context.Background()
	repo := postgres.NewInboundWebhookRepo(pool)
	testConn, liveConn := newTestConnection(t, pool, domain.EnvTest), newTestConnection(t, pool, domain.EnvLive)
	body := []byte("{\"a\":\"\xff\x00\"}")
	var ids []string
	for i, c := range []*domain.Connection{testConn, testConn, testConn, liveConn} {
		result := domain.InboundApplied
		if i == 3 {
			result = domain.InboundInvalidSignature
		}
		key, status := fmt.Sprintf("inv_%d", i), "PAID"
		w := &domain.InboundWebhook{PublicID: id.New(id.Inbound), ConnectionID: c.ID, Provider: "xendit", Source: "webhook",
			SignatureValid: i != 3, Result: result, EventKey: &key, ProviderStatus: &status, RawBody: body, BodyTruncated: i == 3}
		if err := repo.Create(ctx, w); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, w.PublicID)
	}
	exec(t, pool, `UPDATE inbound_webhooks SET received_at = '2026-01-01T00:00:00Z' WHERE public_id = ANY($1)`, ids[:3])

	var seen []string
	page := domain.Page{Limit: 2}
	for {
		res, err := repo.List(ctx, domain.InboundFilter{}, page)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range res.Items {
			if len(w.RawBody) != 0 || w.Connection.PublicID == "" {
				t.Fatalf("row = %+v", w)
			}
			seen = append(seen, w.PublicID)
		}
		if res.NextCursor == "" {
			break
		}
		c, _ := domain.DecodeCursor(res.NextCursor)
		page.After = &c
	}
	if len(seen) != 4 || seen[0] != ids[3] {
		t.Fatalf("pages = %v", seen)
	}

	applied, live := domain.InboundApplied, domain.EnvLive
	cut := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		f    domain.InboundFilter
		want int
	}{
		"connection":  {domain.InboundFilter{ConnectionPublicID: &testConn.PublicID}, 3},
		"result":      {domain.InboundFilter{Result: &applied}, 3},
		"environment": {domain.InboundFilter{Environment: &live}, 1},
		"from":        {domain.InboundFilter{From: &cut}, 1},
		"to":          {domain.InboundFilter{To: &cut}, 3},
	} {
		res, err := repo.List(ctx, tc.f, domain.Page{Limit: 50})
		if err != nil || len(res.Items) != tc.want {
			t.Fatalf("%s: %d rows, want %d (%v)", name, len(res.Items), tc.want, err)
		}
	}
	got, err := repo.GetByPublicID(ctx, ids[3])
	if err != nil || string(got.RawBody) != string(body) || !got.BodyTruncated || got.SignatureValid || got.Connection.Environment != domain.EnvLive {
		t.Fatalf("detail = %+v %v", got, err)
	}
}

// T-B22 / AC-9: two workers running the same attempt record it once.
func TestIntegrationForwarderOptimisticGuard(t *testing.T) {
	pool := openPool(t, pgtest.New(t, 0).DSN)
	ctx := context.Background()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cipher := testCipher(t)
	enc, _ := cipher.Encrypt("kpwh_secret")
	app := newTestApp(t, pool, domain.EnvTest)
	endpoints, deliveries := postgres.NewWebhookEndpointRepo(pool), postgres.NewDeliveryRepo(pool)
	e := &domain.WebhookEndpoint{PublicID: id.New(id.WebhookEndpoint), AppID: app.ID, URL: srv.URL, Events: []string{"*"},
		APIVersion: domain.WebhookAPIV2, SecretEnc: enc, Status: domain.WebhookEndpointActive, RetryPolicy: domain.DefaultRetryPolicy()}
	if err := endpoints.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d := &domain.WebhookDelivery{PublicID: id.New(id.Delivery), AppID: app.ID, EndpointID: &e.ID, EventType: "payment.paid",
		Payload: []byte(`{"a":1}`), Status: domain.WebhookDeliveryPending, MaxAttempts: 8, NextAttemptAt: &now}
	if err := deliveries.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	fwd := worker.NewForwarder(worker.ForwarderDeps{Deliveries: deliveries, Endpoints: endpoints, Tx: postgres.NewTxManager(pool),
		Cipher: cipher, Audit: service.NewAuditService(postgres.NewAuditRepo(pool)), Enqueuer: &stubEnqueuer{}, Log: zap.NewNop()})
	task, _ := worker.NewWebhookForwardTask(d.ID, 1, false)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fwd.HandleWebhookForward(ctx, task); err != nil {
				t.Errorf("forward: %v", err)
			}
		}()
	}
	wg.Wait()
	atts, err := deliveries.Attempts(ctx, d.ID)
	got, _ := deliveries.GetByPublicID(ctx, d.PublicID)
	if err != nil || len(atts) != 1 || got.Attempts != 1 || got.Status != domain.WebhookDeliveryDelivered || hits.Load() != 2 {
		t.Fatalf("attempts=%d delivery=%+v hits=%d %v", len(atts), got, hits.Load(), err)
	}
	if atts[0].RequestHeaders["X-KeiPay-Delivery-Id"] != d.PublicID || atts[0].RequestHeaders["User-Agent"] != "KeiPay-Webhooks/1" {
		t.Fatalf("logged headers = %v", atts[0].RequestHeaders)
	}
}

// T-B31 / T-B32 / AC-8: apps with a webhook_url get one migrated endpoint
// with their secret; existing deliveries get public ids, the endpoint and
// attempt budgets; an old-format task is still processed and signed as
// before; up/down/up works.
func TestIntegrationWebhookRouterMigration(t *testing.T) {
	db := pgtest.New(t, 8)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	cipher := testCipher(t)
	const oldSecret = "sk_legacy_secret_value"
	var got []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-Kei-Signature") == hmac.Sign(oldSecret, b) && r.Header.Get("X-Kei-Event") == "payment.paid" {
			got = append(got, "ok")
		} else {
			got = append(got, "bad")
		}
	}))
	defer srv.Close()
	enc, _ := cipher.Encrypt(oldSecret)
	created := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	exec(t, pool, `INSERT INTO apps (id, public_id, slug, name, environment, webhook_url, webhook_secret_enc, merchant_id, created_at)
		VALUES (1, 'app_01JBKQ7Z8M4X2Y6V3N9T5R1W0A', 'acme', 'Acme', 'live', $1, $2, (SELECT id FROM merchants WHERE is_default), $3),
		       (2, 'app_01JBKQ7Z8M4X2Y6V3N9T5R1W0B', 'globex', 'Globex', 'live', '', 'x', (SELECT id FROM merchants WHERE is_default), $3)`,
		srv.URL, enc, created)
	exec(t, pool, `INSERT INTO payments (id, public_id, app_id, order_id, external_id, amount)
		VALUES (1, 'pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0A', 1, 'o1', 'acme:o1', 1000), (2, 'pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0B', 2, 'o2', 'globex:o2', 1000)`)
	exec(t, pool, `INSERT INTO webhook_deliveries (id, payment_id, app_id, event_type, payload, status, attempts) VALUES
		(1, 1, 1, 'payment.paid', '{"event":"payment.paid","payment_id":1}', 'PENDING', 2),
		(2, 1, 1, 'payment.paid', '{}', 'DELIVERED', 3),
		(3, 1, 1, 'payment.paid', '{}', 'FAILED', 0),
		(4, 2, 2, 'payment.paid', '{}', 'PENDING', 0)`)
	exec(t, pool, `SELECT setval(pg_get_serial_sequence('webhook_deliveries', 'id'), 4)`)
	if err := db.Migrate.Migrate(9); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	endpoints := postgres.NewWebhookEndpointRepo(pool)
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM webhook_endpoints`).Scan(&n)
	acmeEndpoints, err := endpoints.List(ctx, 1, domain.Page{Limit: 10})
	if err != nil || n != 1 || len(acmeEndpoints.Items) != 1 {
		t.Fatalf("endpoints = %d %+v %v", n, acmeEndpoints, err)
	}
	e := acmeEndpoints.Items[0]
	if !id.Valid(e.PublicID, id.WebhookEndpoint) || e.URL != srv.URL || !slices.Equal(e.Events, []string{"*"}) || e.APIVersion != domain.WebhookAPIV1 ||
		!e.LegacySignature || !e.Migrated || e.SecretEnc != enc || e.SecretLast4 != nil || e.Status != domain.WebhookEndpointActive ||
		!e.CreatedAt.Equal(created) || e.RetryPolicy != domain.DefaultRetryPolicy() || len(e.HeadersHint) != 0 {
		t.Fatalf("migrated endpoint = %+v", e)
	}
	deliveries := postgres.NewDeliveryRepo(pool)
	type row struct {
		publicID    string
		endpointID  *int64
		maxAttempts int
		next        *time.Time
	}
	rows := map[int64]row{}
	r, err := pool.Query(ctx, `SELECT id, public_id, endpoint_id, max_attempts, next_attempt_at FROM webhook_deliveries`)
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		var idv int64
		var v row
		if err := r.Scan(&idv, &v.publicID, &v.endpointID, &v.maxAttempts, &v.next); err != nil {
			t.Fatal(err)
		}
		rows[idv] = v
	}
	r.Close()
	for idv, want := range map[int64]struct {
		endpoint    bool
		maxAttempts int
		next        bool
	}{1: {true, 8, true}, 2: {true, 3, false}, 3: {true, 1, false}, 4: {false, 6, true}} {
		v := rows[idv]
		if !id.Valid(v.publicID, id.Delivery) || (v.endpointID != nil) != want.endpoint || v.maxAttempts != want.maxAttempts || (v.next != nil) != want.next {
			t.Fatalf("delivery %d = %+v", idv, v)
		}
		if want.endpoint && *v.endpointID != e.ID {
			t.Fatalf("delivery %d endpoint = %d", idv, *v.endpointID)
		}
	}

	// The pending legacy delivery is sent by a task in the old format.
	fwd := worker.NewForwarder(worker.ForwarderDeps{Deliveries: deliveries, Endpoints: endpoints, Tx: postgres.NewTxManager(pool),
		Cipher: cipher, Audit: service.NewAuditService(postgres.NewAuditRepo(pool)), Enqueuer: &stubEnqueuer{}})
	if err := fwd.HandleWebhookForward(ctx, asynq.NewTask(worker.TypeWebhookForward, []byte(`{"delivery_id":1}`))); err != nil {
		t.Fatal(err)
	}
	if err := fwd.HandleWebhookForward(ctx, asynq.NewTask(worker.TypeWebhookForward, []byte(`{"delivery_id":4}`))); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "ok" {
		t.Fatalf("receiver saw %v", got)
	}
	d1, _ := deliveries.GetByPublicID(ctx, rows[1].publicID)
	d4, _ := deliveries.GetByPublicID(ctx, rows[4].publicID)
	if d1.Status != domain.WebhookDeliveryDelivered || d1.Attempts != 3 || d4.Status != domain.WebhookDeliveryFailed || d4.LastError != "no endpoint" {
		t.Fatalf("after forward: %+v / %+v", d1, d4)
	}

	// A test send (no payment) is dropped by down; up again recreates the
	// endpoint from the untouched webhook_url.
	exec(t, pool, `INSERT INTO webhook_deliveries (public_id, app_id, event_type, payload, status) VALUES ($1, 1, 'payment.paid', '{}', 'PENDING')`,
		id.New(id.Delivery))
	if err := db.Migrate.Steps(-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	var tables int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name IN
		('webhook_endpoints','events','webhook_delivery_attempts','inbound_webhooks')`).Scan(&tables)
	var cols, deliveriesLeft int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='webhook_deliveries'
		AND column_name IN ('public_id','event_id','endpoint_id','max_attempts','next_attempt_at','last_attempt_at','last_response_status')`).Scan(&cols)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&deliveriesLeft)
	if tables != 0 || cols != 0 || deliveriesLeft != 4 {
		t.Fatalf("down left tables=%d columns=%d deliveries=%d", tables, cols, deliveriesLeft)
	}
	if err := db.Migrate.Up(); err != nil {
		t.Fatalf("up again: %v", err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM webhook_endpoints WHERE migrated`).Scan(&n)
	if n != 1 {
		t.Fatalf("migrated endpoints after up/down/up = %d", n)
	}
}
