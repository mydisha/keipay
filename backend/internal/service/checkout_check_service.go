package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// When an expired checkout is read back: after checkoutGrace, so the
// gateway's own callback comes first, at most every checkoutRecheck per
// payment, and for checkoutWindow after the expiry. checkoutBatch caps one
// pass.
const (
	checkoutGrace   = 2 * time.Minute
	checkoutRecheck = 10 * time.Minute
	checkoutWindow  = 72 * time.Hour
	checkoutBatch   = 50
)

// CallbackProcessor applies a gateway event as a callback would;
// WebhookService implements it.
type CallbackProcessor interface {
	Process(ctx context.Context, conn *domain.Connection, ev provider.InboundEvent, raw []byte) error
}

// CheckoutCheckService reads back, from the gateway, the checkouts of
// pending payments that expired without a callback, and applies what it
// says through the callback path: a lost or never-sent callback cannot leave
// a payment pending for good.
type CheckoutCheckService struct {
	checks    domain.CheckoutCheckRepository
	conns     domain.ConnectionRepository
	registry  *provider.Registry
	sealer    CredentialSealer
	callbacks CallbackProcessor
	log       *zap.Logger
	now       func() time.Time
}

// CheckoutCheckDeps are the CheckoutCheckService's collaborators.
type CheckoutCheckDeps struct {
	Checks      domain.CheckoutCheckRepository
	Connections domain.ConnectionRepository
	Registry    *provider.Registry
	Sealer      CredentialSealer
	Callbacks   CallbackProcessor
	Log         *zap.Logger
}

// NewCheckoutCheckService constructs a CheckoutCheckService.
func NewCheckoutCheckService(d CheckoutCheckDeps) *CheckoutCheckService {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &CheckoutCheckService{checks: d.Checks, conns: d.Connections, registry: d.Registry, sealer: d.Sealer,
		callbacks: d.Callbacks, log: log, now: time.Now}
}

// CheckOverdue reads back one batch of overdue checkouts and returns how
// many it read. Every payment it tries is marked checked, whatever the
// outcome, so a failing gateway is asked again only after checkoutRecheck;
// a failure is logged, not returned. Only failing to list is returned.
func (s *CheckoutCheckService) CheckOverdue(ctx context.Context) (int, error) {
	now := s.now().UTC()
	due, err := s.checks.ListOverdue(ctx, domain.OverdueCheckoutQuery{
		ExpiredBefore: now.Add(-checkoutGrace), NotBefore: now.Add(-checkoutWindow),
		CheckedBefore: now.Add(-checkoutRecheck), Limit: checkoutBatch,
	})
	if err != nil {
		return 0, err
	}
	conns := map[int64]*domain.Connection{}
	checked := 0
	for _, c := range due {
		if ctx.Err() != nil {
			return checked, nil
		}
		err := s.check(ctx, c, conns)
		if markErr := s.checks.MarkChecked(ctx, c.PaymentID, now); markErr != nil {
			err = errors.Join(err, markErr)
		}
		checked++
		if err != nil && ctx.Err() == nil {
			logger.Ctx(ctx, s.log).Warn("checkout check failed",
				zap.String(logger.FieldLogType, logger.LogTypeWebhook),
				zap.String(logger.FieldEvent, "checkout.check.failed"),
				zap.String("payment_id", c.PaymentPublicID),
				zap.Error(err))
		}
	}
	return checked, nil
}

// check reads one checkout and applies a status other than pending.
func (s *CheckoutCheckService) check(ctx context.Context, c domain.OverdueCheckout, conns map[int64]*domain.Connection) error {
	conn, ok := conns[c.ConnectionID]
	if !ok {
		var err error
		if conn, err = s.conns.GetByID(ctx, c.ConnectionID); err != nil {
			return fmt.Errorf("load connection: %w", err)
		}
		conns[c.ConnectionID] = conn
	}
	prov, err := s.registry.Get(provider.Name(conn.Provider))
	if err != nil {
		return err
	}
	reader, ok := prov.(provider.CheckoutReader)
	if !ok || !prov.Capabilities().ReadCheckout {
		return nil
	}
	acc, err := openAccount(s.sealer, conn)
	if err != nil {
		return err
	}
	ev, raw, err := reader.GetCheckout(ctx, acc, c.ProviderPaymentID)
	if err != nil {
		return fmt.Errorf("read checkout %s: %w", c.ProviderPaymentID, err)
	}
	// Still pending at the gateway (it may expire a checkout a little late):
	// nothing to apply yet, and no inbound row for every look.
	if ev.Status == domain.PaymentStatusPending {
		return nil
	}
	if ev.Reference != c.Reference {
		return fmt.Errorf("checkout %s belongs to %q, not %q", c.ProviderPaymentID, ev.Reference, c.Reference)
	}
	logger.Ctx(ctx, s.log).Info("checkout read back",
		zap.String(logger.FieldLogType, logger.LogTypeWebhook),
		zap.String(logger.FieldEvent, "checkout.check.read"),
		zap.String("payment_id", c.PaymentPublicID),
		zap.String("provider_status", ev.ProviderStatus))
	return s.callbacks.Process(ctx, conn, *ev, raw)
}
