package domain

import (
	"encoding/json"
	"time"
)

// PaymentStatus is the normalized lifecycle state of a payment.
type PaymentStatus string

const (
	PaymentStatusPending PaymentStatus = "PENDING"
	PaymentStatusPaid    PaymentStatus = "PAID"
	PaymentStatusSettled PaymentStatus = "SETTLED"
	PaymentStatusExpired PaymentStatus = "EXPIRED"
	PaymentStatusFailed  PaymentStatus = "FAILED"
)

// IsTerminal reports whether the status will not change further.
func (s PaymentStatus) IsTerminal() bool {
	switch s {
	case PaymentStatusSettled, PaymentStatusExpired, PaymentStatusFailed:
		return true
	default:
		return false
	}
}

// IsSuccessful reports whether funds have been captured (paid or settled).
func (s PaymentStatus) IsSuccessful() bool {
	return s == PaymentStatusPaid || s == PaymentStatusSettled
}

// CanTransitionTo reports whether a callback moving the payment from s to next
// should be applied. Callbacks can arrive out of order, so status only moves
// forward (PENDING → PAID → SETTLED). A late success after EXPIRED or FAILED is
// still applied, because the funds were captured.
func (s PaymentStatus) CanTransitionTo(next PaymentStatus) bool {
	if next == s {
		return false
	}
	switch s {
	case PaymentStatusPending:
		return true
	case PaymentStatusPaid:
		return next == PaymentStatusSettled
	case PaymentStatusExpired, PaymentStatusFailed:
		return next.IsSuccessful()
	default:
		return false
	}
}

// OrderItem is a single line in an order (product, package, etc.). It is sent to
// Xendit so the hosted invoice can show a breakdown, and stored for our records.
type OrderItem struct {
	Name        string `json:"name"`
	Quantity    int32  `json:"quantity"`
	Price       int64  `json:"price"`
	Category    string `json:"category,omitempty"`
	ReferenceID string `json:"reference_id,omitempty"`
	URL         string `json:"url,omitempty"`
}

// Customer carries optional payer details forwarded to Xendit's invoice.
type Customer struct {
	GivenNames   string `json:"given_names,omitempty"`
	Surname      string `json:"surname,omitempty"`
	Email        string `json:"email,omitempty"`
	MobileNumber string `json:"mobile_number,omitempty"`
}

// Payment is an invoice created on the operator's Xendit account on behalf of
// an app.
type Payment struct {
	ID       int64
	PublicID string
	AppID    int64
	// App is the owning app's summary; repositories fill it on every read.
	App        AppSummary
	OrderID    string
	ExternalID string
	// ConnectionID and Provider name the connection the checkout was created
	// on; nil and "" for payments made before connections existed.
	ConnectionID *int64
	Provider     string
	// XenditInvoiceID is the provider's payment id; the column predates
	// providers other than Xendit.
	XenditInvoiceID string
	Amount          int64
	Currency        string
	Status          PaymentStatus
	Description     string
	InvoiceURL      string
	PayerEmail      string
	Items           []OrderItem
	Metadata        Metadata
	// IdempotencyKey and IdempotencyFingerprint are empty when the request that
	// created the payment sent no Idempotency-Key.
	IdempotencyKey         string
	IdempotencyFingerprint string
	ExpiresAt              *time.Time
	PaidAt                 *time.Time
	// SettledAt is the actual time funds settled to the shared balance, sourced
	// from Xendit's SETTLED callback. Nil until settlement is confirmed.
	SettledAt *time.Time
	// ExpectedSettlementAt is the estimated settlement date the app can show
	// to the user before funds actually settle. Optional, set at creation time.
	ExpectedSettlementAt *time.Time
	Raw                  json.RawMessage `json:"-"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Reference is the gateway reference stored as external_id: "kp_" + public id
// for new payments, "{slug}:{order_id}" for payments made before apps.
func (p *Payment) Reference() string { return p.ExternalID }
