package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// PaymentRepo is a pgx-backed domain.PaymentRepository.
type PaymentRepo struct {
	db *pgxpool.Pool
}

// NewPaymentRepo constructs a PaymentRepo.
func NewPaymentRepo(db *pgxpool.Pool) *PaymentRepo { return &PaymentRepo{db: db} }

const paymentColumns = `id, tenant_id, order_id, external_id, xendit_invoice_id, amount,
	currency, status, description, invoice_url, payer_email, items, expires_at, paid_at,
	settled_at, expected_settlement_at, created_at, updated_at`

func scanPayment(row pgx.Row) (*domain.Payment, error) {
	var p domain.Payment
	var items []byte
	err := row.Scan(&p.ID, &p.TenantID, &p.OrderID, &p.ExternalID, &p.XenditInvoiceID,
		&p.Amount, &p.Currency, &p.Status, &p.Description, &p.InvoiceURL, &p.PayerEmail,
		&items, &p.ExpiresAt, &p.PaidAt, &p.SettledAt, &p.ExpectedSettlementAt,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if len(items) > 0 {
		if err := json.Unmarshal(items, &p.Items); err != nil {
			return nil, err
		}
	}
	return &p, nil
}

// Create inserts a payment and populates generated fields.
func (r *PaymentRepo) Create(ctx context.Context, p *domain.Payment) error {
	var items []byte
	if len(p.Items) > 0 {
		b, err := json.Marshal(p.Items)
		if err != nil {
			return err
		}
		items = b
	}
	const q = `INSERT INTO payments (tenant_id, order_id, external_id, xendit_invoice_id,
		amount, currency, status, description, invoice_url, payer_email, items, expires_at,
		expected_settlement_at, raw)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, created_at, updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, p.TenantID, p.OrderID, p.ExternalID, p.XenditInvoiceID,
		p.Amount, p.Currency, p.Status, p.Description, p.InvoiceURL, p.PayerEmail,
		items, p.ExpiresAt, p.ExpectedSettlementAt, p.Raw).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

// UpdateStatus transitions a payment and stores the latest raw provider payload.
// settledAt is set when the status reflects a settled invoice; it is left
// untouched (COALESCE) when nil so an earlier settlement time is preserved.
func (r *PaymentRepo) UpdateStatus(ctx context.Context, id int64, status domain.PaymentStatus, paidAt, settledAt *time.Time, raw []byte) error {
	const q = `UPDATE payments SET status=$2, paid_at=COALESCE($3, paid_at),
		settled_at=COALESCE($4, settled_at), raw=COALESCE($5, raw), updated_at=now()
		WHERE id=$1`
	ct, err := conn(ctx, r.db).Exec(ctx, q, id, status, paidAt, settledAt, raw)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetByID loads a payment by primary key.
func (r *PaymentRepo) GetByID(ctx context.Context, id int64) (*domain.Payment, error) {
	q := `SELECT ` + paymentColumns + ` FROM payments WHERE id=$1`
	return scanPayment(conn(ctx, r.db).QueryRow(ctx, q, id))
}

// GetByExternalID loads a payment by its Xendit external_id.
func (r *PaymentRepo) GetByExternalID(ctx context.Context, externalID string) (*domain.Payment, error) {
	q := `SELECT ` + paymentColumns + ` FROM payments WHERE external_id=$1`
	return scanPayment(conn(ctx, r.db).QueryRow(ctx, q, externalID))
}

// GetByExternalIDForUpdate loads a payment and locks its row until the caller's
// transaction ends, so concurrent callbacks for one payment apply in turn.
func (r *PaymentRepo) GetByExternalIDForUpdate(ctx context.Context, externalID string) (*domain.Payment, error) {
	q := `SELECT ` + paymentColumns + ` FROM payments WHERE external_id=$1 FOR UPDATE`
	return scanPayment(conn(ctx, r.db).QueryRow(ctx, q, externalID))
}

// List returns filtered payments plus the total count matching the filter.
func (r *PaymentRepo) List(ctx context.Context, f domain.PaymentFilter) ([]domain.Payment, int64, error) {
	var conds []string
	var args []any
	i := 1
	if f.TenantID != nil {
		conds = append(conds, "tenant_id=$"+itoa(i))
		args = append(args, *f.TenantID)
		i++
	}
	if f.Status != nil {
		conds = append(conds, "status=$"+itoa(i))
		args = append(args, *f.Status)
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

	var total int64
	if err := conn(ctx, r.db).QueryRow(ctx, `SELECT count(*) FROM payments`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + paymentColumns + ` FROM payments` + where +
		` ORDER BY id DESC LIMIT $` + itoa(i) + ` OFFSET $` + itoa(i+1)
	args = append(args, limit, f.Offset)

	rows, err := conn(ctx, r.db).Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *p)
	}
	return out, total, rows.Err()
}
