package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
)

// --- fakes: every list is empty unless a test fills it ---

type emptyTenants struct{ domain.TenantRepository }

func (emptyTenants) List(context.Context) ([]domain.Tenant, error) { return nil, nil }

type emptyPayments struct{ domain.PaymentRepository }

func (emptyPayments) List(context.Context, domain.PaymentFilter) ([]domain.Payment, int64, error) {
	return nil, 0, nil
}

type emptyLedger struct{ domain.LedgerRepository }

func (emptyLedger) List(context.Context, domain.LedgerFilter) ([]domain.LedgerEntry, error) {
	return nil, nil
}
func (emptyLedger) Summary(context.Context, domain.LedgerFilter) ([]domain.LedgerSummary, error) {
	return nil, nil
}

type deliveries struct {
	domain.WebhookRepository
	byID map[int64]domain.WebhookDelivery
}

func (deliveries) ListDeliveries(context.Context, *int64, *domain.WebhookDeliveryStatus, int, int) ([]domain.WebhookDelivery, error) {
	return nil, nil
}
func (d deliveries) GetDelivery(_ context.Context, id int64) (*domain.WebhookDelivery, error) {
	if v, ok := d.byID[id]; ok {
		return &v, nil
	}
	return nil, domain.ErrNotFound
}

type replayer struct{ err error }

func (r *replayer) ReplayWebhookForward(context.Context, int64) error { return r.err }

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
	replay   *replayer
	auth     *service.AuthService
	logs     *observer.ObservedLogs
	owner    *domain.User
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
		replay:   &replayer{},
		logs:     logs,
	}
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
	f.deps.Admin = handler.NewAdminHandler(
		service.NewTenantService(emptyTenants{}, nil, nil, directTx{}, auditSvc),
		service.NewPaymentService(emptyPayments{}, nil),
		service.NewLedgerService(emptyLedger{}),
		service.NewDeliveryService(deliveries{byID: map[int64]domain.WebhookDelivery{3: {ID: 3, Status: domain.WebhookDeliveryFailed, Attempts: 2}}},
			f.replay, auditSvc),
		auditSvc,
	)
	f.router = NewRouter(f.deps)
	f.owner = f.addUser(t, "owner@example.com", domain.RoleOwner)
	return f
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

// --- empty lists ---

func TestEmptyListsEncodeAsArrays(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	cases := map[string][]string{
		"/admin/tenants":            {`"tenants":[]`},
		"/admin/payments":           {`"payments":[]`},
		"/admin/ledger":             {`"entries":[]`, `"summary":[]`},
		"/admin/webhook-deliveries": {`"deliveries":[]`},
		"/admin/audit-logs":         {`"data":[]`, `"next_cursor":null`},
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

// --- audit log list ---

func TestAuditLogsShapeAndPaging(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	pub := id.New(id.Audit)
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	f.audit.res = domain.PageResult[domain.AuditLog]{
		Items: []domain.AuditLog{{
			ID: 987654, PublicID: pub, OccurredAt: at, Action: "tenant.update",
			Actor:  domain.Actor{Type: "admin", ID: f.owner.PublicID, IP: "203.0.113.7", UserAgent: "ua", RequestID: "rid"},
			Target: domain.AuditTarget{Type: "tenant", ID: "1"},
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

// --- replay and path ids ---

func TestReplay(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	f.audit.logs = nil
	expectError(t, f.send(owner, http.MethodPost, "/admin/webhook-deliveries/abc/replay", ""), 400, domain.CodeInvalidRequest)
	expectError(t, f.send(owner, http.MethodPost, "/admin/webhook-deliveries/99/replay", ""), 404, domain.CodeNotFound)

	f.replay.err = errors.New("redis down")
	expectError(t, f.send(owner, http.MethodPost, "/admin/webhook-deliveries/3/replay", ""), 500, domain.CodeInternal)
	if len(f.audit.logs) != 0 {
		t.Fatal("failed replay recorded an entry")
	}

	f.replay.err = nil
	w := f.send(owner, http.MethodPost, "/admin/webhook-deliveries/3/replay", "")
	if w.Code != http.StatusAccepted || w.Body.String() != `{"requeued":true}` {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}
	if len(f.audit.logs) != 1 || f.audit.logs[0].Action != "webhook_delivery.replay" || f.audit.logs[0].Actor.RequestID == "" ||
		f.audit.logs[0].Actor.ID != f.owner.PublicID {
		t.Fatalf("entries = %+v", f.audit.logs)
	}
}

func TestInvalidTenantPathID(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	env := expectError(t, f.send(owner, http.MethodPut, "/admin/tenants/x", `{"name":"a"}`), 400, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "id", Rule: "int"})
	expectError(t, f.send(owner, http.MethodPost, "/admin/tenants/x/rotate", ""), 400, domain.CodeInvalidRequest)
}

// --- unknown routes ---

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
			{http.MethodDelete, "/admin/tenants"},
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

// Statuses of the Xendit callback are unchanged: a bad body is 400, and any
// processing failure stays 500 so Xendit retries.
func TestXenditCallbackErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := NewRouter(Deps{
		Log:           zap.NewNop(),
		CallbackToken: "cb-token",
		Webhook:       handler.NewWebhookHandler(service.NewWebhookService(nil, nil, nil, nil, nil, nil), zap.NewNop()),
	})
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/xendit", strings.NewReader(body))
		req.Header.Set("x-callback-token", "cb-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	expectError(t, post(`{not json`), 400, domain.CodeInvalidRequest)
	w := post(`{"id":"","external_id":""}`)
	expectError(t, w, 500, domain.CodeInternal)
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("webhook response sets cookies: %v", w.Header().Values("Set-Cookie"))
	}
}
