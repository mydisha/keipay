package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/xendit"
)

// PaymentService creates payments via Xendit and records them per tenant.
type PaymentService struct {
	payments domain.PaymentRepository
	xnd      xendit.Client
}

// NewPaymentService constructs a PaymentService.
func NewPaymentService(payments domain.PaymentRepository, xnd xendit.Client) *PaymentService {
	return &PaymentService{payments: payments, xnd: xnd}
}

// CreatePaymentInput is a tenant request to create a payment.
type CreatePaymentInput struct {
	OrderID         string
	Amount          int64
	Currency        string
	Description     string
	PayerEmail      string
	InvoiceDuration int
	// Items is the optional order breakdown (products, packages, etc.).
	Items []domain.OrderItem
	// Customer is optional payer detail shown on the hosted invoice.
	Customer *domain.Customer
	// ExpectedSettlementAt is an optional estimate the tenant can show the user.
	ExpectedSettlementAt *time.Time
}

// Create makes a Xendit invoice for the tenant and persists the payment.
// external_id is namespaced as "{slug}:{order_id}" so webhooks route back.
func (s *PaymentService) Create(ctx context.Context, t *domain.Tenant, in CreatePaymentInput) (*domain.Payment, error) {
	orderID := strings.TrimSpace(in.OrderID)
	if orderID == "" {
		return nil, fmt.Errorf("%w: order_id required", domain.ErrInvalidInput)
	}
	if in.Amount <= 0 {
		return nil, fmt.Errorf("%w: amount must be > 0", domain.ErrInvalidInput)
	}
	currency := in.Currency
	if currency == "" {
		currency = "IDR"
	}

	for i, it := range in.Items {
		if strings.TrimSpace(it.Name) == "" {
			return nil, fmt.Errorf("%w: items[%d].name required", domain.ErrInvalidInput, i)
		}
		if it.Quantity <= 0 {
			return nil, fmt.Errorf("%w: items[%d].quantity must be > 0", domain.ErrInvalidInput, i)
		}
		if it.Price < 0 {
			return nil, fmt.Errorf("%w: items[%d].price must be >= 0", domain.ErrInvalidInput, i)
		}
	}

	externalID := t.Slug + ":" + orderID

	// A retried request for the same order gets the original payment back, so a
	// tenant retry never opens a second payable invoice.
	existing, err := s.payments.GetByExternalID(ctx, externalID)
	if err == nil {
		return sameOrder(existing, in.Amount, currency)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	out, err := s.xnd.CreateInvoice(ctx, xendit.CreateInvoiceInput{
		ExternalID:         externalID,
		Amount:             in.Amount,
		Currency:           currency,
		Description:        in.Description,
		PayerEmail:         in.PayerEmail,
		SuccessRedirectURL: t.RedirectSuccessURL,
		FailureRedirectURL: t.RedirectFailureURL,
		InvoiceDuration:    in.InvoiceDuration,
		Items:              in.Items,
		Customer:           in.Customer,
	})
	if err != nil {
		return nil, err
	}

	var expiresAt *time.Time
	if out.ExpiresAt != nil {
		if parsed, perr := time.Parse(time.RFC3339, *out.ExpiresAt); perr == nil {
			expiresAt = &parsed
		}
	}

	p := &domain.Payment{
		TenantID:             t.ID,
		OrderID:              orderID,
		ExternalID:           externalID,
		XenditInvoiceID:      out.InvoiceID,
		Amount:               in.Amount,
		Currency:             currency,
		Status:               out.Status,
		Description:          in.Description,
		InvoiceURL:           out.InvoiceURL,
		PayerEmail:           in.PayerEmail,
		Items:                in.Items,
		ExpiresAt:            expiresAt,
		ExpectedSettlementAt: in.ExpectedSettlementAt,
		Raw:                  out.Raw,
	}
	if err := s.payments.Create(ctx, p); err != nil {
		// The invoice now exists on Xendit without a payment row here; close it so
		// it cannot be paid unrecorded.
		expireErr := s.expireInvoice(ctx, out.InvoiceID)
		if errors.Is(err, domain.ErrConflict) {
			// A concurrent request for the same order won the insert. Its payment
			// is routable via external_id, so return it even if expiry failed.
			if winner, gerr := s.payments.GetByExternalID(ctx, externalID); gerr == nil {
				return sameOrder(winner, in.Amount, currency)
			}
		}
		return nil, errors.Join(err, expireErr)
	}
	return p, nil
}

// sameOrder returns p for a repeated order, or ErrConflict when the repeat asks
// for a different amount or currency than the order was created with.
func sameOrder(p *domain.Payment, amount int64, currency string) (*domain.Payment, error) {
	if p.Amount != amount || p.Currency != currency {
		return nil, fmt.Errorf("%w: order_id already used with a different amount or currency", domain.ErrConflict)
	}
	return p, nil
}

// expireInvoice closes an orphaned invoice. It outlives the request context so a
// disconnected client does not leave the invoice payable.
func (s *PaymentService) expireInvoice(ctx context.Context, invoiceID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.xnd.ExpireInvoice(ctx, invoiceID); err != nil {
		return fmt.Errorf("expire orphan invoice: %w", err)
	}
	return nil
}

// Get returns a payment scoped to a tenant.
func (s *PaymentService) Get(ctx context.Context, tenantID, paymentID int64) (*domain.Payment, error) {
	p, err := s.payments.GetByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if p.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

// List returns payments matching the filter (admin use).
func (s *PaymentService) List(ctx context.Context, f domain.PaymentFilter) ([]domain.Payment, int64, error) {
	return s.payments.List(ctx, f)
}
