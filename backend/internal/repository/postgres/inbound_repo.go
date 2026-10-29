package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// InboundWebhookRepo is a pgx-backed domain.InboundWebhookRepository.
type InboundWebhookRepo struct {
	db *pgxpool.Pool
}

// NewInboundWebhookRepo constructs an InboundWebhookRepo.
func NewInboundWebhookRepo(db *pgxpool.Pool) *InboundWebhookRepo { return &InboundWebhookRepo{db: db} }

const inboundInsert = `INSERT INTO inbound_webhooks (public_id, connection_id, provider, source, signature_valid,
	result, result_detail, event_key, provider_status, reference, payment_id, event_id, raw_body, body_truncated)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`

func inboundArgs(w *domain.InboundWebhook) []any {
	raw := w.RawBody
	if raw == nil {
		raw = []byte{}
	}
	return []any{w.PublicID, w.ConnectionID, w.Provider, w.Source, w.SignatureValid, string(w.Result), w.ResultDetail,
		w.EventKey, w.ProviderStatus, w.Reference, w.PaymentID, w.EventID, raw, w.BodyTruncated}
}

// Claim inserts w unless the dedupe index already holds a claimed row for its
// connection, event key and provider status. A concurrent claim waits for the
// first one's transaction: it wins if that one rolls back.
func (r *InboundWebhookRepo) Claim(ctx context.Context, w *domain.InboundWebhook) (bool, error) {
	q := inboundInsert + ` ON CONFLICT (connection_id, event_key, provider_status)
		WHERE result IN ('applied', 'stale', 'mismatch', 'unmapped') DO NOTHING
		RETURNING id, received_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, inboundArgs(w)...).Scan(&w.ID, &w.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim inbound webhook: %w", err)
	}
	return true, nil
}

// Create inserts w.
func (r *InboundWebhookRepo) Create(ctx context.Context, w *domain.InboundWebhook) error {
	err := conn(ctx, r.db).QueryRow(ctx, inboundInsert+` RETURNING id, received_at`, inboundArgs(w)...).Scan(&w.ID, &w.ReceivedAt)
	if err != nil {
		return fmt.Errorf("insert inbound webhook: %w", err)
	}
	return nil
}

// UpdateOutcome writes the result of processing w.
func (r *InboundWebhookRepo) UpdateOutcome(ctx context.Context, w *domain.InboundWebhook) error {
	ct, err := conn(ctx, r.db).Exec(ctx, `UPDATE inbound_webhooks SET result=$2, result_detail=$3, payment_id=$4, event_id=$5
		WHERE id=$1`, w.ID, string(w.Result), w.ResultDetail, w.PaymentID, w.EventID)
	if err != nil {
		return fmt.Errorf("update inbound webhook: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// inboundColumns joins the connection, payment and event every row shows. %s
// is the body column, an empty bytea in lists.
const inboundColumns = `w.id, w.public_id, w.connection_id, c.public_id, c.name, c.provider, c.environment, c.status,
	w.provider, w.source, w.signature_valid, w.result, w.result_detail, w.event_key, w.provider_status, w.reference,
	w.payment_id, p.public_id, w.event_id, ev.public_id, ev.type, ev.is_test, %s, w.body_truncated, w.received_at`

const inboundFrom = ` FROM inbound_webhooks w
	JOIN connections c ON c.id = w.connection_id
	LEFT JOIN payments p ON p.id = w.payment_id
	LEFT JOIN events ev ON ev.id = w.event_id`

func inboundSelect(withBody bool) string {
	body := "''::bytea"
	if withBody {
		body = "w.raw_body"
	}
	return `SELECT ` + fmt.Sprintf(inboundColumns, body)
}

func scanInbound(row pgx.Row) (*domain.InboundWebhook, error) {
	var w domain.InboundWebhook
	var env, connStatus, result string
	var paymentPublicID, eventPublicID, eventType *string
	var eventTest *bool
	err := row.Scan(&w.ID, &w.PublicID, &w.ConnectionID, &w.Connection.PublicID, &w.Connection.Name, &w.Connection.Provider,
		&env, &connStatus, &w.Provider, &w.Source, &w.SignatureValid, &result, &w.ResultDetail, &w.EventKey,
		&w.ProviderStatus, &w.Reference, &w.PaymentID, &paymentPublicID, &w.EventID, &eventPublicID, &eventType,
		&eventTest, &w.RawBody, &w.BodyTruncated, &w.ReceivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan inbound webhook: %w", err)
	}
	w.Connection.Environment = domain.Environment(env)
	w.Connection.Status = domain.ConnectionStatus(connStatus)
	w.Result = domain.InboundResult(result)
	if paymentPublicID != nil {
		w.PaymentPublicID = *paymentPublicID
	}
	if eventPublicID != nil {
		w.Event = &domain.EventRef{PublicID: *eventPublicID, Type: domain.EventType(*eventType), IsTest: *eventTest}
	}
	return &w, nil
}

// List returns one page of callbacks, newest first, using a keyset on
// (received_at, public_id).
func (r *InboundWebhookRepo) List(ctx context.Context, f domain.InboundFilter, p domain.Page) (domain.PageResult[domain.InboundWebhook], error) {
	var w filters
	if f.ConnectionPublicID != nil {
		w.add("w.connection_id = (SELECT id FROM connections WHERE public_id = ?)", *f.ConnectionPublicID)
	}
	if f.Result != nil {
		w.add("w.result = ?", string(*f.Result))
	}
	if f.Environment != nil {
		w.add("c.environment = ?", string(*f.Environment))
	}
	if f.From != nil {
		w.add("w.received_at >= ?", *f.From)
	}
	if f.To != nil {
		w.add("w.received_at <= ?", *f.To)
	}
	if p.After != nil {
		w.add("(w.received_at, w.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := inboundSelect(false) + inboundFrom + w.where() +
		` ORDER BY w.received_at DESC, w.public_id DESC LIMIT ` + w.arg(p.Limit+1)
	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.InboundWebhook]{}, fmt.Errorf("list inbound webhooks: %w", err)
	}
	defer rows.Close()
	items := make([]domain.InboundWebhook, 0, p.Limit+1)
	for rows.Next() {
		v, err := scanInbound(rows)
		if err != nil {
			return domain.PageResult[domain.InboundWebhook]{}, err
		}
		items = append(items, *v)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.InboundWebhook]{}, fmt.Errorf("list inbound webhooks: %w", err)
	}
	return keysetPage(items, p.Limit, func(v domain.InboundWebhook) domain.Cursor {
		return domain.Cursor{Time: v.ReceivedAt, ID: v.PublicID}
	}), nil
}

// GetByPublicID loads a callback with its raw body by inb_ id.
func (r *InboundWebhookRepo) GetByPublicID(ctx context.Context, publicID string) (*domain.InboundWebhook, error) {
	return scanInbound(conn(ctx, r.db).QueryRow(ctx, inboundSelect(true)+inboundFrom+` WHERE w.public_id=$1`, publicID))
}
