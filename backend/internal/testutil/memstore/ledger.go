package memstore

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Ledger is an in-memory domain.LedgerRepository with the unique
// (payment_id, type) rule. Set CreateErr to fail inserts.
type Ledger struct {
	mu      sync.Mutex
	entries []domain.LedgerEntry

	CreateErr error
}

func (l *Ledger) snapshot() func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	saved := slices.Clone(l.entries)
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.entries = saved
	}
}

// Create inserts e unless the payment already has an entry of its type.
func (l *Ledger) Create(_ context.Context, e *domain.LedgerEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.CreateErr != nil {
		return l.CreateErr
	}
	for _, v := range l.entries {
		if v.PaymentID == e.PaymentID && v.Type == e.Type {
			return nil
		}
	}
	e.ID = int64(len(l.entries) + 1)
	l.entries = append(l.entries, *e)
	return nil
}

// MarkSettled sets settled_at on the payment's entry if not already set.
func (l *Ledger) MarkSettled(_ context.Context, paymentID int64, settledAt time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, v := range l.entries {
		if v.PaymentID == paymentID && v.SettledAt == nil {
			at := settledAt
			l.entries[i].SettledAt = &at
		}
	}
	return nil
}

// List returns every entry.
func (l *Ledger) List(context.Context, domain.LedgerFilter) ([]domain.LedgerEntry, error) {
	return l.Entries(), nil
}

// Summary returns nothing; tests read Entries.
func (l *Ledger) Summary(context.Context, domain.LedgerFilter) ([]domain.LedgerSummary, error) {
	return nil, nil
}

// Entries returns a copy of the entries in insertion order.
func (l *Ledger) Entries() []domain.LedgerEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.entries)
}
