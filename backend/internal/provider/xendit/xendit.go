// Package xendit is the provider adapter for hosted checkouts through the
// Xendit Invoice API. Every call builds its SDK client from the connection's
// own secret key, so no credential is shared between connections.
package xendit

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	xnd "github.com/xendit/xendit-go/v6"
	"github.com/xendit/xendit-go/v6/common"
	"github.com/xendit/xendit-go/v6/invoice"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Name is the provider name of the adapter.
const Name provider.Name = "xendit"

// CallbackTokenHeader is the header Xendit signs callbacks with.
const CallbackTokenHeader = "x-callback-token"

// Credential keys.
const (
	keySecretKey     = "secret_key"
	keyCallbackToken = "callback_token"
)

// Setting keys.
const (
	settingInvoiceDuration = "invoice_duration"
	settingLocale          = "locale"
)

const (
	devKeyPrefix  = "xnd_development_"
	prodKeyPrefix = "xnd_production_"
)

const defaultTestTimeout = 10 * time.Second

// methodCodes maps channels to Xendit invoice payment_methods codes.
var methodCodes = map[domain.Channel]string{
	domain.ChannelQRIS:             "QRIS",
	domain.ChannelVABCA:            "BCA",
	domain.ChannelVABNI:            "BNI",
	domain.ChannelVABRI:            "BRI",
	domain.ChannelVAMandiri:        "MANDIRI",
	domain.ChannelVAPermata:        "PERMATA",
	domain.ChannelVABSI:            "BSI",
	domain.ChannelEwalletOVO:       "OVO",
	domain.ChannelEwalletDANA:      "DANA",
	domain.ChannelEwalletShopeePay: "SHOPEEPAY",
	domain.ChannelEwalletLinkAja:   "LINKAJA",
	domain.ChannelCard:             "CREDIT_CARD",
	domain.ChannelRetailAlfamart:   "ALFAMART",
	domain.ChannelRetailIndomaret:  "INDOMARET",
}

// Adapter implements provider.Provider for Xendit.
type Adapter struct {
	baseURL     string
	transport   http.RoundTripper
	testTimeout time.Duration
}

// Option configures an Adapter.
type Option func(*Adapter)

// WithBaseURL points the adapter at another API server, e.g. a test server.
func WithBaseURL(u string) Option { return func(a *Adapter) { a.baseURL = strings.TrimRight(u, "/") } }

// WithTransport replaces the HTTP transport.
func WithTransport(rt http.RoundTripper) Option { return func(a *Adapter) { a.transport = rt } }

// WithTestTimeout bounds TestCredentials; the default is 10 seconds.
func WithTestTimeout(d time.Duration) Option { return func(a *Adapter) { a.testTimeout = d } }

// New returns the Xendit adapter.
func New(opts ...Option) *Adapter {
	a := &Adapter{transport: http.DefaultTransport, testTimeout: defaultTestTimeout}
	for _, o := range opts {
		o(a)
	}
	return a
}

var _ provider.Provider = (*Adapter)(nil)

// Descriptor describes Xendit for the console.
func (a *Adapter) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		Name:        Name,
		DisplayName: "Xendit",
		Description: "Hosted checkout through the Xendit Invoice API.",
		DocsURL:     "https://docs.xendit.co",
		Webhook: provider.WebhookSetup{
			Instructions: []string{
				"In the Xendit dashboard, open Settings → Webhooks.",
				`Paste the callback URL into "Invoices paid" and save.`,
				"Copy the webhook verification token shown on that page into this connection, if you have not already.",
			},
			DashboardURL: "https://dashboard.xendit.co/settings/developers#webhooks",
		},
	}
}

// CredentialSchema is the secret API key and the webhook verification token.
func (a *Adapter) CredentialSchema() provider.Schema {
	return provider.Schema{Fields: []provider.Field{
		{
			Key:         keySecretKey,
			Type:        provider.TypeString,
			Label:       "Secret API key",
			Description: "Xendit dashboard → Settings → API keys. A key with Invoice read and write permission is enough.",
			Placeholder: "xnd_development_…",
			Secret:      true,
			Required:    true,
			MaxLength:   provider.IntPtr(255),
			Pattern:     `^xnd_(development|production)_[A-Za-z0-9]+$`,
			PatternHint: "Starts with xnd_development_ (test) or xnd_production_ (live).",
		},
		{
			Key:         keyCallbackToken,
			Type:        provider.TypeString,
			Label:       "Webhook verification token",
			Description: "Xendit dashboard → Settings → Webhooks → Webhook verification token.",
			Secret:      true,
			Required:    true,
			MinLength:   provider.IntPtr(8),
			MaxLength:   provider.IntPtr(255),
		},
	}}
}

// SettingsSchema is the default invoice lifetime and checkout language.
func (a *Adapter) SettingsSchema() provider.Schema {
	return provider.Schema{Fields: []provider.Field{
		{
			Key:         settingInvoiceDuration,
			Type:        provider.TypeInteger,
			Label:       "Invoice lifetime (seconds)",
			Description: "Used when a payment request has no invoice_duration. Empty uses Xendit's default (24 hours).",
			Minimum:     provider.Int64Ptr(60),
			Maximum:     provider.Int64Ptr(31536000),
		},
		{
			Key:     settingLocale,
			Type:    provider.TypeEnum,
			Label:   "Checkout language",
			Options: []provider.Option{{Value: "id", Label: "Indonesian"}, {Value: "en", Label: "English"}},
		},
	}}
}

// Capabilities lists every invoice channel KeiPay maps.
func (a *Adapter) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Channels:            domain.AllChannels(),
		Currencies:          []string{"IDR"},
		CanRestrictChannels: true,
		CanCancel:           true,
		ReportsFee:          true,
	}
}

// ValidateAccount keeps development keys on test connections and production
// keys on live ones.
func (a *Adapter) ValidateAccount(env domain.Environment, c provider.Credentials) []domain.FieldError {
	key := c[keySecretKey]
	ok := (strings.HasPrefix(key, devKeyPrefix) && env == domain.EnvTest) ||
		(strings.HasPrefix(key, prodKeyPrefix) && env == domain.EnvLive)
	if ok {
		return nil
	}
	return []domain.FieldError{{Field: "credentials." + keySecretKey, Rule: "environment"}}
}

// EnvironmentForKey returns the environment a secret key belongs to.
func EnvironmentForKey(key string) (domain.Environment, bool) {
	switch {
	case strings.HasPrefix(key, devKeyPrefix):
		return domain.EnvTest, true
	case strings.HasPrefix(key, prodKeyPrefix):
		return domain.EnvLive, true
	default:
		return "", false
	}
}

// TestCredentials lists one invoice, which needs the invoice read permission
// KeiPay uses anyway.
func (a *Adapter) TestCredentials(ctx context.Context, acc provider.Account) error {
	ctx, cancel := context.WithTimeout(ctx, a.testTimeout)
	defer cancel()
	api, rec := a.client(acc)
	_, resp, sdkErr, failed := call(api.InvoiceApi.GetInvoices(ctx).Limit(1).Execute)
	// A 2xx answer means the key was accepted, even if the list in it could
	// not be decoded.
	if !failed || (resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	return classify(ctx, false, resp, sdkErr, rec)
}

// CreateCheckout creates a hosted invoice.
func (a *Adapter) CreateCheckout(ctx context.Context, acc provider.Account, in provider.CheckoutInput) (*provider.CheckoutOutput, error) {
	req := buildInvoiceRequest(acc.Settings, in)
	api, rec := a.client(acc)
	resp, httpResp, sdkErr, failed := call(api.InvoiceApi.CreateInvoice(ctx).CreateInvoiceRequest(req).Execute)
	if failed {
		return nil, fmt.Errorf("xendit create invoice: %w", classify(ctx, true, httpResp, sdkErr, rec))
	}
	raw, _ := json.Marshal(resp)
	out := &provider.CheckoutOutput{
		ProviderPaymentID: resp.GetId(),
		CheckoutURL:       resp.GetInvoiceUrl(),
		Status:            checkoutStatus(string(resp.GetStatus())),
		Raw:               raw,
	}
	if exp := resp.GetExpiryDate(); !exp.IsZero() {
		t := exp.UTC().Truncate(time.Second)
		out.ExpiresAt = &t
	}
	return out, nil
}

// CancelCheckout expires an unpaid invoice.
func (a *Adapter) CancelCheckout(ctx context.Context, acc provider.Account, invoiceID string) error {
	api, rec := a.client(acc)
	_, resp, sdkErr, failed := call(api.InvoiceApi.ExpireInvoice(ctx, invoiceID).Execute)
	if failed {
		return fmt.Errorf("xendit expire invoice %s: %w", invoiceID, classify(ctx, false, resp, sdkErr, rec))
	}
	return nil
}

// VerifyWebhook compares x-callback-token with the connection's token in
// constant time.
func (a *Adapter) VerifyWebhook(acc provider.Account, h http.Header, _ []byte) error {
	got, want := h.Get(CallbackTokenHeader), acc.Credentials[keyCallbackToken]
	if got == "" || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return provider.ErrInvalidSignature
	}
	return nil
}

// invoiceCallback is the subset of Xendit's invoice callback KeiPay reads.
type invoiceCallback struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
	Status     string `json:"status"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	PaidAmount int64  `json:"paid_amount"`
	FeesPaid   int64  `json:"fees_paid_amount"`
	PaidAt     string `json:"paid_at"`
	// SettlementDate is sent on PAID callbacks as an estimate and on SETTLED
	// callbacks as the event time; Updated is the fallback.
	SettlementDate string `json:"settlement_date"`
	Updated        string `json:"updated"`
}

// ErrInvalidCallback is a callback body that is not a Xendit invoice callback.
var ErrInvalidCallback = errors.New("invalid xendit callback")

// ParseWebhook reads an invoice callback.
func (a *Adapter) ParseWebhook(raw []byte) (*provider.InboundEvent, error) {
	var cb invoiceCallback
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCallback, err)
	}
	if cb.ID == "" || cb.ExternalID == "" {
		return nil, fmt.Errorf("%w: missing id or external_id", ErrInvalidCallback)
	}
	settledAt := parseTime(cb.SettlementDate)
	if settledAt == nil {
		settledAt = parseTime(cb.Updated)
	}
	// An unknown status is left empty: the callback is recorded, never applied.
	status, _ := MapStatus(cb.Status)
	return &provider.InboundEvent{
		EventKey:          cb.ID,
		Reference:         cb.ExternalID,
		ProviderPaymentID: cb.ID,
		ProviderStatus:    cb.Status,
		Status:            status,
		Amount:            cb.Amount,
		Currency:          cb.Currency,
		PaidAmount:        cb.PaidAmount,
		Fee:               cb.FeesPaid,
		PaidAt:            parseTime(cb.PaidAt),
		SettledAt:         settledAt,
		Source:            "webhook",
	}, nil
}

// MapStatus normalizes a Xendit invoice status and reports whether Xendit's
// status is one KeiPay knows.
func MapStatus(s string) (domain.PaymentStatus, bool) {
	switch s {
	case "PENDING":
		return domain.PaymentStatusPending, true
	case "PAID":
		return domain.PaymentStatusPaid, true
	case "SETTLED":
		return domain.PaymentStatusSettled, true
	case "EXPIRED":
		return domain.PaymentStatusExpired, true
	case "FAILED", "STOPPED":
		return domain.PaymentStatusFailed, true
	default:
		return "", false
	}
}

// checkoutStatus is the status of a new invoice; one KeiPay does not know is
// still pending, since nothing was paid yet.
func checkoutStatus(s string) domain.PaymentStatus {
	if status, ok := MapStatus(s); ok {
		return status
	}
	return domain.PaymentStatusPending
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}

func buildInvoiceRequest(settings provider.Settings, in provider.CheckoutInput) invoice.CreateInvoiceRequest {
	req := *invoice.NewCreateInvoiceRequest(in.Reference, float64(in.Amount))
	if in.Description != "" {
		req.SetDescription(in.Description)
	}
	if in.PayerEmail != "" {
		req.SetPayerEmail(in.PayerEmail)
	}
	if in.Currency != "" {
		req.SetCurrency(in.Currency)
	}
	if in.SuccessRedirectURL != "" {
		req.SetSuccessRedirectUrl(in.SuccessRedirectURL)
	}
	if in.FailureRedirectURL != "" {
		req.SetFailureRedirectUrl(in.FailureRedirectURL)
	}
	if secs := invoiceDuration(settings, in.ExpiresIn); secs > 0 {
		req.SetInvoiceDuration(strconv.FormatInt(secs, 10))
	}
	if locale := provider.StringSetting(settings, settingLocale); locale != "" {
		req.SetLocale(locale)
	}
	if len(in.Channels) > 0 {
		methods := make([]string, 0, len(in.Channels))
		for _, ch := range in.Channels {
			if code, ok := methodCodes[ch]; ok {
				methods = append(methods, code)
			}
		}
		req.SetPaymentMethods(methods)
	}
	if len(in.Items) > 0 {
		items := make([]invoice.InvoiceItem, 0, len(in.Items))
		for _, it := range in.Items {
			item := *invoice.NewInvoiceItem(it.Name, float32(it.Price), float32(it.Quantity))
			if it.Category != "" {
				item.SetCategory(it.Category)
			}
			if it.ReferenceID != "" {
				item.SetReferenceId(it.ReferenceID)
			}
			if it.URL != "" {
				item.SetUrl(it.URL)
			}
			items = append(items, item)
		}
		req.SetItems(items)
	}
	if in.Customer != nil {
		cust := *invoice.NewCustomerObject()
		if in.Customer.GivenNames != "" {
			cust.SetGivenNames(in.Customer.GivenNames)
		}
		if in.Customer.Surname != "" {
			cust.SetSurname(in.Customer.Surname)
		}
		if in.Customer.Email != "" {
			cust.SetEmail(in.Customer.Email)
		}
		if in.Customer.MobileNumber != "" {
			cust.SetMobileNumber(in.Customer.MobileNumber)
		}
		req.SetCustomer(cust)
	}
	return req
}

// invoiceDuration is the request's lifetime when set, else the connection's
// setting, else 0 (Xendit's default).
func invoiceDuration(settings provider.Settings, expiresIn time.Duration) int64 {
	if secs := int64(expiresIn / time.Second); secs > 0 {
		return secs
	}
	if secs, ok := provider.IntSetting(settings, settingInvoiceDuration); ok && secs > 0 {
		return secs
	}
	return 0
}

// client builds an SDK client for the account's key whose transport records
// the error of a request that got no response, so failures can be classified.
func (a *Adapter) client(acc provider.Account) (*xnd.APIClient, *recorder) {
	api := xnd.NewClient(acc.Credentials[keySecretKey])
	rec := &recorder{base: a.transport}
	if cfg, ok := api.GetConfig().(*xnd.Configuration); ok {
		cfg.HTTPClient = &http.Client{Transport: rec}
		if a.baseURL != "" {
			cfg.Servers = xnd.ServerConfigurations{{URL: a.baseURL}}
		}
	}
	return api, rec
}

// call runs one SDK request and reports whether it failed. The SDK (v6.4.0,
// common.NewXenditSdkError) dereferences a nil body when a request fails
// before any response arrives, which panics; that panic is turned into a
// failure here and classified from what the transport recorded.
func call[T any](execute func() (T, *http.Response, *common.XenditSdkError)) (out T, resp *http.Response, sdkErr *common.XenditSdkError, failed bool) {
	defer func() {
		if recover() != nil {
			failed = true
		}
	}()
	out, resp, sdkErr = execute()
	return out, resp, sdkErr, sdkErr != nil
}

// recorder is an http.RoundTripper that keeps the last transport error.
type recorder struct {
	base http.RoundTripper
	mu   sync.Mutex
	err  error
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.base.RoundTrip(req)
	if err != nil {
		r.mu.Lock()
		r.err = err
		r.mu.Unlock()
	}
	return resp, err
}

func (r *recorder) lastErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// classify turns an SDK failure into one provider error class. create
// selects the class for failures after the request may have been sent:
// ErrUnknownOutcome when creating, ErrRetryable otherwise.
func classify(ctx context.Context, create bool, resp *http.Response, sdkErr *common.XenditSdkError, rec *recorder) error {
	msg := ""
	if sdkErr != nil {
		msg = sdkErr.Error()
	}
	uncertain := provider.ErrRetryable
	if create {
		uncertain = provider.ErrUnknownOutcome
	}
	if resp != nil {
		switch s := resp.StatusCode; {
		case s == http.StatusTooManyRequests:
			return provider.NewStatusError(provider.ErrRetryable, s, msg)
		case s >= 500:
			return provider.NewStatusError(uncertain, s, msg)
		case s >= 400:
			return provider.NewStatusError(provider.ErrRejected, s, msg)
		default:
			return provider.NewStatusError(uncertain, s, "unreadable response")
		}
	}
	if terr := rec.lastErr(); terr != nil {
		if notSent(terr) && ctx.Err() == nil {
			return fmt.Errorf("%w: %v", provider.ErrRetryable, terr)
		}
		return fmt.Errorf("%w: %v", uncertain, terr)
	}
	return fmt.Errorf("%w: %s", uncertain, msg)
}
