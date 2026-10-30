package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// CheckoutCheckRepo is a pgx-backed domain.CheckoutCheckRepository.
type CheckoutCheckRepo struct {
	db *pgxpool.Pool
}

// NewCheckoutCheckRepo constructs a CheckoutCheckRepo.
func NewCheckoutCheckRepo(db *pgxpool.Pool) *CheckoutCheckRepo { return &CheckoutCheckRepo{db: db} }

// ListOverdue reads idx_payments_pending_expiry.
func (r *CheckoutCheckRepo) ListOverdue(ctx context.Context, q domain.OverdueCheckoutQuery) ([]domain.OverdueCheckout, error) {
	const sql = `SELECT id, public_id, connection_id, xendit_invoice_id, external_id, expires_at
		FROM payments
		WHERE status = 'PENDING' AND expires_at IS NOT NULL
		  AND expires_at <= $1 AND expires_at > $2
		  AND connection_id IS NOT NULL AND xendit_invoice_id <> ''
		  AND (status_checked_at IS NULL OR status_checked_at <= $3)
		ORDER BY expires_at, id
		LIMIT $4`
	rows, err := conn(ctx, r.db).Query(ctx, sql, q.ExpiredBefore, q.NotBefore, q.CheckedBefore, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("list overdue checkouts: %w", err)
	}
	defer rows.Close()
	var out []domain.OverdueCheckout
	for rows.Next() {
		var c domain.OverdueCheckout
		if err := rows.Scan(&c.PaymentID, &c.PaymentPublicID, &c.ConnectionID, &c.ProviderPaymentID, &c.Reference, &c.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan overdue checkout: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list overdue checkouts: %w", err)
	}
	return out, nil
}

// MarkChecked sets status_checked_at.
func (r *CheckoutCheckRepo) MarkChecked(ctx context.Context, paymentID int64, at time.Time) error {
	if _, err := conn(ctx, r.db).Exec(ctx, `UPDATE payments SET status_checked_at = $2 WHERE id = $1`, paymentID, at); err != nil {
		return fmt.Errorf("mark checkout checked: %w", err)
	}
	return nil
}
