package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// LedgerRepo is a pgx-backed domain.LedgerRepository.
type LedgerRepo struct {
	db *pgxpool.Pool
}

// NewLedgerRepo constructs a LedgerRepo.
func NewLedgerRepo(db *pgxpool.Pool) *LedgerRepo { return &LedgerRepo{db: db} }

// Create inserts a ledger entry. Duplicate (payment_id, type) is treated as a
// no-op conflict so webhook retries do not double-count.
func (r *LedgerRepo) Create(ctx context.Context, e *domain.LedgerEntry) error {
	const q = `INSERT INTO ledger_entries (app_id, payment_id, amount, xendit_fee, net_amount, type, settled_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (payment_id, type) DO NOTHING
		RETURNING id, created_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, e.AppID, e.PaymentID, e.Amount, e.XenditFee, e.NetAmount, e.Type, e.SettledAt).
		Scan(&e.ID, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // conflict skipped; not an error for idempotency
	}
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}

// MarkSettled sets settled_at on the payment's PAYMENT entry, keeping the first
// value if a later callback repeats it.
func (r *LedgerRepo) MarkSettled(ctx context.Context, paymentID int64, settledAt time.Time) error {
	const q = `UPDATE ledger_entries SET settled_at = COALESCE(settled_at, $2)
		WHERE payment_id = $1 AND type = $3`
	if _, err := conn(ctx, r.db).Exec(ctx, q, paymentID, settledAt, domain.LedgerEntryTypePayment); err != nil {
		return fmt.Errorf("mark ledger entry settled: %w", err)
	}
	return nil
}

func ledgerFilters(f domain.LedgerFilter) *filters {
	w := &filters{}
	if f.AppPublicID != nil {
		w.add("a.public_id = ?", *f.AppPublicID)
	}
	if f.From != nil {
		w.add("l.created_at >= ?", *f.From)
	}
	if f.To != nil {
		w.add("l.created_at <= ?", *f.To)
	}
	return w
}

// List returns filtered ledger entries, newest first, with their app summary
// and payment public id.
func (r *LedgerRepo) List(ctx context.Context, f domain.LedgerFilter) ([]domain.LedgerEntry, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	w := ledgerFilters(f)
	q := `SELECT l.id, l.app_id, l.payment_id, p.public_id, a.public_id, a.slug, a.name, a.environment,
		l.amount, l.xendit_fee, l.net_amount, l.type, l.settled_at, l.created_at
		FROM ledger_entries l
		JOIN apps a ON a.id = l.app_id
		JOIN payments p ON p.id = l.payment_id` + w.where() +
		` ORDER BY l.id DESC LIMIT ` + w.arg(limit) + ` OFFSET ` + w.arg(max(f.Offset, 0))

	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var out []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var env string
		if err := rows.Scan(&e.ID, &e.AppID, &e.PaymentID, &e.PaymentPublicID, &e.App.PublicID, &e.App.Slug,
			&e.App.Name, &env, &e.Amount, &e.XenditFee, &e.NetAmount, &e.Type, &e.SettledAt, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		e.App.Environment = domain.Environment(env)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Summary aggregates ledger totals per app.
func (r *LedgerRepo) Summary(ctx context.Context, f domain.LedgerFilter) ([]domain.LedgerSummary, error) {
	w := ledgerFilters(f)
	q := `SELECT a.public_id, a.slug, a.name, a.environment, count(*), COALESCE(sum(l.amount),0),
		COALESCE(sum(l.xendit_fee),0), COALESCE(sum(l.net_amount),0)
		FROM ledger_entries l JOIN apps a ON a.id = l.app_id` + w.where() +
		` GROUP BY a.id ORDER BY a.id`

	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return nil, fmt.Errorf("summarize ledger: %w", err)
	}
	defer rows.Close()

	var out []domain.LedgerSummary
	for rows.Next() {
		var s domain.LedgerSummary
		var env string
		if err := rows.Scan(&s.App.PublicID, &s.App.Slug, &s.App.Name, &env, &s.Count, &s.GrossAmount,
			&s.TotalFee, &s.NetAmount); err != nil {
			return nil, fmt.Errorf("scan ledger summary: %w", err)
		}
		s.App.Environment = domain.Environment(env)
		out = append(out, s)
	}
	return out, rows.Err()
}
