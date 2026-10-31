// Package doku is the provider adapter for hosted checkouts through DOKU
// Checkout. Requests and notifications are signed with DOKU's non-SNAP
// scheme (HMAC-SHA256 over Client-Id, Request-Id, Request-Timestamp,
// Request-Target and the body digest) using the connection's own client id
// and secret key.
package doku

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Name is the provider name of the adapter.
const Name provider.Name = "doku"

// Credential keys.
const (
	keyClientID  = "client_id"
	keySecretKey = "secret_key"
)

// Setting keys.
const (
	settingPaymentDueDate = "payment_due_date"
	settingLanguage       = "language"
	settingAutoRedirect   = "auto_redirect"
)

const (
	defaultTestTimeout = 10 * time.Second
	readTimeout        = 30 * time.Second
	// maxDueMinutes is the largest payment_due_date DOKU takes (6 digits).
	maxDueMinutes = 999999
)

// currency is the only currency DOKU Checkout charges in.
const currency = "IDR"

// wib is the zone of the dates DOKU writes without an offset (UTC+7).
var wib = time.FixedZone("WIB", 7*60*60)

// methodCodes maps channels to DOKU Checkout payment_method_types.
var methodCodes = map[domain.Channel]string{
	domain.ChannelQRIS:             "QRIS",
	domain.ChannelVABCA:            "VIRTUAL_ACCOUNT_BCA",
	domain.ChannelVABNI:            "VIRTUAL_ACCOUNT_BNI",
	domain.ChannelVABRI:            "VIRTUAL_ACCOUNT_BRI",
	domain.ChannelVAMandiri:        "VIRTUAL_ACCOUNT_BANK_MANDIRI",
	domain.ChannelVAPermata:        "VIRTUAL_ACCOUNT_BANK_PERMATA",
	domain.ChannelVABSI:            "VIRTUAL_ACCOUNT_BANK_SYARIAH_MANDIRI",
	domain.ChannelEwalletOVO:       "EMONEY_OVO",
	domain.ChannelEwalletDANA:      "EMONEY_DANA",
	domain.ChannelEwalletShopeePay: "EMONEY_SHOPEE_PAY",
	domain.ChannelEwalletLinkAja:   "EMONEY_LINKAJA",
	domain.ChannelCard:             "CREDIT_CARD",
	domain.ChannelRetailAlfamart:   "ONLINE_TO_OFFLINE_ALFA",
	domain.ChannelRetailIndomaret:  "ONLINE_TO_OFFLINE_INDOMARET",
}

// Adapter implements provider.Provider for DOKU.
type Adapter struct {
	baseURL     string
	transport   http.RoundTripper
	testTimeout time.Duration
	now         func() time.Time
}

// Option configures an Adapter.
type Option func(*Adapter)

// WithBaseURL points the adapter at another API server, e.g. a test server,
// for both environments.
func WithBaseURL(u string) Option { return func(a *Adapter) { a.baseURL = strings.TrimRight(u, "/") } }

// WithTransport replaces the HTTP transport.
func WithTransport(rt http.RoundTripper) Option { return func(a *Adapter) { a.transport = rt } }

// WithTestTimeout bounds TestCredentials; the default is 10 seconds.
func WithTestTimeout(d time.Duration) Option { return func(a *Adapter) { a.testTimeout = d } }

// WithClock replaces the clock that stamps requests.
func WithClock(now func() time.Time) Option { return func(a *Adapter) { a.now = now } }

// New returns the DOKU adapter.
func New(opts ...Option) *Adapter {
	a := &Adapter{transport: http.DefaultTransport, testTimeout: defaultTestTimeout, now: time.Now}
	for _, o := range opts {
		o(a)
	}
	return a
}

var (
	_ provider.Provider       = (*Adapter)(nil)
	_ provider.CheckoutReader = (*Adapter)(nil)
)

// Descriptor describes DOKU for the console.
func (a *Adapter) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		Name:        Name,
		DisplayName: "DOKU",
		Description: "Hosted checkout through DOKU Checkout.",
		DocsURL:     "https://developers.doku.com",
		Webhook: provider.WebhookSetup{
			Instructions: []string{
				"In the DOKU Back Office, open Settings → Payment Settings.",
				"For every payment method this connection offers, choose Configure and paste the callback URL as its Notification URL.",
				"DOKU signs each notification with the path of that URL, so paste it exactly as shown.",
			},
			DashboardURL: "https://dashboard.doku.com",
		},
	}
}

// CredentialSchema is the client id and the secret key of the DOKU account.
func (a *Adapter) CredentialSchema() provider.Schema {
	return provider.Schema{Fields: []provider.Field{
		{
			Key:         keyClientID,
			Type:        provider.TypeString,
			Label:       "Client ID",
			Description: "DOKU Back Office → Integration → API Keys → Client ID.",
			Placeholder: "BRN-…",
			Required:    true,
			MaxLength:   provider.IntPtr(64),
			Pattern:     `^[A-Za-z0-9-]+$`,
			PatternHint: "Letters, digits and dashes, as shown in the Back Office.",
		},
		{
			Key:         keySecretKey,
			Type:        provider.TypeString,
			Label:       "Secret key",
			Description: "DOKU Back Office → Integration → API Keys → Secret Key. It signs requests and verifies notifications.",
			Placeholder: "SK-…",
			Secret:      true,
			Required:    true,
			MinLength:   provider.IntPtr(8),
			MaxLength:   provider.IntPtr(255),
		},
	}}
}

// SettingsSchema is the default checkout lifetime, its language and whether
// DOKU sends the customer back after paying.
func (a *Adapter) SettingsSchema() provider.Schema {
	return provider.Schema{Fields: []provider.Field{
		{
			Key:         settingPaymentDueDate,
			Type:        provider.TypeInteger,
			Label:       "Checkout lifetime (minutes)",
			Description: "Used when a payment request has no invoice_duration. Empty uses DOKU's default (60 minutes).",
			Minimum:     provider.Int64Ptr(1),
			Maximum:     provider.Int64Ptr(maxDueMinutes),
		},
		{
			Key:     settingLanguage,
			Type:    provider.TypeEnum,
			Label:   "Checkout language",
			Options: []provider.Option{{Value: "ID", Label: "Indonesian"}, {Value: "EN", Label: "English"}},
		},
		{
			Key:         settingAutoRedirect,
			Type:        provider.TypeBoolean,
			Label:       "Return the customer after payment",
			Description: "Sends the customer to the payment's success URL once paid.",
			Default:     true,
		},
	}}
}

// Capabilities lists every Checkout channel KeiPay maps. DOKU reports no fee
// and has no transaction list API, so DOKU connections are not reconciled.
func (a *Adapter) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Channels:            domain.AllChannels(),
		Currencies:          []string{currency},
		CanRestrictChannels: true,
		ReadCheckout:        true,
	}
}

// ValidateAccount has no rule beyond the schema: DOKU's client ids and keys
// do not say which environment they belong to.
func (a *Adapter) ValidateAccount(domain.Environment, provider.Credentials) []domain.FieldError {
	return nil
}

// TestCredentials asks for the status of an order that does not exist. DOKU
// checks the signature first, so a not-found answer means the client id and
// secret key were accepted.
func (a *Adapter) TestCredentials(ctx context.Context, acc provider.Account) error {
	ctx, cancel := context.WithTimeout(ctx, a.testTimeout)
	defer cancel()
	ans, err := a.call(ctx, acc, http.MethodGet, statusPath(invoicePrefix+newRequestID()), nil)
	if err != nil {
		return fmt.Errorf("doku test credentials: %w", transportError(ctx, false, err))
	}
	if ans.Status == http.StatusNotFound || (ans.Status >= 200 && ans.Status < 300) {
		return nil
	}
	return fmt.Errorf("doku test credentials: %w", statusError(false, ans))
}

// checkoutRequest is the body of POST /checkout/v1/payment.
type checkoutRequest struct {
	Order    checkoutOrder     `json:"order"`
	Payment  checkoutPayment   `json:"payment"`
	Customer *checkoutCustomer `json:"customer,omitempty"`
}

type checkoutOrder struct {
	Amount            int64      `json:"amount"`
	InvoiceNumber     string     `json:"invoice_number"`
	Currency          string     `json:"currency"`
	CallbackURL       string     `json:"callback_url,omitempty"`
	CallbackURLResult string     `json:"callback_url_result,omitempty"`
	CallbackURLCancel string     `json:"callback_url_cancel,omitempty"`
	AutoRedirect      bool       `json:"auto_redirect"`
	Language          string     `json:"language,omitempty"`
	LineItems         []lineItem `json:"line_items,omitempty"`
}

type lineItem struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Price    int64  `json:"price"`
	Quantity int32  `json:"quantity"`
	Category string `json:"category,omitempty"`
	URL      string `json:"url,omitempty"`
}

type checkoutPayment struct {
	PaymentDueDate     int64    `json:"payment_due_date,omitempty"`
	PaymentMethodTypes []string `json:"payment_method_types,omitempty"`
}

type checkoutCustomer struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

// checkoutResponse is the part of DOKU's answer KeiPay reads.
type checkoutResponse struct {
	Response struct {
		Order struct {
			InvoiceNumber string `json:"invoice_number"`
		} `json:"order"`
		Payment struct {
			URL         string `json:"url"`
			ExpiredDate string `json:"expired_date"`
		} `json:"payment"`
	} `json:"response"`
}

const checkoutPath = "/checkout/v1/payment"

// CreateCheckout creates a DOKU Checkout payment page. The provider payment
// id is the invoice number, which the status API is keyed by.
func (a *Adapter) CreateCheckout(ctx context.Context, acc provider.Account, in provider.CheckoutInput) (*provider.CheckoutOutput, error) {
	req, err := buildCheckoutRequest(acc.Settings, in)
	if err != nil {
		return nil, fmt.Errorf("doku create checkout: %w", err)
	}
	ans, err := a.call(ctx, acc, http.MethodPost, checkoutPath, req)
	if err != nil {
		return nil, fmt.Errorf("doku create checkout: %w", transportError(ctx, true, err))
	}
	if ans.Status < 200 || ans.Status > 299 {
		return nil, fmt.Errorf("doku create checkout: %w", statusError(true, ans))
	}
	var resp checkoutResponse
	if err := json.Unmarshal(ans.Body, &resp); err != nil || resp.Response.Payment.URL == "" {
		// The page may exist; only a read can tell.
		return nil, fmt.Errorf("doku create checkout: %w",
			provider.NewStatusError(provider.ErrUnknownOutcome, ans.Status, "answer without a payment url"))
	}
	out := &provider.CheckoutOutput{
		ProviderPaymentID: req.Order.InvoiceNumber,
		CheckoutURL:       resp.Response.Payment.URL,
		Status:            domain.PaymentStatusPending,
		Raw:               ans.Body,
	}
	if t, err := time.ParseInLocation("20060102150405", resp.Response.Payment.ExpiredDate, wib); err == nil {
		t = t.UTC()
		out.ExpiresAt = &t
	}
	return out, nil
}

func buildCheckoutRequest(settings provider.Settings, in provider.CheckoutInput) (*checkoutRequest, error) {
	if in.Currency != "" && !strings.EqualFold(in.Currency, currency) {
		return nil, fmt.Errorf("%w: DOKU Checkout charges only %s, not %s", provider.ErrRejected, currency, in.Currency)
	}
	invoice, err := invoiceNumber(in.Reference)
	if err != nil {
		return nil, err
	}
	autoRedirect := true
	if v, ok := settings[settingAutoRedirect].(bool); ok {
		autoRedirect = v
	}
	req := &checkoutRequest{
		Order: checkoutOrder{
			// IDR has no minor unit, so the amount is already in rupiah.
			Amount:            in.Amount,
			InvoiceNumber:     invoice,
			Currency:          currency,
			CallbackURL:       in.SuccessRedirectURL,
			CallbackURLResult: in.SuccessRedirectURL,
			CallbackURLCancel: in.FailureRedirectURL,
			AutoRedirect:      autoRedirect && in.SuccessRedirectURL != "",
			Language:          provider.StringSetting(settings, settingLanguage),
			LineItems:         lineItems(in.Items, in.Amount),
		},
		Payment:  checkoutPayment{PaymentDueDate: dueMinutes(settings, in.ExpiresIn)},
		Customer: customer(in.Customer, in.PayerEmail),
	}
	for _, ch := range in.Channels {
		if code, ok := methodCodes[ch]; ok {
			req.Payment.PaymentMethodTypes = append(req.Payment.PaymentMethodTypes, code)
		}
	}
	return req, nil
}

// lineItems are the order's items when they add up to the amount; DOKU
// refuses items that do not for the channels that read them, so they are
// left out then.
func lineItems(items []domain.OrderItem, amount int64) []lineItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]lineItem, 0, len(items))
	var total int64
	for _, it := range items {
		total += it.Price * int64(it.Quantity)
		out = append(out, lineItem{
			ID: it.ReferenceID, Name: it.Name, Price: it.Price, Quantity: it.Quantity, Category: it.Category, URL: it.URL,
		})
	}
	if total != amount {
		return nil
	}
	return out
}

// customer is the payer as DOKU takes it, nil when nothing is known.
func customer(c *domain.Customer, payerEmail string) *checkoutCustomer {
	out := checkoutCustomer{Email: payerEmail}
	if c != nil {
		out.Name = strings.TrimSpace(c.GivenNames + " " + c.Surname)
		if c.Email != "" {
			out.Email = c.Email
		}
		out.Phone = phone(c.MobileNumber)
	}
	if out == (checkoutCustomer{}) {
		return nil
	}
	return &out
}

// phone is a mobile number in DOKU's {calling code}{number} form: digits
// only, "+62812…" becoming "62812…".
func phone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dueMinutes is the request's lifetime rounded up to whole minutes when
// set, else the connection's setting, else 0 (DOKU's default).
func dueMinutes(settings provider.Settings, expiresIn time.Duration) int64 {
	if expiresIn > 0 {
		m := int64((expiresIn + time.Minute - 1) / time.Minute)
		return min(m, maxDueMinutes)
	}
	if m, ok := provider.IntSetting(settings, settingPaymentDueDate); ok && m > 0 {
		return m
	}
	return 0
}

// CancelCheckout asks DOKU to cancel an unpaid order. DOKU only cancels
// orders of some virtual accounts and QRIS, and only when the merchant
// enabled order cancellation, so a refusal is common and reported as
// ErrRejected.
func (a *Adapter) CancelCheckout(ctx context.Context, acc provider.Account, invoice string) error {
	body := map[string]any{"order": map[string]string{"invoice_number": invoice}}
	ans, err := a.call(ctx, acc, http.MethodPost, "/checkout/v3/cancellations", body)
	if err != nil {
		return fmt.Errorf("doku cancel order %s: %w", invoice, transportError(ctx, false, err))
	}
	if ans.Status < 200 || ans.Status > 299 {
		return fmt.Errorf("doku cancel order %s: %w", invoice, statusError(false, ans))
	}
	return nil
}

// VerifyWebhook recomputes the notification's Signature with the
// connection's secret key over the path DOKU called.
func (a *Adapter) VerifyWebhook(acc provider.Account, r provider.CallbackRequest) error {
	if !verifyNotification(r.Header, r.Body, r.Path, acc.Credentials[keyClientID], acc.Credentials[keySecretKey]) {
		return provider.ErrInvalidSignature
	}
	return nil
}

// notification is the part of a DOKU notification, or of a status answer,
// KeiPay reads.
type notification struct {
	Order struct {
		InvoiceNumber string `json:"invoice_number"`
		Amount        amount `json:"amount"`
		Currency      string `json:"currency"`
		Status        string `json:"status"`
	} `json:"order"`
	Transaction struct {
		Status string `json:"status"`
		Date   string `json:"date"`
	} `json:"transaction"`
}

// amount is an order amount DOKU writes as a number or as a quoted number.
type amount string

func (n *amount) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = amount(s)
		return nil
	}
	var v json.Number
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("amount %s is not a number", b)
	}
	*n = amount(v)
	return nil
}

// ErrInvalidCallback is a body that is not a DOKU notification.
var ErrInvalidCallback = errors.New("invalid doku notification")

// ParseWebhook reads a notification. DOKU keys an order by its invoice
// number, so that is the event key; the status tells the events of one order
// apart.
func (a *Adapter) ParseWebhook(raw []byte) (*provider.InboundEvent, error) {
	var n notification
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCallback, err)
	}
	invoice := n.Order.InvoiceNumber
	providerStatus := n.Transaction.Status
	// An order whose page expired before a method was chosen has no
	// transaction status of its own.
	if (providerStatus == "" || providerStatus == "PENDING") && n.Order.Status == "ORDER_EXPIRED" {
		providerStatus = n.Order.Status
	}
	if invoice == "" || providerStatus == "" {
		return nil, fmt.Errorf("%w: missing invoice_number or status", ErrInvalidCallback)
	}
	var minor int64
	if n.Order.Amount != "" {
		var err error
		if minor, err = domain.MinorUnits(string(n.Order.Amount), currency); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCallback, err)
		}
	}
	// An unknown status is left empty: the callback is recorded, never applied.
	status, _ := MapStatus(providerStatus)
	ev := &provider.InboundEvent{
		EventKey:          invoice,
		Reference:         reference(invoice),
		ProviderPaymentID: invoice,
		ProviderStatus:    providerStatus,
		Status:            status,
		Amount:            minor,
		Currency:          strings.ToUpper(n.Order.Currency),
		Source:            domain.InboundSourceWebhook,
	}
	if status == domain.PaymentStatusPaid {
		ev.PaidAmount = minor
		if t, err := time.Parse(time.RFC3339, n.Transaction.Date); err == nil {
			ev.PaidAt = &t
		}
	}
	return ev, nil
}

// MapStatus normalizes a DOKU transaction or order status and reports
// whether DOKU's status is one KeiPay knows. Refund statuses are known but
// have no KeiPay status, so they come back empty and are recorded unapplied.
func MapStatus(s string) (domain.PaymentStatus, bool) {
	switch s {
	// TIMEOUT (an e-wallet that did not answer yet) and REDIRECT (a card
	// waiting for 3-D Secure) are not final.
	case "PENDING", "TIMEOUT", "REDIRECT", "ORDER_GENERATED", "ORDER_RECOVERED":
		return domain.PaymentStatusPending, true
	case "SUCCESS":
		return domain.PaymentStatusPaid, true
	case "FAILED", "CANCELLED":
		return domain.PaymentStatusFailed, true
	case "EXPIRED", "ORDER_EXPIRED":
		return domain.PaymentStatusExpired, true
	case "REFUNDED", "PARTIAL_REFUNDED", "VOIDED":
		return "", true
	default:
		return "", false
	}
}
