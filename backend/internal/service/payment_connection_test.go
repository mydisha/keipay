package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

func assertCode(t *testing.T, err error, code domain.ErrorCode) *domain.Error {
	t.Helper()
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != code {
		t.Fatalf("err = %v, want %s", err, code)
	}
	return coded
}

// Without a usable default connection a payment is not
// eligible, but replays never look at the connection.
func TestCreateNeedsAnEligibleConnection(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()
	first, _, err := f.create(t, f.acme, order1, "K1")
	if err != nil {
		t.Fatal(err)
	}

	// No default connection.
	f.acme.DefaultConnectionID, f.acme.DefaultConnection = nil, nil
	_, _, err = f.create(t, f.acme, CreatePaymentInput{OrderID: "ord-2", Amount: 1}, "")
	coded := assertCode(t, err, domain.CodeNoEligibleConnection)
	if fmt.Sprint(coded.Details["reasons"]) != "[no_default_connection]" {
		t.Fatalf("reasons = %v", coded.Details["reasons"])
	}
	for _, key := range []string{"K1", ""} {
		got, replayed, err := f.create(t, f.acme, order1, key)
		if err != nil || !replayed || got.ID != first.ID {
			t.Fatalf("replay %q without a connection: %+v %v %v", key, got, replayed, err)
		}
	}

	// Disabled default connection.
	f.acme.DefaultConnectionID = &f.conn.ID
	c, _ := f.conns.GetByID(ctx, f.conn.ID)
	c.Status = domain.ConnectionDisabled
	if err := f.conns.Update(ctx, c); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.create(t, f.acme, CreatePaymentInput{OrderID: "ord-2", Amount: 1}, "")
	coded = assertCode(t, err, domain.CodeNoEligibleConnection)
	if fmt.Sprint(coded.Details["reasons"]) != "[connection_disabled]" {
		t.Fatalf("reasons = %v", coded.Details["reasons"])
	}
	if got, replayed, err := f.create(t, f.acme, order1, "K1"); err != nil || !replayed || got.ID != first.ID {
		t.Fatalf("replay with a disabled connection: %v %v", replayed, err)
	}
	if len(f.fake.Creates) != 1 || len(f.payments.All()) != 1 {
		t.Fatalf("creates=%d payments=%d", len(f.fake.Creates), len(f.payments.All()))
	}
}

// Gateway failures map to their codes and store nothing.
func TestCreateProviderErrors(t *testing.T) {
	cases := []struct {
		err  error
		code domain.ErrorCode
	}{
		{provider.NewStatusError(provider.ErrRejected, 400, "amount below minimum"), domain.CodeProviderRejected},
		{fmt.Errorf("x: %w", provider.ErrRetryable), domain.CodeProviderUnavailable},
		{provider.NewStatusError(provider.ErrUnknownOutcome, 502, "bad gateway"), domain.CodeProviderUnavailable},
	}
	for _, tc := range cases {
		f := newPaymentFixture(t)
		f.fake.CreateErr = tc.err
		_, _, err := f.create(t, f.acme, order1, "")
		coded := assertCode(t, err, tc.code)
		if tc.code == domain.CodeProviderRejected && coded.Details["provider"] != "fake" {
			t.Fatalf("details = %v", coded.Details)
		}
		if !errors.Is(err, tc.err) {
			t.Fatalf("cause lost: %v", err)
		}
		if len(f.payments.All()) != 0 {
			t.Fatal("a failed checkout stored a payment")
		}
		if f.logs.FilterField(zap.String("connection_id", f.conn.PublicID)).Len() != 1 {
			t.Fatalf("gateway error not logged: %v", f.logs.All())
		}
		// The order can be retried once the gateway recovers.
		f.fake.CreateErr = nil
		if _, replayed, err := f.create(t, f.acme, order1, ""); err != nil || replayed {
			t.Fatalf("retry: %v %v", replayed, err)
		}
	}
}

// The payment records its connection; channels and
// expiry reach the provider.
func TestCreateUsesTheDefaultConnection(t *testing.T) {
	f := newPaymentFixture(t)
	ctx := context.Background()
	expires := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	f.fake.CreateOut = &provider.CheckoutOutput{ProviderPaymentID: "inv_x", CheckoutURL: "https://checkout.example/inv_x",
		Status: domain.PaymentStatusPending, ExpiresAt: &expires}

	in := order1
	in.InvoiceDuration = 900
	p, _, err := f.create(t, f.acme, in, "")
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := f.payments.GetByPublicID(ctx, f.acme.ID, p.PublicID)
	if stored.ConnectionID == nil || *stored.ConnectionID != f.conn.ID || stored.Provider != "fake" ||
		stored.XenditInvoiceID != "inv_x" || stored.InvoiceURL != "https://checkout.example/inv_x" || !stored.ExpiresAt.Equal(expires) {
		t.Fatalf("stored = %+v", stored)
	}
	call := f.fake.Creates[0]
	if call.Input.Channels != nil || call.Input.ExpiresIn != 15*time.Minute || call.Account.Environment != domain.EnvLive ||
		call.Account.Credentials["api_key"] != "key-fake-live-0123456789" || call.Account.Settings["locale"] != "en" {
		t.Fatalf("call = %+v", call)
	}

	// A strict subset of channels restricts the checkout, in capability order.
	if err := f.conns.ReplaceChannels(ctx, f.conn.ID, []domain.ConnectionChannel{
		{Channel: domain.ChannelEwalletOVO, Enabled: true}, {Channel: domain.ChannelQRIS, Enabled: true}, {Channel: domain.ChannelVABCA, Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.create(t, f.acme, CreatePaymentInput{OrderID: "ord-2", Amount: 1}, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.fake.Creates[1].Input.Channels; !slices.Equal(got, []domain.Channel{domain.ChannelQRIS, domain.ChannelEwalletOVO}) {
		t.Fatalf("channels = %v", got)
	}
	// Every channel enabled: no restriction.
	if err := f.conns.ReplaceChannels(ctx, f.conn.ID, []domain.ConnectionChannel{
		{Channel: domain.ChannelEwalletOVO, Enabled: true}, {Channel: domain.ChannelQRIS, Enabled: true}, {Channel: domain.ChannelVABCA, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.create(t, f.acme, CreatePaymentInput{OrderID: "ord-3", Amount: 1}, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.fake.Creates[2].Input.Channels; got != nil {
		t.Fatalf("full set restricted the checkout: %v", got)
	}

	// A provider that cannot restrict channels never gets a list.
	f.fake.Caps.CanRestrictChannels = false
	if err := f.conns.ReplaceChannels(ctx, f.conn.ID, []domain.ConnectionChannel{{Channel: domain.ChannelQRIS, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.create(t, f.acme, CreatePaymentInput{OrderID: "ord-4", Amount: 1}, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.fake.Creates[3].Input.Channels; got != nil {
		t.Fatalf("channels = %v", got)
	}
}
