package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// cancelTimeout bounds closing an orphaned checkout.
const cancelTimeout = 10 * time.Second

// PaymentService creates checkouts on each app's default connection and
// records them per app.
type PaymentService struct {
	payments domain.PaymentRepository
	conns    domain.ConnectionRepository
	registry *provider.Registry
	sealer   CredentialSealer
	log      *zap.Logger
}

// NewPaymentService constructs a PaymentService. log receives gateway errors,
// whose text never reaches the app.
func NewPaymentService(payments domain.PaymentRepository, conns domain.ConnectionRepository, registry *provider.Registry,
	sealer CredentialSealer, log *zap.Logger) *PaymentService {
	if log == nil {
		log = zap.NewNop()
	}
	return &PaymentService{payments: payments, conns: conns, registry: registry, sealer: sealer, log: log}
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

// Create makes a checkout on the app's default connection and persists the
// payment. It is idempotent per Idempotency-Key (checked first, against a
// fingerprint of the request) and per order_id, and replays are answered
// before the connection is looked at; replayed reports that an existing
// payment was returned. The gateway reference is "kp_" + the payment's
// public id.
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
	// payable checkout.
	existing, err := s.findExisting(ctx, app, idempotencyKey, fingerprint, norm)
	if existing != nil || err != nil {
		return existing, existing != nil, err
	}

	gw, err := s.gateway(ctx, app)
	if err != nil {
		return nil, false, err
	}
	publicID := id.New(id.Payment)
	externalID := "kp_" + publicID
	out, err := gw.prov.CreateCheckout(ctx, gw.account, provider.CheckoutInput{
		Reference:          externalID,
		Amount:             norm.Amount,
		Currency:           norm.Currency,
		Description:        norm.Description,
		PayerEmail:         norm.PayerEmail,
		Channels:           gw.channels,
		Items:              norm.Items,
		Customer:           norm.Customer,
		SuccessRedirectURL: app.RedirectSuccessURL,
		FailureRedirectURL: app.RedirectFailureURL,
		ExpiresIn:          time.Duration(norm.InvoiceDuration) * time.Second,
	})
	if err != nil {
		return nil, false, s.checkoutError(app, gw.conn, err)
	}

	p = &domain.Payment{
		PublicID:               publicID,
		AppID:                  app.ID,
		App:                    app.Summary(),
		OrderID:                norm.OrderID,
		ExternalID:             externalID,
		ConnectionID:           &gw.conn.ID,
		Provider:               gw.conn.Provider,
		XenditInvoiceID:        out.ProviderPaymentID,
		Amount:                 norm.Amount,
		Currency:               norm.Currency,
		Status:                 out.Status,
		Description:            norm.Description,
		InvoiceURL:             out.CheckoutURL,
		PayerEmail:             norm.PayerEmail,
		Items:                  norm.Items,
		Metadata:               meta,
		IdempotencyKey:         idempotencyKey,
		IdempotencyFingerprint: fingerprint,
		ExpiresAt:              out.ExpiresAt,
		ExpectedSettlementAt:   in.ExpectedSettlementAt,
		Raw:                    out.Raw,
	}
	if err := s.payments.Create(ctx, p); err != nil {
		// The checkout now exists at the gateway without a payment row here;
		// close it so it cannot be paid unrecorded.
		cancelErr := s.cancelCheckout(ctx, gw, out.ProviderPaymentID)
		if errors.Is(err, domain.ErrConflict) {
			// A concurrent request with the same key or order won the insert. Its
			// payment is routable by its own reference, so answer with it even if
			// cancelling ours failed.
			winner, ferr := s.findExisting(ctx, app, idempotencyKey, fingerprint, norm)
			if winner != nil || ferr != nil {
				return winner, winner != nil, ferr
			}
		}
		return nil, false, errors.Join(err, cancelErr)
	}
	return p, false, nil
}

// gateway is the connection a checkout is created on, with what the call needs.
type gateway struct {
	conn     *domain.Connection
	prov     provider.Provider
	account  provider.Account
	channels []domain.Channel
}

// gateway resolves the app's default connection. Without a usable one the
// payment is not eligible; a connection that cannot be opened is internal.
func (s *PaymentService) gateway(ctx context.Context, app *domain.App) (*gateway, error) {
	if app.DefaultConnectionID == nil {
		return nil, domain.NoEligibleConnection(domain.ReasonNoDefaultConnection)
	}
	c, err := s.conns.GetByID(ctx, *app.DefaultConnectionID)
	if err != nil {
		return nil, fmt.Errorf("load default connection: %w", err)
	}
	if c.Status != domain.ConnectionActive {
		return nil, domain.NoEligibleConnection(domain.ReasonConnectionDisabled)
	}
	prov, err := s.registry.Get(provider.Name(c.Provider))
	if err != nil {
		return nil, domain.Internal(fmt.Errorf("connection %s: %w", c.PublicID, err))
	}
	acc, err := openAccount(s.sealer, c)
	if err != nil {
		return nil, domain.Internal(err)
	}
	rows, err := s.conns.Channels(ctx, c.ID)
	if err != nil {
		return nil, fmt.Errorf("load channels: %w", err)
	}
	return &gateway{conn: c, prov: prov, account: acc, channels: restrictedChannels(prov.Capabilities(), rows)}, nil
}

// restrictedChannels is the channel list a checkout is limited to: the
// enabled channels when they are a strict subset of the provider's, else
// none, because sending the full list can make a gateway refuse methods the
// account has not activated.
func restrictedChannels(caps provider.Capabilities, rows []domain.ConnectionChannel) []domain.Channel {
	if !caps.CanRestrictChannels {
		return nil
	}
	var enabled []domain.Channel
	for _, st := range effectiveChannels(caps.Channels, rows) {
		if st.Enabled {
			enabled = append(enabled, st.Channel)
		}
	}
	if len(enabled) == len(caps.Channels) {
		return nil
	}
	return enabled
}

// checkoutError maps a CreateCheckout failure to what the app sees; the
// gateway's own text only goes to the log.
func (s *PaymentService) checkoutError(app *domain.App, c *domain.Connection, err error) error {
	fields := []zap.Field{zap.String("app_id", app.PublicID), zap.String("connection_id", c.PublicID),
		zap.String("provider", c.Provider), zap.Error(err)}
	switch {
	case errors.Is(err, provider.ErrRejected):
		s.log.Warn("provider rejected checkout", fields...)
		return domain.ProviderRejected(c.Provider, err)
	case errors.Is(err, provider.ErrRetryable), errors.Is(err, provider.ErrUnknownOutcome):
		s.log.Warn("provider unavailable for checkout", fields...)
		return domain.ProviderUnavailable(err)
	default:
		return fmt.Errorf("create checkout: %w", err)
	}
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

// cancelCheckout closes an orphaned checkout. It outlives the request context
// so a disconnected client does not leave the checkout payable.
func (s *PaymentService) cancelCheckout(ctx context.Context, gw *gateway, providerPaymentID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cancelTimeout)
	defer cancel()
	if err := gw.prov.CancelCheckout(ctx, gw.account, providerPaymentID); err != nil {
		return fmt.Errorf("cancel orphan checkout: %w", err)
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
