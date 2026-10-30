package xendit

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// The invoice object reads like its callback: an expired invoice is the
// EXPIRED event, marked as read back rather than received.
func TestGetCheckoutReadsTheInvoice(t *testing.T) {
	invoice := `{"id":"inv_9","external_id":"kp_pay_x","status":"EXPIRED","amount":30000,"currency":"IDR",
		"expiry_date":"2026-10-29T08:52:15.879Z","updated":"2026-10-29T08:52:15.879Z","merchant_name":"ignored"}`
	fx, srv := newFakeXendit(t, http.StatusOK, invoice)
	ev, raw, err := New(WithBaseURL(srv.URL)).GetCheckout(context.Background(), account, "inv_9")
	if err != nil {
		t.Fatal(err)
	}
	req := fx.last(t)
	if req.method != http.MethodGet || req.path != "/v2/invoices/inv_9" || req.user != testKey {
		t.Fatalf("request = %s %s as %q", req.method, req.path, req.user)
	}
	if ev.EventKey != "inv_9" || ev.Reference != "kp_pay_x" || ev.ProviderStatus != "EXPIRED" ||
		ev.Status != domain.PaymentStatusExpired || ev.Amount != 30000 || ev.Currency != "IDR" || ev.Source != domain.InboundSourcePoll {
		t.Fatalf("event = %+v", ev)
	}
	if string(raw) != invoice {
		t.Fatalf("raw = %s", raw)
	}
}

// A paid invoice carries what the callback would: paid amount, paid time and
// the merchant fee when Xendit reports what the merchant received.
func TestGetCheckoutPaidInvoice(t *testing.T) {
	_, srv := newFakeXendit(t, http.StatusOK, `{"id":"inv_2","external_id":"kp_pay_y","status":"PAID","amount":50000,
		"paid_amount":50000,"adjusted_received_amount":47500,"currency":"IDR","paid_at":"2026-10-29T08:00:00Z"}`)
	ev, _, err := New(WithBaseURL(srv.URL)).GetCheckout(context.Background(), account, "inv_2")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Status != domain.PaymentStatusPaid || ev.PaidAmount != 50000 || ev.Fee == nil || *ev.Fee != 2500 || ev.PaidAt == nil {
		t.Fatalf("event = %+v", ev)
	}
}

// Ids are escaped into the path, never interpreted.
func TestGetCheckoutEscapesTheID(t *testing.T) {
	fx, srv := newFakeXendit(t, http.StatusOK, `{"id":"a/b?c","external_id":"r","status":"PENDING"}`)
	if _, _, err := New(WithBaseURL(srv.URL)).GetCheckout(context.Background(), account, "a/b?c"); err != nil {
		t.Fatal(err)
	}
	if req := fx.last(t); req.path != "/v2/invoices/a/b?c" || req.query != "" {
		t.Fatalf("path %q query %q", req.path, req.query)
	}
}

func TestGetCheckoutErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"unknown invoice", http.StatusNotFound, `{"error_code":"INVOICE_NOT_FOUND_ERROR"}`, provider.ErrNotFound},
		{"bad key", http.StatusUnauthorized, `{"error_code":"INVALID_API_KEY"}`, provider.ErrRejected},
		{"no permission", http.StatusForbidden, `{"error_code":"REQUEST_FORBIDDEN_ERROR"}`, provider.ErrRejected},
		{"rate limited", http.StatusTooManyRequests, `{}`, provider.ErrRetryable},
		{"server error", http.StatusBadGateway, `{}`, provider.ErrRetryable},
		{"unreadable invoice", http.StatusOK, `{"id":"inv_1"}`, provider.ErrRetryable},
		{"not json", http.StatusOK, `<html>`, provider.ErrRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := newFakeXendit(t, tc.status, tc.body)
			if _, _, err := New(WithBaseURL(srv.URL)).GetCheckout(context.Background(), account, "inv_1"); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// A gateway that cannot be reached may work on a later try.
func TestGetCheckoutConnectionRefused(t *testing.T) {
	_, srv := newFakeXendit(t, http.StatusOK, `{}`)
	srv.Close()
	if _, _, err := New(WithBaseURL(srv.URL)).GetCheckout(context.Background(), account, "inv_1"); !errors.Is(err, provider.ErrRetryable) {
		t.Fatalf("err = %v", err)
	}
}
