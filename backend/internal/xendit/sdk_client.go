package xendit

import (
	"context"
	"encoding/json"
	"fmt"

	xendit "github.com/xendit/xendit-go/v6"
	"github.com/xendit/xendit-go/v6/invoice"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// SDKClient implements Client using the official xendit-go SDK.
type SDKClient struct {
	api *xendit.APIClient
}

// NewSDKClient constructs an SDKClient with the shared account secret key.
func NewSDKClient(secretKey string) *SDKClient {
	return &SDKClient{api: xendit.NewClient(secretKey)}
}

// CreateInvoice creates a hosted invoice on the shared Xendit account.
func (c *SDKClient) CreateInvoice(ctx context.Context, in CreateInvoiceInput) (*CreateInvoiceOutput, error) {
	req := *invoice.NewCreateInvoiceRequest(in.ExternalID, float64(in.Amount))
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
	if in.InvoiceDuration > 0 {
		req.SetInvoiceDuration(fmt.Sprintf("%d", in.InvoiceDuration))
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

	resp, _, err := c.api.InvoiceApi.CreateInvoice(ctx).
		CreateInvoiceRequest(req).
		Execute()
	if err != nil {
		return nil, fmt.Errorf("xendit create invoice: %w", err)
	}

	raw, _ := json.Marshal(resp)
	out := &CreateInvoiceOutput{
		InvoiceID:  resp.GetId(),
		InvoiceURL: resp.GetInvoiceUrl(),
		Status:     MapStatus(string(resp.GetStatus())),
		Raw:        raw,
	}
	if exp := resp.GetExpiryDate(); !exp.IsZero() {
		s := exp.Format("2006-01-02T15:04:05Z07:00")
		out.ExpiresAt = &s
	}
	return out, nil
}

// ExpireInvoice closes an unpaid invoice so it can no longer be paid.
func (c *SDKClient) ExpireInvoice(ctx context.Context, invoiceID string) error {
	if _, _, err := c.api.InvoiceApi.ExpireInvoice(ctx, invoiceID).Execute(); err != nil {
		return fmt.Errorf("xendit expire invoice %s: %w", invoiceID, err)
	}
	return nil
}

// MapStatus normalizes a Xendit invoice/callback status to a domain.PaymentStatus.
func MapStatus(s string) domain.PaymentStatus {
	switch s {
	case "PAID":
		return domain.PaymentStatusPaid
	case "SETTLED":
		return domain.PaymentStatusSettled
	case "EXPIRED":
		return domain.PaymentStatusExpired
	case "FAILED", "STOPPED":
		return domain.PaymentStatusFailed
	default:
		return domain.PaymentStatusPending
	}
}
