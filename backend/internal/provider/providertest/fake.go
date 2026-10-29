// Package providertest has a programmable provider.Provider for tests: every
// gateway method returns what the test sets and records how it was called.
package providertest

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Name is the provider name of a Fake built by New.
const Name provider.Name = "fake"

// TokenHeader carries the callback token VerifyWebhook compares with the
// "token" credential.
const TokenHeader = "X-Fake-Token"

// CheckoutCall is one recorded CreateCheckout call.
type CheckoutCall struct {
	Account provider.Account
	Input   provider.CheckoutInput
}

// CancelCall is one recorded CancelCheckout call.
type CancelCall struct {
	Account           provider.Account
	ProviderPaymentID string
}

// Fake is a provider.Provider whose results are set by the test. The zero
// results succeed: TestCredentials returns nil and CreateCheckout returns
// numbered pending checkouts.
type Fake struct {
	mu sync.Mutex

	Desc       provider.Descriptor
	Creds      provider.Schema
	SettingsSc provider.Schema
	Caps       provider.Capabilities
	// AccountRules is returned by ValidateAccount.
	AccountRules func(env domain.Environment, c provider.Credentials) []domain.FieldError

	TestErr   error
	CreateErr error
	// CreateOut, when set, is returned by CreateCheckout instead of a
	// numbered checkout.
	CreateOut *provider.CheckoutOutput
	CancelErr error

	Tests   []provider.Account
	Creates []CheckoutCall
	Cancels []CancelCall
}

// New returns a Fake named "fake" with two secret string credentials
// (api_key, token), the settings invoice_duration (integer) and locale (enum),
// and three channels that can be restricted.
func New() *Fake {
	return &Fake{
		Desc: provider.Descriptor{
			Name: Name, DisplayName: "Fake", Description: "A provider for tests.", DocsURL: "https://example.com/docs",
			Webhook: provider.WebhookSetup{Instructions: []string{"Paste the callback URL."}, DashboardURL: "https://example.com/dashboard"},
		},
		Creds: provider.Schema{Fields: []provider.Field{
			{Key: "api_key", Label: "API key", Type: provider.TypeString, Secret: true, Required: true, MaxLength: provider.IntPtr(255)},
			{Key: "token", Label: "Callback token", Type: provider.TypeString, Secret: true, Required: true, MinLength: provider.IntPtr(8)},
		}},
		SettingsSc: provider.Schema{Fields: []provider.Field{
			{Key: "invoice_duration", Label: "Lifetime", Type: provider.TypeInteger,
				Minimum: provider.Int64Ptr(60), Maximum: provider.Int64Ptr(31536000)},
			{Key: "locale", Label: "Language", Type: provider.TypeEnum, Default: "en",
				Options: []provider.Option{{Value: "id", Label: "Indonesian"}, {Value: "en", Label: "English"}}},
		}},
		Caps: provider.Capabilities{
			Channels:   []domain.Channel{domain.ChannelQRIS, domain.ChannelVABCA, domain.ChannelEwalletOVO},
			Currencies: []string{"IDR"}, CanRestrictChannels: true, CanCancel: true, ReportsFee: true,
		},
	}
}

// Descriptor returns Desc.
func (f *Fake) Descriptor() provider.Descriptor { return f.Desc }

// CredentialSchema returns Creds.
func (f *Fake) CredentialSchema() provider.Schema { return f.Creds }

// SettingsSchema returns SettingsSc.
func (f *Fake) SettingsSchema() provider.Schema { return f.SettingsSc }

// Capabilities returns Caps.
func (f *Fake) Capabilities() provider.Capabilities { return f.Caps }

// ValidateAccount runs AccountRules, if set.
func (f *Fake) ValidateAccount(env domain.Environment, c provider.Credentials) []domain.FieldError {
	if f.AccountRules == nil {
		return nil
	}
	return f.AccountRules(env, c)
}

// TestCredentials records the call and returns TestErr, or the context's
// error when it ends first.
func (f *Fake) TestCredentials(ctx context.Context, a provider.Account) error {
	f.mu.Lock()
	f.Tests = append(f.Tests, a)
	err := f.TestErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return ctx.Err()
}

// CreateCheckout records the call and returns CreateErr, CreateOut or a
// numbered pending checkout.
func (f *Fake) CreateCheckout(_ context.Context, a provider.Account, in provider.CheckoutInput) (*provider.CheckoutOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	f.Creates = append(f.Creates, CheckoutCall{Account: a, Input: in})
	if f.CreateOut != nil {
		out := *f.CreateOut
		return &out, nil
	}
	n := strconv.Itoa(len(f.Creates))
	return &provider.CheckoutOutput{
		ProviderPaymentID: "inv_" + n,
		CheckoutURL:       "https://checkout.example/inv_" + n,
		Status:            domain.PaymentStatusPending,
	}, nil
}

// CancelCheckout records the call and returns CancelErr.
func (f *Fake) CancelCheckout(_ context.Context, a provider.Account, providerPaymentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CancelErr != nil {
		return f.CancelErr
	}
	f.Cancels = append(f.Cancels, CancelCall{Account: a, ProviderPaymentID: providerPaymentID})
	return nil
}

// VerifyWebhook compares TokenHeader with the "token" credential.
func (f *Fake) VerifyWebhook(a provider.Account, h http.Header, _ []byte) error {
	got, want := h.Get(TokenHeader), a.Credentials["token"]
	if got == "" || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return provider.ErrInvalidSignature
	}
	return nil
}

// Callback is the body ParseWebhook reads.
type Callback struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
}

// ParseWebhook decodes a Callback; status is taken as a PaymentStatus, and a
// value that is not one is unknown (empty Status).
func (f *Fake) ParseWebhook(raw []byte) (*provider.InboundEvent, error) {
	var cb Callback
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, err
	}
	if cb.ID == "" || cb.Reference == "" {
		return nil, errors.New("missing id or reference")
	}
	status := domain.PaymentStatus(cb.Status)
	switch status {
	case domain.PaymentStatusPending, domain.PaymentStatusPaid, domain.PaymentStatusSettled,
		domain.PaymentStatusExpired, domain.PaymentStatusFailed:
	default:
		status = ""
	}
	return &provider.InboundEvent{
		EventKey: cb.ID, Reference: cb.Reference, ProviderPaymentID: cb.ID, ProviderStatus: cb.Status,
		Status: status, Amount: cb.Amount, Currency: cb.Currency, Source: "webhook",
	}, nil
}

// Calls returns how many times each gateway method ran.
func (f *Fake) Calls() (tests, creates, cancels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Tests), len(f.Creates), len(f.Cancels)
}
