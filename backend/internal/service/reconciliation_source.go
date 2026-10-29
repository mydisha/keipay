package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// ErrTooManyTransactions means a run's range holds more gateway transactions
// than one run compares.
var ErrTooManyTransactions = errors.New("too many gateway transactions")

// Limits of one run's gateway read.
const (
	maxRunTransactions  = 100000
	transactionPageSize = 50
	pageAttempts        = 3
)

// pageBackoff is the wait before the second and third try of a page.
var pageBackoff = []time.Duration{time.Second, 2 * time.Second}

// TransactionSource yields a connection's gateway transactions that
// occurred within [from, to), in any order. An error from yield stops it and
// is returned.
type TransactionSource interface {
	Transactions(ctx context.Context, conn *domain.Connection, from, to time.Time, yield func(domain.GatewayTransaction) error) error
}

// ProviderSource is the TransactionSource that pulls transactions from the
// connection's provider adapter, page by page.
type ProviderSource struct {
	registry *provider.Registry
	sealer   CredentialSealer
	// sleep waits between tries of a page; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
	// maxRows is maxRunTransactions; tests lower it.
	maxRows int
}

// NewProviderSource constructs a ProviderSource that opens connection
// credentials with sealer.
func NewProviderSource(registry *provider.Registry, sealer CredentialSealer) *ProviderSource {
	return &ProviderSource{registry: registry, sealer: sealer, sleep: sleepContext, maxRows: maxRunTransactions}
}

// Transactions pages through the provider's transactions. A page that fails
// with ErrRetryable is tried up to three times; more than 100000
// transactions is ErrTooManyTransactions.
func (s *ProviderSource) Transactions(ctx context.Context, conn *domain.Connection, from, to time.Time,
	yield func(domain.GatewayTransaction) error) error {
	prov, err := s.registry.Get(provider.Name(conn.Provider))
	if err != nil {
		return fmt.Errorf("connection %s: %w", conn.PublicID, err)
	}
	lister, ok := prov.(provider.TransactionLister)
	if !ok || !prov.Capabilities().ListTransactions {
		return fmt.Errorf("provider %s cannot list transactions", conn.Provider)
	}
	acc, err := openAccount(s.sealer, conn)
	if err != nil {
		return err
	}
	q := provider.TransactionQuery{From: from, To: to, Limit: transactionPageSize}
	rows := 0
	for {
		page, err := s.page(ctx, lister, acc, q)
		if err != nil {
			return err
		}
		for _, t := range page.Transactions {
			rows++
			if rows > s.maxRows {
				return fmt.Errorf("%w: more than %d", ErrTooManyTransactions, s.maxRows)
			}
			if err := yield(t); err != nil {
				return err
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		q.Cursor = page.NextCursor
	}
}

// page reads one page, trying again after a retryable failure.
func (s *ProviderSource) page(ctx context.Context, lister provider.TransactionLister, acc provider.Account,
	q provider.TransactionQuery) (*provider.TransactionPage, error) {
	for attempt := 1; ; attempt++ {
		page, err := lister.ListTransactions(ctx, acc, q)
		if err == nil {
			return page, nil
		}
		if !errors.Is(err, provider.ErrRetryable) || attempt == pageAttempts {
			return nil, fmt.Errorf("list transactions (try %d): %w", attempt, err)
		}
		if err := s.sleep(ctx, pageBackoff[attempt-1]); err != nil {
			return nil, err
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
