package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/hmac"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

var (
	appIDRe = regexp.MustCompile(`^app_[0-9A-HJKMNP-TV-Z]{26}$`)
	keyIDRe = regexp.MustCompile(`^key_[0-9A-HJKMNP-TV-Z]{26}$`)
	payIDRe = regexp.MustCompile(`^pay_[0-9A-HJKMNP-TV-Z]{26}$`)
)

func decode(t *testing.T, w *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, want, w.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return m
}

// keysOf fails unless obj has exactly the given keys.
func keysOf(t *testing.T, label string, obj any, want ...string) map[string]any {
	t.Helper()
	m, ok := obj.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want object", label, obj)
	}
	var got []string
	for k := range m {
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s keys = %v, want %v", label, got, want)
	}
	return m
}

var appKeys = []string{"id", "slug", "name", "environment", "status", "metadata",
	"redirect_success_url", "redirect_failure_url", "default_connection", "created_at", "updated_at"}
var apiKeyKeys = []string{"id", "app_id", "label", "environment", "prefix", "last4", "status",
	"last_used_at", "expires_at", "revoked_at", "created_at"}

type createdApp struct {
	id, keyID, apiKey, apiSecret string
}

// createApp creates an app through the API and makes the fixture's
// connection of its environment its default, so /v1 payments work.
func (f *apiFixture) createApp(t *testing.T, b *browser, body string) createdApp {
	t.Helper()
	m := decode(t, f.send(b, http.MethodPost, "/admin/apps", body), http.StatusCreated)
	f.useDefaultConnection(t, m["app"].(map[string]any)["id"].(string))
	return createdApp{
		id:        m["app"].(map[string]any)["id"].(string),
		keyID:     m["key"].(map[string]any)["id"].(string),
		apiKey:    m["api_key"].(string),
		apiSecret: m["api_secret"].(string),
	}
}

// T-16 / AC-8: response shapes of App, ApiKey and AppCreatedResponse.
func TestAppResponseShapes(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)

	m := decode(t, f.send(owner, http.MethodPost, "/admin/apps",
		`{"name":"Acme","slug":"acme","environment":"test","metadata":{"tier":12345678901234567890,"note":"ü"}}`),
		http.StatusCreated)
	// T-B33 / AC-18: no webhook_url or webhook_secret any more.
	keysOf(t, "created", m, "app", "key", "api_key", "api_secret")
	app := keysOf(t, "app", m["app"], appKeys...)
	key := keysOf(t, "key", m["key"], apiKeyKeys...)
	if !appIDRe.MatchString(app["id"].(string)) || app["environment"] != "test" || app["status"] != "active" || app["slug"] != "acme" {
		t.Fatalf("app = %v", app)
	}
	if !strings.Contains(f.send(owner, http.MethodGet, "/admin/apps/"+app["id"].(string), "").Body.String(), `"tier":12345678901234567890`) {
		t.Fatal("metadata number lost its digits")
	}
	if !keyIDRe.MatchString(key["id"].(string)) || key["app_id"] != app["id"] || key["label"] != "Default" || key["prefix"] != "kp_test_" ||
		key["status"] != "active" || key["last_used_at"] != nil || key["expires_at"] != nil || key["revoked_at"] != nil ||
		key["last4"] != m["api_key"].(string)[len(m["api_key"].(string))-4:] {
		t.Fatalf("key = %v", key)
	}
	for _, field := range []string{"api_key", "api_secret"} {
		if m[field] == "" {
			t.Fatalf("%s empty", field)
		}
	}

	// Empty metadata is {} and the list wraps apps in data/next_cursor.
	decode(t, f.send(owner, http.MethodPost, "/admin/apps", `{"name":"Bare","slug":"bare","environment":"live"}`), http.StatusCreated)
	list := decode(t, f.send(owner, http.MethodGet, "/admin/apps?limit=1", ""), http.StatusOK)
	keysOf(t, "list", list, "data", "next_cursor")
	first := keysOf(t, "list item", list["data"].([]any)[0], appKeys...)
	if first["slug"] != "bare" || fmt.Sprint(first["metadata"]) != "map[]" || list["next_cursor"] == nil {
		t.Fatalf("list = %v", list)
	}
	rest := decode(t, f.send(owner, http.MethodGet, "/admin/apps?limit=1&cursor="+list["next_cursor"].(string), ""), http.StatusOK)
	if len(rest["data"].([]any)) != 1 || rest["next_cursor"] != nil {
		t.Fatalf("second page = %v", rest)
	}
	live := decode(t, f.send(owner, http.MethodGet, "/admin/apps?environment=live&status=active", ""), http.StatusOK)
	if len(live["data"].([]any)) != 1 {
		t.Fatalf("filtered = %v", live)
	}

	// Keys list, single key and created key shapes.
	keys := decode(t, f.send(owner, http.MethodGet, "/admin/apps/"+app["id"].(string)+"/keys", ""), http.StatusOK)
	keysOf(t, "keys", keys, "data", "next_cursor")
	keysOf(t, "keys item", keys["data"].([]any)[0], apiKeyKeys...)
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ck := decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app["id"].(string)+"/keys", `{"label":"Server","expires_at":"`+exp+`"}`), http.StatusCreated)
	keysOf(t, "created key", ck, "key", "api_key", "api_secret")
	if k := ck["key"].(map[string]any); k["expires_at"] == nil || k["label"] != "Server" {
		t.Fatalf("created key = %v", k)
	}
	revoked := decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app["id"].(string)+"/keys/"+key["id"].(string)+"/revoke", ""), http.StatusOK)
	if k := keysOf(t, "revoked", revoked["key"], apiKeyKeys...); k["status"] != "revoked" || k["revoked_at"] == nil {
		t.Fatalf("revoked = %v", k)
	}
	rotated := decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app["id"].(string)+"/keys/"+ck["key"].(map[string]any)["id"].(string)+"/rotate", ""), http.StatusCreated)
	keysOf(t, "rotated", rotated, "key", "api_key", "api_secret")

	// Lower-case ids are accepted.
	lower := strings.ToLower(app["id"].(string))
	lower = "app_" + lower[4:]
	decode(t, f.send(owner, http.MethodGet, "/admin/apps/"+lower, ""), http.StatusOK)
}

// T-16: path and query errors.
func TestAppRequestErrors(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"live"}`)

	for _, path := range []string{"/admin/apps/x", "/admin/apps/usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", "/admin/apps/app_01JBKQ7Z8M4X2Y6V3N9T5R1W0", "/admin/apps/x/keys"} {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, path, ""), 400, domain.CodeInvalidRequest), domain.FieldError{Field: "id", Rule: "public_id"})
	}
	for _, path := range []string{"/keys/x/rotate", "/keys/app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C/revoke"} {
		expectFields(t, expectError(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+path, ""), 400, domain.CodeInvalidRequest), domain.FieldError{Field: "key_id", Rule: "public_id"})
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/apps/"+id.New(id.App), ""), 404, domain.CodeNotFound)
	expectError(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/keys/"+id.New(id.APIKey)+"/revoke", ""), 404, domain.CodeNotFound)

	for q, field := range map[string]string{"environment=prod": "environment", "status=gone": "status"} {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/apps?"+q, ""), 400, domain.CodeInvalidRequest), domain.FieldError{Field: field, Rule: "oneof"})
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/apps?cursor=nope", ""), 400, domain.CodeInvalidCursor)
	expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/apps?limit=0", ""), 400, domain.CodeInvalidRequest), domain.FieldError{Field: "limit", Rule: "range"})

	cases := map[string]domain.FieldError{
		"app_id=1":       {Field: "app_id", Rule: "public_id"},
		"app_id=pay_x":   {Field: "app_id", Rule: "public_id"},
		"status=paid":    {Field: "status", Rule: "oneof"},
		"from=yesterday": {Field: "from", Rule: "datetime"},
		"to=2026-01-01":  {Field: "to", Rule: "datetime"},
		"limit=500":      {Field: "limit", Rule: "range"},
	}
	for q, want := range cases {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/payments?"+q, ""), 400, domain.CodeInvalidRequest), want)
	}
	decode(t, f.send(owner, http.MethodGet, "/admin/payments?app_id="+strings.ToLower(app.id[:4])+app.id[4:]+"&status=PAID&from=2026-01-01T00:00:00Z&to=2026-12-31T00:00:00%2B07:00", ""), http.StatusOK)
	// The legacy offset ledger ignores a malformed app_id.
	decode(t, f.send(owner, http.MethodGet, "/admin/ledger?app_id=1", ""), http.StatusOK)
}

// AC-9: PATCH refuses slug and environment and needs an updatable field.
func TestAppPatch(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"live"}`)
	path := "/admin/apps/" + app.id
	f.audit.logs = nil

	cases := []struct {
		body string
		want []domain.FieldError
	}{
		{`{"slug":"acme"}`, []domain.FieldError{{Field: "slug", Rule: "immutable"}}},
		{`{"environment":"live","name":"x"}`, []domain.FieldError{{Field: "environment", Rule: "immutable"}}},
		{`{"slug":"","environment":null}`, []domain.FieldError{{Field: "slug", Rule: "immutable"}, {Field: "environment", Rule: "immutable"}}},
		{`{}`, []domain.FieldError{{Field: "body", Rule: "required"}}},
		{`{"unknown":1}`, []domain.FieldError{{Field: "body", Rule: "required"}}},
		{`{"name":1,"webhook_url":null}`, []domain.FieldError{{Field: "name", Rule: "type"}}},
		{`{"metadata":{"a":[1]}}`, []domain.FieldError{{Field: "metadata.a", Rule: "type"}}},
		{`{"webhook_url":"https://acme.example/hook"}`, []domain.FieldError{{Field: "webhook_url", Rule: "moved"}}},
		{`{"webhook_url":null,"slug":"x","name":" "}`, []domain.FieldError{{Field: "name", Rule: "required"}, {Field: "slug", Rule: "immutable"},
			{Field: "webhook_url", Rule: "moved"}}},
	}
	for _, tc := range cases {
		expectFields(t, expectError(t, f.send(owner, http.MethodPatch, path, tc.body), 400, domain.CodeInvalidRequest), tc.want...)
	}
	expectError(t, f.send(owner, http.MethodPatch, path, `[1]`), 400, domain.CodeInvalidRequest)
	if len(f.audit.logs) != 0 {
		t.Fatalf("refused patches recorded %v", f.audit.actions())
	}

	m := decode(t, f.send(owner, http.MethodPatch, path, `{"name":"Acme Ünïcode","metadata":{"a":"b"}}`), http.StatusOK)
	got := keysOf(t, "app", m["app"], appKeys...)
	if got["name"] != "Acme Ünïcode" || got["slug"] != "acme" || fmt.Sprint(got["metadata"]) != "map[a:b]" {
		t.Fatalf("patched = %v", got)
	}
	decode(t, f.send(owner, http.MethodPatch, path, `{"name":"Acme Ünïcode"}`), http.StatusOK)
	if acts := f.audit.actions(); !slices.Equal(acts, []string{"app.update"}) {
		t.Fatalf("audit = %v", acts)
	}

	// Duplicate slug in the same environment conflicts; another environment is fine.
	expectError(t, f.send(owner, http.MethodPost, "/admin/apps", `{"name":"Again","slug":"ACME","environment":"live"}`), 409, domain.CodeConflict)
	f.createApp(t, owner, `{"name":"Acme Test","slug":"acme","environment":"test"}`)
}

// AC-12 at the HTTP layer: app and key mutations record the signed-in user
// with the request origin; a failing audit insert answers 500 and keeps
// nothing.
func TestAppMutationsAuditAndRollback(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	owner.remote = "203.0.113.9:4444"
	mutate := func(method, target, body string) *httptest.ResponseRecorder {
		return f.send(owner, method, target, body, "User-Agent", "qa-agent/1.0", "X-Request-ID", "req-qa-1")
	}
	f.audit.logs = nil

	w := mutate(http.MethodPost, "/admin/apps", `{"name":"Acme","slug":"acme","environment":"test"}`)
	var created struct {
		App struct{ ID string }
		Key struct{ ID string }
	}
	_ = json.Unmarshal(decodeRaw(t, w, http.StatusCreated), &created)
	base := "/admin/apps/" + created.App.ID
	decode(t, mutate(http.MethodPatch, base, `{"name":"Acme 2"}`), http.StatusOK)
	decode(t, mutate(http.MethodPost, base+"/disable", ""), http.StatusOK)
	decode(t, mutate(http.MethodPost, base+"/disable", ""), http.StatusOK)
	decode(t, mutate(http.MethodPost, base+"/enable", ""), http.StatusOK)
	decode(t, mutate(http.MethodPost, base+"/keys", `{"label":"Second"}`), http.StatusCreated)
	decode(t, mutate(http.MethodPost, base+"/keys/"+created.Key.ID+"/rotate", ""), http.StatusCreated)
	decode(t, mutate(http.MethodPost, base+"/keys/"+created.Key.ID+"/revoke", ""), http.StatusOK)
	decode(t, mutate(http.MethodPost, base+"/keys/"+created.Key.ID+"/revoke", ""), http.StatusOK)

	want := []string{"app.create", "api_key.create", "app.update", "app.disable", "app.enable", "api_key.create", "api_key.rotate", "api_key.revoke"}
	if got := f.audit.actions(); !slices.Equal(got, want) {
		t.Fatalf("audit = %v, want %v", got, want)
	}
	actor := domain.Actor{Type: domain.ActorAdmin, ID: f.owner.PublicID, IP: "203.0.113.9", UserAgent: "qa-agent/1.0", RequestID: "req-qa-1"}
	for _, l := range f.audit.logs {
		if l.Actor != actor {
			t.Fatalf("%s actor = %+v", l.Action, l.Actor)
		}
		if (l.Target.Type == "app" && !appIDRe.MatchString(l.Target.ID)) || (l.Target.Type == "api_key" && !keyIDRe.MatchString(l.Target.ID)) {
			t.Fatalf("%s target = %+v", l.Action, l.Target)
		}
	}

	appsBefore, keysBefore := len(f.apps.All()), len(f.keys.All())
	f.audit.err = errors.New("pq: relation audit_logs at 10.1.2.3 does not exist")
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodPost, "/admin/apps", `{"name":"Globex","slug":"globex","environment":"live"}`},
		{http.MethodPatch, base, `{"name":"Changed"}`},
		{http.MethodPost, base + "/disable", ""},
		{http.MethodPost, base + "/keys", `{"label":"Third"}`},
	} {
		w := mutate(tc.method, tc.target, tc.body)
		env := expectError(t, w, http.StatusInternalServerError, domain.CodeInternal)
		if env.Error.Message != "internal server error" || strings.Contains(w.Body.String(), "10.1.2.3") || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("%s %s: %s", tc.method, tc.target, w.Body.String())
		}
	}
	stored, _ := f.apps.GetByPublicID(t.Context(), created.App.ID)
	if len(f.apps.All()) != appsBefore || len(f.keys.All()) != keysBefore || stored.Name != "Acme 2" || stored.Status != domain.AppActive {
		t.Fatalf("changes survived a failed audit insert: apps=%d keys=%d app=%+v", len(f.apps.All()), len(f.keys.All()), stored)
	}
}

func decodeRaw(t *testing.T, w *httptest.ResponseRecorder, want int) []byte {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, want, w.Body.String())
	}
	return w.Body.Bytes()
}

// AC-8: viewers read apps and keys but every write is 403.
func TestViewerCannotWriteApps(t *testing.T) {
	f := newAPI(t, "")
	app := f.createApp(t, f.signIn(t, f.owner), `{"name":"Acme","slug":"acme","environment":"live"}`)
	_, viewer := f.as(t, domain.RoleViewer)
	decode(t, f.send(viewer, http.MethodGet, "/admin/apps", ""), http.StatusOK)
	decode(t, f.send(viewer, http.MethodGet, "/admin/apps/"+app.id+"/keys", ""), http.StatusOK)
	base := "/admin/apps/" + app.id
	for _, tc := range []struct{ method, target string }{
		{http.MethodPost, "/admin/apps"}, {http.MethodPatch, base}, {http.MethodPost, base + "/disable"},
		{http.MethodPost, base + "/enable"}, {http.MethodPost, base + "/keys"},
		{http.MethodPost, base + "/keys/" + app.keyID + "/rotate"}, {http.MethodPost, base + "/keys/" + app.keyID + "/revoke"},
	} {
		expectError(t, f.send(viewer, tc.method, tc.target, `{"name":"x","slug":"x-y-z","environment":"live","label":"x"}`), 403, domain.CodeForbidden)
	}
	if k := f.keys.Get(app.keyID); k.RevokedAt != nil || len(f.keys.All()) != 1 {
		t.Fatal("a viewer changed keys")
	}
}

// v1 is a signed client of the /v1 API.
type v1Client struct {
	f              *apiFixture
	apiKey, secret string
}

func (c v1Client) do(method, target, body string, headers ...string) *httptest.ResponseRecorder {
	ts := signedTimestamp(c.secret, body)
	h := append([]string{"X-Api-Key", c.apiKey, "X-Timestamp", ts, "X-Signature", hmac.Sign(c.secret, []byte(ts+"."+body))}, headers...)
	return c.f.send(nil, method, target, body, h...)
}

// T-16 / AC-14, AC-15: /v1 payments with metadata and Idempotency-Key.
func TestV1PaymentsOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	other := f.createApp(t, owner, `{"name":"Globex","slug":"globex","environment":"test"}`)
	c := v1Client{f: f, apiKey: app.apiKey, secret: app.apiSecret}

	body := `{"order_id":"ord-1","amount":150000,"metadata":{"channel":"mobile","n":12345678901234567890}}`
	w := c.do(http.MethodPost, "/v1/payments", body, "Idempotency-Key", "idem-1")
	m := decode(t, w, http.StatusCreated)
	if w.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("first create marked as replayed")
	}
	keysOf(t, "payment", m, "id", "payment_id", "reference", "order_id", "status", "amount", "currency", "invoice_url", "metadata")
	pub := m["id"].(string)
	if !payIDRe.MatchString(pub) || m["reference"] != "kp_"+pub || !strings.Contains(w.Body.String(), `"n":12345678901234567890`) {
		t.Fatalf("payment = %s", w.Body.String())
	}

	// Same key and body again: the same payment, marked as replayed.
	w = c.do(http.MethodPost, "/v1/payments", body+" ", "Idempotency-Key", "idem-1")
	if r := decode(t, w, http.StatusCreated); r["id"] != pub || w.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %s %v", w.Body.String(), w.Header())
	}
	expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-1","amount":1}`, "Idempotency-Key", "idem-1"), 409, domain.CodeIdempotencyKeyReused)
	expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-1","amount":1}`), 409, domain.CodeConflict)
	w = c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-1","amount":150000}`)
	if decode(t, w, http.StatusCreated)["id"] != pub || w.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("order replay: %s", w.Body.String())
	}
	expectFields(t, expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-2","amount":1}`, "Idempotency-Key", "has space"), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "Idempotency-Key", Rule: "format"})
	expectFields(t, expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-2","amount":1,"metadata":"x"}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "metadata", Rule: "object"})

	// Get by pay_ id (any case) and legacy integer; another app sees 404.
	var created struct {
		PaymentID int64 `json:"payment_id"`
	}
	_ = json.Unmarshal(c.do(http.MethodGet, "/v1/payments/"+pub, "").Body.Bytes(), &created)
	for _, ref := range []string{pub, "pay_" + strings.ToLower(pub[4:]), strconv.FormatInt(created.PaymentID, 10)} {
		got := decode(t, c.do(http.MethodGet, "/v1/payments/"+ref, ""), http.StatusOK)
		if got["id"] != pub || fmt.Sprint(got["metadata"]) != "map[channel:mobile n:1.2345678901234567e+19]" {
			t.Fatalf("get %s = %v", ref, got)
		}
		expectError(t, v1Client{f: f, apiKey: other.apiKey, secret: other.apiSecret}.do(http.MethodGet, "/v1/payments/"+ref, ""), 404, domain.CodeNotFound)
	}
	for _, ref := range []string{"abc", "pay_x", "-1", "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C"} {
		expectFields(t, expectError(t, c.do(http.MethodGet, "/v1/payments/"+ref, ""), 400, domain.CodeInvalidRequest), domain.FieldError{Field: "id", Rule: "public_id"})
	}
	expectError(t, c.do(http.MethodGet, "/v1/payments/0", ""), 404, domain.CodeNotFound)
	expectError(t, c.do(http.MethodGet, "/v1/payments/"+strings.Repeat("9", 20), ""), 400, domain.CodeInvalidRequest)
	expectError(t, c.do(http.MethodGet, "/v1/payments/9999999999999999999", ""), 404, domain.CodeNotFound)

	// The admin list carries app summaries and pay_ ids, with strict filters.
	list := decode(t, f.send(owner, http.MethodGet, "/admin/payments?app_id="+app.id, ""), http.StatusOK)
	rows := list["data"].([]any)
	if len(rows) != 1 || list["next_cursor"] != nil {
		t.Fatalf("admin list = %v", list)
	}
	row := keysOf(t, "admin payment", rows[0], "id", "reference", "app", "order_id", "xendit_invoice_id", "amount", "currency", "status",
		"description", "invoice_url", "payer_email", "items", "metadata", "expires_at", "paid_at", "settled_at",
		"expected_settlement_at", "created_at", "updated_at")
	summary := keysOf(t, "app summary", row["app"], "id", "slug", "name", "environment")
	if row["id"] != pub || summary["id"] != app.id || summary["environment"] != "test" || row["paid_at"] != nil ||
		fmt.Sprint(row["items"]) != "[]" {
		t.Fatalf("admin row = %v", row)
	}
	if none := decode(t, f.send(owner, http.MethodGet, "/admin/payments?app_id="+id.New(id.App), ""), http.StatusOK); len(none["data"].([]any)) != 0 {
		t.Fatalf("unknown app_id = %v", none)
	}
}

// AC-5, AC-6 over HTTP: rotation keeps both keys working until the old one
// is revoked; a disabled app gets tenant_inactive until it is enabled.
func TestV1KeyLifecycleOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"live"}`)
	oldKey := v1Client{f: f, apiKey: app.apiKey, secret: app.apiSecret}
	missing := "/v1/payments/" + id.New(id.Payment)
	expectError(t, oldKey.do(http.MethodGet, missing, ""), 404, domain.CodeNotFound)

	r := decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/keys/"+app.keyID+"/rotate", ""), http.StatusCreated)
	newKey := v1Client{f: f, apiKey: r["api_key"].(string), secret: r["api_secret"].(string)}
	expectError(t, oldKey.do(http.MethodGet, missing, ""), 404, domain.CodeNotFound)
	expectError(t, newKey.do(http.MethodGet, missing, ""), 404, domain.CodeNotFound)

	decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/keys/"+app.keyID+"/revoke", ""), http.StatusOK)
	expectError(t, oldKey.do(http.MethodGet, missing, ""), 401, domain.CodeAPIKeyRevoked)
	expectError(t, newKey.do(http.MethodGet, missing, ""), 404, domain.CodeNotFound)

	decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/disable", ""), http.StatusOK)
	expectError(t, newKey.do(http.MethodGet, missing, ""), 403, domain.CodeTenantInactive)
	decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/enable", ""), http.StatusOK)
	expectError(t, newKey.do(http.MethodGet, missing, ""), 404, domain.CodeNotFound)

	keys := decode(t, f.send(owner, http.MethodGet, "/admin/apps/"+app.id+"/keys", ""), http.StatusOK)["data"].([]any)
	statuses := map[string]any{}
	for _, k := range keys {
		statuses[k.(map[string]any)["id"].(string)] = k.(map[string]any)["status"]
	}
	if statuses[app.keyID] != "revoked" || len(statuses) != 2 {
		t.Fatalf("statuses = %v", statuses)
	}
	for _, k := range keys {
		if km := k.(map[string]any); km["status"] == "active" && km["last_used_at"] == nil {
			t.Fatalf("used key has no last_used_at: %v", km)
		}
	}
}

// T-18 / AC-11: no log line, audit snapshot or list response carries a full
// key, key hash, API secret, webhook secret or ciphertext.
func TestAppSecretsNeverLeak(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	var secrets []string
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	secrets = append(secrets, app.apiKey, app.apiSecret)
	ck := decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/keys", `{"label":"Two"}`), http.StatusCreated)
	secrets = append(secrets, ck["api_key"].(string), ck["api_secret"].(string))
	rk := decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/keys/"+app.keyID+"/rotate", ""), http.StatusCreated)
	secrets = append(secrets, rk["api_key"].(string), rk["api_secret"].(string))

	c := v1Client{f: f, apiKey: app.apiKey, secret: app.apiSecret}
	decode(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"o-1","amount":1000}`), http.StatusCreated)
	bad := v1Client{f: f, apiKey: app.apiKey, secret: "wrong"}
	expectError(t, bad.do(http.MethodPost, "/v1/payments", `{}`), 401, domain.CodeInvalidSignature)
	decode(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/keys/"+app.keyID+"/revoke", ""), http.StatusOK)
	expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"o-2","amount":1}`), 401, domain.CodeAPIKeyRevoked)

	for _, k := range f.keys.All() {
		secrets = append(secrets, k.Hash, k.SecretEnc)
	}
	if len(secrets) < 11 {
		t.Fatalf("collected %d secrets", len(secrets))
	}

	var bodies []string
	for _, path := range []string{"/admin/apps", "/admin/apps/" + app.id, "/admin/apps/" + app.id + "/keys", "/admin/audit-logs", "/admin/payments"} {
		bodies = append(bodies, decodeRawBody(t, f.send(owner, http.MethodGet, path, "")))
	}
	for _, body := range bodies {
		for _, s := range secrets {
			if strings.Contains(body, s) {
				t.Fatalf("response leaks %q: %s", s[:6], body)
			}
		}
	}
	for _, l := range f.audit.logs {
		raw := string(l.Before) + string(l.After)
		for _, s := range secrets {
			if strings.Contains(raw, s) {
				t.Fatalf("%s snapshot leaks %q: %s", l.Action, s[:6], raw)
			}
		}
	}
	for _, e := range f.logs.All() {
		line := e.Message + fmt.Sprint(e.ContextMap())
		for _, s := range secrets {
			if strings.Contains(line, s) {
				t.Fatalf("log line leaks %q: %s", s[:6], line)
			}
		}
	}
}

func decodeRawBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}
