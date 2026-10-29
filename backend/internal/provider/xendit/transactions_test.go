package xendit

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Fixtures follow the List Transactions response of Xendit's Balance and
// Transaction API (the field names of the SDK's TransactionResponse and
// FeeResponse models): amounts and fees are JSON numbers in major units.
const (
	transactionsPage1 = `{"has_more":true,"links":[],"data":[
		{"id":"txn_1","product_id":"inv_1","type":"PAYMENT","status":"SUCCESS","channel_category":"VIRTUAL_ACCOUNT",
		 "channel_code":"BCA","reference_id":"kp_pay_A","account_identifier":null,"currency":"IDR","amount":25000000,
		 "cashflow":"MONEY_IN","settlement_status":"SETTLED","estimated_settlement_time":"2026-10-02T00:00:00.000Z",
		 "business_id":"biz","fee":{"xendit_fee":4500,"value_added_tax":495,"xendit_withholding_tax":0,
		 "third_party_withholding_tax":0,"status":"COMPLETED"},
		 "created":"2026-10-01T10:00:00.000Z","updated":"2026-10-01T10:00:01.000Z"},
		{"id":"txn_2","product_id":"inv_2","type":"PAYMENT","status":"SUCCESS","channel_category":"QR_CODE",
		 "channel_code":"QRIS","reference_id":"acme:ORDER-ü/1","currency":"IDR","amount":16777217,"cashflow":"MONEY_IN",
		 "settlement_status":"PENDING","business_id":"biz","fee":{"xendit_fee":1000,"value_added_tax":110},
		 "created":"2026-10-01T09:00:00.000Z","updated":"2026-10-01T09:00:00.000Z"}]}`
	transactionsPage2 = `{"has_more":false,"links":[],"data":[
		{"id":"txn_3","reference_id":"kp_pay_C","type":"PAYMENT","status":"PENDING","currency":"IDR","amount":1000,
		 "created":"2026-10-01T08:00:00.000Z"},
		{"id":"txn_4","reference_id":"kp_pay_D","type":"PAYMENT","status":"WEIRD","currency":"USD","amount":12.345,
		 "fee":{"xendit_fee":0.5,"value_added_tax":0.055},"created":"2026-10-01T07:00:00.000Z"},
		{"id":"txn_5","reference_id":"kp_pay_E","type":"PAYMENT","status":"REVERSED","currency":"IDR","amount":"7000",
		 "created":"2026-10-01T06:00:00.000Z"},
		{"id":"txn_6","reference_id":"kp_pay_F","type":"PAYMENT","status":"SUCCESS","currency":"IDR","amount":1,
		 "created":"2026-10-02T00:00:00.000Z"}]}`
)

type transactionsAPI struct {
	mu       sync.Mutex
	requests []*http.Request
	pages    map[string]string // after_id → body
	status   int
}

func newTransactionsAPI(t *testing.T) (*transactionsAPI, *httptest.Server) {
	t.Helper()
	api := &transactionsAPI{pages: map[string]string{"": transactionsPage1, "txn_2": transactionsPage2}, status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.requests = append(api.requests, r.Clone(context.Background()))
		status, body := api.status, api.pages[r.URL.Query().Get("after_id")]
		api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return api, srv
}

var txWindow = provider.TransactionQuery{
	From: time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC),
	To:   time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
}

func fee(v int64) *int64 { return &v }

func TestListTransactionsPagesAndMaps(t *testing.T) {
	api, srv := newTransactionsAPI(t)
	a := New(WithBaseURL(srv.URL))
	ctx := context.Background()

	q := txWindow
	q.Limit = 50
	first, err := a.ListTransactions(ctx, account, q)
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor != "txn_2" || len(first.Transactions) != 2 {
		t.Fatalf("first page = %+v", first)
	}
	q.Cursor = first.NextCursor
	second, err := a.ListTransactions(ctx, account, q)
	if err != nil {
		t.Fatal(err)
	}
	if second.NextCursor != "" {
		t.Fatalf("last page cursor = %q", second.NextCursor)
	}

	got := append(first.Transactions, second.Transactions...)
	want := []domain.GatewayTransaction{
		{Reference: "kp_pay_A", ProviderPaymentID: "txn_1", ProviderStatus: "SUCCESS", Status: domain.PaymentStatusSettled,
			Amount: 25000000, Currency: "IDR", Fee: fee(4995), OccurredAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{Reference: "acme:ORDER-ü/1", ProviderPaymentID: "txn_2", ProviderStatus: "SUCCESS", Status: domain.PaymentStatusPaid,
			Amount: 16777217, Currency: "IDR", Fee: fee(1110), OccurredAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
		{Reference: "kp_pay_C", ProviderPaymentID: "txn_3", ProviderStatus: "PENDING", Status: domain.PaymentStatusPending,
			Amount: 1000, Currency: "IDR", OccurredAt: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)},
		// USD has two decimals: 12.345 rounds half away from zero to 1235
		// cents, the fee 0.5 + 0.055 to 50 + 6.
		{Reference: "kp_pay_D", ProviderPaymentID: "txn_4", ProviderStatus: "WEIRD", Status: "",
			Amount: 1235, Currency: "USD", Fee: fee(56), OccurredAt: time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)},
		{Reference: "kp_pay_E", ProviderPaymentID: "txn_5", ProviderStatus: "REVERSED", Status: domain.PaymentStatusFailed,
			Amount: 7000, Currency: "IDR", OccurredAt: time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC)},
		// txn_6 is at exactly To and is dropped: the range is half-open.
	}
	if len(got) != len(want) {
		t.Fatalf("transactions = %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Reference != w.Reference || g.ProviderPaymentID != w.ProviderPaymentID || g.ProviderStatus != w.ProviderStatus ||
			g.Status != w.Status || g.Amount != w.Amount || g.Currency != w.Currency || !g.OccurredAt.Equal(w.OccurredAt) ||
			(g.Fee == nil) != (w.Fee == nil) || (g.Fee != nil && *g.Fee != *w.Fee) {
			t.Errorf("transaction %d = %+v (fee %v), want %+v (fee %v)", i, g, g.Fee, w, w.Fee)
		}
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.requests) != 2 {
		t.Fatalf("requests = %d", len(api.requests))
	}
	for i, r := range api.requests {
		user, pass, ok := r.BasicAuth()
		if r.Method != http.MethodGet || r.URL.Path != "/transactions" || !ok || user != testKey || pass != "" {
			t.Fatalf("request %d: %s %s auth=%q/%q", i, r.Method, r.URL.Path, user, pass)
		}
		qs := r.URL.Query()
		if qs.Get("types") != "PAYMENT" || qs.Get("created[gte]") != "2026-09-30T17:00:00Z" ||
			qs.Get("created[lte]") != "2026-10-02T00:00:00Z" || qs.Get("limit") != "50" {
			t.Fatalf("request %d query = %v", i, qs)
		}
	}
	if api.requests[0].URL.Query().Has("after_id") || api.requests[1].URL.Query().Get("after_id") != "txn_2" {
		t.Fatalf("after_id = %q, %q", api.requests[0].URL.RawQuery, api.requests[1].URL.RawQuery)
	}
}

func TestListTransactionsCapsLimit(t *testing.T) {
	api, srv := newTransactionsAPI(t)
	a := New(WithBaseURL(srv.URL))
	for _, limit := range []int{0, 51, -1} {
		q := txWindow
		q.Limit = limit
		if _, err := a.ListTransactions(context.Background(), account, q); err != nil {
			t.Fatal(err)
		}
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	for _, r := range api.requests {
		if r.URL.Query().Get("limit") != "50" {
			t.Fatalf("limit = %q", r.URL.Query().Get("limit"))
		}
	}
}

// Failures are classified like the other gateway calls.
func TestListTransactionsErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  error
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error_code":"INVALID_API_KEY"}`, provider.ErrRejected},
		{"forbidden", http.StatusForbidden, `{"error_code":"REQUEST_FORBIDDEN_ERROR"}`, provider.ErrRejected},
		{"bad request", http.StatusBadRequest, `{"error_code":"API_VALIDATION_ERROR"}`, provider.ErrRejected},
		{"rate limited", http.StatusTooManyRequests, `{}`, provider.ErrRetryable},
		{"unavailable", http.StatusServiceUnavailable, `oops`, provider.ErrRetryable},
		{"malformed json", http.StatusOK, `{"has_more":`, provider.ErrRetryable},
		{"bad amount", http.StatusOK, `{"has_more":false,"data":[{"id":"t","amount":"abc","currency":"IDR"}]}`, provider.ErrRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, srv := newTransactionsAPI(t)
			api.status, api.pages[""] = tc.status, tc.body
			_, err := New(WithBaseURL(srv.URL)).ListTransactions(context.Background(), account, txWindow)
			if !errors.Is(err, tc.class) {
				t.Fatalf("err = %v, want %v", err, tc.class)
			}
			for _, other := range []error{provider.ErrRejected, provider.ErrRetryable} {
				if other != tc.class && errors.Is(err, other) {
					t.Fatalf("err = %v is also %v", err, other)
				}
			}
		})
	}
}

func TestListTransactionsConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	_, err = New(WithBaseURL("http://"+addr)).ListTransactions(context.Background(), account, txWindow)
	if !errors.Is(err, provider.ErrRetryable) || errors.Is(err, provider.ErrRejected) {
		t.Fatalf("err = %v, want ErrRetryable", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("error leaks the secret key")
	}
}

func TestTransactionsURLDefaultsToXendit(t *testing.T) {
	u, err := url.Parse(New().transactionsURL(txWindow))
	if err != nil || u.Host != "api.xendit.co" || u.Path != "/transactions" {
		t.Fatalf("url = %v %v", u, err)
	}
}
