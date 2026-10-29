package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

type plainCipher struct{}

func (plainCipher) Encrypt(s string) (string, error) { return "enc:" + s, nil }
func (plainCipher) Decrypt(s string) (string, error) { return strings.TrimPrefix(s, "enc:"), nil }

type memNonces struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (m *memNonces) Acquire(_ context.Context, key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen[key] {
		return false
	}
	m.seen[key] = true
	return true
}

type queued struct {
	id      int64
	attempt int
	manual  bool
}

// recordingQueue records the delivery attempts and reconciliation runs the
// services enqueue; set err to make the queue unavailable.
type recordingQueue struct {
	mu    sync.Mutex
	calls []queued
	runs  []queued
	err   error
}

func (q *recordingQueue) EnqueueReconciliation(_ context.Context, runID int64, runCount int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.runs = append(q.runs, queued{id: runID, attempt: runCount})
	return nil
}

func (q *recordingQueue) EnqueueDelivery(_ context.Context, id int64, attempt int, manual bool, _ time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.calls = append(q.calls, queued{id: id, attempt: attempt, manual: manual})
	return nil
}

// auditRepo records entries and serves a canned page to List.
type auditRepo struct {
	logs []domain.AuditLog
	page domain.Page
	res  domain.PageResult[domain.AuditLog]
	err  error
}

func (a *auditRepo) Create(_ context.Context, l *domain.AuditLog) error {
	if a.err != nil {
		return a.err
	}
	a.logs = append(a.logs, *l)
	return nil
}
func (a *auditRepo) List(_ context.Context, p domain.Page) (domain.PageResult[domain.AuditLog], error) {
	a.page = p
	return a.res, nil
}

func (a *auditRepo) actions() []string {
	out := []string{}
	for _, l := range a.logs {
		out = append(out, l.Action)
	}
	return out
}

type directTx struct{}

func (directTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// testPassword is the password of every user the fixture creates.
const testPassword = "correct-horse-1"

type apiFixture struct {
	router   *gin.Engine
	deps     Deps
	users    *memstore.Users
	sessions *memstore.Sessions
	failures *memstore.Failures
	audit    *auditRepo
	auth     *service.AuthService
	logs     *observer.ObservedLogs
	owner    *domain.User
	apps     *memstore.Apps
	keys     *memstore.APIKeys
	payments *memstore.Payments
	conns    *memstore.Connections
	appSvc   *service.AppService
	connSvc  *service.ConnectionService
	// fake is the provider every app's default connection uses.
	fake *providertest.Fake
	// testConn and liveConn are the fake connections createApp makes the
	// default of new test and live apps.
	testConn, liveConn *domain.Connection
	// xenditAPI stands in for the Xendit API the xendit adapter calls.
	xenditAPI *fakeXenditAPI
	hooks     *memstore.Webhooks
	reports   *memstore.Reports
	recon     *memstore.Reconciliations
	reconSvc  *service.ReconciliationService
	queue     *recordingQueue
}

// apiOption adjusts the fixture before the router is built.
type apiOption func(cfg *service.AuthConfig, d *Deps)

func newAPI(t *testing.T, frontendDir string, opts ...apiOption) *apiFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.DebugLevel)
	log := zap.New(core)
	f := &apiFixture{
		users:    memstore.NewUsers(time.Now),
		sessions: memstore.NewSessions(time.Now),
		failures: memstore.NewFailures(time.Now),
		audit:    &auditRepo{},
		logs:     logs,
		apps:     memstore.NewApps(time.Now),
		fake:     providertest.New(),
	}
	f.keys = memstore.NewAPIKeys(f.apps, time.Now)
	f.payments = memstore.NewPayments(f.apps, time.Now)
	f.conns = memstore.NewConnections(f.apps, time.Now)
	f.xenditAPI = newFakeXenditAPI(t)
	cfg := service.AuthConfig{
		IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 12 * time.Hour,
		MaxFailuresPerIP: 20, MaxFailuresPerEmail: 5, FailureWindow: 15 * time.Minute,
	}
	auditSvc := service.NewAuditService(f.audit)
	f.deps = Deps{Log: log, FrontendDir: frontendDir}
	for _, opt := range opts {
		opt(&cfg, &f.deps)
	}
	hasher := service.NewBcryptHasher(bcrypt.MinCost)
	auth, err := service.NewAuthService(service.AuthDeps{
		Users: f.users, Sessions: f.sessions, Failures: f.failures, Tx: directTx{},
		Audit: auditSvc, Hasher: hasher, Log: log,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.auth = auth
	f.deps.Sessions = auth
	f.deps.Auth = handler.NewAuthHandler(auth, f.deps.Cookies, service.NewToken)
	f.deps.Users = handler.NewUserHandler(service.NewUserService(f.users, directTx{}, auditSvc, hasher))
	registry, err := provider.NewRegistry(f.fake, xendit.New(xendit.WithBaseURL(f.xenditAPI.URL)))
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := crypto.New(testMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	f.hooks = memstore.NewWebhooks(f.apps, f.payments, f.conns, time.Now)
	f.reports = memstore.NewReports()
	f.recon = memstore.NewReconciliations(f.conns, f.payments, f.reports, f.users, time.Now)
	f.queue = &recordingQueue{}
	tx := memstore.NewTx(f.apps, f.keys, f.conns, f.payments, f.hooks, f.reports, f.recon)
	f.appSvc = service.NewAppService(f.apps, f.keys, f.conns, plainCipher{}, tx, auditSvc)
	f.connSvc = service.NewConnectionService(service.ConnectionDeps{
		Connections: f.conns, Merchants: f.conns.Merchants(), Apps: f.apps, Payments: f.payments, Registry: registry,
		Sealer: sealer, Tx: tx, Audit: auditSvc, Log: log, BaseURL: "https://pay.example.com",
	})
	paymentSvc := service.NewPaymentService(f.payments, f.conns, registry, sealer, log)
	webhookSvc := service.NewWebhookService(service.WebhookDeps{
		Payments: f.payments, Reports: f.reports, Inbound: f.hooks.Inbound(), Events: f.hooks.Events(),
		Endpoints: f.hooks.Endpoints(), Deliveries: f.hooks.Deliveries(), Tx: tx, Enqueuer: f.queue, Log: log,
	})
	endpointSvc := service.NewWebhookEndpointService(service.WebhookEndpointDeps{
		Apps: f.apps, Endpoints: f.hooks.Endpoints(), Events: f.hooks.Events(), Deliveries: f.hooks.Deliveries(),
		Cipher: plainCipher{}, Tx: tx, Audit: auditSvc, Enqueuer: f.queue, Log: log,
	})
	deliverySvc := service.NewDeliveryService(service.DeliveryDeps{
		Deliveries: f.hooks.Deliveries(), Endpoints: f.hooks.Endpoints(), Tx: tx, Enqueuer: f.queue, Audit: auditSvc, Log: log,
	})
	f.deps.Apps = handler.NewAppHandler(f.appSvc)
	f.deps.Endpoints = handler.NewWebhookEndpointHandler(endpointSvc)
	f.deps.Deliveries = handler.NewDeliveryHandler(deliverySvc, service.NewInboundService(f.hooks.Inbound()))
	f.deps.Connections = handler.NewConnectionHandler(f.connSvc)
	f.deps.Webhook = handler.NewWebhookHandler(webhookSvc, f.connSvc, log)
	f.deps.Payment = handler.NewPaymentHandler(paymentSvc)
	f.deps.AppResolver = f.appSvc
	f.deps.NonceStore = &memNonces{seen: map[string]bool{}}
	f.deps.SecretDecryptor = plainCipher{}
	f.deps.Admin = handler.NewAdminHandler(paymentSvc, auditSvc)
	f.deps.Reports = handler.NewReportHandler(service.NewPaymentReportService(f.reports), log)
	f.reconSvc = service.NewReconciliationService(service.ReconciliationDeps{
		Runs: f.recon, Connections: f.conns, Registry: registry, Source: service.NewProviderSource(registry, sealer),
		Tx: tx, Enqueuer: f.queue, Audit: auditSvc, Log: log,
	})
	f.deps.Reconciliation = handler.NewReconciliationHandler(f.reconSvc, log)
	f.router = NewRouter(f.deps)
	f.owner = f.addUser(t, "owner@example.com", domain.RoleOwner)
	ctx := domain.WithActor(context.Background(), domain.SystemActor("test"))
	for _, env := range []domain.Environment{domain.EnvTest, domain.EnvLive} {
		d, err := f.connSvc.Create(ctx, service.CreateConnectionInput{Provider: "fake", Name: "fake-" + string(env), Environment: env,
			Credentials: map[string]any{"api_key": "fake-api-key-" + string(env) + "-0001", "token": "fake-token-" + string(env)}})
		if err != nil {
			t.Fatal(err)
		}
		if env == domain.EnvTest {
			f.testConn = d.Connection
		} else {
			f.liveConn = d.Connection
		}
	}
	f.audit.logs = nil
	return f
}

// useDefaultConnection makes the fixture's connection of the app's
// environment its default, without an audit entry.
func (f *apiFixture) useDefaultConnection(t *testing.T, appID string) {
	t.Helper()
	ctx := context.Background()
	a, err := f.apps.GetByPublicID(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	conn := f.testConn
	if a.Environment == domain.EnvLive {
		conn = f.liveConn
	}
	a.DefaultConnectionID = &conn.ID
	if err := f.apps.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
}

// rebuild builds the router again after a test changed f.deps.
func (f *apiFixture) rebuild() { f.router = NewRouter(f.deps) }

func (f *apiFixture) addUser(t *testing.T, email string, role domain.Role) *domain.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	u := &domain.User{PublicID: id.New(id.User), Email: email, Name: "Name of " + email, Role: role, PasswordHash: string(hash)}
	f.users.Add(u)
	return u
}

// browser keeps cookies between requests like a real one and sends the CSRF
// header from its keipay_csrf cookie on unsafe methods.
type browser struct {
	cookies map[string]string
	remote  string
}

func newBrowser() *browser {
	return &browser{cookies: map[string]string{middleware.CSRFCookie: "boot-csrf-token"}, remote: "192.0.2.10:5000"}
}

func (b *browser) absorb(w *httptest.ResponseRecorder) {
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
			continue
		}
		b.cookies[c.Name] = c.Value
	}
}

func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// send makes a request as b (nil sends no cookies) with extra header pairs.
func (f *apiFixture) send(b *browser, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if b != nil {
		for k, v := range b.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		if v, ok := b.cookies[middleware.CSRFCookie]; ok && unsafeMethod(method) {
			req.Header.Set(middleware.CSRFHeader, v)
		}
		req.RemoteAddr = b.remote
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if b != nil {
		b.absorb(w)
	}
	return w
}

// signIn logs u in through the API and returns the signed-in browser.
func (f *apiFixture) signIn(t *testing.T, u *domain.User) *browser {
	t.Helper()
	b := newBrowser()
	w := f.send(b, http.MethodPost, "/admin/login", `{"email":"`+u.Email+`","password":"`+testPassword+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("sign in %s: %d %s", u.Email, w.Code, w.Body.String())
	}
	return b
}

// as creates a user with role and signs them in.
func (f *apiFixture) as(t *testing.T, role domain.Role) (*domain.User, *browser) {
	t.Helper()
	u := f.addUser(t, string(role)+"-"+strings.ToLower(id.New(id.User)[14:])+"@example.com", role)
	return u, f.signIn(t, u)
}

type errorEnvelope struct {
	Error struct {
		Code    domain.ErrorCode `json:"code"`
		Message string           `json:"message"`
		Details map[string]any   `json:"details"`
	} `json:"error"`
}

func expectError(t *testing.T, w *httptest.ResponseRecorder, status int, code domain.ErrorCode) errorEnvelope {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, status, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	var env errorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if env.Error.Code != code || env.Error.Details == nil {
		t.Fatalf("error = %+v, want %s with object details", env.Error, code)
	}
	return env
}

func expectFields(t *testing.T, env errorEnvelope, want ...domain.FieldError) {
	t.Helper()
	fields, _ := env.Error.Details["fields"].([]any)
	if len(fields) != len(want) {
		t.Fatalf("fields = %v, want %v", env.Error.Details["fields"], want)
	}
	for i, w := range want {
		f := fields[i].(map[string]any)
		if f["field"] != w.Field || f["rule"] != w.Rule {
			t.Fatalf("fields = %v, want %v", fields, want)
		}
	}
}

func TestEmptyListsEncodeAsArrays(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	cases := map[string][]string{
		"/admin/apps":                {`"data":[]`, `"next_cursor":null`},
		"/admin/payments":            {`"data":[]`, `"next_cursor":null`},
		"/admin/payment-reports":     {`"data":[]`, `"next_cursor":null`},
		"/admin/reconciliation-runs": {`"data":[]`, `"next_cursor":null`},
		"/admin/webhook-deliveries":  {`"data":[]`, `"next_cursor":null`},
		"/admin/inbound-webhooks":    {`"data":[]`, `"next_cursor":null`},
		"/admin/audit-logs":          {`"data":[]`, `"next_cursor":null`},
	}
	for path, wants := range cases {
		w := f.send(owner, http.MethodGet, path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", path, w.Code, w.Body.String())
		}
		for _, want := range wants {
			if !strings.Contains(w.Body.String(), want) {
				t.Fatalf("%s: body %s does not contain %s", path, w.Body.String(), want)
			}
		}
	}
}

func TestAuditLogsShapeAndPaging(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	pub := id.New(id.Audit)
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	f.audit.res = domain.PageResult[domain.AuditLog]{
		Items: []domain.AuditLog{{
			ID: 987654, PublicID: pub, OccurredAt: at, Action: "app.update",
			Actor:  domain.Actor{Type: "admin", ID: f.owner.PublicID, IP: "203.0.113.7", UserAgent: "ua", RequestID: "rid"},
			Target: domain.AuditTarget{Type: "app", ID: "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"},
			Before: json.RawMessage(`{"name":"Old"}`),
		}},
		NextCursor: domain.EncodeCursor(domain.Cursor{Time: at, ID: pub}),
	}

	w := f.send(owner, http.MethodGet, "/admin/audit-logs?limit=1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	if f.audit.page.Limit != 1 || f.audit.page.After != nil {
		t.Fatalf("page = %+v", f.audit.page)
	}
	var body struct {
		Data       []map[string]any `json:"data"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.NextCursor == nil {
		t.Fatalf("body = %s", w.Body.String())
	}
	entry := body.Data[0]
	wantKeys := []string{"id", "occurred_at", "actor", "action", "target", "before", "after", "ip", "user_agent", "request_id"}
	if len(entry) != len(wantKeys) {
		t.Fatalf("entry keys = %v, want %v", entry, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := entry[k]; !ok {
			t.Fatalf("entry missing %q: %v", k, entry)
		}
	}
	if !regexp.MustCompile(`^aud_[0-9A-HJKMNP-TV-Z]{26}$`).MatchString(entry["id"].(string)) {
		t.Fatalf("id = %v", entry["id"])
	}
	if strings.Contains(w.Body.String(), "987654") {
		t.Fatal("internal id exposed")
	}
	if entry["after"] != nil || entry["occurred_at"] != "2026-01-02T03:04:05.123456Z" {
		t.Fatalf("after/occurred_at = %v / %v", entry["after"], entry["occurred_at"])
	}
	if a := entry["actor"].(map[string]any); a["type"] != "admin" || a["id"] != f.owner.PublicID || len(a) != 2 {
		t.Fatalf("actor = %v", a)
	}

	w = f.send(owner, http.MethodGet, "/admin/audit-logs?cursor="+*body.NextCursor, "")
	if w.Code != http.StatusOK || f.audit.page.After == nil || f.audit.page.After.ID != pub || f.audit.page.Limit != 50 {
		t.Fatalf("cursor page: %d page=%+v", w.Code, f.audit.page)
	}
}

func TestAuditLogsQueryErrors(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	for _, q := range []string{"limit=0", "limit=201", "limit=-1", "limit=abc"} {
		env := expectError(t, f.send(owner, http.MethodGet, "/admin/audit-logs?"+q, ""), 400, domain.CodeInvalidRequest)
		expectFields(t, env, domain.FieldError{Field: "limit", Rule: "range"})
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/audit-logs?cursor=nope", ""), 400, domain.CodeInvalidCursor)
	expectError(t, f.send(nil, http.MethodGet, "/admin/audit-logs", ""), 401, domain.CodeSessionRequired)
}

func TestUnknownAPIRoutesReturnNotFoundEnvelope(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>console</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, frontendDir := range []string{"", dist} {
		f := newAPI(t, frontendDir)
		owner := f.signIn(t, f.owner)
		cases := []struct{ method, path string }{
			{http.MethodGet, "/admin/x"},
			{http.MethodGet, "/v1/x"},
			{http.MethodPost, "/webhooks/x"},
			{http.MethodGet, "/healthz/x"},
			// Wrong method on a known path is a 404, not a 405.
			{http.MethodDelete, "/admin/apps"},
			// Tenants were replaced by apps.
			{http.MethodGet, "/admin/tenants"},
			{http.MethodPost, "/admin/tenants"},
			{http.MethodPut, "/admin/tenants/1"},
			{http.MethodPost, "/admin/tenants/1/rotate"},
			{http.MethodGet, "/admin/login"},
			// The bearer-era endpoint is gone.
			{http.MethodPost, "/admin/change-password"},
			// Non-GET to a console path.
			{http.MethodPost, "/apps"},
		}
		for _, tc := range cases {
			w := f.send(owner, tc.method, tc.path, "")
			expectError(t, w, http.StatusNotFound, domain.CodeNotFound)
		}
	}
	f := newAPI(t, dist)
	if w := f.send(nil, http.MethodGet, "/settings", ""); w.Code != http.StatusOK || w.Body.String() != "<html>console</html>" {
		t.Fatalf("console path: %d %q", w.Code, w.Body.String())
	}
	f = newAPI(t, "")
	expectError(t, f.send(nil, http.MethodGet, "/settings", ""), http.StatusNotFound, domain.CodeNotFound)
}
