// Package xendit wraps the Xendit Invoice API for the single shared account.
package xendit

import (
	"context"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// CreateInvoiceInput is the normalized request to create a hosted invoice.
type CreateInvoiceInput struct {
	ExternalID         string
	Amount             int64
	Currency           string
	Description        string
	PayerEmail         string
	SuccessRedirectURL string
	FailureRedirectURL string
	// InvoiceDuration is the seconds until the invoice expires; 0 uses Xendit default.
	InvoiceDuration int
	// Items is the optional order breakdown shown on the hosted invoice.
	Items []domain.OrderItem
	// Customer is optional payer detail shown on the hosted invoice.
	Customer *domain.Customer
}

// CreateInvoiceOutput is the normalized result of invoice creation.
type CreateInvoiceOutput struct {
	InvoiceID  string
	InvoiceURL string
	Status     domain.PaymentStatus
	ExpiresAt  *string // RFC3339 from Xendit; parsed by caller
	Raw        []byte
}

// Client is the port the payment service depends on. Implemented by SDKClient
// and mockable in tests.
type Client interface {
	CreateInvoice(ctx context.Context, in CreateInvoiceInput) (*CreateInvoiceOutput, error)
	// ExpireInvoice closes an unpaid invoice so it can no longer be paid.
	ExpireInvoice(ctx context.Context, invoiceID string) error
}
