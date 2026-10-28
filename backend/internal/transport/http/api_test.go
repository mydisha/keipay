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
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
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

type auditRepo struct {
	logs []domain.AuditLog
	page domain.Page
	res  domain.PageResult[domain.AuditLog]
}

func (a *auditRepo) Create(_ context.Context, l *domain.AuditLog) error {
	a.logs = append(a.logs, *l)
	return nil
}
func (a *auditRepo) List(_ context.Context, p domain.Page) (domain.PageResult[domain.AuditLog], error) {
	a.page = p
	return a.res, nil
}

type adminRepo struct {
	user *domain.AdminUser
	err  error
}

func (a *adminRepo) GetByUsername(_ context.Context, name string) (*domain.AdminUser, error) {
	if a.err != nil {
		return nil, a.err
	}
	if a.user == nil || a.user.Username != name {
		return nil, nil
	}
	cp := *a.user
	return &cp, nil
}
func (a *adminRepo) Create(context.Context, *domain.AdminUser) error { return nil }
func (a *adminRepo) UpdatePassword(_ context.Context, _ int64, hash string) error {
	a.user.PasswordHash = hash
	return nil
}

type directTx struct{}

func (directTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type apiFixture struct {
	router *gin.Engine
	admins *adminRepo
	audit  *auditRepo
	replay *replayer
	auth   *service.AuthService
}

func newAPI(t *testing.T, frontendDir string) *apiFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hash, err := bcrypt.GenerateFromPassword([]byte("current-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f := &apiFixture{
		admins: &adminRepo{user: &domain.AdminUser{ID: 1, Username: "admin", PasswordHash: string(hash)}},
		audit:  &auditRepo{},
		replay: &replayer{},
	}
	auditSvc := service.NewAuditService(f.audit)
	f.auth = service.NewAuthService(f.admins, "test-jwt-secret", directTx{}, auditSvc)
	admin := handler.NewAdminHandler(
		f.auth,
		service.NewTenantService(emptyTenants{}, nil, nil, directTx{}, auditSvc),
		service.NewPaymentService(emptyPayments{}, nil),
		service.NewLedgerService(emptyLedger{}),
		service.NewDeliveryService(deliveries{byID: map[int64]domain.WebhookDelivery{3: {ID: 3, Status: domain.WebhookDeliveryFailed, Attempts: 2}}},
			f.replay, auditSvc),
		auditSvc,
	)
	f.router = NewRouter(Deps{Log: zap.NewNop(), JWTVerifier: f.auth, Admin: admin, FrontendDir: frontendDir})
	return f
}

func (f *apiFixture) token(t *testing.T) string {
	t.Helper()
	res, err := f.auth.Login(context.Background(), "admin", "current-pass")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return res.Token
}

func (f *apiFixture) do(method, target, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
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

// --- T-8: empty lists ---

func TestEmptyListsEncodeAsArrays(t *testing.T) {
	f := newAPI(t, "")
	tok := f.token(t)
	cases := map[string][]string{
		"/admin/tenants":            {`"tenants":[]`},
		"/admin/payments":           {`"payments":[]`},
		"/admin/ledger":             {`"entries":[]`, `"summary":[]`},
		"/admin/webhook-deliveries": {`"deliveries":[]`},
		"/admin/audit-logs":         {`"data":[]`, `"next_cursor":null`},
	}
	for path, wants := range cases {
		w := f.do(http.MethodGet, path, tok, "")
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

// --- T-16: audit log list ---

func TestAuditLogsShapeAndPaging(t *testing.T) {
	f := newAPI(t, "")
	tok := f.token(t)
	pub := id.New(id.Audit)
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	f.audit.res = domain.PageResult[domain.AuditLog]{
		Items: []domain.AuditLog{{
			ID: 987654, PublicID: pub, OccurredAt: at, Action: "tenant.update",
			Actor:  domain.Actor{Type: "admin", ID: "admin", IP: "203.0.113.7", UserAgent: "ua", RequestID: "rid"},
			Target: domain.AuditTarget{Type: "tenant", ID: "1"},
			Before: json.RawMessage(`{"name":"Old"}`),
		}},
		NextCursor: domain.EncodeCursor(domain.Cursor{Time: at, ID: pub}),
	}

	w := f.do(http.MethodGet, "/admin/audit-logs?limit=1", tok, "")
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
	if a := entry["actor"].(map[string]any); a["type"] != "admin" || a["id"] != "admin" || len(a) != 2 {
		t.Fatalf("actor = %v", a)
	}

	w = f.do(http.MethodGet, "/admin/audit-logs?cursor="+*body.NextCursor, tok, "")
	if w.Code != http.StatusOK || f.audit.page.After == nil || f.audit.page.After.ID != pub || f.audit.page.Limit != 50 {
		t.Fatalf("cursor page: %d page=%+v", w.Code, f.audit.page)
	}
}

func TestAuditLogsQueryErrors(t *testing.T) {
	f := newAPI(t, "")
	tok := f.token(t)
	for _, q := range []string{"limit=0", "limit=201", "limit=-1", "limit=abc"} {
		env := expectError(t, f.do(http.MethodGet, "/admin/audit-logs?"+q, tok, ""), 400, domain.CodeInvalidRequest)
		fields, _ := env.Error.Details["fields"].([]any)
		if len(fields) != 1 || fields[0].(map[string]any)["field"] != "limit" || fields[0].(map[string]any)["rule"] != "range" {
			t.Fatalf("%s: details = %v", q, env.Error.Details)
		}
	}
	expectError(t, f.do(http.MethodGet, "/admin/audit-logs?cursor=nope", tok, ""), 400, domain.CodeInvalidCursor)
	expectError(t, f.do(http.MethodGet, "/admin/audit-logs", "", ""), 401, domain.CodeMissingToken)
}

// --- T-17: login and change password ---

func TestLoginErrors(t *testing.T) {
	f := newAPI(t, "")
	env := expectError(t, f.do(http.MethodPost, "/admin/login", "", `{"username":"admin","password":"wrong"}`), 401, domain.CodeInvalidCredentials)
	if env.Error.Message == "" {
		t.Fatal("empty message")
	}
	expectError(t, f.do(http.MethodPost, "/admin/login", "", `{"username":"ghost","password":"x"}`), 401, domain.CodeInvalidCredentials)
	expectError(t, f.do(http.MethodPost, "/admin/login", "", `{"username":"admin"}`), 400, domain.CodeInvalidRequest)

	f.admins.err = errors.New("dial tcp 10.0.0.5:5432: connection refused")
	w := f.do(http.MethodPost, "/admin/login", "", `{"username":"admin","password":"current-pass"}`)
	env = expectError(t, w, 500, domain.CodeInternal)
	if env.Error.Message != "internal server error" || strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Fatalf("internal error leaked: %s", w.Body.String())
	}
}

func TestChangePassword(t *testing.T) {
	f := newAPI(t, "")
	tok := f.token(t)
	expectError(t, f.do(http.MethodPost, "/admin/change-password", tok, `{"current_password":"wrong","new_password":"long-enough"}`), 401, domain.CodeInvalidCredentials)
	expectError(t, f.do(http.MethodPost, "/admin/change-password", tok, `{"current_password":"current-pass","new_password":"short"}`), 400, domain.CodeInvalidRequest)
	if len(f.audit.logs) != 0 {
		t.Fatalf("failed attempts recorded %d entries", len(f.audit.logs))
	}

	w := f.do(http.MethodPost, "/admin/change-password", tok, `{"current_password":"current-pass","new_password":"long-enough"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	if len(f.audit.logs) != 1 || f.audit.logs[0].Actor.ID != "admin" || f.audit.logs[0].Actor.Type != domain.ActorAdmin {
		t.Fatalf("entries = %+v", f.audit.logs)
	}

	f.admins.err = errors.New("db down")
	expectError(t, f.do(http.MethodPost, "/admin/change-password", tok, `{"current_password":"long-enough","new_password":"another-one"}`), 500, domain.CodeInternal)
}

// --- replay and path ids ---

func TestReplay(t *testing.T) {
	f := newAPI(t, "")
	tok := f.token(t)
	expectError(t, f.do(http.MethodPost, "/admin/webhook-deliveries/abc/replay", tok, ""), 400, domain.CodeInvalidRequest)
	expectError(t, f.do(http.MethodPost, "/admin/webhook-deliveries/99/replay", tok, ""), 404, domain.CodeNotFound)

	f.replay.err = errors.New("redis down")
	expectError(t, f.do(http.MethodPost, "/admin/webhook-deliveries/3/replay", tok, ""), 500, domain.CodeInternal)
	if len(f.audit.logs) != 0 {
		t.Fatal("failed replay recorded an entry")
	}

	f.replay.err = nil
	w := f.do(http.MethodPost, "/admin/webhook-deliveries/3/replay", tok, "")
	if w.Code != http.StatusAccepted || w.Body.String() != `{"requeued":true}` {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}
	if len(f.audit.logs) != 1 || f.audit.logs[0].Action != "webhook_delivery.replay" || f.audit.logs[0].Actor.RequestID == "" {
		t.Fatalf("entries = %+v", f.audit.logs)
	}
}

func TestInvalidTenantPathID(t *testing.T) {
	f := newAPI(t, "")
	tok := f.token(t)
	env := expectError(t, f.do(http.MethodPut, "/admin/tenants/x", tok, `{"name":"a"}`), 400, domain.CodeInvalidRequest)
	fields, _ := env.Error.Details["fields"].([]any)
	if len(fields) != 1 || fields[0].(map[string]any)["field"] != "id" || fields[0].(map[string]any)["rule"] != "int" {
		t.Fatalf("details = %v", env.Error.Details)
	}
	expectError(t, f.do(http.MethodPost, "/admin/tenants/x/rotate", tok, ""), 400, domain.CodeInvalidRequest)
}

// --- T-6: unknown routes ---

func TestUnknownAPIRoutesReturnNotFoundEnvelope(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>console</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, frontendDir := range []string{"", dist} {
		f := newAPI(t, frontendDir)
		tok := f.token(t)
		cases := []struct{ method, path string }{
			{http.MethodGet, "/admin/x"},
			{http.MethodGet, "/v1/x"},
			{http.MethodPost, "/webhooks/x"},
			{http.MethodGet, "/healthz/x"},
			// Wrong method on a known path is a 404, not a 405.
			{http.MethodDelete, "/admin/tenants"},
			{http.MethodGet, "/admin/login"},
			// Non-GET to a console path.
			{http.MethodPost, "/apps"},
		}
		for _, tc := range cases {
			w := f.do(tc.method, tc.path, tok, "")
			expectError(t, w, http.StatusNotFound, domain.CodeNotFound)
		}
	}
	f := newAPI(t, dist)
	if w := f.do(http.MethodGet, "/settings", "", ""); w.Code != http.StatusOK || w.Body.String() != "<html>console</html>" {
		t.Fatalf("console path: %d %q", w.Code, w.Body.String())
	}
	f = newAPI(t, "")
	expectError(t, f.do(http.MethodGet, "/settings", "", ""), http.StatusNotFound, domain.CodeNotFound)
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
	expectError(t, post(`{"id":"","external_id":""}`), 500, domain.CodeInternal)
}
