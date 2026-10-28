// Package provider is the port every payment gateway adapter implements, with
// the registry of built-in adapters, the schema format their credentials and
// settings are described in, the generic validator for that format and the
// error classes adapters wrap gateway failures into.
package provider

import (
	"context"
	"net/http"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Name identifies a provider adapter, e.g. "xendit".
type Name string

// Credentials are decrypted credential values keyed by CredentialSchema field.
type Credentials map[string]string

// Settings are validated provider settings with defaults filled in, keyed by
// SettingsSchema field. Integers are int64 or json.Number.
type Settings map[string]any

// Account is what every gateway call receives: the connection's environment,
// credentials and settings.
type Account struct {
	Environment domain.Environment
	Credentials Credentials
	Settings    Settings
}

// Capabilities describe what a provider can do.
type Capabilities struct {
	// Channels the provider can offer, in display order.
	Channels            []domain.Channel
	Currencies          []string
	CanRestrictChannels bool
	CanCancel           bool
	ReportsFee          bool
}

// WebhookSetup tells an operator where to paste a connection's callback URL.
type WebhookSetup struct {
	Instructions []string
	DashboardURL string
}

// Descriptor is the human-facing description of a provider.
type Descriptor struct {
	Name        Name
	DisplayName string
	Description string
	DocsURL     string
	Webhook     WebhookSetup
}

// CheckoutInput is a request to create a hosted checkout.
type CheckoutInput struct {
	// Reference is the payment's gateway reference (payments.external_id).
	Reference   string
	Amount      int64
	Currency    string
	Description string
	PayerEmail  string
	// Channels limits the checkout; empty means no restriction.
	Channels           []domain.Channel
	Items              []domain.OrderItem
	Customer           *domain.Customer
	SuccessRedirectURL string
	FailureRedirectURL string
	// ExpiresIn is the checkout lifetime; 0 uses the settings, then the
	// gateway default.
	ExpiresIn time.Duration
}

// CheckoutOutput is a created checkout.
type CheckoutOutput struct {
	ProviderPaymentID string
	CheckoutURL       string
	Status            domain.PaymentStatus
	ExpiresAt         *time.Time
	Raw               []byte
}

// InboundEvent is a parsed gateway callback.
type InboundEvent struct {
	// EventKey and ProviderStatus together deduplicate callbacks.
	EventKey string
	// Reference is the payment's gateway reference.
	Reference         string
	ProviderPaymentID string
	ProviderStatus    string
	Status            domain.PaymentStatus
	Amount            int64
	Currency          string
	PaidAmount        int64
	Fee               int64
	PaidAt            *time.Time
	SettledAt         *time.Time
	Source            string
}

// Provider is a payment gateway adapter. Every method that calls the gateway
// wraps its failure in exactly one of ErrRejected, ErrRetryable,
// ErrUnknownOutcome or ErrNotFound.
type Provider interface {
	Descriptor() Descriptor
	CredentialSchema() Schema
	SettingsSchema() Schema
	Capabilities() Capabilities
	// ValidateAccount adds provider-specific cross-field rules after the
	// generic schema check; nil when valid.
	ValidateAccount(env domain.Environment, c Credentials) []domain.FieldError
	// TestCredentials makes the cheapest authenticated call the provider has.
	TestCredentials(ctx context.Context, a Account) error
	CreateCheckout(ctx context.Context, a Account, in CheckoutInput) (*CheckoutOutput, error)
	// CancelCheckout makes an unpaid checkout unpayable.
	CancelCheckout(ctx context.Context, a Account, providerPaymentID string) error
	// VerifyWebhook authenticates a callback; failure is ErrInvalidSignature.
	VerifyWebhook(a Account, h http.Header, rawBody []byte) error
	ParseWebhook(rawBody []byte) (*InboundEvent, error)
}
