package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
)

const testMasterKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// fakeXenditAPI stands in for the Xendit API: listing invoices answers 401
// (every key is wrong), creating one succeeds.
type fakeXenditAPI struct {
	*httptest.Server
	mu    sync.Mutex
	calls []string
}

func newFakeXenditAPI(t *testing.T) *fakeXenditAPI {
	t.Helper()
	f := &fakeXenditAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error_code":"INVALID_API_KEY","message":"API key is invalid"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"inv_x1","external_id":"x","user_id":"u","status":"PENDING","merchant_name":"m",
			"merchant_profile_picture_url":"","amount":1,"expiry_date":"2026-10-30T09:00:00Z","invoice_url":"https://checkout.xendit.co/web/inv_x1",
			"available_banks":[],"available_retail_outlets":[],"available_ewallets":[],"available_qr_codes":[],
			"available_direct_debits":[],"available_paylaters":[],"should_send_email":false,
			"created":"2026-10-29T09:00:00Z","updated":"2026-10-29T09:00:00Z"}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func systemCtx() context.Context {
	return domain.WithActor(context.Background(), domain.SystemActor("test"))
}

// addXendit creates a Xendit test connection with its own token.
func (f *apiFixture) addXendit(t *testing.T, name, key, token string) *domain.Connection {
	t.Helper()
	d, err := f.connSvc.Create(systemCtx(), service.CreateConnectionInput{Provider: "xendit", Name: name, Environment: domain.EnvTest,
		Credentials: map[string]any{"secret_key": key, "callback_token": token}})
	if err != nil {
		t.Fatal(err)
	}
	return d.Connection
}

// addPayment stores a pending payment of a new test app made through conn
// (nil for a payment from before connections).
func (f *apiFixture) addPayment(t *testing.T, conn *domain.Connection, amount int64) *domain.Payment {
	t.Helper()
	ctx := context.Background()
	app := &domain.App{PublicID: id.New(id.App), Slug: "app-" + strings.ToLower(id.New(id.App)[20:]), Name: "App",
		Environment: domain.EnvTest, Status: domain.AppActive}
	if err := f.apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: "ord-1", Amount: amount, Currency: "IDR",
		Status: domain.PaymentStatusPending}
	p.ExternalID = "kp_" + p.PublicID
	if conn != nil {
		p.ConnectionID, p.Provider = &conn.ID, conn.Provider
	}
	if err := f.payments.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *apiFixture) paymentStatus(t *testing.T, p *domain.Payment) domain.PaymentStatus {
	t.Helper()
	got, err := f.payments.GetByPublicID(context.Background(), p.AppID, p.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

func xenditCallbackBody(p *domain.Payment, invoiceID, status string) string {
	return fmt.Sprintf(`{"id":%q,"external_id":%q,"status":%q,"amount":%d,"currency":"IDR","paid_amount":%d}`,
		invoiceID, p.ExternalID, status, p.Amount, p.Amount)
}

// The Xendit descriptor GET /admin/providers must serve, field for field.
const xenditDescriptor = `{
  "name": "xendit",
  "display_name": "Xendit",
  "description": "Hosted checkout through the Xendit Invoice API.",
  "docs_url": "https://docs.xendit.co",
  "credential_schema": {"fields": [
    {"key": "secret_key", "type": "string", "label": "Secret API key",
     "description": "Xendit dashboard → Settings → API keys. A key with Invoice read and write permission is enough.",
     "placeholder": "xnd_development_…", "secret": true, "required": true, "default": null, "max_length": 255,
     "pattern": "^xnd_(development|production)_[A-Za-z0-9]+$",
     "pattern_hint": "Starts with xnd_development_ (test) or xnd_production_ (live)."},
    {"key": "callback_token", "type": "string", "label": "Webhook verification token",
     "description": "Xendit dashboard → Settings → Webhooks → Webhook verification token.",
     "secret": true, "required": true, "default": null, "min_length": 8, "max_length": 255}
  ]},
  "settings_schema": {"fields": [
    {"key": "invoice_duration", "type": "integer", "label": "Invoice lifetime (seconds)",
     "description": "Used when a payment request has no invoice_duration. Empty uses Xendit's default (24 hours).",
     "secret": false, "required": false, "default": null, "minimum": 60, "maximum": 31536000},
    {"key": "locale", "type": "enum", "label": "Checkout language", "description": "", "secret": false, "required": false,
     "default": null, "options": [{"value": "id", "label": "Indonesian"}, {"value": "en", "label": "English"}]}
  ]},
  "capabilities": {
    "channels": [
      {"code": "QRIS", "label": "QRIS"}, {"code": "VA_BCA", "label": "BCA Virtual Account"},
      {"code": "VA_BNI", "label": "BNI Virtual Account"}, {"code": "VA_BRI", "label": "BRI Virtual Account"},
      {"code": "VA_MANDIRI", "label": "Mandiri Virtual Account"}, {"code": "VA_PERMATA", "label": "Permata Virtual Account"},
      {"code": "VA_BSI", "label": "BSI Virtual Account"}, {"code": "EWALLET_OVO", "label": "OVO"},
      {"code": "EWALLET_DANA", "label": "DANA"}, {"code": "EWALLET_SHOPEEPAY", "label": "ShopeePay"},
      {"code": "EWALLET_LINKAJA", "label": "LinkAja"}, {"code": "CARD", "label": "Credit or debit card"},
      {"code": "RETAIL_ALFAMART", "label": "Alfamart"}, {"code": "RETAIL_INDOMARET", "label": "Indomaret"}
    ],
    "currencies": ["IDR"], "can_restrict_channels": true, "can_cancel": true, "reports_fee": true,
    "list_transactions": true,
    "read_checkout": true
  },
  "webhook_setup": {
    "instructions": [
      "In the Xendit dashboard, open Settings → Webhooks.",
      "Paste the callback URL into \"Invoices paid\" and save.",
      "Copy the webhook verification token shown on that page into this connection, if you have not already."
    ],
    "dashboard_url": "https://dashboard.xendit.co/settings/developers#webhooks"
  }
}`

func TestProvidersDescribeXendit(t *testing.T) {
	f := newAPI(t, "")
	for _, role := range []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleViewer} {
		_, b := f.as(t, role)
		m := decode(t, f.send(b, http.MethodGet, "/admin/providers", ""), http.StatusOK)
		data := m["data"].([]any)
		if len(data) != 2 || data[0].(map[string]any)["name"] != "fake" {
			t.Fatalf("%s: providers = %v", role, data)
		}
		var want any
		if err := json.Unmarshal([]byte(xenditDescriptor), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(data[1], want) {
			got, _ := json.MarshalIndent(data[1], "", "  ")
			t.Fatalf("%s: xendit descriptor =\n%s", role, got)
		}
	}
}

var connectionListKeys = []string{"id", "name", "provider", "environment", "status", "legacy_webhook_alias", "last_tested_at",
	"last_test_status", "created_at", "updated_at"}

var connectionKeys = []string{"id", "name", "provider", "environment", "status", "legacy_webhook_alias", "last_tested_at",
	"last_test_status", "created_at", "updated_at", "credentials", "settings", "channels", "webhook_url", "last_test_error"}

// The admin flow end to end, checking shapes.
func TestConnectionAdminFlow(t *testing.T) {
	f := newAPI(t, "")
	_, admin := f.as(t, domain.RoleAdmin)
	f.audit.logs = nil

	body := `{"provider":"xendit","name":" Xendit-Test ","environment":"test",
		"credentials":{"secret_key":"xnd_development_flow0123456789","callback_token":"flow-token"},"settings":{"invoice_duration":3600}}`
	w := f.send(admin, http.MethodPost, "/admin/connections", body)
	m := decode(t, w, http.StatusCreated)
	c := keysOf(t, "connection", m["connection"], connectionKeys...)
	conID := c["id"].(string)
	if !regexpConnID.MatchString(conID) || c["name"] != "xendit-test" || c["status"] != "active" || c["last_test_status"] != nil ||
		c["last_tested_at"] != nil || c["legacy_webhook_alias"] != false || c["webhook_url"] != "https://pay.example.com/webhooks/xendit/"+conID {
		t.Fatalf("created = %v", c)
	}
	if fmt.Sprint(c["credentials"]) != "[map[key:secret_key last4:6789] map[key:callback_token last4:<nil>]]" {
		t.Fatalf("credentials = %v", c["credentials"])
	}
	if fmt.Sprint(c["settings"]) != "map[invoice_duration:3600]" || len(c["channels"].([]any)) != 14 {
		t.Fatalf("settings/channels = %v / %v", c["settings"], c["channels"])
	}
	keysOf(t, "channel", c["channels"].([]any)[1], "channel", "label", "enabled")

	expectError(t, f.send(admin, http.MethodPost, "/admin/connections", body), 409, domain.CodeConflict)
	env := expectError(t, f.send(admin, http.MethodPost, "/admin/connections",
		`{"provider":"xendit","name":"x","environment":"live","credentials":{"secret_key":"xnd_development_a","callback_token":"short"},"settings":[]}`),
		400, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "settings", Rule: "type"})
	env = expectError(t, f.send(admin, http.MethodPost, "/admin/connections",
		`{"provider":"xendit","name":"x","environment":"live","credentials":{"secret_key":"xnd_development_a","callback_token":"short"}}`),
		400, domain.CodeInvalidRequest)
	expectFields(t, env, domain.FieldError{Field: "name", Rule: "slug"}, domain.FieldError{Field: "credentials.callback_token", Rule: "min_length"})

	// Detail, list, filters.
	keysOf(t, "detail", decode(t, f.send(admin, http.MethodGet, "/admin/connections/"+strings.ToLower(conID), ""), 200)["connection"], connectionKeys...)
	list := decode(t, f.send(admin, http.MethodGet, "/admin/connections?provider=xendit&environment=test&status=active", ""), 200)
	keysOf(t, "list", list, "data", "next_cursor")
	if items := list["data"].([]any); len(items) != 1 {
		t.Fatalf("list = %v", items)
	}
	keysOf(t, "list item", list["data"].([]any)[0], connectionListKeys...)
	for _, q := range []string{"provider=midtrans", "environment=prod", "status=gone"} {
		env := expectError(t, f.send(admin, http.MethodGet, "/admin/connections?"+q, ""), 400, domain.CodeInvalidRequest)
		expectFields(t, env, domain.FieldError{Field: strings.Split(q, "=")[0], Rule: "oneof"})
	}
	expectError(t, f.send(admin, http.MethodGet, "/admin/connections?cursor=x", ""), 400, domain.CodeInvalidCursor)
	expectFields(t, expectError(t, f.send(admin, http.MethodGet, "/admin/connections/nope", ""), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "id", Rule: "public_id"})
	expectError(t, f.send(admin, http.MethodGet, "/admin/connections/"+id.New(id.Connection), ""), 404, domain.CodeNotFound)

	// PATCH.
	path := "/admin/connections/" + conID
	expectFields(t, expectError(t, f.send(admin, http.MethodPatch, path, `{"provider":"fake","environment":"live","credentials":{}}`), 400,
		domain.CodeInvalidRequest), domain.FieldError{Field: "provider", Rule: "immutable"},
		domain.FieldError{Field: "environment", Rule: "immutable"}, domain.FieldError{Field: "credentials", Rule: "immutable"})
	expectFields(t, expectError(t, f.send(admin, http.MethodPatch, path, `{"other":1}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "body", Rule: "required"})
	expectFields(t, expectError(t, f.send(admin, http.MethodPatch, path, `{"name":5,"settings":null}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "name", Rule: "type"}, domain.FieldError{Field: "settings", Rule: "type"})
	m = decode(t, f.send(admin, http.MethodPatch, path, `{"name":"xendit-renamed","settings":{"locale":"id"}}`), 200)
	if c := m["connection"].(map[string]any); c["name"] != "xendit-renamed" || fmt.Sprint(c["settings"]) != "map[locale:id]" {
		t.Fatalf("patched = %v", c)
	}

	// Credentials: merge, then test result is reset.
	decode(t, f.send(admin, http.MethodPost, path+"/test", ""), 200)
	expectFields(t, expectError(t, f.send(admin, http.MethodPatch, path+"/credentials", `{"credentials":{}}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "credentials", Rule: "required"})
	expectFields(t, expectError(t, f.send(admin, http.MethodPatch, path+"/credentials", `{"credentials":"x"}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "credentials", Rule: "type"})
	m = decode(t, f.send(admin, http.MethodPatch, path+"/credentials", `{"credentials":{"callback_token":"a-much-longer-token"}}`), 200)
	c = m["connection"].(map[string]any)
	if c["last_test_status"] != nil || c["last_tested_at"] != nil ||
		fmt.Sprint(c["credentials"]) != "[map[key:secret_key last4:6789] map[key:callback_token last4:oken]]" {
		t.Fatalf("after credentials = %v", c)
	}

	// Channels.
	expectFields(t, expectError(t, f.send(admin, http.MethodPut, path+"/channels", `{"channels":[{"channel":"QRIS"}]}`), 400,
		domain.CodeInvalidRequest), domain.FieldError{Field: "channels.0.enabled", Rule: "required"})
	expectFields(t, expectError(t, f.send(admin, http.MethodPut, path+"/channels", `{}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "channels", Rule: "required"})
	expectFields(t, expectError(t, f.send(admin, http.MethodPut, path+"/channels",
		`{"channels":[{"channel":"QRIS","enabled":false},{"channel":"NOPE","enabled":true},{"channel":"QRIS","enabled":true}]}`), 400,
		domain.CodeInvalidRequest), domain.FieldError{Field: "channels.1.channel", Rule: "oneof"}, domain.FieldError{Field: "channels.2.channel", Rule: "duplicate"},
		domain.FieldError{Field: "channels", Rule: "min_enabled"})
	m = decode(t, f.send(admin, http.MethodPut, path+"/channels", `{"channels":[{"channel":"QRIS","enabled":true}]}`), 200)
	chans := m["connection"].(map[string]any)["channels"].([]any)
	if len(chans) != 14 || chans[0].(map[string]any)["enabled"] != true || chans[1].(map[string]any)["enabled"] != false ||
		chans[1].(map[string]any)["label"] != "BCA Virtual Account" {
		t.Fatalf("channels = %v", chans)
	}

	// Test: the fake Xendit API refuses every key.
	m = decode(t, f.send(admin, http.MethodPost, path+"/test", ""), 200)
	keysOf(t, "test", m, "result", "message", "warnings", "tested_at", "connection")
	c = m["connection"].(map[string]any)
	if w, ok := m["warnings"].([]any); !ok || len(w) != 0 {
		t.Fatalf("a failed test has warnings: %v", m["warnings"])
	}
	if m["result"] != "failed" || m["message"] != "The provider rejected the credentials (HTTP 401)." || c["last_test_status"] != "failed" ||
		c["last_test_error"] != m["message"] || c["last_tested_at"] != m["tested_at"] {
		t.Fatalf("test = %v", m)
	}

	// Disable while an app uses it, then free it, disable and enable.
	app := f.createApp(t, admin, `{"name":"Acme","slug":"acme","environment":"test"}`)
	decode(t, f.send(admin, http.MethodPatch, "/admin/apps/"+app.id, `{"default_connection_id":"`+conID+`"}`), 200)
	env = expectError(t, f.send(admin, http.MethodPost, path+"/disable", ""), 409, domain.CodeConnectionInUse)
	apps := env.Error.Details["apps"].([]any)
	if env.Error.Details["app_count"] != 1.0 || len(apps) != 1 {
		t.Fatalf("details = %v", env.Error.Details)
	}
	keysOf(t, "in-use app", apps[0], "id", "slug", "name", "environment")
	decode(t, f.send(admin, http.MethodPatch, "/admin/apps/"+app.id, `{"default_connection_id":null}`), 200)
	if c := decode(t, f.send(admin, http.MethodPost, path+"/disable", ""), 200)["connection"].(map[string]any); c["status"] != "disabled" {
		t.Fatalf("disabled = %v", c)
	}
	expectFields(t, expectError(t, f.send(admin, http.MethodPatch, "/admin/apps/"+app.id, `{"default_connection_id":"`+conID+`"}`), 400,
		domain.CodeInvalidRequest), domain.FieldError{Field: "default_connection_id", Rule: "status"})
	if c := decode(t, f.send(admin, http.MethodPost, path+"/enable", ""), 200)["connection"].(map[string]any); c["status"] != "active" {
		t.Fatalf("enabled = %v", c)
	}

	want := []string{"connection.create", "connection.update", "connection.test", "connection.update_credentials",
		"connection.update_channels", "connection.test", "app.create", "api_key.create", "app.update", "app.update",
		"connection.disable", "connection.enable"}
	if got := f.audit.actions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("audit = %v\nwant    %v", got, want)
	}
}

// Viewers read, admins and owners write.
func TestConnectionPermissions(t *testing.T) {
	f := newAPI(t, "")
	_, viewer := f.as(t, domain.RoleViewer)
	path := "/admin/connections/" + f.testConn.PublicID
	for _, read := range []string{"/admin/providers", "/admin/connections", path} {
		if w := f.send(viewer, http.MethodGet, read, ""); w.Code != http.StatusOK {
			t.Fatalf("viewer GET %s: %d", read, w.Code)
		}
	}
	writes := [][2]string{{http.MethodPost, "/admin/connections"}, {http.MethodPatch, path}, {http.MethodPatch, path + "/credentials"},
		{http.MethodPut, path + "/channels"}, {http.MethodPost, path + "/test"}, {http.MethodPost, path + "/disable"},
		{http.MethodPost, path + "/enable"}}
	for _, wr := range writes {
		expectError(t, f.send(viewer, wr[0], wr[1], `{}`), 403, domain.CodeForbidden)
	}
	for _, role := range []domain.Role{domain.RoleAdmin, domain.RoleOwner} {
		_, b := f.as(t, role)
		if w := f.send(b, http.MethodPost, path+"/test", ""); w.Code != http.StatusOK {
			t.Fatalf("%s test: %d %s", role, w.Code, w.Body.String())
		}
	}
}

// Credential values never appear in responses, audit entries or logs, in any
// casing or base64-encoded.
func TestConnectionCredentialsNeverLeak(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	secrets := []string{"xnd_development_LeakCheckSecret77", "leak-check-callback-token", "xnd_development_RotatedSecret88",
		"rotated-callback-token-99"}
	var bodies strings.Builder
	record := func(w *httptest.ResponseRecorder, status int) map[string]any {
		bodies.WriteString(w.Body.String())
		return decode(t, w, status)
	}
	m := record(f.send(owner, http.MethodPost, "/admin/connections", `{"provider":"xendit","name":"leak","environment":"test",
		"credentials":{"secret_key":"`+secrets[0]+`","callback_token":"`+secrets[1]+`"}}`), 201)
	path := "/admin/connections/" + m["connection"].(map[string]any)["id"].(string)
	record(f.send(owner, http.MethodPatch, path+"/credentials", `{"credentials":{"secret_key":"`+secrets[2]+`","callback_token":"`+secrets[3]+`"}}`), 200)
	record(f.send(owner, http.MethodPost, path+"/test", ""), 200)
	record(f.send(owner, http.MethodGet, path, ""), 200)
	record(f.send(owner, http.MethodGet, "/admin/connections", ""), 200)
	// A rejected update must not echo the value either.
	bodies.WriteString(f.send(owner, http.MethodPatch, path+"/credentials", `{"credentials":{"secret_key":"`+secrets[0]+`-bad!"}}`).Body.String())

	audit, _ := json.Marshal(f.audit.logs)
	var logs strings.Builder
	for _, e := range f.logs.All() {
		fmt.Fprintf(&logs, "%s %v\n", e.Message, e.ContextMap())
	}
	for _, s := range secrets {
		for _, form := range []string{s, strings.ToLower(s), strings.ToUpper(s), base64.StdEncoding.EncodeToString([]byte(s)),
			base64.RawURLEncoding.EncodeToString([]byte(s))} {
			for name, text := range map[string]string{"responses": bodies.String(), "audit": string(audit), "logs": logs.String()} {
				if strings.Contains(strings.ToLower(text), strings.ToLower(form)) {
					t.Fatalf("%s contain %q", name, form)
				}
			}
		}
	}
	if !strings.Contains(string(audit), `"changed_keys":["callback_token","secret_key"]`) {
		t.Fatalf("audit = %s", audit)
	}
}

var regexpConnID = regexp.MustCompile(`^con_[0-9A-HJKMNP-TV-Z]{26}$`)

// Each connection verifies its own callbacks.
func TestConnectionCallbacks(t *testing.T) {
	f := newAPI(t, "")
	a := f.addXendit(t, "xendit-a", "xnd_development_aaa", "token-of-a")
	b := f.addXendit(t, "xendit-b", "xnd_development_bbb", "token-of-b")
	pa := f.addPayment(t, a, 5000)
	post := func(target, token, body string) *httptest.ResponseRecorder {
		headers := []string{}
		if token != "" {
			headers = append(headers, "x-callback-token", token)
		}
		return f.send(nil, http.MethodPost, target, body, headers...)
	}
	urlA, urlB := "/webhooks/xendit/"+a.PublicID, "/webhooks/xendit/"+b.PublicID

	// B's token at A's URL is refused and logged with A's id.
	expectError(t, post(urlA, "token-of-b", xenditCallbackBody(pa, "inv_a", "PAID")), 401, domain.CodeInvalidCallbackToken)
	expectError(t, post(urlA, "", xenditCallbackBody(pa, "inv_a", "PAID")), 401, domain.CodeInvalidCallbackToken)
	events := f.logs.FilterField(stringField("event", "callback.invalid_token")).All()
	if len(events) != 2 || events[0].ContextMap()["connection_id"] != a.PublicID {
		t.Fatalf("security events = %v", events)
	}
	if f.paymentStatus(t, pa) != domain.PaymentStatusPending {
		t.Fatal("a refused callback was applied")
	}

	// Unknown, malformed and provider-mismatched URLs are 404.
	for _, target := range []string{"/webhooks/xendit/" + id.New(id.Connection), "/webhooks/xendit/con_nope", "/webhooks/xendit/" + id.New(id.App),
		"/webhooks/fake/" + a.PublicID, "/webhooks/midtrans/" + a.PublicID} {
		expectError(t, post(target, "token-of-a", xenditCallbackBody(pa, "inv_a", "PAID")), 404, domain.CodeNotFound)
	}
	if n := f.logs.FilterField(stringField("event", "callback.unknown_connection")).Len(); n != 5 {
		t.Fatalf("unknown connection events = %d", n)
	}

	// A valid token with a body that is not a callback is 400.
	expectError(t, post(urlA, "token-of-a", `{not json`), 400, domain.CodeInvalidRequest)
	expectError(t, post(urlA, "token-of-a", `{"status":"PAID"}`), 400, domain.CodeInvalidRequest)

	// The right token is applied; lower-case ids are accepted.
	w := post(strings.ToLower(urlA), "token-of-a", xenditCallbackBody(pa, "inv_a", "PAID"))
	if w.Code != http.StatusOK || w.Body.String() != `{"received":true}` || f.paymentStatus(t, pa) != domain.PaymentStatusPaid {
		t.Fatalf("valid callback: %d %s", w.Code, w.Body.String())
	}

	// A's payment reported at B's URL with B's token: acknowledged, not applied.
	pa2 := f.addPayment(t, a, 7000)
	w = post(urlB, "token-of-b", xenditCallbackBody(pa2, "inv_a2", "PAID"))
	if w.Code != http.StatusOK || f.paymentStatus(t, pa2) != domain.PaymentStatusPending {
		t.Fatalf("other connection's payment: %d %s", w.Code, w.Body.String())
	}
	mismatch := f.logs.FilterField(stringField("event", "callback.mismatch")).All()
	if len(mismatch) != 1 || mismatch[0].ContextMap()["connection_id"] != b.PublicID || mismatch[0].ContextMap()["payment_connection_db_id"] != a.ID {
		t.Fatalf("mismatch events = %v", mismatch)
	}
	// The same event at A's own URL is still applied, not dropped as a duplicate.
	if w := post(urlA, "token-of-a", xenditCallbackBody(pa2, "inv_a2", "PAID")); w.Code != 200 || f.paymentStatus(t, pa2) != domain.PaymentStatusPaid {
		t.Fatalf("own callback after a mismatch: %d %s", w.Code, w.Body.String())
	}

	// An unknown reference is a 500 so the gateway retries.
	ghost := &domain.Payment{ExternalID: "kp_pay_ghost", Amount: 1}
	expectError(t, post(urlA, "token-of-a", xenditCallbackBody(ghost, "inv_g", "PAID")), 500, domain.CodeInternal)

	// A disabled connection still applies its callbacks.
	pa3 := f.addPayment(t, a, 9000)
	if _, err := f.connSvc.Disable(systemCtx(), a.PublicID); err != nil {
		t.Fatal(err)
	}
	if w := post(urlA, "token-of-a", xenditCallbackBody(pa3, "inv_a3", "PAID")); w.Code != 200 || f.paymentStatus(t, pa3) != domain.PaymentStatusPaid {
		t.Fatalf("disabled connection: %d %s", w.Code, w.Body.String())
	}

	// Another provider's connection verifies with its own scheme.
	pf := f.addPayment(t, f.testConn, 100)
	body := fmt.Sprintf(`{"id":"evt_1","reference":%q,"status":"PAID","amount":100}`, pf.ExternalID)
	expectError(t, f.send(nil, http.MethodPost, "/webhooks/fake/"+f.testConn.PublicID, body, "x-callback-token", "fake-token-test"),
		401, domain.CodeInvalidCallbackToken)
	if w := f.send(nil, http.MethodPost, "/webhooks/fake/"+f.testConn.PublicID, body, providertest.TokenHeader, "fake-token-test"); w.Code != 200 ||
		f.paymentStatus(t, pf) != domain.PaymentStatusPaid {
		t.Fatalf("fake callback: %d %s", w.Code, w.Body.String())
	}
	if w := post(urlA, "token-of-a", "{}"); w.Header().Get("Set-Cookie") != "" {
		t.Fatal("callback response sets cookies")
	}
}

// countingReader reports how many bytes the server read from a request body.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// Callback URLs are public: a body is never read for a URL naming no
// connection, and is capped for one that does.
func TestCallbackBodyLimit(t *testing.T) {
	f := newAPI(t, "")
	a := f.addXendit(t, "xendit-a", "xnd_development_aaa", "token-of-a")
	post := func(target, body string) (*httptest.ResponseRecorder, int) {
		cr := &countingReader{r: strings.NewReader(body)}
		req := httptest.NewRequest(http.MethodPost, target, cr)
		req.Header.Set("x-callback-token", "token-of-a")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w, cr.n
	}
	pad := func(body string, size int) string { return body + strings.Repeat(" ", size-len(body)) }
	huge := strings.Repeat("x", 4<<20)

	for _, target := range []string{"/webhooks/xendit/" + id.New(id.Connection), "/webhooks/xendit/con_nope",
		"/webhooks/midtrans/" + a.PublicID, "/webhooks/xendit"} {
		w, read := post(target, huge)
		expectError(t, w, 404, domain.CodeNotFound)
		if read != 0 {
			t.Fatalf("%s read %d bytes of an unknown connection's body", target, read)
		}
	}

	p := f.addPayment(t, a, 5000)
	w, read := post("/webhooks/xendit/"+a.PublicID, pad(xenditCallbackBody(p, "inv_big", "PAID"), handler.MaxCallbackBytes+1))
	expectError(t, w, http.StatusRequestEntityTooLarge, domain.CodeRequestTooLarge)
	if read > handler.MaxCallbackBytes+1 || f.paymentStatus(t, p) != domain.PaymentStatusPending {
		t.Fatalf("over the limit: read %d bytes, status %s", read, f.paymentStatus(t, p))
	}
	if n := f.logs.FilterField(stringField("event", "webhook.inbound.too_large")).Len(); n != 1 {
		t.Fatalf("too_large log events = %d", n)
	}

	w, _ = post("/webhooks/xendit/"+a.PublicID, pad(xenditCallbackBody(p, "inv_big", "PAID"), handler.MaxCallbackBytes))
	if w.Code != http.StatusOK || f.paymentStatus(t, p) != domain.PaymentStatusPaid {
		t.Fatalf("at the limit: %d %s", w.Code, w.Body.String())
	}
}

// /webhooks/xendit is the imported connection's URL.
func TestLegacyCallbackAlias(t *testing.T) {
	f := newAPI(t, "")
	legacy := f.addPayment(t, nil, 5000)
	body := xenditCallbackBody(legacy, "inv_legacy", "PAID")
	expectError(t, f.send(nil, http.MethodPost, "/webhooks/xendit", body, "x-callback-token", "legacy-token-1"), 404, domain.CodeNotFound)

	res, err := f.connSvc.ImportLegacyXendit(context.Background(), "xnd_development_legacy1", "legacy-token-1")
	if err != nil || !res.Created || res.PaymentsBackfilled == 0 {
		t.Fatalf("import = %+v %v", res, err)
	}
	expectError(t, f.send(nil, http.MethodPost, "/webhooks/xendit", body, "x-callback-token", "token-of-another"), 401, domain.CodeInvalidCallbackToken)
	expectError(t, f.send(nil, http.MethodPost, "/webhooks/xendit", `{not json`, "x-callback-token", "legacy-token-1"), 400, domain.CodeInvalidRequest)
	if w := f.send(nil, http.MethodPost, "/webhooks/xendit", body, "x-callback-token", "legacy-token-1"); w.Code != 200 ||
		f.paymentStatus(t, legacy) != domain.PaymentStatusPaid {
		t.Fatalf("alias: %d %s", w.Code, w.Body.String())
	}
	// The connection's own URL reaches the same connection.
	second := f.addPayment(t, res.Connection, 100)
	if w := f.send(nil, http.MethodPost, "/webhooks/xendit/"+res.Connection.PublicID, xenditCallbackBody(second, "inv_2", "PAID"),
		"x-callback-token", "legacy-token-1"); w.Code != 200 || f.paymentStatus(t, second) != domain.PaymentStatusPaid {
		t.Fatalf("own url: %d %s", w.Code, w.Body.String())
	}
	m := decode(t, f.send(f.signIn(t, f.owner), http.MethodGet, "/admin/connections/"+res.Connection.PublicID, ""), 200)
	if c := m["connection"].(map[string]any); c["legacy_webhook_alias"] != true || c["name"] != "xendit-imported" {
		t.Fatalf("imported = %v", c)
	}
}

// /v1/payments errors keep the envelope; replays win.
func TestV1PaymentsConnectionErrors(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	c := v1Client{f: f, apiKey: app.apiKey, secret: app.apiSecret}
	body := `{"order_id":"ord-1","amount":1000}`
	decode(t, c.do(http.MethodPost, "/v1/payments", body, "Idempotency-Key", "k-1"), 201)

	f.fake.CreateErr = provider.NewStatusError(provider.ErrRejected, 400, "amount below the minimum of xendit")
	env := expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-2","amount":1}`), 422, domain.CodeProviderRejected)
	if env.Error.Details["provider"] != "fake" || strings.Contains(env.Error.Message, "minimum") {
		t.Fatalf("rejected = %+v", env.Error)
	}
	f.fake.CreateErr = fmt.Errorf("dial: %w", provider.ErrRetryable)
	expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-2","amount":1}`), 503, domain.CodeProviderUnavailable)
	f.fake.CreateErr = provider.NewStatusError(provider.ErrUnknownOutcome, 502, "bad gateway")
	expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-2","amount":1}`), 503, domain.CodeProviderUnavailable)
	f.fake.CreateErr = nil

	decode(t, f.send(owner, http.MethodPatch, "/admin/apps/"+app.id, `{"default_connection_id":null}`), 200)
	env = expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-2","amount":1}`), 422, domain.CodeNoEligibleConnection)
	if fmt.Sprint(env.Error.Details["reasons"]) != "[no_default_connection]" {
		t.Fatalf("reasons = %v", env.Error.Details)
	}
	w := c.do(http.MethodPost, "/v1/payments", body, "Idempotency-Key", "k-1")
	if w.Code != http.StatusCreated || w.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay without a connection: %d %s", w.Code, w.Body.String())
	}

	decode(t, f.send(owner, http.MethodPatch, "/admin/apps/"+app.id, `{"default_connection_id":"`+f.testConn.PublicID+`"}`), 200)
	stored, _ := f.conns.GetByPublicID(context.Background(), f.testConn.PublicID)
	stored.Status = domain.ConnectionDisabled
	if err := f.conns.Update(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	env = expectError(t, c.do(http.MethodPost, "/v1/payments", `{"order_id":"ord-2","amount":1}`), 422, domain.CodeNoEligibleConnection)
	if fmt.Sprint(env.Error.Details["reasons"]) != "[connection_disabled]" {
		t.Fatalf("reasons = %v", env.Error.Details)
	}
	if w := c.do(http.MethodPost, "/v1/payments", body); w.Code != 201 || w.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("order replay with a disabled connection: %d", w.Code)
	}
	if n := len(f.payments.All()); n != 1 {
		t.Fatalf("payments = %d", n)
	}
}

// PATCH /admin/apps sets and clears default_connection_id.
func TestAppDefaultConnectionOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	m := decode(t, f.send(owner, http.MethodPost, "/admin/apps", `{"name":"Acme","slug":"acme","environment":"test"}`), 201)
	appID := m["app"].(map[string]any)["id"].(string)
	if m["app"].(map[string]any)["default_connection"] != nil {
		t.Fatalf("new app = %v", m["app"])
	}
	path := "/admin/apps/" + appID
	for body, rule := range map[string]string{
		`{"default_connection_id":"con_nope"}`:                                "public_id",
		`{"default_connection_id":5}`:                                         "type",
		`{"default_connection_id":"` + id.New(id.Connection) + `"}`:           "exists",
		`{"default_connection_id":"` + f.liveConn.PublicID + `"}`:             "environment",
		`{"default_connection_id":"` + strings.ToLower(id.New(id.App)) + `"}`: "public_id",
	} {
		expectFields(t, expectError(t, f.send(owner, http.MethodPatch, path, body), 400, domain.CodeInvalidRequest),
			domain.FieldError{Field: "default_connection_id", Rule: rule})
	}
	m = decode(t, f.send(owner, http.MethodPatch, path, `{"default_connection_id":"`+strings.ToLower(f.testConn.PublicID)+`"}`), 200)
	dc := keysOf(t, "default_connection", m["app"].(map[string]any)["default_connection"], "id", "name", "provider", "environment", "status")
	if dc["id"] != f.testConn.PublicID || dc["name"] != "fake-test" || dc["provider"] != "fake" || dc["status"] != "active" {
		t.Fatalf("default_connection = %v", dc)
	}
	list := decode(t, f.send(owner, http.MethodGet, "/admin/apps", ""), 200)
	if got := list["data"].([]any)[0].(map[string]any)["default_connection"].(map[string]any)["id"]; got != f.testConn.PublicID {
		t.Fatalf("list default = %v", got)
	}
	m = decode(t, f.send(owner, http.MethodPatch, path, `{"default_connection_id":null}`), 200)
	if m["app"].(map[string]any)["default_connection"] != nil {
		t.Fatalf("cleared = %v", m["app"])
	}
	actions := f.audit.actions()
	if n := len(actions); n < 2 || actions[n-1] != "app.update" || actions[n-2] != "app.update" {
		t.Fatalf("audit = %v", actions)
	}
}

// Rejected callbacks are limited per sender and, for failed verification,
// per connection: at the limit they are refused with 429 and not recorded,
// while a verified callback from the same sender still goes through, parsed
// or not.
func TestCallbackRateLimitOverHTTP(t *testing.T) {
	f := newAPI(t, "")
	a := f.addXendit(t, "xendit-a", "xnd_development_aaa", "token-of-a")
	pa := f.addPayment(t, a, 5000)
	target := "/webhooks/xendit/" + a.PublicID
	for i := 0; i < testCallbackLimit-2; i++ {
		f.callbackFailures.Inc("con:"+a.PublicID, 15*time.Minute)
	}
	for i := 0; i < 2; i++ {
		expectError(t, f.send(nil, http.MethodPost, target, `{}`, "x-callback-token", "wrong"), 401, domain.CodeInvalidCallbackToken)
	}
	rows := len(f.hooks.AllInbound())
	w := f.send(nil, http.MethodPost, target, `{}`, "x-callback-token", "wrong")
	expectError(t, w, http.StatusTooManyRequests, domain.CodeRateLimited)
	if w.Header().Get("Retry-After") == "" || len(f.hooks.AllInbound()) != rows {
		t.Fatalf("blocked: Retry-After %q, rows %d -> %d", w.Header().Get("Retry-After"), rows, len(f.hooks.AllInbound()))
	}
	if n := f.logs.FilterField(stringField("event", "callback.rate_limited")).Len(); n != 1 {
		t.Fatalf("rate_limited events = %d", n)
	}
	// A verified callback with an unparsable body is still recorded: the
	// connection's limit only counts failed verifications.
	expectError(t, f.send(nil, http.MethodPost, target, `not json`, "x-callback-token", "token-of-a"), http.StatusBadRequest, domain.CodeInvalidRequest)
	if len(f.hooks.AllInbound()) != rows+1 {
		t.Fatal("the unparsable verified callback was not recorded")
	}
	// Accepted callbacks are never limited.
	w = f.send(nil, http.MethodPost, target, xenditCallbackBody(pa, "inv_ok", "PAID"), "x-callback-token", "token-of-a")
	if w.Code != http.StatusOK || f.paymentStatus(t, pa) != domain.PaymentStatusPaid {
		t.Fatalf("valid callback while blocked: %d %s", w.Code, w.Body.String())
	}
}
