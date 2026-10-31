package doku

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// TestSandbox creates a checkout on a real DOKU sandbox account and reads it
// back. It runs only when DOKU_SANDBOX_CLIENT_ID and DOKU_SANDBOX_SECRET_KEY
// are set.
func TestSandbox(t *testing.T) {
	clientID, secret := os.Getenv("DOKU_SANDBOX_CLIENT_ID"), os.Getenv("DOKU_SANDBOX_SECRET_KEY")
	if clientID == "" || secret == "" {
		t.Skip("DOKU_SANDBOX_CLIENT_ID and DOKU_SANDBOX_SECRET_KEY are not set")
	}
	acc := provider.Account{
		Environment: domain.EnvTest,
		Credentials: provider.Credentials{keyClientID: clientID, keySecretKey: secret},
	}
	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := a.TestCredentials(ctx, acc); err != nil {
		t.Fatalf("test credentials: %v", err)
	}
	wrong := provider.Account{Environment: domain.EnvTest,
		Credentials: provider.Credentials{keyClientID: clientID, keySecretKey: secret + "x"}}
	if err := a.TestCredentials(ctx, wrong); err == nil {
		t.Fatal("a wrong secret key passed the credential test")
	}

	ref := id.New(id.Payment)
	in := provider.CheckoutInput{
		Reference: "kp_" + ref, Amount: 15000, Currency: "IDR",
		Items:     []domain.OrderItem{{Name: "Sandbox item", Quantity: 1, Price: 15000}},
		Customer:  &domain.Customer{GivenNames: "Sandbox", Email: "sandbox@example.com", MobileNumber: "+6281234567890"},
		Channels:  []domain.Channel{domain.ChannelVABCA, domain.ChannelQRIS, domain.ChannelCard},
		ExpiresIn: 30 * time.Minute, SuccessRedirectURL: "https://example.com/ok",
	}
	out, err := a.CreateCheckout(ctx, acc, in)
	if err != nil {
		t.Fatalf("create checkout: %v", err)
	}
	if !strings.HasPrefix(out.ProviderPaymentID, invoicePrefix) || out.CheckoutURL == "" || out.ExpiresAt == nil {
		t.Fatalf("out = %+v", out)
	}
	t.Logf("checkout %s: %s, expires %s", out.ProviderPaymentID, out.CheckoutURL, out.ExpiresAt)

	ev, raw, err := a.GetCheckout(ctx, acc, out.ProviderPaymentID)
	switch {
	case err == nil:
		if ev.Reference != in.Reference || ev.Status != domain.PaymentStatusPending {
			t.Fatalf("status = %+v (%s)", ev, raw)
		}
	case errors.Is(err, provider.ErrNotFound):
		// Accounts registered before DOKU's order-level status answer not
		// found until the customer picks a method.
		t.Logf("status before a method was chosen: %v", err)
	default:
		t.Fatalf("status: %v", err)
	}
}
