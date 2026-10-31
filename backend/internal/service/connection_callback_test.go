package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
)

func TestVerifyCallbackPassesThePublicPath(t *testing.T) {
	body := []byte(`{"id":"evt-1","reference":"kp_pay_A","status":"PAID"}`)
	cases := map[string]struct{ baseURL, want string }{
		"root":           {"https://pay.example.com/", "/webhooks/fake/con_A"},
		"no slash":       {"https://pay.example.com", "/webhooks/fake/con_A"},
		"prefix":         {"https://example.com/pay/", "/pay/webhooks/fake/con_A"},
		"escaped prefix": {"https://example.com/my%20pay", "/my%20pay/webhooks/fake/con_A"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newConnFixture(t)
			f.svc.baseURL = tc.baseURL
			c := f.addConnection(t, "acme", domain.EnvTest)
			h := http.Header{}
			h.Set(providertest.TokenHeader, "tok-acme-123")

			ev, err := f.svc.VerifyCallback(c, provider.CallbackRequest{Path: "/webhooks/fake/con_A", Header: h, Body: body})
			if err != nil {
				t.Fatal(err)
			}
			if ev.Reference != "kp_pay_A" || ev.Status != domain.PaymentStatusPaid {
				t.Fatalf("event = %+v", ev)
			}
			if len(f.fake.Verifies) != 1 {
				t.Fatalf("verifies = %d", len(f.fake.Verifies))
			}
			got := f.fake.Verifies[0]
			if got.Path != tc.want || string(got.Body) != string(body) || got.Header.Get(providertest.TokenHeader) == "" {
				t.Fatalf("request = %+v, want path %q", got, tc.want)
			}
		})
	}
}

func TestVerifyCallbackRejections(t *testing.T) {
	f := newConnFixture(t)
	c := f.addConnection(t, "acme", domain.EnvTest)

	h := http.Header{}
	h.Set(providertest.TokenHeader, "tok-other-123")
	_, err := f.svc.VerifyCallback(c, provider.CallbackRequest{Path: "/x", Header: h, Body: []byte(`{"id":"e","reference":"r"}`)})
	if !errors.Is(err, domain.ErrInvalidCallbackToken) {
		t.Fatalf("wrong token: err = %v", err)
	}

	h.Set(providertest.TokenHeader, "tok-acme-123")
	_, err = f.svc.VerifyCallback(c, provider.CallbackRequest{Path: "/x", Header: h, Body: []byte(`{"id":""}`)})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("unreadable body: err = %v", err)
	}
}
