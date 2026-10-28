package postgres

import (
	"context"
	"errors"
	"strings"
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
	const q = `INSERT INTO ledger_entries (tenant_id, payment_id, amount, xendit_fee, net_amount, type, settled_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (payment_id, type) DO NOTHING
		RETURNING id, created_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, e.TenantID, e.PaymentID, e.Amount, e.XenditFee, e.NetAmount, e.Type, e.SettledAt).
		Scan(&e.ID, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // conflict skipped; not an error for idempotency
	}
	return err
}

// MarkSettled sets settled_at on the payment's PAYMENT entry, keeping the first
// value if a later callback repeats it.
func (r *LedgerRepo) MarkSettled(ctx context.Context, paymentID int64, settledAt time.Time) error {
	const q = `UPDATE ledger_entries SET settled_at = COALESCE(settled_at, $2)
		WHERE payment_id = $1 AND type = $3`
	_, err := conn(ctx, r.db).Exec(ctx, q, paymentID, settledAt, domain.LedgerEntryTypePayment)
	return err
}

func ledgerWhere(f domain.LedgerFilter) (string, []any, int) {
	var conds []string
	var args []any
	i := 1
	if f.TenantID != nil {
		conds = append(conds, "tenant_id=$"+itoa(i))
		args = append(args, *f.TenantID)
		i++
	}
	if f.From != nil {
		conds = append(conds, "created_at>=$"+itoa(i))
		args = append(args, *f.From)
		i++
	}
	if f.To != nil {
		conds = append(conds, "created_at<=$"+itoa(i))
		args = append(args, *f.To)
		i++
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return where, args, i
}

// List returns filtered ledger entries.
func (r *LedgerRepo) List(ctx context.Context, f domain.LedgerFilter) ([]domain.LedgerEntry, error) {
	where, args, i := ledgerWhere(f)
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, payment_id, amount, xendit_fee, net_amount, type, settled_at, created_at
		FROM ledger_entries` + where +
		` ORDER BY id DESC LIMIT $` + itoa(i) + ` OFFSET $` + itoa(i+1)
	args = append(args, limit, f.Offset)

	rows, err := conn(ctx, r.db).Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.PaymentID, &e.Amount,
			&e.XenditFee, &e.NetAmount, &e.Type, &e.SettledAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Summary aggregates ledger totals grouped by tenant.
func (r *LedgerRepo) Summary(ctx context.Context, f domain.LedgerFilter) ([]domain.LedgerSummary, error) {
	where, args, _ := ledgerWhere(f)
	q := `SELECT tenant_id, count(*), COALESCE(sum(amount),0), COALESCE(sum(xendit_fee),0), COALESCE(sum(net_amount),0)
		FROM ledger_entries` + where + ` GROUP BY tenant_id ORDER BY tenant_id`

	rows, err := conn(ctx, r.db).Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.LedgerSummary
	for rows.Next() {
		var s domain.LedgerSummary
		if err := rows.Scan(&s.TenantID, &s.Count, &s.GrossAmount, &s.TotalFee, &s.NetAmount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
