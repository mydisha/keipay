package doku

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

const (
	testClientID = "BRN-0001-1234567890"
	testSecret   = "SK-test-secret-0123"
	testULID     = "01JBQ7X3V4W5Y6Z7A8B9C0D1E2"
)

var (
	account = provider.Account{
		Environment: domain.EnvTest,
		Credentials: provider.Credentials{keyClientID: testClientID, keySecretKey: testSecret},
	}
	fixedNow = time.Date(2026, 10, 31, 2, 3, 4, 567, time.UTC)
)

// fakeDOKU is a test double of the DOKU API that records requests.
type fakeDOKU struct {
	mu       sync.Mutex
	requests []recordedRequest
	status   int
	body     string
}

type recordedRequest struct {
	method, path string
	header       http.Header
	raw          []byte
	body         map[string]any
}

func newFakeDOKU(t *testing.T, status int, body string) (*fakeDOKU, *httptest.Server) {
	t.Helper()
	f := &fakeDOKU{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{r.Method, r.URL.EscapedPath(), r.Header.Clone(), raw, body})
		status, resp := f.status, f.body
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeDOKU) last(t *testing.T) recordedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("no request reached the server")
	}
	return f.requests[len(f.requests)-1]
}

func newAdapter(srv *httptest.Server) *Adapter {
	return New(WithBaseURL(srv.URL+"/"), WithClock(func() time.Time { return fixedNow }))
}

// checkoutJSON is the create-payment answer from DOKU's documentation.
const checkoutJSON = `{"message":["SUCCESS"],"response":{"order":{"amount":"150000","invoice_number":"KP` + testULID + `",
	"currency":"IDR","session_id":"abc"},"payment":{"payment_method_types":["VIRTUAL_ACCOUNT_BCA","QRIS"],
	"payment_due_date":60,"token_id":"tok","url":"https://sandbox.doku.com/checkout-link-v2/tok",
	"expired_date":"20261031100304"}}}`

func checkout() provider.CheckoutInput {
	return provider.CheckoutInput{
		Reference: "kp_pay_" + testULID, Amount: 150000, Currency: "IDR", Description: "Order 1", PayerEmail: "payer@example.com",
		Items:              []domain.OrderItem{{Name: "Paket", Quantity: 2, Price: 75000, Category: "event", ReferenceID: "sku-1"}},
		Customer:           &domain.Customer{GivenNames: "Budi", Surname: "Santoso", Email: "budi@example.com", MobileNumber: "+62 812-3456-789"},
		SuccessRedirectURL: "https://acme.example/ok", FailureRedirectURL: "https://acme.example/fail",
	}
}

// expectedSignature computes a non-SNAP signature independently of the
// adapter, from DOKU's documented recipe.
func expectedSignature(clientID, requestID, ts, target string, body []byte) string {
	lines := []string{"Client-Id:" + clientID, "Request-Id:" + requestID, "Request-Timestamp:" + ts, "Request-Target:" + target}
	if body != nil {
		sum := sha256.Sum256(body)
		lines = append(lines, "Digest:"+base64.StdEncoding.EncodeToString(sum[:]))
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(strings.Join(lines, "\n")))
	return "HMACSHA256=" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestCreateCheckoutRequest(t *testing.T) {
	fx, srv := newFakeDOKU(t, http.StatusOK, checkoutJSON)
	out, err := newAdapter(srv).CreateCheckout(context.Background(), account, checkout())
	if err != nil {
		t.Fatal(err)
	}
	wantExp := time.Date(2026, 10, 31, 3, 3, 4, 0, time.UTC) // 10:03:04 WIB
	if out.ProviderPaymentID != "KP"+testULID || out.CheckoutURL != "https://sandbox.doku.com/checkout-link-v2/tok" ||
		out.Status != domain.PaymentStatusPending || out.ExpiresAt == nil || !out.ExpiresAt.Equal(wantExp) || string(out.Raw) != checkoutJSON {
		t.Fatalf("out = %+v", out)
	}

	req := fx.last(t)
	if req.method != http.MethodPost || req.path != "/checkout/v1/payment" {
		t.Fatalf("request = %s %s", req.method, req.path)
	}
	h := req.header
	if h.Get("Client-Id") != testClientID || h.Get("Request-Timestamp") != "2026-10-31T02:03:04Z" ||
		len(h.Get("Request-Id")) == 0 || h.Get("Content-Type") != "application/json" {
		t.Fatalf("headers = %v", h)
	}
	want := expectedSignature(testClientID, h.Get("Request-Id"), "2026-10-31T02:03:04Z", "/checkout/v1/payment", req.raw)
	if h.Get("Signature") != want {
		t.Fatalf("signature = %q, want %q", h.Get("Signature"), want)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(`{
		"order":{"amount":150000,"invoice_number":"KP`+testULID+`","currency":"IDR",
			"callback_url":"https://acme.example/ok","callback_url_result":"https://acme.example/ok",
			"callback_url_cancel":"https://acme.example/fail","auto_redirect":true,
			"line_items":[{"id":"sku-1","name":"Paket","price":75000,"quantity":2,"category":"event"}]},
		"payment":{},
		"customer":{"name":"Budi Santoso","email":"budi@example.com","phone":"628123456789"}}`), &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req.body, body) {
		t.Fatalf("body = %v\nwant %v", req.body, body)
	}
}

func TestCreateCheckoutChannelsAndSettings(t *testing.T) {
	fx, srv := newFakeDOKU(t, http.StatusOK, checkoutJSON)
	a := newAdapter(srv)
	in := checkout()
	in.Channels = []domain.Channel{domain.ChannelVABCA, domain.ChannelEwalletShopeePay, domain.ChannelRetailAlfamart}
	in.Items[0].Price = 1 // items no longer add up to the amount
	in.Customer = nil
	in.PayerEmail = ""
	in.SuccessRedirectURL = ""
	acc := account
	acc.Settings = provider.Settings{settingPaymentDueDate: int64(30), settingLanguage: "EN", settingAutoRedirect: true}
	if _, err := a.CreateCheckout(context.Background(), acc, in); err != nil {
		t.Fatal(err)
	}
	body := fx.last(t).body
	order, payment := body["order"].(map[string]any), body["payment"].(map[string]any)
	methods := payment["payment_method_types"].([]any)
	if len(methods) != 3 || methods[0] != "VIRTUAL_ACCOUNT_BCA" || methods[1] != "EMONEY_SHOPEE_PAY" || methods[2] != "ONLINE_TO_OFFLINE_ALFA" {
		t.Fatalf("methods = %v", methods)
	}
	if payment["payment_due_date"] != float64(30) || order["language"] != "EN" {
		t.Fatalf("settings not sent: %v", body)
	}
	if _, ok := order["line_items"]; ok {
		t.Fatalf("line items that do not add up were sent: %v", order)
	}
	if _, ok := body["customer"]; ok {
		t.Fatalf("empty customer was sent: %v", body)
	}
	// Without a page to return to, DOKU is not asked to redirect.
	if order["auto_redirect"] != false {
		t.Fatalf("auto_redirect = %v", order["auto_redirect"])
	}

	// The request's lifetime wins over the setting, rounded up to minutes.
	in.ExpiresIn = 90 * time.Second
	if _, err := a.CreateCheckout(context.Background(), acc, in); err != nil {
		t.Fatal(err)
	}
	if got := fx.last(t).body["payment"].(map[string]any)["payment_due_date"]; got != float64(2) {
		t.Fatalf("payment_due_date = %v", got)
	}
}

func TestCreateCheckoutAutoRedirectOff(t *testing.T) {
	fx, srv := newFakeDOKU(t, http.StatusOK, checkoutJSON)
	acc := account
	acc.Settings = provider.Settings{settingAutoRedirect: false}
	if _, err := newAdapter(srv).CreateCheckout(context.Background(), acc, checkout()); err != nil {
		t.Fatal(err)
	}
	if got := fx.last(t).body["order"].(map[string]any)["auto_redirect"]; got != false {
		t.Fatalf("auto_redirect = %v", got)
	}
}

func TestDueMinutes(t *testing.T) {
	cases := []struct {
		settings provider.Settings
		in       time.Duration
		want     int64
	}{
		{nil, 0, 0},
		{nil, time.Minute, 1},
		{nil, time.Minute + time.Nanosecond, 2},
		{nil, 59 * time.Second, 1},
		{provider.Settings{settingPaymentDueDate: int64(45)}, 0, 45},
		{provider.Settings{settingPaymentDueDate: json.Number("45")}, 0, 45},
		{provider.Settings{settingPaymentDueDate: int64(45)}, 10 * time.Minute, 10},
		{nil, 20000 * time.Hour, maxDueMinutes},
	}
	for _, tc := range cases {
		if got := dueMinutes(tc.settings, tc.in); got != tc.want {
			t.Errorf("dueMinutes(%v, %v) = %d, want %d", tc.settings, tc.in, got, tc.want)
		}
	}
}

func TestCreateCheckoutRefusesBeforeCalling(t *testing.T) {
	fx, srv := newFakeDOKU(t, http.StatusOK, checkoutJSON)
	a := newAdapter(srv)
	in := checkout()
	in.Currency = "USD"
	if _, err := a.CreateCheckout(context.Background(), account, in); !errors.Is(err, provider.ErrRejected) {
		t.Fatalf("other currency: err = %v", err)
	}
	in = checkout()
	in.Reference = strings.Repeat("r", 65)
	if _, err := a.CreateCheckout(context.Background(), account, in); !errors.Is(err, provider.ErrRejected) {
		t.Fatalf("long reference: err = %v", err)
	}
	if len(fx.requests) != 0 {
		t.Fatalf("%d requests reached DOKU", len(fx.requests))
	}
}

func TestCreateCheckoutWithoutURLIsUnknown(t *testing.T) {
	_, srv := newFakeDOKU(t, http.StatusOK, `{"message":["SUCCESS"],"response":{}}`)
	_, err := newAdapter(srv).CreateCheckout(context.Background(), account, checkout())
	if !errors.Is(err, provider.ErrUnknownOutcome) {
		t.Fatalf("err = %v", err)
	}
}

func TestEveryChannelHasAMethodCode(t *testing.T) {
	for _, ch := range New().Capabilities().Channels {
		if methodCodes[ch] == "" {
			t.Errorf("channel %s has no DOKU method code", ch)
		}
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		status       int
		body         string
		create, read error
		message      string
	}{
		{400, `{"error_messages":["order.amount must greater than 0","order.invoice_number must be filled"]}`,
			provider.ErrRejected, provider.ErrRejected, "order.amount must greater than 0; order.invoice_number must be filled"},
		{401, `{"error":{"code":"INVALID_SIGNATURE","message":"Invalid Header Signature","type":"Invalid Header"}}`,
			provider.ErrRejected, provider.ErrRejected, "INVALID_SIGNATURE Invalid Header Signature"},
		{429, `{"responseCode":"4290000","responseMessage":"Too Many Requests"}`,
			provider.ErrRetryable, provider.ErrRetryable, "Too Many Requests"},
		{500, `oops`, provider.ErrUnknownOutcome, provider.ErrRetryable, "oops"},
		{503, ``, provider.ErrUnknownOutcome, provider.ErrRetryable, ""},
	}
	for _, tc := range cases {
		_, srv := newFakeDOKU(t, tc.status, tc.body)
		a := newAdapter(srv)
		_, err := a.CreateCheckout(context.Background(), account, checkout())
		assertClass(t, "create", tc.status, err, tc.create)
		if !strings.Contains(err.Error(), tc.message) {
			t.Errorf("create %d: message %q lacks %q", tc.status, err, tc.message)
		}
		_, _, err = a.GetCheckout(context.Background(), account, "KP"+testULID)
		assertClass(t, "read", tc.status, err, tc.read)
		assertClass(t, "cancel", tc.status, a.CancelCheckout(context.Background(), account, "KP"+testULID), tc.read)
	}
}

func assertClass(t *testing.T, name string, status int, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s %d: err = %v, want %v", name, status, err, want)
	}
	if got, ok := provider.StatusCode(err); !ok || got != status {
		t.Errorf("%s %d: status = %d, %v", name, status, got, ok)
	}
}

func TestErrorMessageIsBounded(t *testing.T) {
	if got := errorMessage([]byte(strings.Repeat("x", 5000))); len(got) != maxErrorBody {
		t.Fatalf("len = %d", len(got))
	}
}

func TestRefusedConnectionIsRetryable(t *testing.T) {
	a := New(WithBaseURL("http://127.0.0.1:1"))
	if _, err := a.CreateCheckout(context.Background(), account, checkout()); !errors.Is(err, provider.ErrRetryable) {
		t.Fatalf("create: err = %v", err)
	}
	if _, _, err := a.GetCheckout(context.Background(), account, "KPX"); !errors.Is(err, provider.ErrRetryable) {
		t.Fatalf("read: err = %v", err)
	}
}

func TestSentButUnansweredCreateIsUnknown(t *testing.T) {
	a := New(WithTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})))
	if _, err := a.CreateCheckout(context.Background(), account, checkout()); !errors.Is(err, provider.ErrUnknownOutcome) {
		t.Fatalf("err = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHostFollowsTheEnvironment(t *testing.T) {
	var hosts []string
	a := New(WithTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Scheme+"://"+r.URL.Host)
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})))
	live := account
	live.Environment = domain.EnvLive
	for _, acc := range []provider.Account{account, live} {
		if err := a.TestCredentials(context.Background(), acc); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(hosts, []string{"https://api-sandbox.doku.com", "https://api.doku.com"}) {
		t.Fatalf("hosts = %v", hosts)
	}
}

func TestTestCredentials(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		fx, srv := newFakeDOKU(t, status, `{}`)
		if err := newAdapter(srv).TestCredentials(context.Background(), account); err != nil {
			t.Fatalf("%d: %v", status, err)
		}
		req := fx.last(t)
		if req.method != http.MethodGet || !strings.HasPrefix(req.path, "/orders/v1/status/KP") {
			t.Fatalf("request = %s %s", req.method, req.path)
		}
		// A GET is signed without a Digest.
		want := expectedSignature(testClientID, req.header.Get("Request-Id"), "2026-10-31T02:03:04Z", req.path, nil)
		if req.header.Get("Signature") != want || len(req.raw) != 0 {
			t.Fatalf("signature = %q, want %q", req.header.Get("Signature"), want)
		}
	}
	_, srv := newFakeDOKU(t, http.StatusUnauthorized, `{"error":{"code":"INVALID_CLIENT_ID","message":"Invalid Client-Id"}}`)
	err := newAdapter(srv).TestCredentials(context.Background(), account)
	assertClass(t, "test", http.StatusUnauthorized, err, provider.ErrRejected)
}

func TestCancelCheckout(t *testing.T) {
	fx, srv := newFakeDOKU(t, http.StatusOK, `{}`)
	if err := newAdapter(srv).CancelCheckout(context.Background(), account, "KP"+testULID); err != nil {
		t.Fatal(err)
	}
	req := fx.last(t)
	if req.method != http.MethodPost || req.path != "/checkout/v3/cancellations" ||
		req.body["order"].(map[string]any)["invoice_number"] != "KP"+testULID {
		t.Fatalf("request = %s %s %v", req.method, req.path, req.body)
	}
}

func TestSchemaAcceptsTheSandboxValues(t *testing.T) {
	a := New()
	_, errs := provider.ValidateValues(a.CredentialSchema(),
		map[string]any{keyClientID: testClientID, keySecretKey: testSecret}, "credentials")
	if errs != nil {
		t.Fatalf("errors = %v", errs)
	}
	_, errs = provider.ValidateValues(a.CredentialSchema(),
		map[string]any{keyClientID: "BRN 0001", keySecretKey: "short"}, "credentials")
	if len(errs) != 2 || errs[0].Rule != "pattern" || errs[1].Rule != "min_length" {
		t.Fatalf("errors = %v", errs)
	}
	settings, errs := provider.ValidateValues(a.SettingsSchema(), map[string]any{}, "settings")
	if errs != nil || settings[settingAutoRedirect] != true {
		t.Fatalf("settings = %v, errors = %v", settings, errs)
	}
	if a.ValidateAccount(domain.EnvLive, nil) != nil {
		t.Fatal("ValidateAccount has rules")
	}
}

func TestRegistryAcceptsTheAdapter(t *testing.T) {
	if _, err := provider.NewRegistry(New()); err != nil {
		t.Fatal(err)
	}
}
