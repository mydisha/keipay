package doku

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

const notificationPath = "/webhooks/doku/con_01JBQ7X3V4W5Y6Z7A8B9C0D1E2"

// vaNotification is DOKU's virtual account notification sample, for an
// invoice KeiPay created.
const vaNotification = `{"service":{"id":"VIRTUAL_ACCOUNT"},"acquirer":{"id":"BCA"},"channel":{"id":"VIRTUAL_ACCOUNT_BCA"},
	"transaction":{"status":"SUCCESS","date":"2026-10-31T03:24:23Z","original_request_id":"15022aab"},
	"order":{"invoice_number":"KP` + testULID + `","amount":150000},
	"virtual_account_info":{"virtual_account_number":"1900600000000046"}}`

func signedHeader(body []byte, target string) http.Header {
	h := http.Header{}
	h.Set("Client-Id", testClientID)
	h.Set("Request-Id", "req-123")
	h.Set("Request-Timestamp", "2026-10-31T03:24:24Z")
	h.Set("Signature", expectedSignature(testClientID, "req-123", "2026-10-31T03:24:24Z", target, body))
	return h
}

func TestVerifyWebhook(t *testing.T) {
	a := New()
	body := []byte(vaNotification)
	ok := provider.CallbackRequest{Path: notificationPath, Header: signedHeader(body, notificationPath), Body: body}
	if err := a.VerifyWebhook(account, ok); err != nil {
		t.Fatalf("valid notification: %v", err)
	}

	mutate := map[string]func(r *provider.CallbackRequest){
		"other path":      func(r *provider.CallbackRequest) { r.Path = "/webhooks/doku/con_other" },
		"other body":      func(r *provider.CallbackRequest) { r.Body = []byte(`{"order":{"amount":1}}`) },
		"other client id": func(r *provider.CallbackRequest) { r.Header.Set("Client-Id", "BRN-0001-9999999999") },
		"other request":   func(r *provider.CallbackRequest) { r.Header.Set("Request-Id", "req-124") },
		"other timestamp": func(r *provider.CallbackRequest) { r.Header.Set("Request-Timestamp", "2026-10-31T03:24:25Z") },
		"no signature":    func(r *provider.CallbackRequest) { r.Header.Del("Signature") },
		"no request id":   func(r *provider.CallbackRequest) { r.Header.Del("Request-Id") },
		"no timestamp":    func(r *provider.CallbackRequest) { r.Header.Del("Request-Timestamp") },
		"bare signature": func(r *provider.CallbackRequest) {
			r.Header.Set("Signature", r.Header.Get("Signature")[len("HMACSHA256="):])
		},
	}
	for name, m := range mutate {
		r := ok
		r.Header = ok.Header.Clone()
		m(&r)
		if err := a.VerifyWebhook(account, r); !errors.Is(err, provider.ErrInvalidSignature) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	other := account
	other.Credentials = provider.Credentials{keyClientID: testClientID, keySecretKey: "SK-another-secret"}
	if err := a.VerifyWebhook(other, ok); !errors.Is(err, provider.ErrInvalidSignature) {
		t.Errorf("other secret: err = %v", err)
	}
	empty := account
	empty.Credentials = provider.Credentials{}
	if err := a.VerifyWebhook(empty, ok); !errors.Is(err, provider.ErrInvalidSignature) {
		t.Errorf("no stored credentials: err = %v", err)
	}
}

func TestParseWebhook(t *testing.T) {
	ev, err := New().ParseWebhook([]byte(vaNotification))
	if err != nil {
		t.Fatal(err)
	}
	paidAt := time.Date(2026, 10, 31, 3, 24, 23, 0, time.UTC)
	if ev.EventKey != "KP"+testULID || ev.Reference != "kp_pay_"+testULID || ev.ProviderPaymentID != "KP"+testULID ||
		ev.ProviderStatus != "SUCCESS" || ev.Status != domain.PaymentStatusPaid || ev.Amount != 150000 || ev.PaidAmount != 150000 ||
		ev.Currency != "" || ev.Fee != nil || ev.PaidAt == nil || !ev.PaidAt.Equal(paidAt) || ev.Source != domain.InboundSourceWebhook {
		t.Fatalf("event = %+v", ev)
	}
}

func TestParseWebhookAmounts(t *testing.T) {
	cases := map[string]int64{`150000`: 150000, `"150000"`: 150000, `"150000.00"`: 150000, `null`: 0,
		// Rounded half away from zero, like every gateway amount.
		`"150000.5"`: 150001}
	for raw, want := range cases {
		ev, err := New().ParseWebhook([]byte(`{"order":{"invoice_number":"INV-1","amount":` + raw + `},"transaction":{"status":"PENDING"}}`))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if ev.Amount != want || ev.PaidAmount != 0 {
			t.Errorf("%s: amount = %d, paid = %d", raw, ev.Amount, ev.PaidAmount)
		}
	}
	for _, bad := range []string{`"abc"`, `true`, `{}`, `"1e400"`} {
		_, err := New().ParseWebhook([]byte(`{"order":{"invoice_number":"INV-1","amount":` + bad + `},"transaction":{"status":"SUCCESS"}}`))
		if !errors.Is(err, ErrInvalidCallback) {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}

func TestParseWebhookRejects(t *testing.T) {
	for _, bad := range []string{
		``, `[]`, `{}`,
		`{"order":{"amount":1},"transaction":{"status":"SUCCESS"}}`,
		`{"order":{"invoice_number":"INV-1"},"transaction":{}}`,
	} {
		if _, err := New().ParseWebhook([]byte(bad)); !errors.Is(err, ErrInvalidCallback) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestParseWebhookExpiredOrder(t *testing.T) {
	ev, err := New().ParseWebhook([]byte(`{"order":{"invoice_number":"INV-1","amount":"5000","status":"ORDER_EXPIRED"},
		"transaction":{"status":"PENDING"},"channel":{"id":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.ProviderStatus != "ORDER_EXPIRED" || ev.Status != domain.PaymentStatusExpired || ev.Reference != "INV-1" {
		t.Fatalf("event = %+v", ev)
	}
	ev, err = New().ParseWebhook([]byte(`{"order":{"invoice_number":"INV-1","status":"ORDER_GENERATED"},"transaction":{"status":"PENDING"}}`))
	if err != nil || ev.ProviderStatus != "PENDING" || ev.Status != domain.PaymentStatusPending {
		t.Fatalf("generated order: %+v, %v", ev, err)
	}
}

func TestMapStatus(t *testing.T) {
	cases := map[string]struct {
		want  domain.PaymentStatus
		known bool
	}{
		"PENDING": {domain.PaymentStatusPending, true}, "TIMEOUT": {domain.PaymentStatusPending, true},
		"REDIRECT": {domain.PaymentStatusPending, true}, "ORDER_GENERATED": {domain.PaymentStatusPending, true},
		"ORDER_RECOVERED": {domain.PaymentStatusPending, true}, "SUCCESS": {domain.PaymentStatusPaid, true},
		"FAILED": {domain.PaymentStatusFailed, true}, "CANCELLED": {domain.PaymentStatusFailed, true},
		"EXPIRED": {domain.PaymentStatusExpired, true}, "ORDER_EXPIRED": {domain.PaymentStatusExpired, true},
		"REFUNDED": {"", true}, "PARTIAL_REFUNDED": {"", true}, "VOIDED": {"", true},
		"success": {"", false}, "SETTLED": {"", false}, "": {"", false},
	}
	for s, tc := range cases {
		got, known := MapStatus(s)
		if got != tc.want || known != tc.known {
			t.Errorf("MapStatus(%q) = %q, %v; want %q, %v", s, got, known, tc.want, tc.known)
		}
	}
}

func TestParseWebhookUnknownStatus(t *testing.T) {
	ev, err := New().ParseWebhook([]byte(`{"order":{"invoice_number":"INV-1","amount":5000},"transaction":{"status":"REFUNDED"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != "" || ev.ProviderStatus != "REFUNDED" || ev.PaidAt != nil {
		t.Fatalf("event = %+v", ev)
	}
}

func TestGetCheckoutReadsTheStatus(t *testing.T) {
	fx, srv := newFakeDOKU(t, http.StatusOK, `{"order":{"invoice_number":"KP`+testULID+`","amount":"150000"},
		"transaction":{"status":"EXPIRED","date":"2026-10-31T03:00:00Z","original_request_id":"x"},
		"service":{"id":"VIRTUAL_ACCOUNT"},"channel":{"id":"VIRTUAL_ACCOUNT_BNI"}}`)
	ev, raw, err := newAdapter(srv).GetCheckout(context.Background(), account, "KP"+testULID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != domain.PaymentStatusExpired || ev.Reference != "kp_pay_"+testULID || ev.Amount != 150000 ||
		ev.Source != domain.InboundSourcePoll || len(raw) == 0 {
		t.Fatalf("event = %+v", ev)
	}
	if req := fx.last(t); req.method != http.MethodGet || req.path != "/orders/v1/status/KP"+testULID {
		t.Fatalf("request = %s %s", req.method, req.path)
	}
}

func TestGetCheckoutEscapesTheInvoice(t *testing.T) {
	fx, srv := newFakeDOKU(t, http.StatusNotFound, `{}`)
	_, _, err := newAdapter(srv).GetCheckout(context.Background(), account, "acme:order/1")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	req := fx.last(t)
	if req.path != "/orders/v1/status/acme:order%2F1" {
		t.Fatalf("path = %s", req.path)
	}
	// The escaped path is what was signed.
	want := expectedSignature(testClientID, req.header.Get("Request-Id"), "2026-10-31T02:03:04Z", req.path, nil)
	if req.header.Get("Signature") != want {
		t.Fatal("signature does not cover the escaped path")
	}
}

func TestGetCheckoutUnreadableAnswerIsRetryable(t *testing.T) {
	_, srv := newFakeDOKU(t, http.StatusOK, `{"order":{}}`)
	if _, _, err := newAdapter(srv).GetCheckout(context.Background(), account, "INV-1"); !errors.Is(err, provider.ErrRetryable) {
		t.Fatalf("err = %v", err)
	}
}

func TestReferenceRoundTrip(t *testing.T) {
	cases := []struct{ ref, invoice string }{
		{"kp_pay_" + testULID, "KP" + testULID},
		// Not a ULID: sent as it is.
		{"kp_pay_short", "kp_pay_short"},
		{"kp_pay_" + "01jbq7x3v4w5y6z7a8b9c0d1e2", "kp_pay_01jbq7x3v4w5y6z7a8b9c0d1e2"},
		{"acme:order-1", "acme:order-1"},
	}
	for _, tc := range cases {
		inv, err := invoiceNumber(tc.ref)
		if err != nil || inv != tc.invoice {
			t.Errorf("invoiceNumber(%q) = %q, %v; want %q", tc.ref, inv, err, tc.invoice)
		}
		if got := reference(inv); got != tc.ref {
			t.Errorf("reference(%q) = %q, want %q", inv, got, tc.ref)
		}
	}
	if inv, _ := invoiceNumber("kp_pay_" + testULID); len(inv) > 30 {
		t.Fatalf("invoice %q exceeds DOKU's card limit", inv)
	}
	if _, err := invoiceNumber(""); !errors.Is(err, provider.ErrRejected) {
		t.Fatalf("empty reference: err = %v", err)
	}
	if inv, err := invoiceNumber(strings.Repeat("r", 64)); err != nil || len(inv) != 64 {
		t.Fatalf("64 characters: %v", err)
	}
	// Unicode counts in bytes, so a reference never exceeds DOKU's limit.
	if inv, err := invoiceNumber("pesanan-ñ"); err != nil || inv != "pesanan-ñ" {
		t.Fatalf("unicode within limit: %q, %v", inv, err)
	}
	if _, err := invoiceNumber(strings.Repeat("é", 33)); !errors.Is(err, provider.ErrRejected) {
		t.Fatalf("66-byte unicode reference: err = %v", err)
	}
}
