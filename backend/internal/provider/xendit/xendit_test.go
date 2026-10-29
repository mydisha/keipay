package xendit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

const testKey = "xnd_development_abc123"

var account = provider.Account{
	Environment: domain.EnvTest,
	Credentials: provider.Credentials{"secret_key": testKey, "callback_token": "cb-token-123"},
}

// fakeXendit is a test double of the Xendit API that records requests.
type fakeXendit struct {
	mu       sync.Mutex
	requests []recordedRequest
	status   int
	body     string
	delay    time.Duration
}

type recordedRequest struct {
	method, path, query, user string
	body                      map[string]any
}

func newFakeXendit(t *testing.T, status int, body string) (*fakeXendit, *httptest.Server) {
	t.Helper()
	f := &fakeXendit{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		user, _, _ := r.BasicAuth()
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{r.Method, r.URL.Path, r.URL.RawQuery, user, body})
		status, resp, delay := f.status, f.body, f.delay
		f.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeXendit) last(t *testing.T) recordedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("no request reached the server")
	}
	return f.requests[len(f.requests)-1]
}

const invoiceJSON = `{"id":"inv_123","external_id":"kp_pay_x","user_id":"u","status":"PENDING","merchant_name":"m",
	"merchant_profile_picture_url":"","amount":150000,"expiry_date":"2026-10-30T09:00:00.456Z",
	"invoice_url":"https://checkout.xendit.co/web/inv_123","available_banks":[],"available_retail_outlets":[],
	"available_ewallets":[],"available_qr_codes":[],"available_direct_debits":[],"available_paylaters":[],
	"should_send_email":false,"created":"2026-10-29T09:00:00Z","updated":"2026-10-29T09:00:00Z"}`

func checkout() provider.CheckoutInput {
	return provider.CheckoutInput{
		Reference: "kp_pay_x", Amount: 150000, Currency: "IDR", Description: "Order 1", PayerEmail: "payer@example.com",
		Items:              []domain.OrderItem{{Name: "Paket", Quantity: 2, Price: 75000, Category: "event"}},
		Customer:           &domain.Customer{GivenNames: "Budi", Email: "budi@example.com"},
		SuccessRedirectURL: "https://acme.example/ok", FailureRedirectURL: "https://acme.example/fail",
	}
}

func TestCreateCheckoutRequest(t *testing.T) {
	fx, srv := newFakeXendit(t, http.StatusOK, invoiceJSON)
	a := New(WithBaseURL(srv.URL + "/"))

	out, err := a.CreateCheckout(context.Background(), account, checkout())
	if err != nil {
		t.Fatal(err)
	}
	wantExp := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	if out.ProviderPaymentID != "inv_123" || out.CheckoutURL != "https://checkout.xendit.co/web/inv_123" ||
		out.Status != domain.PaymentStatusPending || out.ExpiresAt == nil || !out.ExpiresAt.Equal(wantExp) || len(out.Raw) == 0 {
		t.Fatalf("out = %+v", out)
	}
	req := fx.last(t)
	if req.method != http.MethodPost || req.path != "/v2/invoices/" || req.user != testKey {
		t.Fatalf("request = %s %s as %q", req.method, req.path, req.user)
	}
	b := req.body
	if b["external_id"] != "kp_pay_x" || b["amount"] != 150000.0 || b["currency"] != "IDR" || b["description"] != "Order 1" ||
		b["payer_email"] != "payer@example.com" || b["success_redirect_url"] != "https://acme.example/ok" ||
		b["failure_redirect_url"] != "https://acme.example/fail" {
		t.Fatalf("body = %v", b)
	}
	items := b["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "Paket" || items[0].(map[string]any)["quantity"] != 2.0 {
		t.Fatalf("items = %v", items)
	}
	if c := b["customer"].(map[string]any); c["given_names"] != "Budi" || c["email"] != "budi@example.com" {
		t.Fatalf("customer = %v", c)
	}
	for _, k := range []string{"payment_methods", "invoice_duration", "locale"} {
		if _, ok := b[k]; ok {
			t.Fatalf("%s sent without being set: %v", k, b)
		}
	}
}

func TestCreateCheckoutChannelsAndSettings(t *testing.T) {
	fx, srv := newFakeXendit(t, http.StatusOK, invoiceJSON)
	a := New(WithBaseURL(srv.URL))
	acc := account
	acc.Settings = provider.Settings{"invoice_duration": json.Number("3600"), "locale": "id"}

	in := checkout()
	in.Channels = []domain.Channel{domain.ChannelQRIS, domain.ChannelVAMandiri, domain.ChannelEwalletShopeePay, domain.ChannelCard, domain.ChannelRetailIndomaret}
	in.ExpiresIn = 15 * time.Minute
	if _, err := a.CreateCheckout(context.Background(), acc, in); err != nil {
		t.Fatal(err)
	}
	b := fx.last(t).body
	if !reflect.DeepEqual(b["payment_methods"], []any{"QRIS", "MANDIRI", "SHOPEEPAY", "CREDIT_CARD", "INDOMARET"}) {
		t.Fatalf("payment_methods = %v", b["payment_methods"])
	}
	if b["invoice_duration"] != "900" || b["locale"] != "id" {
		t.Fatalf("duration/locale = %v / %v, want the request's 900 and the setting's id", b["invoice_duration"], b["locale"])
	}

	// No request lifetime: the setting applies.
	if _, err := a.CreateCheckout(context.Background(), acc, checkout()); err != nil {
		t.Fatal(err)
	}
	if b := fx.last(t).body; b["invoice_duration"] != "3600" {
		t.Fatalf("invoice_duration = %v, want the setting", b["invoice_duration"])
	}
}

func TestEveryChannelHasAMethodCode(t *testing.T) {
	for _, ch := range New().Capabilities().Channels {
		if methodCodes[ch] == "" {
			t.Errorf("%s has no payment_methods code", ch)
		}
	}
	if len(methodCodes) != len(domain.AllChannels()) {
		t.Fatalf("%d codes for %d channels", len(methodCodes), len(domain.AllChannels()))
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		status       int
		create, test error
	}{
		{400, provider.ErrRejected, provider.ErrRejected},
		{401, provider.ErrRejected, provider.ErrRejected},
		{403, provider.ErrRejected, provider.ErrRejected},
		{404, provider.ErrRejected, provider.ErrRejected},
		{409, provider.ErrRejected, provider.ErrRejected},
		{422, provider.ErrRejected, provider.ErrRejected},
		{429, provider.ErrRetryable, provider.ErrRetryable},
		{500, provider.ErrUnknownOutcome, provider.ErrRetryable},
		{502, provider.ErrUnknownOutcome, provider.ErrRetryable},
		{503, provider.ErrUnknownOutcome, provider.ErrRetryable},
	}
	for _, tc := range cases {
		_, srv := newFakeXendit(t, tc.status, `{"error_code":"API_VALIDATION_ERROR","message":"amount must be at least 1000"}`)
		a := New(WithBaseURL(srv.URL))
		_, createErr := a.CreateCheckout(context.Background(), account, checkout())
		testErr := a.TestCredentials(context.Background(), account)
		cancelErr := a.CancelCheckout(context.Background(), account, "inv_1")
		for name, got := range map[string][2]error{"create": {createErr, tc.create}, "test": {testErr, tc.test}, "cancel": {cancelErr, tc.test}} {
			assertClass(t, name, tc.status, got[0], got[1])
			if code, ok := provider.StatusCode(got[0]); !ok || code != tc.status {
				t.Errorf("%s %d: status = %d %v", name, tc.status, code, ok)
			}
		}
	}
}

func assertClass(t *testing.T, name string, status int, err, want error) {
	t.Helper()
	classes := []error{provider.ErrRejected, provider.ErrRetryable, provider.ErrUnknownOutcome, provider.ErrNotFound}
	matched := 0
	for _, c := range classes {
		if errors.Is(err, c) {
			matched++
		}
	}
	if !errors.Is(err, want) || matched != 1 {
		t.Errorf("%s %d: err = %v, want exactly %v", name, status, err, want)
	}
	if err != nil && strings.Contains(err.Error(), testKey) {
		t.Errorf("%s %d: error carries the key: %v", name, status, err)
	}
}

func TestRefusedConnectionIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	a := New(WithBaseURL(url))
	_, err := a.CreateCheckout(context.Background(), account, checkout())
	assertClass(t, "create", 0, err, provider.ErrRetryable)
	assertClass(t, "test", 0, a.TestCredentials(context.Background(), account), provider.ErrRetryable)
}

func TestTimeoutClasses(t *testing.T) {
	fx, srv := newFakeXendit(t, http.StatusOK, invoiceJSON)
	fx.delay = 2 * time.Second
	a := New(WithBaseURL(srv.URL), WithTestTimeout(50*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := a.CreateCheckout(ctx, account, checkout())
	assertClass(t, "create", 0, err, provider.ErrUnknownOutcome)
	assertClass(t, "test", 0, a.TestCredentials(context.Background(), account), provider.ErrRetryable)
}

func TestTestCredentials(t *testing.T) {
	fx, srv := newFakeXendit(t, http.StatusOK, `[]`)
	a := New(WithBaseURL(srv.URL))
	if err := a.TestCredentials(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	req := fx.last(t)
	if req.method != http.MethodGet || req.path != "/v2/invoices" || req.query != "limit=1" || req.user != testKey {
		t.Fatalf("request = %+v", req)
	}
	// A 2xx answer accepts the key even when its body is unexpected.
	fx.body = `{"unexpected":true}`
	if err := a.TestCredentials(context.Background(), account); err != nil {
		t.Fatalf("2xx with another body: %v", err)
	}
}

func TestCancelCheckout(t *testing.T) {
	fx, srv := newFakeXendit(t, http.StatusOK, invoiceJSON)
	if err := New(WithBaseURL(srv.URL)).CancelCheckout(context.Background(), account, "inv_123"); err != nil {
		t.Fatal(err)
	}
	if req := fx.last(t); req.method != http.MethodPost || req.path != "/invoices/inv_123/expire!" {
		t.Fatalf("request = %+v", req)
	}
}

func TestVerifyWebhook(t *testing.T) {
	a := New()
	cases := map[string]string{"different": "cb-token-124", "missing": "", "prefix": "cb-token", "longer": "cb-token-1234",
		"other case": "CB-TOKEN-123"}
	for name, token := range cases {
		h := http.Header{}
		if token != "" {
			h.Set("X-Callback-Token", token)
		}
		if err := a.VerifyWebhook(account, h, nil); !errors.Is(err, provider.ErrInvalidSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	h := http.Header{}
	h.Set("x-callback-token", "cb-token-123")
	if err := a.VerifyWebhook(account, h, nil); err != nil {
		t.Fatalf("valid token: %v", err)
	}
	empty := provider.Account{Credentials: provider.Credentials{}}
	h.Set("x-callback-token", "")
	if err := a.VerifyWebhook(empty, h, nil); !errors.Is(err, provider.ErrInvalidSignature) {
		t.Fatalf("empty stored token: %v", err)
	}
}

func TestParseWebhook(t *testing.T) {
	a := New()
	raw := `{"id":"inv_9","external_id":"kp_pay_x","status":"SETTLED","amount":5000,"currency":"IDR","paid_amount":5000,
		"fees_paid_amount":0,"adjusted_received_amount":4850,"paid_at":"2026-01-02T03:04:05Z","settlement_date":"2026-01-03T00:00:00Z",
		"updated":"2026-01-04T00:00:00Z","merchant_name":"ignored"}`
	ev, err := a.ParseWebhook([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	paid := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	settled := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	fee := int64(150)
	want := provider.InboundEvent{EventKey: "inv_9", Reference: "kp_pay_x", ProviderPaymentID: "inv_9", ProviderStatus: "SETTLED",
		Status: domain.PaymentStatusSettled, Amount: 5000, Currency: "IDR", PaidAmount: 5000, Fee: &fee, PaidAt: &paid,
		SettledAt: &settled, Source: "webhook"}
	if !reflect.DeepEqual(*ev, want) {
		t.Fatalf("event = %+v\nwant    %+v", *ev, want)
	}

	ev, _ = a.ParseWebhook([]byte(`{"id":"i","external_id":"r","status":"SETTLED","updated":"2026-01-04T00:00:00Z"}`))
	if ev.SettledAt == nil || !ev.SettledAt.Equal(time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)) || ev.PaidAt != nil {
		t.Fatalf("fallback = %+v", ev)
	}
	if ev.Fee != nil {
		t.Fatalf("missing fee = %v, want nil", *ev.Fee)
	}
	ev, _ = a.ParseWebhook([]byte(`{"id":"i","external_id":"r","status":"PAID","paid_at":"not a time"}`))
	if ev.PaidAt != nil || ev.SettledAt != nil {
		t.Fatalf("bad times = %+v", ev)
	}

	for _, bad := range []string{`{not json`, `{"external_id":"r"}`, `{"id":"i"}`, `{"id":"i","external_id":"r","amount":"5000"}`, `[]`} {
		if _, err := a.ParseWebhook([]byte(bad)); !errors.Is(err, ErrInvalidCallback) {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}

// Known statuses keep their mapping; anything else is unknown, so a
// callback carrying it is recorded but never applied.
func TestMapStatus(t *testing.T) {
	known := map[string]domain.PaymentStatus{
		"PAID": domain.PaymentStatusPaid, "SETTLED": domain.PaymentStatusSettled, "EXPIRED": domain.PaymentStatusExpired,
		"FAILED": domain.PaymentStatusFailed, "STOPPED": domain.PaymentStatusFailed, "PENDING": domain.PaymentStatusPending,
	}
	for in, want := range known {
		if got, ok := MapStatus(in); got != want || !ok {
			t.Errorf("MapStatus(%q) = %s, %v; want %s, true", in, got, ok, want)
		}
	}
	for _, in := range []string{"SOMETHING_NEW", "", "paid", "PAÏD"} {
		if got, ok := MapStatus(in); got != "" || ok {
			t.Errorf("MapStatus(%q) = %q, %v; want unknown", in, got, ok)
		}
	}
}

// An unknown status parses with an empty Status and the gateway's own
// value kept for the inbound log.
func TestParseWebhookUnknownStatus(t *testing.T) {
	ev, err := New().ParseWebhook([]byte(`{"id":"inv_1","external_id":"kp_pay_x","status":"REFUNDED","amount":5000}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "" || ev.ProviderStatus != "REFUNDED" || ev.EventKey != "inv_1" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestValidateAccount(t *testing.T) {
	a := New()
	cases := []struct {
		key   string
		env   domain.Environment
		valid bool
	}{
		{"xnd_development_abc", domain.EnvTest, true},
		{"xnd_production_abc", domain.EnvLive, true},
		{"xnd_development_abc", domain.EnvLive, false},
		{"xnd_production_abc", domain.EnvTest, false},
		{"other", domain.EnvTest, false},
	}
	for _, tc := range cases {
		errs := a.ValidateAccount(tc.env, provider.Credentials{"secret_key": tc.key})
		if tc.valid != (errs == nil) {
			t.Errorf("%s on %s: %v", tc.key, tc.env, errs)
		}
		if !tc.valid && (len(errs) != 1 || errs[0] != (domain.FieldError{Field: "credentials.secret_key", Rule: "environment"})) {
			t.Errorf("%s on %s: %v", tc.key, tc.env, errs)
		}
	}
	for key, want := range map[string]domain.Environment{"xnd_development_x": domain.EnvTest, "xnd_production_x": domain.EnvLive} {
		if env, ok := EnvironmentForKey(key); !ok || env != want {
			t.Errorf("EnvironmentForKey(%s) = %s %v", key, env, ok)
		}
	}
	if _, ok := EnvironmentForKey("sk_x"); ok {
		t.Error("unknown prefix has an environment")
	}
}

// The CI values must pass the schema: the e2e server imports them.
func TestSchemaAcceptsTheCIValues(t *testing.T) {
	a := New()
	_, errs := provider.ValidateValues(a.CredentialSchema(),
		map[string]any{"secret_key": "xnd_development_ci", "callback_token": "ci-callback-token"}, "credentials")
	if errs != nil {
		t.Fatalf("errors = %v", errs)
	}
	_, errs = provider.ValidateValues(a.CredentialSchema(),
		map[string]any{"secret_key": "xnd_development_a-b", "callback_token": "seven77"}, "credentials")
	if len(errs) != 2 || errs[0].Rule != "pattern" || errs[1].Rule != "min_length" {
		t.Fatalf("errors = %v", errs)
	}
}

func TestBasicAuthIsTheAccountKey(t *testing.T) {
	_, srv := newFakeXendit(t, http.StatusOK, `[]`)
	var seen string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Get("Authorization")
		return http.DefaultTransport.RoundTrip(r)
	})
	other := account
	other.Credentials = provider.Credentials{"secret_key": "xnd_development_other"}
	if err := New(WithBaseURL(srv.URL), WithTransport(rt)).TestCredentials(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if seen != "Basic "+base64.StdEncoding.EncodeToString([]byte("xnd_development_other:")) {
		t.Fatalf("Authorization = %q", seen)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The fee is what Xendit kept, gross minus adjusted_received_amount;
// fees_paid_amount (charged to the customer) never sets it.
func TestParseWebhookFee(t *testing.T) {
	cases := []struct {
		name string
		body string
		want *int64
	}{
		{"paid amount minus received", `"amount":50000,"paid_amount":50000,"adjusted_received_amount":47500`, i64(2500)},
		{"amount when paid amount is missing", `"amount":50000,"adjusted_received_amount":47500`, i64(2500)},
		{"paid amount wins over amount", `"amount":50000,"paid_amount":50100,"adjusted_received_amount":47500`, i64(2600)},
		{"all received is a known zero", `"amount":50000,"paid_amount":50000,"adjusted_received_amount":50000`, i64(0)},
		{"no received amount is unknown", `"amount":50000,"paid_amount":50000`, nil},
		{"null received amount is unknown", `"amount":50000,"paid_amount":50000,"adjusted_received_amount":null`, nil},
		{"customer fee zero is not a merchant fee", `"amount":50000,"paid_amount":50000,"fees_paid_amount":0`, nil},
		{"customer fee is not a merchant fee", `"amount":50000,"paid_amount":50000,"fees_paid_amount":1500`, nil},
		{"more received than paid is passed on", `"amount":50000,"paid_amount":50000,"adjusted_received_amount":50005`, i64(-5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := New().ParseWebhook([]byte(`{"id":"inv_1","external_id":"kp_pay_A","status":"PAID",` + tc.body + `}`))
			if err != nil {
				t.Fatal(err)
			}
			if (ev.Fee == nil) != (tc.want == nil) || (ev.Fee != nil && *ev.Fee != *tc.want) {
				t.Fatalf("fee = %v, want %v", deref(ev.Fee), deref(tc.want))
			}
		})
	}
}

// Regression: an ordinary PAID callback carries fees_paid_amount 0 while
// Xendit keeps its fee. The callback's fee must equal the Transactions API's
// fee for the same payment, or every such payment reconciles as
// fee_mismatch.
func TestCallbackFeeMatchesTransactionFee(t *testing.T) {
	ev, err := New().ParseWebhook([]byte(`{"id":"inv_1","external_id":"kp_pay_A","status":"PAID","currency":"IDR",
		"amount":50000,"paid_amount":50000,"fees_paid_amount":0,"adjusted_received_amount":47500}`))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := normalizeTransaction(transaction{ID: "txn_1", ReferenceID: "kp_pay_A", Status: "SUCCESS", Currency: "IDR",
		Amount: "50000", Fee: &transactionFee{XenditFee: "2275", ValueAddedTax: "225"}})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Fee == nil || tx.Fee == nil || *ev.Fee != *tx.Fee {
		t.Fatalf("callback fee %v, transaction fee %v", deref(ev.Fee), deref(tx.Fee))
	}
}

func i64(v int64) *int64 { return &v }

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
