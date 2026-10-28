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

// WebhookRepo is a pgx-backed domain.WebhookRepository.
type WebhookRepo struct {
	db *pgxpool.Pool
}

// NewWebhookRepo constructs a WebhookRepo.
func NewWebhookRepo(db *pgxpool.Pool) *WebhookRepo { return &WebhookRepo{db: db} }

// CreateEventIfNew inserts an idempotency record for an inbound Xendit callback.
// Returns false when (xendit_id, status) was already recorded.
func (r *WebhookRepo) CreateEventIfNew(ctx context.Context, xenditID, status string) (bool, error) {
	const q = `INSERT INTO webhook_events (xendit_id, status) VALUES ($1,$2)
		ON CONFLICT (xendit_id, status) DO NOTHING RETURNING id`
	var id int64
	err := conn(ctx, r.db).QueryRow(ctx, q, xenditID, status).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CreateDelivery inserts an outbound delivery record.
func (r *WebhookRepo) CreateDelivery(ctx context.Context, d *domain.WebhookDelivery) error {
	const q = `INSERT INTO webhook_deliveries (payment_id, tenant_id, event_type, payload, status, attempts)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at, updated_at`
	return conn(ctx, r.db).QueryRow(ctx, q, d.PaymentID, d.TenantID, d.EventType, d.Payload, d.Status, d.Attempts).
		Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

// GetDelivery loads a delivery by id.
func (r *WebhookRepo) GetDelivery(ctx context.Context, id int64) (*domain.WebhookDelivery, error) {
	const q = `SELECT id, payment_id, tenant_id, event_type, payload, status, attempts, last_error, created_at, updated_at
		FROM webhook_deliveries WHERE id=$1`
	var d domain.WebhookDelivery
	err := conn(ctx, r.db).QueryRow(ctx, q, id).Scan(&d.ID, &d.PaymentID, &d.TenantID, &d.EventType,
		&d.Payload, &d.Status, &d.Attempts, &d.LastError, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// MarkDelivered sets a delivery to DELIVERED with the final attempt count.
func (r *WebhookRepo) MarkDelivered(ctx context.Context, id int64, attempts int) error {
	const q = `UPDATE webhook_deliveries SET status=$2, attempts=$3, last_error='', updated_at=now() WHERE id=$1`
	_, err := conn(ctx, r.db).Exec(ctx, q, id, domain.WebhookDeliveryDelivered, attempts)
	return err
}

// MarkFailed sets a delivery to FAILED with attempt count and last error.
func (r *WebhookRepo) MarkFailed(ctx context.Context, id int64, attempts int, lastErr string) error {
	const q = `UPDATE webhook_deliveries SET status=$2, attempts=$3, last_error=$4, updated_at=now() WHERE id=$1`
	_, err := conn(ctx, r.db).Exec(ctx, q, id, domain.WebhookDeliveryFailed, attempts, lastErr)
	return err
}

// ListDeliveries returns deliveries filtered by tenant and/or status.
func (r *WebhookRepo) ListDeliveries(ctx context.Context, tenantID *int64, status *domain.WebhookDeliveryStatus, limit, offset int) ([]domain.WebhookDelivery, error) {
	var conds []string
	var args []any
	i := 1
	if tenantID != nil {
		conds = append(conds, "tenant_id=$"+itoa(i))
		args = append(args, *tenantID)
		i++
	}
	if status != nil {
		conds = append(conds, "status=$"+itoa(i))
		args = append(args, *status)
		i++
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, payment_id, tenant_id, event_type, payload, status, attempts, last_error, created_at, updated_at
		FROM webhook_deliveries` + where +
		` ORDER BY id DESC LIMIT $` + itoa(i) + ` OFFSET $` + itoa(i+1)
	args = append(args, limit, offset)

	rows, err := conn(ctx, r.db).Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return scanDeliveries(rows)
}

// ListUnsentDeliveries returns PENDING deliveries that were never attempted and
// were created before olderThan, oldest first. Backed by idx_deliveries_unsent.
func (r *WebhookRepo) ListUnsentDeliveries(ctx context.Context, olderThan time.Time, limit int) ([]domain.WebhookDelivery, error) {
	const q = `SELECT id, payment_id, tenant_id, event_type, payload, status, attempts, last_error, created_at, updated_at
		FROM webhook_deliveries
		WHERE status = 'PENDING' AND attempts = 0 AND created_at < $1
		ORDER BY created_at LIMIT $2`
	rows, err := conn(ctx, r.db).Query(ctx, q, olderThan, limit)
	if err != nil {
		return nil, err
	}
	return scanDeliveries(rows)
}

func scanDeliveries(rows pgx.Rows) ([]domain.WebhookDelivery, error) {
	defer rows.Close()

	var out []domain.WebhookDelivery
	for rows.Next() {
		var d domain.WebhookDelivery
		if err := rows.Scan(&d.ID, &d.PaymentID, &d.TenantID, &d.EventType, &d.Payload,
			&d.Status, &d.Attempts, &d.LastError, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
