package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/xendit"
)

// PaymentService creates payments via Xendit and records them per app.
type PaymentService struct {
	payments domain.PaymentRepository
	xnd      xendit.Client
}

// NewPaymentService constructs a PaymentService.
func NewPaymentService(payments domain.PaymentRepository, xnd xendit.Client) *PaymentService {
	return &PaymentService{payments: payments, xnd: xnd}
}

// CreatePaymentInput is an app's request to create a payment.
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
	// ExpectedSettlementAt is an optional estimate the app can show the user.
	ExpectedSettlementAt *time.Time
	// Metadata is raw JSON; omitted or null is {}.
	Metadata json.RawMessage
}

// PaymentRef names a payment by its pay_ id or, for older integrations, its
// integer id. Exactly one is set.
type PaymentRef struct {
	PublicID string
	ID       int64
}

// Create makes a Xendit invoice for the app and persists the payment. It is
// idempotent per Idempotency-Key (checked first, against a fingerprint of the
// request) and per order_id; replayed reports that an existing payment was
// returned. The gateway reference is "kp_" + the payment's public id.
func (s *PaymentService) Create(ctx context.Context, app *domain.App, in CreatePaymentInput, idempotencyKey string) (p *domain.Payment, replayed bool, err error) {
	var fe fieldErrors
	meta, metaErrs := parseMetadata(in.Metadata)
	fe = append(fe, metaErrs...)
	fe.check("Idempotency-Key", idempotencyKeyRule(idempotencyKey))
	if err := fe.err(); err != nil {
		return nil, false, err
	}
	norm, err := normalizePayment(in, meta)
	if err != nil {
		return nil, false, err
	}
	var fingerprint string
	if idempotencyKey != "" {
		fingerprint = paymentFingerprint(norm)
	}

	// A retry gets the original payment back, so it never opens a second
	// payable invoice.
	existing, err := s.findExisting(ctx, app, idempotencyKey, fingerprint, norm)
	if existing != nil || err != nil {
		return existing, existing != nil, err
	}

	publicID := id.New(id.Payment)
	externalID := "kp_" + publicID
	out, err := s.xnd.CreateInvoice(ctx, xendit.CreateInvoiceInput{
		ExternalID:         externalID,
		Amount:             norm.Amount,
		Currency:           norm.Currency,
		Description:        norm.Description,
		PayerEmail:         norm.PayerEmail,
		SuccessRedirectURL: app.RedirectSuccessURL,
		FailureRedirectURL: app.RedirectFailureURL,
		InvoiceDuration:    norm.InvoiceDuration,
		Items:              norm.Items,
		Customer:           norm.Customer,
	})
	if err != nil {
		return nil, false, err
	}

	var expiresAt *time.Time
	if out.ExpiresAt != nil {
		if parsed, perr := time.Parse(time.RFC3339, *out.ExpiresAt); perr == nil {
			expiresAt = &parsed
		}
	}
	p = &domain.Payment{
		PublicID:               publicID,
		AppID:                  app.ID,
		App:                    app.Summary(),
		OrderID:                norm.OrderID,
		ExternalID:             externalID,
		XenditInvoiceID:        out.InvoiceID,
		Amount:                 norm.Amount,
		Currency:               norm.Currency,
		Status:                 out.Status,
		Description:            norm.Description,
		InvoiceURL:             out.InvoiceURL,
		PayerEmail:             norm.PayerEmail,
		Items:                  norm.Items,
		Metadata:               meta,
		IdempotencyKey:         idempotencyKey,
		IdempotencyFingerprint: fingerprint,
		ExpiresAt:              expiresAt,
		ExpectedSettlementAt:   in.ExpectedSettlementAt,
		Raw:                    out.Raw,
	}
	if err := s.payments.Create(ctx, p); err != nil {
		// The invoice now exists on Xendit without a payment row here; close it so
		// it cannot be paid unrecorded.
		expireErr := s.expireInvoice(ctx, out.InvoiceID)
		if errors.Is(err, domain.ErrConflict) {
			// A concurrent request with the same key or order won the insert. Its
			// payment is routable by its own reference, so answer with it even if
			// expiring ours failed.
			winner, ferr := s.findExisting(ctx, app, idempotencyKey, fingerprint, norm)
			if winner != nil || ferr != nil {
				return winner, winner != nil, ferr
			}
		}
		return nil, false, errors.Join(err, expireErr)
	}
	return p, false, nil
}

// normalizePayment trims and defaults the input and checks the business rules.
func normalizePayment(in CreatePaymentInput, meta domain.Metadata) (normalizedPayment, error) {
	orderID := strings.TrimSpace(in.OrderID)
	if orderID == "" {
		return normalizedPayment{}, fmt.Errorf("%w: order_id required", domain.ErrInvalidInput)
	}
	if in.Amount <= 0 {
		return normalizedPayment{}, fmt.Errorf("%w: amount must be > 0", domain.ErrInvalidInput)
	}
	for i, it := range in.Items {
		if strings.TrimSpace(it.Name) == "" {
			return normalizedPayment{}, fmt.Errorf("%w: items[%d].name required", domain.ErrInvalidInput, i)
		}
		if it.Quantity <= 0 {
			return normalizedPayment{}, fmt.Errorf("%w: items[%d].quantity must be > 0", domain.ErrInvalidInput, i)
		}
		if it.Price < 0 {
			return normalizedPayment{}, fmt.Errorf("%w: items[%d].price must be >= 0", domain.ErrInvalidInput, i)
		}
	}
	currency := in.Currency
	if currency == "" {
		currency = "IDR"
	}
	items := in.Items
	if len(items) == 0 {
		items = nil
	}
	n := normalizedPayment{
		OrderID:         orderID,
		Amount:          in.Amount,
		Currency:        currency,
		Description:     in.Description,
		PayerEmail:      in.PayerEmail,
		InvoiceDuration: in.InvoiceDuration,
		Items:           items,
		Customer:        in.Customer,
		Metadata:        meta,
	}
	if in.ExpectedSettlementAt != nil {
		n.ExpectedSettlementAt = in.ExpectedSettlementAt.UTC().Format(time.RFC3339Nano)
	}
	return n, nil
}

// findExisting returns the payment a repeated request refers to, or nil when
// there is none. The Idempotency-Key decides first: the same key with another
// fingerprint is ErrIdempotencyKeyReused. Otherwise a payment with the same
// order_id is returned when amount and currency match, else ErrConflict; the
// key is not attached to it.
func (s *PaymentService) findExisting(ctx context.Context, app *domain.App, key, fingerprint string, in normalizedPayment) (*domain.Payment, error) {
	if key != "" {
		p, err := s.payments.GetByIdempotencyKey(ctx, app.ID, key)
		switch {
		case err == nil && p.IdempotencyFingerprint == fingerprint:
			return p, nil
		case err == nil:
			return nil, domain.ErrIdempotencyKeyReused
		case !errors.Is(err, domain.ErrNotFound):
			return nil, fmt.Errorf("load payment by idempotency key: %w", err)
		}
	}
	p, err := s.payments.GetByOrderID(ctx, app.ID, in.OrderID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load payment by order id: %w", err)
	}
	if p.Amount != in.Amount || p.Currency != in.Currency {
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

// Get returns one of the app's payments; another app's payment is not found.
func (s *PaymentService) Get(ctx context.Context, app *domain.App, ref PaymentRef) (*domain.Payment, error) {
	var (
		p   *domain.Payment
		err error
	)
	if ref.PublicID != "" {
		p, err = s.payments.GetByPublicID(ctx, app.ID, ref.PublicID)
	} else {
		p, err = s.payments.GetByID(ctx, app.ID, ref.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("get payment: %w", err)
	}
	return p, nil
}

// List returns one page of payments matching the filter, newest first.
func (s *PaymentService) List(ctx context.Context, f domain.PaymentFilter, req domain.PageRequest) (domain.PageResult[domain.Payment], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.Payment]{}, err
	}
	res, err := s.payments.List(ctx, f, p)
	if err != nil {
		return domain.PageResult[domain.Payment]{}, fmt.Errorf("list payments: %w", err)
	}
	return res, nil
}
