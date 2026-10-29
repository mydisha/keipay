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

// EventRepo is a pgx-backed domain.EventRepository.
type EventRepo struct {
	db *pgxpool.Pool
}

// NewEventRepo constructs an EventRepo.
func NewEventRepo(db *pgxpool.Pool) *EventRepo { return &EventRepo{db: db} }

// Create inserts an event with its own created_at and fills in its id.
func (r *EventRepo) Create(ctx context.Context, e *domain.Event) error {
	const q = `INSERT INTO events (public_id, type, api_version, app_id, payment_id, data, is_test, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`
	err := conn(ctx, r.db).QueryRow(ctx, q, e.PublicID, string(e.Type), e.APIVersion, e.AppID, e.PaymentID,
		[]byte(e.Data), e.IsTest, e.CreatedAt).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// DeliveryRepo is a pgx-backed domain.DeliveryRepository.
type DeliveryRepo struct {
	db *pgxpool.Pool
}

// NewDeliveryRepo constructs a DeliveryRepo.
func NewDeliveryRepo(db *pgxpool.Pool) *DeliveryRepo { return &DeliveryRepo{db: db} }

// deliveryColumns joins everything a delivery row shows, so no list loads
// apps, endpoints, events or payments one by one. %s is the payload column,
// NULL in lists.
const deliveryColumns = `d.id, d.public_id, d.payment_id, p.public_id, d.app_id, a.public_id, a.slug, a.name, a.environment,
	d.event_id, ev.public_id, ev.type, ev.is_test,
	d.endpoint_id, e.public_id, e.url, e.description, e.deleted_at IS NOT NULL,
	d.event_type, %s, d.status, d.attempts, d.max_attempts, d.next_attempt_at, d.last_attempt_at,
	d.last_response_status, d.last_error, d.created_at, d.updated_at`

const deliveryFrom = ` FROM webhook_deliveries d
	JOIN apps a ON a.id = d.app_id
	LEFT JOIN payments p ON p.id = d.payment_id
	LEFT JOIN events ev ON ev.id = d.event_id
	LEFT JOIN webhook_endpoints e ON e.id = d.endpoint_id`

func deliverySelect(withPayload bool) string {
	payload := "NULL::jsonb"
	if withPayload {
		payload = "d.payload"
	}
	return `SELECT ` + fmt.Sprintf(deliveryColumns, payload)
}

// deliveryTargets returns scan destinations for deliveryColumns; finish
// fills the optional references once the row is scanned.
func deliveryTargets(d *domain.WebhookDelivery) (targets []any, finish func()) {
	var paymentPublicID, env, eventPublicID, eventType, endpointPublicID, endpointURL, endpointDesc *string
	var eventTest *bool
	var endpointDeleted bool
	var status string
	var payload []byte
	targets = []any{&d.ID, &d.PublicID, &d.PaymentID, &paymentPublicID, &d.AppID, &d.App.PublicID, &d.App.Slug, &d.App.Name, &env,
		&d.EventID, &eventPublicID, &eventType, &eventTest,
		&d.EndpointID, &endpointPublicID, &endpointURL, &endpointDesc, &endpointDeleted,
		&d.EventType, &payload, &status, &d.Attempts, &d.MaxAttempts, &d.NextAttemptAt, &d.LastAttemptAt,
		&d.LastResponseStatus, &d.LastError, &d.CreatedAt, &d.UpdatedAt}
	finish = func() {
		d.App.Environment = domain.Environment(*env)
		d.Status = domain.WebhookDeliveryStatus(status)
		d.Payload = payload
		if paymentPublicID != nil {
			d.PaymentPublicID = *paymentPublicID
		}
		if eventPublicID != nil {
			d.Event = &domain.EventRef{PublicID: *eventPublicID, Type: domain.EventType(*eventType), IsTest: *eventTest}
		}
		if endpointPublicID != nil {
			d.Endpoint = &domain.EndpointRef{PublicID: *endpointPublicID, URL: *endpointURL, Description: *endpointDesc, Deleted: endpointDeleted}
		}
	}
	return targets, finish
}

func scanDelivery(row pgx.Row) (*domain.WebhookDelivery, error) {
	var d domain.WebhookDelivery
	targets, finish := deliveryTargets(&d)
	if err := row.Scan(targets...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan delivery: %w", err)
	}
	finish()
	return &d, nil
}

// Create inserts a delivery and fills in its id and timestamps.
func (r *DeliveryRepo) Create(ctx context.Context, d *domain.WebhookDelivery) error {
	const q = `INSERT INTO webhook_deliveries (public_id, payment_id, app_id, event_id, endpoint_id, event_type,
		payload, status, attempts, max_attempts, next_attempt_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, created_at, updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, d.PublicID, d.PaymentID, d.AppID, d.EventID, d.EndpointID, d.EventType,
		[]byte(d.Payload), string(d.Status), d.Attempts, d.MaxAttempts, d.NextAttemptAt).
		Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert delivery: %w", err)
	}
	return nil
}

// GetByPublicID loads a delivery with its payload by dlv_ id.
func (r *DeliveryRepo) GetByPublicID(ctx context.Context, publicID string) (*domain.WebhookDelivery, error) {
	return scanDelivery(conn(ctx, r.db).QueryRow(ctx, deliverySelect(true)+deliveryFrom+` WHERE d.public_id=$1`, publicID))
}

// GetByPublicIDForUpdate is GetByPublicID with a row lock on the delivery
// until the transaction in ctx ends.
func (r *DeliveryRepo) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.WebhookDelivery, error) {
	return scanDelivery(conn(ctx, r.db).QueryRow(ctx,
		deliverySelect(true)+deliveryFrom+` WHERE d.public_id=$1 FOR UPDATE OF d`, publicID))
}

// List returns one page of deliveries, newest first, using a keyset on
// (created_at, public_id). Id filters resolve through the unique public ids,
// so the per-app and per-endpoint indexes serve them.
func (r *DeliveryRepo) List(ctx context.Context, f domain.DeliveryFilter, p domain.Page) (domain.PageResult[domain.WebhookDelivery], error) {
	var w filters
	if f.AppPublicID != nil {
		w.add("d.app_id = (SELECT id FROM apps WHERE public_id = ?)", *f.AppPublicID)
	}
	if f.EndpointPublicID != nil {
		w.add("d.endpoint_id = (SELECT id FROM webhook_endpoints WHERE public_id = ?)", *f.EndpointPublicID)
	}
	if f.EventPublicID != nil {
		w.add("d.event_id = (SELECT id FROM events WHERE public_id = ?)", *f.EventPublicID)
	}
	if f.EventType != nil {
		w.add("d.event_type = ?", string(*f.EventType))
	}
	if f.Status != nil {
		w.add("d.status = ?", string(*f.Status))
	}
	if f.Environment != nil {
		w.add("a.environment = ?", string(*f.Environment))
	}
	if f.From != nil {
		w.add("d.created_at >= ?", *f.From)
	}
	if f.To != nil {
		w.add("d.created_at <= ?", *f.To)
	}
	if p.After != nil {
		w.add("(d.created_at, d.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := deliverySelect(false) + deliveryFrom + w.where() +
		` ORDER BY d.created_at DESC, d.public_id DESC LIMIT ` + w.arg(p.Limit+1)
	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.WebhookDelivery]{}, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	items := make([]domain.WebhookDelivery, 0, p.Limit+1)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return domain.PageResult[domain.WebhookDelivery]{}, err
		}
		items = append(items, *d)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.WebhookDelivery]{}, fmt.Errorf("list deliveries: %w", err)
	}
	return keysetPage(items, p.Limit, func(d domain.WebhookDelivery) domain.Cursor {
		return domain.Cursor{Time: d.CreatedAt, ID: d.PublicID}
	}), nil
}

// Attempts returns the delivery's attempts, oldest first.
func (r *DeliveryRepo) Attempts(ctx context.Context, deliveryID int64) ([]domain.DeliveryAttempt, error) {
	rows, err := conn(ctx, r.db).Query(ctx, `SELECT id, delivery_id, attempt_no, manual, url, request_headers,
		response_status, response_body, response_body_truncated, error, duration_ms, started_at
		FROM webhook_delivery_attempts WHERE delivery_id=$1 ORDER BY attempt_no`, deliveryID)
	if err != nil {
		return nil, fmt.Errorf("list delivery attempts: %w", err)
	}
	defer rows.Close()
	out := []domain.DeliveryAttempt{}
	for rows.Next() {
		var a domain.DeliveryAttempt
		var headers []byte
		if err := rows.Scan(&a.ID, &a.DeliveryID, &a.AttemptNo, &a.Manual, &a.URL, &headers, &a.ResponseStatus,
			&a.ResponseBody, &a.ResponseBodyTruncated, &a.Error, &a.DurationMS, &a.StartedAt); err != nil {
			return nil, fmt.Errorf("scan delivery attempt: %w", err)
		}
		if err := json.Unmarshal(headers, &a.RequestHeaders); err != nil {
			return nil, fmt.Errorf("decode attempt headers: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list delivery attempts: %w", err)
	}
	return out, nil
}

// LoadForSend loads a delivery with its payload, event reference and full
// endpoint in one query.
func (r *DeliveryRepo) LoadForSend(ctx context.Context, id int64) (*domain.WebhookDelivery, *domain.WebhookEndpoint, error) {
	var d domain.WebhookDelivery
	var ep endpointRow
	targets, finish := deliveryTargets(&d)
	// The endpoint belongs to the delivery's app, so endpointColumns can read
	// the app summary from the same apps row.
	q := deliverySelect(true) + `, ` + endpointColumns + deliveryFrom + ` WHERE d.id=$1`
	if err := conn(ctx, r.db).QueryRow(ctx, q, id).Scan(append(targets, ep.targets()...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, domain.ErrNotFound
		}
		return nil, nil, fmt.Errorf("load delivery %d: %w", id, err)
	}
	finish()
	endpoint, err := ep.endpoint()
	if err != nil {
		return nil, nil, err
	}
	return &d, endpoint, nil
}

// Requeue writes the replay state of d.
func (r *DeliveryRepo) Requeue(ctx context.Context, d *domain.WebhookDelivery) error {
	err := conn(ctx, r.db).QueryRow(ctx, `UPDATE webhook_deliveries SET status=$2, max_attempts=$3, next_attempt_at=$4,
		updated_at=now() WHERE id=$1 RETURNING updated_at`,
		d.ID, string(d.Status), d.MaxAttempts, d.NextAttemptAt).Scan(&d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("requeue delivery: %w", err)
	}
	return nil
}

// Fail ends a PENDING delivery without an attempt.
func (r *DeliveryRepo) Fail(ctx context.Context, id int64, lastErr string) (bool, error) {
	ct, err := conn(ctx, r.db).Exec(ctx, `UPDATE webhook_deliveries SET status='FAILED', last_error=$2,
		next_attempt_at=NULL, updated_at=now() WHERE id=$1 AND status='PENDING'`, id, lastErr)
	if err != nil {
		return false, fmt.Errorf("fail delivery: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

// RecordAttempt stores one attempt and the delivery's new state, guarded by
// the attempt count the worker read, so two workers on the same attempt
// write it once.
func (r *DeliveryRepo) RecordAttempt(ctx context.Context, d *domain.WebhookDelivery, prevAttempts int, a *domain.DeliveryAttempt) (bool, error) {
	db := conn(ctx, r.db)
	err := db.QueryRow(ctx, `UPDATE webhook_deliveries SET status=$3, attempts=$4, next_attempt_at=$5,
		last_attempt_at=$6, last_response_status=$7, last_error=$8, updated_at=now()
		WHERE id=$1 AND attempts=$2 AND status='PENDING' RETURNING updated_at`,
		d.ID, prevAttempts, string(d.Status), d.Attempts, d.NextAttemptAt, d.LastAttemptAt,
		d.LastResponseStatus, d.LastError).Scan(&d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("update delivery: %w", err)
	}
	headers, err := json.Marshal(a.RequestHeaders)
	if err != nil {
		return false, fmt.Errorf("encode attempt headers: %w", err)
	}
	err = db.QueryRow(ctx, `INSERT INTO webhook_delivery_attempts (delivery_id, attempt_no, manual, url, request_headers,
		response_status, response_body, response_body_truncated, error, duration_ms, started_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		a.DeliveryID, a.AttemptNo, a.Manual, a.URL, headers, a.ResponseStatus, a.ResponseBody,
		a.ResponseBodyTruncated, a.Error, a.DurationMS, a.StartedAt).Scan(&a.ID)
	if err != nil {
		return false, fmt.Errorf("insert delivery attempt: %w", err)
	}
	return true, nil
}

// ListDue returns PENDING deliveries whose next attempt was due before
// olderThan, oldest first. Backed by idx_deliveries_due.
func (r *DeliveryRepo) ListDue(ctx context.Context, olderThan time.Time, limit int) ([]domain.WebhookDelivery, error) {
	rows, err := conn(ctx, r.db).Query(ctx, `SELECT id, public_id, attempts, max_attempts, next_attempt_at
		FROM webhook_deliveries WHERE status='PENDING' AND next_attempt_at < $1::timestamptz
		ORDER BY next_attempt_at LIMIT $2`, olderThan, limit)
	if err != nil {
		return nil, fmt.Errorf("list due deliveries: %w", err)
	}
	defer rows.Close()
	var out []domain.WebhookDelivery
	for rows.Next() {
		d := domain.WebhookDelivery{Status: domain.WebhookDeliveryPending}
		if err := rows.Scan(&d.ID, &d.PublicID, &d.Attempts, &d.MaxAttempts, &d.NextAttemptAt); err != nil {
			return nil, fmt.Errorf("scan due delivery: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due deliveries: %w", err)
	}
	return out, nil
}
