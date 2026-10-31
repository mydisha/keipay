package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

const (
	dokuClientID = "BRN-0001-1234567890"
	dokuSecret   = "SK-api-test-secret"
)

func (f *apiFixture) addDOKU(t *testing.T, name string) *domain.Connection {
	t.Helper()
	d, err := f.connSvc.Create(systemCtx(), service.CreateConnectionInput{Provider: "doku", Name: name, Environment: domain.EnvTest,
		Credentials: map[string]any{"client_id": dokuClientID, "secret_key": dokuSecret}})
	if err != nil {
		t.Fatal(err)
	}
	return d.Connection
}

// dokuNotification is a DOKU HTTP notification for p with its signed
// headers, as DOKU sends it to target. The signature follows DOKU's recipe
// independently of the adapter.
func dokuNotification(p *domain.Payment, status, target, requestID string) (string, []string) {
	invoice := "KP" + strings.TrimPrefix(p.ExternalID, "kp_pay_")
	body := fmt.Sprintf(`{"service":{"id":"VIRTUAL_ACCOUNT"},"channel":{"id":"VIRTUAL_ACCOUNT_BCA"},`+
		`"order":{"invoice_number":%q,"amount":%d},"transaction":{"status":%q,"date":"2026-10-31T03:24:23Z"}}`,
		invoice, p.Amount, status)
	ts := "2026-10-31T03:24:24Z"
	sum := sha256.Sum256([]byte(body))
	components := strings.Join([]string{
		"Client-Id:" + dokuClientID, "Request-Id:" + requestID, "Request-Timestamp:" + ts,
		"Request-Target:" + target, "Digest:" + base64.StdEncoding.EncodeToString(sum[:]),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(dokuSecret))
	mac.Write([]byte(components))
	sig := "HMACSHA256=" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return body, []string{"Client-Id", dokuClientID, "Request-Id", requestID, "Request-Timestamp", ts, "Signature", sig}
}

func TestDOKUNotifications(t *testing.T) {
	f := newAPI(t, "")
	conn := f.addDOKU(t, "doku-test")
	p := f.addPayment(t, conn, 150000)
	url := "/webhooks/doku/" + conn.PublicID

	// Signed for another connection's URL: the path is part of the signature.
	body, headers := dokuNotification(p, "SUCCESS", "/webhooks/doku/con_other", "req-1")
	expectError(t, f.send(nil, http.MethodPost, url, body, headers...), 401, domain.CodeInvalidCallbackToken)

	body, headers = dokuNotification(p, "SUCCESS", url, "req-2")
	if w := f.send(nil, http.MethodPost, url, body, headers...); w.Code != http.StatusOK {
		t.Fatalf("valid: %d %s", w.Code, w.Body.String())
	}
	if got := f.paymentStatus(t, p); got != domain.PaymentStatusPaid {
		t.Fatalf("status = %s", got)
	}
	// DOKU's retry carries a new Request-Id; the order and status are the same.
	body, headers = dokuNotification(p, "SUCCESS", url, "req-3")
	if w := f.send(nil, http.MethodPost, url, body, headers...); w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}

	var results []string
	for _, r := range f.hooks.AllInbound() {
		results = append(results, string(r.Result))
	}
	if strings.Join(results, ",") != "invalid_signature,applied,duplicate" {
		t.Fatalf("results = %v", results)
	}

	// The connection does not take another provider's path.
	body, headers = dokuNotification(p, "SUCCESS", url, "req-4")
	expectError(t, f.send(nil, http.MethodPost, "/webhooks/xendit/"+conn.PublicID, body, headers...), 404, domain.CodeNotFound)
}

func TestProvidersDescribeDOKU(t *testing.T) {
	f := newAPI(t, "")
	_, b := f.as(t, domain.RoleViewer)
	data := decode(t, f.send(b, http.MethodGet, "/admin/providers", ""), http.StatusOK)["data"].([]any)
	doku := data[0].(map[string]any)
	caps := doku["capabilities"].(map[string]any)
	if doku["name"] != "doku" || doku["display_name"] != "DOKU" || caps["read_checkout"] != true ||
		caps["list_transactions"] != false || caps["can_restrict_channels"] != true {
		t.Fatalf("doku = %v", doku)
	}
	var keys []string
	for _, fl := range doku["credential_schema"].(map[string]any)["fields"].([]any) {
		keys = append(keys, fl.(map[string]any)["key"].(string))
	}
	if strings.Join(keys, ",") != "client_id,secret_key" {
		t.Fatalf("credential keys = %v", keys)
	}
}
