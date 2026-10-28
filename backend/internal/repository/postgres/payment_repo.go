package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// PaymentRepo is a pgx-backed domain.PaymentRepository.
type PaymentRepo struct {
	db *pgxpool.Pool
}

// NewPaymentRepo constructs a PaymentRepo.
func NewPaymentRepo(db *pgxpool.Pool) *PaymentRepo { return &PaymentRepo{db: db} }

// paymentSelect joins the owning app so every payment carries its summary.
const paymentSelect = `SELECT p.id, p.public_id, p.app_id, a.public_id, a.slug, a.name, a.environment,
	p.order_id, p.external_id, p.xendit_invoice_id, p.amount, p.currency, p.status, p.description,
	p.invoice_url, p.payer_email, p.items, p.metadata, COALESCE(p.idempotency_key, ''),
	COALESCE(p.idempotency_fingerprint, ''), p.expires_at, p.paid_at, p.settled_at,
	p.expected_settlement_at, p.created_at, p.updated_at
	FROM payments p JOIN apps a ON a.id = p.app_id`

func scanPayment(row pgx.Row) (*domain.Payment, error) {
	var p domain.Payment
	var env string
	var items, meta []byte
	err := row.Scan(&p.ID, &p.PublicID, &p.AppID, &p.App.PublicID, &p.App.Slug, &p.App.Name, &env,
		&p.OrderID, &p.ExternalID, &p.XenditInvoiceID, &p.Amount, &p.Currency, &p.Status, &p.Description,
		&p.InvoiceURL, &p.PayerEmail, &items, &meta, &p.IdempotencyKey, &p.IdempotencyFingerprint,
		&p.ExpiresAt, &p.PaidAt, &p.SettledAt, &p.ExpectedSettlementAt, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan payment: %w", err)
	}
	p.App.Environment = domain.Environment(env)
	if len(items) > 0 {
		if err := json.Unmarshal(items, &p.Items); err != nil {
			return nil, fmt.Errorf("decode payment items: %w", err)
		}
	}
	if p.Metadata, err = decodeMetadata(meta); err != nil {
		return nil, err
	}
	return &p, nil
}

// nullIfEmpty stores "" as SQL NULL, so the partial unique index ignores
// payments created without an idempotency key.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Create inserts a payment and fills in its id and timestamps.
func (r *PaymentRepo) Create(ctx context.Context, p *domain.Payment) error {
	var items []byte
	if len(p.Items) > 0 {
		b, err := json.Marshal(p.Items)
		if err != nil {
			return fmt.Errorf("encode payment items: %w", err)
		}
		items = b
	}
	meta, err := encodeMetadata(p.Metadata)
	if err != nil {
		return err
	}
	const q = `INSERT INTO payments (public_id, app_id, order_id, external_id, xendit_invoice_id,
		amount, currency, status, description, invoice_url, payer_email, items, metadata,
		idempotency_key, idempotency_fingerprint, expires_at, expected_settlement_at, raw)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id, created_at, updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, p.PublicID, p.AppID, p.OrderID, p.ExternalID, p.XenditInvoiceID,
		p.Amount, p.Currency, p.Status, p.Description, p.InvoiceURL, p.PayerEmail, items, meta,
		nullIfEmpty(p.IdempotencyKey), nullIfEmpty(p.IdempotencyFingerprint), p.ExpiresAt,
		p.ExpectedSettlementAt, p.Raw).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	switch uniqueConstraint(err) {
	case "":
	case "payments_app_order_key":
		return domain.ErrDuplicateOrderID
	case "payments_app_idempotency_key":
		return domain.ErrDuplicateIdempotencyKey
	default:
		return fmt.Errorf("%w: payment exists", domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert payment: %w", err)
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
		return fmt.Errorf("update payment status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PaymentRepo) getOne(ctx context.Context, where string, args ...any) (*domain.Payment, error) {
	return scanPayment(conn(ctx, r.db).QueryRow(ctx, paymentSelect+` WHERE `+where, args...))
}

// GetByReferenceForUpdate loads the payment whose external_id equals ref and
// locks its row until the caller's transaction ends, so concurrent callbacks
// for one payment apply in turn.
func (r *PaymentRepo) GetByReferenceForUpdate(ctx context.Context, ref string) (*domain.Payment, error) {
	return r.getOne(ctx, `p.external_id=$1 FOR UPDATE OF p`, ref)
}

// GetByPublicID loads one of the app's payments by pay_ id.
func (r *PaymentRepo) GetByPublicID(ctx context.Context, appID int64, publicID string) (*domain.Payment, error) {
	return r.getOne(ctx, `p.app_id=$1 AND p.public_id=$2`, appID, publicID)
}

// GetByID loads one of the app's payments by its legacy integer id.
func (r *PaymentRepo) GetByID(ctx context.Context, appID, id int64) (*domain.Payment, error) {
	return r.getOne(ctx, `p.app_id=$1 AND p.id=$2`, appID, id)
}

// GetByOrderID loads one of the app's payments by order_id.
func (r *PaymentRepo) GetByOrderID(ctx context.Context, appID int64, orderID string) (*domain.Payment, error) {
	return r.getOne(ctx, `p.app_id=$1 AND p.order_id=$2`, appID, orderID)
}

// GetByIdempotencyKey loads the app's payment created with an Idempotency-Key.
func (r *PaymentRepo) GetByIdempotencyKey(ctx context.Context, appID int64, key string) (*domain.Payment, error) {
	return r.getOne(ctx, `p.app_id=$1 AND p.idempotency_key=$2`, appID, key)
}

// List returns one page of payments, newest first, using a keyset on
// (created_at, public_id).
func (r *PaymentRepo) List(ctx context.Context, f domain.PaymentFilter, p domain.Page) (domain.PageResult[domain.Payment], error) {
	var w filters
	if f.AppPublicID != nil {
		w.add("a.public_id = ?", *f.AppPublicID)
	}
	if f.Status != nil {
		w.add("p.status = ?", string(*f.Status))
	}
	if f.From != nil {
		w.add("p.created_at >= ?", *f.From)
	}
	if f.To != nil {
		w.add("p.created_at <= ?", *f.To)
	}
	if p.After != nil {
		w.add("(p.created_at, p.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := paymentSelect + w.where() + ` ORDER BY p.created_at DESC, p.public_id DESC LIMIT ` + w.arg(p.Limit+1)
	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.Payment]{}, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Payment, 0, p.Limit+1)
	for rows.Next() {
		pay, err := scanPayment(rows)
		if err != nil {
			return domain.PageResult[domain.Payment]{}, err
		}
		items = append(items, *pay)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.Payment]{}, fmt.Errorf("list payments: %w", err)
	}
	return keysetPage(items, p.Limit, func(pay domain.Payment) domain.Cursor {
		return domain.Cursor{Time: pay.CreatedAt, ID: pay.PublicID}
	}), nil
}
