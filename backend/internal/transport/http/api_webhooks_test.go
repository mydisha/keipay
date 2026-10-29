package http

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

var (
	endpointKeys = []string{"id", "app", "url", "description", "events", "api_version", "legacy_signature", "headers",
		"retry_policy", "status", "disabled_reason", "disabled_at", "consecutive_failures", "secret_last4",
		"previous_secret_expires_at", "migrated", "created_at", "updated_at"}
	deliveryKeys = []string{"id", "app", "endpoint", "event", "event_type", "payment_id", "status", "attempts", "max_attempts",
		"next_attempt_at", "last_attempt_at", "last_response_status", "last_error", "created_at", "updated_at"}
	inboundKeys = []string{"id", "connection", "provider", "source", "signature_valid", "result", "result_detail", "event_key",
		"provider_status", "reference", "payment_id", "event", "received_at"}
	whePattern = regexp.MustCompile(`^whe_[0-9A-HJKMNP-TV-Z]{26}$`)
	dlvPattern = regexp.MustCompile(`^dlv_[0-9A-HJKMNP-TV-Z]{26}$`)
	inbPattern = regexp.MustCompile(`^inb_[0-9A-HJKMNP-TV-Z]{26}$`)
)

// The served catalog is the spec's EventType enum, in order.
func TestEventTypesMatchSpec(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Components struct {
			Schemas struct {
				EventType struct {
					Enum []string `yaml:"enum"`
				} `yaml:"EventType"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	f := newAPI(t, "")
	_, viewer := f.as(t, domain.RoleViewer)
	body := decode(t, f.send(viewer, http.MethodGet, "/admin/event-types", ""), http.StatusOK)
	keysOf(t, "catalog", body, "data", "wildcards")
	var got []string
	for _, e := range body["data"].([]any) {
		entry := keysOf(t, "entry", e, "type", "description", "example")
		ex := keysOf(t, "example", entry["example"], "id", "type", "api_version", "created_at", "data")
		if ex["type"] != entry["type"] || ex["api_version"] != "v2" {
			t.Fatalf("example = %v", ex)
		}
		keysOf(t, "data", ex["data"], "id", "order_id", "reference", "status", "amount", "currency", "metadata", "provider",
			"connection_id", "paid_at", "settled_at")
		got = append(got, entry["type"].(string))
	}
	if len(s.Components.Schemas.EventType.Enum) == 0 || !slices.Equal(got, s.Components.Schemas.EventType.Enum) {
		t.Fatalf("catalog %v, spec %v", got, s.Components.Schemas.EventType.Enum)
	}
	w := body["wildcards"].([]any)
	if len(w) != 2 || w[0].(map[string]any)["pattern"] != "payment.*" || w[1].(map[string]any)["pattern"] != "*" {
		t.Fatalf("wildcards = %v", w)
	}
}

// createEndpoint creates an endpoint of app over HTTP and returns the body.
func (f *apiFixture) createEndpoint(t *testing.T, b *browser, appID, body string) map[string]any {
	t.Helper()
	return decode(t, f.send(b, http.MethodPost, "/admin/apps/"+appID+"/webhook-endpoints", body), http.StatusCreated)
}

// Shapes, statuses and the request parsing of every endpoint operation.
func TestWebhookEndpointAPI(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	f.audit.logs = nil

	created := f.createEndpoint(t, owner, app.id, `{"url":"http://127.0.0.1:9/hook","description":"Orders","events":["payment.*"],
		"headers":[{"name":"Authorization","value":"Bearer token-value-9876"}],"retry_policy":{"max_attempts":3}}`)
	keysOf(t, "created", created, "endpoint", "secret")
	secret := created["secret"].(string)
	if !regexp.MustCompile(`^kpwh_[0-9a-f]{48}$`).MatchString(secret) {
		t.Fatalf("secret = %q", secret)
	}
	e := keysOf(t, "endpoint", created["endpoint"], endpointKeys...)
	if !whePattern.MatchString(e["id"].(string)) || e["secret_last4"] != secret[len(secret)-4:] || e["status"] != "active" ||
		e["api_version"] != "v2" || e["disabled_reason"] != nil || e["migrated"] != false || e["previous_secret_expires_at"] != nil {
		t.Fatalf("endpoint = %v", e)
	}
	if fmt.Sprint(e["headers"]) != "[map[name:Authorization value_last4:9876]]" ||
		fmt.Sprint(e["retry_policy"]) != "map[backoff:exponential interval_seconds:30 max_attempts:3]" ||
		e["app"].(map[string]any)["id"] != app.id {
		t.Fatalf("headers/policy/app = %v %v %v", e["headers"], e["retry_policy"], e["app"])
	}
	path := "/admin/webhook-endpoints/" + e["id"].(string)

	got := decode(t, f.send(owner, http.MethodGet, strings.ToLower(path[:len(path)-26])+path[len(path)-26:], ""), http.StatusOK)
	keysOf(t, "get", got, "endpoint")
	list := decode(t, f.send(owner, http.MethodGet, "/admin/apps/"+app.id+"/webhook-endpoints", ""), http.StatusOK)
	keysOf(t, "list", list, "data", "next_cursor")
	if len(list["data"].([]any)) != 1 {
		t.Fatalf("list = %v", list)
	}
	for _, body := range []string{decodeRawBody(t, f.send(owner, http.MethodGet, path, "")),
		decodeRawBody(t, f.send(owner, http.MethodGet, "/admin/apps/"+app.id+"/webhook-endpoints", ""))} {
		if strings.Contains(body, secret) || strings.Contains(body, "token-value") {
			t.Fatalf("read leaks a secret: %s", body)
		}
	}

	// Request parsing.
	expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/webhook-endpoints/"+id.New(id.App), ""), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "id", Rule: "public_id"})
	expectError(t, f.send(owner, http.MethodGet, "/admin/webhook-endpoints/"+id.New(id.WebhookEndpoint), ""), 404, domain.CodeNotFound)
	expectError(t, f.send(owner, http.MethodGet, "/admin/apps/"+id.New(id.App)+"/webhook-endpoints", ""), 404, domain.CodeNotFound)
	expectError(t, f.send(owner, http.MethodPost, "/admin/apps/"+id.New(id.App)+"/webhook-endpoints", `{"url":"https://x.example","events":["*"]}`),
		404, domain.CodeNotFound)
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/webhook-endpoints",
		`{"url":"","events":["payment.nope"],"retry_policy":{"interval_seconds":5}}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "url", Rule: "required"}, domain.FieldError{Field: "events.0", Rule: "oneof"},
		domain.FieldError{Field: "retry_policy.interval_seconds", Rule: "min"})
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, "/admin/apps/"+app.id+"/webhook-endpoints",
		`{"url":"https://x.example","events":["*"],"retry_policy":{"max_attempts":"x"}}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "retry_policy.max_attempts", Rule: "type"})
	for body, want := range map[string][]domain.FieldError{
		`{}`:                             {{Field: "body", Rule: "required"}},
		`{"status":"disabled"}`:          {{Field: "status", Rule: "immutable"}},
		`{"secret":"x","app_id":null}`:   {{Field: "secret", Rule: "immutable"}, {Field: "app_id", Rule: "immutable"}},
		`{"url":null,"events":"*"}`:      {{Field: "url", Rule: "type"}, {Field: "events", Rule: "type"}},
		`{"headers":null}`:               {{Field: "headers", Rule: "type"}},
		`{"retry_policy":{"backoff":1}}`: {{Field: "retry_policy", Rule: "type"}},
		`{"legacy_signature":"yes"}`:     {{Field: "legacy_signature", Rule: "type"}},
	} {
		expectFields(t, expectError(t, f.send(owner, http.MethodPatch, path, body), 400, domain.CodeInvalidRequest), want...)
	}
	if acts := f.audit.actions(); !slices.Equal(acts, []string{"webhook_endpoint.create"}) {
		t.Fatalf("audit = %v", acts)
	}

	patched := decode(t, f.send(owner, http.MethodPatch, path, `{"events":["*"],"api_version":"v1","headers":[]}`), http.StatusOK)
	pe := patched["endpoint"].(map[string]any)
	if fmt.Sprint(pe["events"]) != "[*]" || pe["api_version"] != "v1" || len(pe["headers"].([]any)) != 0 {
		t.Fatalf("patched = %v", pe)
	}

	// Rotate: optional body, overlap rules.
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, path+"/rotate-secret", `{"overlap_hours":"x"}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "overlap_hours", Rule: "type"})
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, path+"/rotate-secret", `{"overlap_hours":1.5}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "overlap_hours", Rule: "type"})
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, path+"/rotate-secret", `{"overlap_hours":169}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "overlap_hours", Rule: "max"})
	rotated := decode(t, f.send(owner, http.MethodPost, path+"/rotate-secret", ""), http.StatusOK)
	keysOf(t, "rotated", rotated, "endpoint", "secret")
	if rotated["secret"] == secret || rotated["endpoint"].(map[string]any)["previous_secret_expires_at"] == nil {
		t.Fatalf("rotated = %v", rotated)
	}
	decode(t, f.send(owner, http.MethodPost, path+"/rotate-secret", `{"overlap_hours":0}`), http.StatusOK)

	// Disable, enable, test.
	if d := decode(t, f.send(owner, http.MethodPost, path+"/disable", ""), 200)["endpoint"].(map[string]any); d["status"] != "disabled" ||
		d["disabled_reason"] != "manual" || d["disabled_at"] == nil {
		t.Fatalf("disabled = %v", d)
	}
	sent := decode(t, f.send(owner, http.MethodPost, path+"/test", `{"event_type":"payment.paid"}`), http.StatusAccepted)
	d := keysOf(t, "test delivery", sent["delivery"], deliveryKeys...)
	if !dlvPattern.MatchString(d["id"].(string)) || d["payment_id"] != nil || d["max_attempts"] != float64(1) ||
		d["event"].(map[string]any)["test"] != true || d["endpoint"].(map[string]any)["id"] != e["id"] || d["status"] != "PENDING" {
		t.Fatalf("test delivery = %v", d)
	}
	if len(f.queue.calls) != 1 || f.queue.calls[0].attempt != 1 {
		t.Fatalf("queued = %+v", f.queue.calls)
	}
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, path+"/test", `{"event_type":"payment.*"}`), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "event_type", Rule: "oneof"})
	if en := decode(t, f.send(owner, http.MethodPost, path+"/enable", ""), 200)["endpoint"].(map[string]any); en["status"] != "active" {
		t.Fatalf("enabled = %v", en)
	}

	// Delete: 204, then gone; its delivery stays readable.
	w := f.send(owner, http.MethodDelete, path, "")
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("delete: %d %q", w.Code, w.Body.String())
	}
	expectError(t, f.send(owner, http.MethodDelete, path, ""), 404, domain.CodeNotFound)
	expectError(t, f.send(owner, http.MethodGet, path, ""), 404, domain.CodeNotFound)
	detail := decode(t, f.send(owner, http.MethodGet, "/admin/webhook-deliveries/"+d["id"].(string), ""), http.StatusOK)
	if detail["delivery"].(map[string]any)["endpoint"].(map[string]any)["deleted"] != true {
		t.Fatalf("delivery of a deleted endpoint = %v", detail)
	}

	want := []string{"webhook_endpoint.create", "webhook_endpoint.update", "webhook_endpoint.rotate_secret", "webhook_endpoint.rotate_secret",
		"webhook_endpoint.disable", "webhook_endpoint.test", "webhook_endpoint.enable", "webhook_endpoint.delete"}
	if acts := f.audit.actions(); !slices.Equal(acts, want) {
		t.Fatalf("audit = %v", acts)
	}
}

// List filters are validated strictly; the detail carries the body and the
// attempt log.
func TestDeliveryAPI(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	e := f.createEndpoint(t, owner, app.id, `{"url":"https://hooks.example.com","events":["*"]}`)["endpoint"].(map[string]any)
	sent := decode(t, f.send(owner, http.MethodPost, "/admin/webhook-endpoints/"+e["id"].(string)+"/test", `{"event_type":"payment.paid"}`), 202)
	dlv := sent["delivery"].(map[string]any)

	for q, want := range map[string]domain.FieldError{
		"app_id=1":                {Field: "app_id", Rule: "public_id"},
		"endpoint_id=" + app.id:   {Field: "endpoint_id", Rule: "public_id"},
		"event_id=evt_x":          {Field: "event_id", Rule: "public_id"},
		"event_type=payment.*":    {Field: "event_type", Rule: "oneof"},
		"status=pending":          {Field: "status", Rule: "oneof"},
		"environment=prod":        {Field: "environment", Rule: "oneof"},
		"from=yesterday":          {Field: "from", Rule: "datetime"},
		"to=2026-01-01":           {Field: "to", Rule: "datetime"},
		"limit=0":                 {Field: "limit", Rule: "range"},
		"event_type=payment.paid": {},
	} {
		w := f.send(owner, http.MethodGet, "/admin/webhook-deliveries?"+q, "")
		if want.Field == "" {
			decode(t, w, http.StatusOK)
			continue
		}
		expectFields(t, expectError(t, w, 400, domain.CodeInvalidRequest), want)
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/webhook-deliveries?cursor=x", ""), 400, domain.CodeInvalidCursor)

	evt := dlv["event"].(map[string]any)["id"].(string)
	for _, q := range []string{"", "app_id=" + strings.ToLower(app.id), "endpoint_id=" + e["id"].(string), "event_id=" + evt,
		"status=PENDING", "environment=test", "event_type=payment.paid", "from=2000-01-01T00:00:00Z"} {
		list := decode(t, f.send(owner, http.MethodGet, "/admin/webhook-deliveries?"+q, ""), http.StatusOK)
		keysOf(t, "list", list, "data", "next_cursor")
		if len(list["data"].([]any)) != 1 {
			t.Fatalf("%s: %v", q, list)
		}
		keysOf(t, "row", list["data"].([]any)[0], deliveryKeys...)
	}
	for _, q := range []string{"app_id=" + id.New(id.App), "environment=live", "status=FAILED", "to=2000-01-01T00:00:00Z"} {
		if list := decode(t, f.send(owner, http.MethodGet, "/admin/webhook-deliveries?"+q, ""), http.StatusOK); len(list["data"].([]any)) != 0 {
			t.Fatalf("%s matched: %v", q, list)
		}
	}

	detail := decode(t, f.send(owner, http.MethodGet, "/admin/webhook-deliveries/"+dlv["id"].(string), ""), http.StatusOK)
	dd := keysOf(t, "detail", detail["delivery"], append(slices.Clone(deliveryKeys), "payload", "attempt_log")...)
	if len(dd["attempt_log"].([]any)) != 0 || dd["payload"].(map[string]any)["type"] != "payment.paid" {
		t.Fatalf("detail = %v", dd)
	}
	expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/webhook-deliveries/3", ""), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "id", Rule: "public_id"})
	expectError(t, f.send(owner, http.MethodGet, "/admin/webhook-deliveries/"+id.New(id.Delivery), ""), 404, domain.CodeNotFound)

	// Replay: 409 while PENDING, 202 once finished; old integer ids are 400.
	f.audit.logs = nil
	env := expectError(t, f.send(owner, http.MethodPost, "/admin/webhook-deliveries/"+dlv["id"].(string)+"/replay", ""), 409,
		domain.CodeDeliveryNotReplayable)
	if env.Error.Details["reason"] != "in_progress" {
		t.Fatalf("details = %v", env.Error.Details)
	}
	expectFields(t, expectError(t, f.send(owner, http.MethodPost, "/admin/webhook-deliveries/3/replay", ""), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "id", Rule: "public_id"})
	finishDelivery(t, f, dlv["id"].(string))
	replayed := decode(t, f.send(owner, http.MethodPost, "/admin/webhook-deliveries/"+dlv["id"].(string)+"/replay", ""), http.StatusAccepted)
	r := keysOf(t, "replayed", replayed["delivery"], deliveryKeys...)
	if r["status"] != "PENDING" || r["max_attempts"] != float64(2) {
		t.Fatalf("replayed = %v", r)
	}
	if len(f.audit.logs) != 1 || f.audit.logs[0].Action != "webhook_delivery.replay" || f.audit.logs[0].Target.ID != dlv["id"] ||
		f.audit.logs[0].Actor.ID != f.owner.PublicID || f.audit.logs[0].Actor.RequestID == "" {
		t.Fatalf("audit = %+v", f.audit.logs)
	}
	last := f.queue.calls[len(f.queue.calls)-1]
	if last.attempt != 2 || !last.manual {
		t.Fatalf("queued = %+v", last)
	}
}

// finishDelivery records one failed attempt of a delivery.
func finishDelivery(t *testing.T, f *apiFixture, publicID string) {
	t.Helper()
	ctx := context.Background()
	d, err := f.hooks.Deliveries().GetByPublicID(ctx, publicID)
	if err != nil {
		t.Fatal(err)
	}
	next := *d
	next.Status, next.Attempts, next.NextAttemptAt = domain.WebhookDeliveryFailed, 1, nil
	status := 500
	a := &domain.DeliveryAttempt{DeliveryID: d.ID, AttemptNo: 1, URL: "https://hooks.example.com", ResponseStatus: &status,
		RequestHeaders: map[string]string{"Authorization": "[REDACTED]"}}
	if ok, err := f.hooks.Deliveries().RecordAttempt(ctx, &next, 0, a); err != nil || !ok {
		t.Fatalf("record: %v %v", ok, err)
	}
}

// Callbacks to known connections are recorded with their result; the log
// reads back with filters and bodies.
func TestInboundWebhookAPI(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	a := f.addXendit(t, "xendit-a", "xnd_development_aaa", "token-of-a")
	p := f.addPayment(t, a, 5000)
	url := "/webhooks/xendit/" + a.PublicID

	big := `{"id":"x","pad":"` + strings.Repeat("z", 10*1024) + `"}`
	expectError(t, f.send(nil, http.MethodPost, url, big, "x-callback-token", "wrong"), 401, domain.CodeInvalidCallbackToken)
	expectError(t, f.send(nil, http.MethodPost, url, "{\"bad\xff", "x-callback-token", "token-of-a"), 400, domain.CodeInvalidRequest)
	expectError(t, f.send(nil, http.MethodPost, "/webhooks/xendit/"+id.New(id.Connection), big, "x-callback-token", "token-of-a"), 404, domain.CodeNotFound)
	if w := f.send(nil, http.MethodPost, url, xenditCallbackBody(p, "inv_1", "PAID"), "x-callback-token", "token-of-a"); w.Code != 200 {
		t.Fatalf("valid: %d %s", w.Code, w.Body.String())
	}
	if w := f.send(nil, http.MethodPost, url, xenditCallbackBody(p, "inv_1", "PAID"), "x-callback-token", "token-of-a"); w.Code != 200 {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body.String())
	}
	if w := f.send(nil, http.MethodPost, url, xenditCallbackBody(p, "inv_1", "REFUNDED"), "x-callback-token", "token-of-a"); w.Code != 200 {
		t.Fatalf("unmapped: %d %s", w.Code, w.Body.String())
	}
	ghost := &domain.Payment{ExternalID: "kp_pay_unknown", Amount: 1000}
	expectError(t, f.send(nil, http.MethodPost, url, xenditCallbackBody(ghost, "inv_e2e_1", "PAID"), "x-callback-token", "token-of-a"),
		500, domain.CodeInternal)

	rows := f.hooks.AllInbound()
	var results []string
	for _, r := range rows {
		results = append(results, string(r.Result))
	}
	if strings.Join(results, ",") != "invalid_signature,invalid_payload,applied,duplicate,unmapped,error" {
		t.Fatalf("results = %v", results)
	}
	if !rows[0].BodyTruncated || len(rows[0].RawBody) != 4096 || rows[0].SignatureValid || rows[0].EventKey != nil {
		t.Fatalf("invalid signature row: truncated=%v len=%d", rows[0].BodyTruncated, len(rows[0].RawBody))
	}

	list := decode(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks?limit=2", ""), http.StatusOK)
	keysOf(t, "list", list, "data", "next_cursor")
	first := keysOf(t, "row", list["data"].([]any)[0], inboundKeys...)
	if !inbPattern.MatchString(first["id"].(string)) || list["next_cursor"] == nil ||
		first["connection"].(map[string]any)["id"] != a.PublicID || first["source"] != "webhook" {
		t.Fatalf("list = %v", list)
	}
	for q, n := range map[string]int{"result=applied": 1, "result=invalid_signature": 1, "connection_id=" + a.PublicID: 6,
		"connection_id=" + id.New(id.Connection): 0, "environment=test": 6, "environment=live": 0,
		"from=2000-01-01T00:00:00Z&to=2999-01-01T00:00:00Z": 6} {
		if got := decode(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks?"+q, ""), 200)["data"].([]any); len(got) != n {
			t.Fatalf("%s: %d rows", q, len(got))
		}
	}
	for q, want := range map[string]domain.FieldError{
		"connection_id=1": {Field: "connection_id", Rule: "public_id"}, "result=ok": {Field: "result", Rule: "oneof"},
		"environment=x": {Field: "environment", Rule: "oneof"}, "from=x": {Field: "from", Rule: "datetime"},
	} {
		expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks?"+q, ""), 400, domain.CodeInvalidRequest), want)
	}

	applied := decode(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks/"+rows[2].PublicID, ""), http.StatusOK)
	ad := keysOf(t, "detail", applied["inbound_webhook"], append(slices.Clone(inboundKeys), "raw_body", "raw_body_truncated")...)
	if ad["result"] != "applied" || ad["payment_id"] != p.PublicID || ad["event"].(map[string]any)["type"] != "payment.paid" ||
		ad["raw_body"] != xenditCallbackBody(p, "inv_1", "PAID") || ad["event_key"] != "inv_1" || ad["provider_status"] != "PAID" {
		t.Fatalf("applied detail = %v", ad)
	}
	bad := decode(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks/"+rows[1].PublicID, ""), http.StatusOK)["inbound_webhook"].(map[string]any)
	if bad["raw_body"] != "{\"bad�" || bad["signature_valid"] != true || bad["event_key"] != nil {
		t.Fatalf("invalid payload detail = %v", bad)
	}
	cut := decode(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks/"+rows[0].PublicID, ""), http.StatusOK)["inbound_webhook"].(map[string]any)
	if cut["raw_body_truncated"] != true || len(cut["raw_body"].(string)) != 4096 {
		t.Fatal("truncated body not reported")
	}
	expectError(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks/"+id.New(id.Inbound), ""), 404, domain.CodeNotFound)
	expectFields(t, expectError(t, f.send(owner, http.MethodGet, "/admin/inbound-webhooks/"+id.New(id.Delivery), ""), 400, domain.CodeInvalidRequest),
		domain.FieldError{Field: "id", Rule: "public_id"})
}

// Viewers read every webhook resource; every endpoint write and replay is 403
// for them, allowed for admins.
func TestWebhookPermissions(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	e := f.createEndpoint(t, owner, app.id, `{"url":"https://hooks.example.com","events":["*"]}`)["endpoint"].(map[string]any)
	path := "/admin/webhook-endpoints/" + e["id"].(string)
	sent := decode(t, f.send(owner, http.MethodPost, path+"/test", `{"event_type":"payment.paid"}`), 202)["delivery"].(map[string]any)
	dlv := "/admin/webhook-deliveries/" + sent["id"].(string)
	finishDelivery(t, f, sent["id"].(string))

	_, viewer := f.as(t, domain.RoleViewer)
	for _, read := range []string{"/admin/event-types", "/admin/apps/" + app.id + "/webhook-endpoints", path, "/admin/webhook-deliveries",
		dlv, "/admin/inbound-webhooks"} {
		if w := f.send(viewer, http.MethodGet, read, ""); w.Code != http.StatusOK {
			t.Fatalf("viewer GET %s: %d", read, w.Code)
		}
	}
	writes := [][2]string{{http.MethodPost, "/admin/apps/" + app.id + "/webhook-endpoints"}, {http.MethodPatch, path},
		{http.MethodDelete, path}, {http.MethodPost, path + "/rotate-secret"}, {http.MethodPost, path + "/disable"},
		{http.MethodPost, path + "/enable"}, {http.MethodPost, path + "/test"}, {http.MethodPost, dlv + "/replay"}}
	for _, wr := range writes {
		expectError(t, f.send(viewer, wr[0], wr[1], `{}`), 403, domain.CodeForbidden)
	}
	_, admin := f.as(t, domain.RoleAdmin)
	decode(t, f.send(admin, http.MethodPost, dlv+"/replay", ""), http.StatusAccepted)
	decode(t, f.send(admin, http.MethodPost, path+"/disable", ""), http.StatusOK)
	expectError(t, f.send(nil, http.MethodGet, "/admin/event-types", ""), 401, domain.CodeSessionRequired)
}
