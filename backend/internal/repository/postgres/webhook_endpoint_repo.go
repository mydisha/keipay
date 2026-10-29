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

// WebhookEndpointRepo is a pgx-backed domain.WebhookEndpointRepository.
type WebhookEndpointRepo struct {
	db *pgxpool.Pool
}

// NewWebhookEndpointRepo constructs a WebhookEndpointRepo.
func NewWebhookEndpointRepo(db *pgxpool.Pool) *WebhookEndpointRepo {
	return &WebhookEndpointRepo{db: db}
}

// endpointColumns needs webhook_endpoints as e and the endpoint's app as a.
const endpointColumns = `e.id, e.public_id, e.app_id, a.public_id, a.slug, a.name, a.environment,
	e.url, e.description, e.events, e.api_version, e.legacy_signature, e.secret_enc, e.secret_last4,
	e.previous_secret_enc, e.previous_secret_expires_at, e.headers_enc, e.headers_hint, e.status,
	e.disabled_reason, e.disabled_at, e.consecutive_failures, e.retry_max_attempts, e.retry_backoff,
	e.retry_interval_seconds, e.migrated, e.deleted_at, e.created_at, e.updated_at`

const endpointFrom = ` FROM webhook_endpoints e JOIN apps a ON a.id = e.app_id`

// endpointRow scans endpointColumns. Every column is nullable here, so the
// same scan serves a LEFT JOIN that found no endpoint.
type endpointRow struct {
	id, appID                                                         *int64
	publicID, appPublicID, appSlug, appName, appEnv, url, description *string
	events                                                            []string
	apiVersion, secretEnc, secretLast4, prevSecretEnc, headersEnc     *string
	status, disabledReason, backoff                                   *string
	legacySignature, migrated                                         *bool
	headersHint                                                       []byte
	prevSecretExpiresAt, disabledAt, deletedAt, createdAt, updatedAt  *time.Time
	failures, maxAttempts, interval                                   *int
}

func (r *endpointRow) targets() []any {
	return []any{&r.id, &r.publicID, &r.appID, &r.appPublicID, &r.appSlug, &r.appName, &r.appEnv,
		&r.url, &r.description, &r.events, &r.apiVersion, &r.legacySignature, &r.secretEnc, &r.secretLast4,
		&r.prevSecretEnc, &r.prevSecretExpiresAt, &r.headersEnc, &r.headersHint, &r.status,
		&r.disabledReason, &r.disabledAt, &r.failures, &r.maxAttempts, &r.backoff,
		&r.interval, &r.migrated, &r.deletedAt, &r.createdAt, &r.updatedAt}
}

// endpoint converts a scanned row; nil when the row held no endpoint.
func (r *endpointRow) endpoint() (*domain.WebhookEndpoint, error) {
	if r.id == nil {
		return nil, nil
	}
	e := &domain.WebhookEndpoint{
		ID: *r.id, PublicID: *r.publicID, AppID: *r.appID,
		App: domain.AppSummary{PublicID: *r.appPublicID, Slug: *r.appSlug, Name: *r.appName, Environment: domain.Environment(*r.appEnv)},
		URL: *r.url, Description: *r.description, Events: r.events, APIVersion: domain.WebhookAPIVersion(*r.apiVersion),
		LegacySignature: *r.legacySignature, SecretEnc: *r.secretEnc, SecretLast4: r.secretLast4,
		PreviousSecretEnc: r.prevSecretEnc, PreviousSecretExpiresAt: r.prevSecretExpiresAt, HeadersEnc: r.headersEnc,
		Status: domain.WebhookEndpointStatus(*r.status), DisabledAt: r.disabledAt, ConsecutiveFailures: *r.failures,
		RetryPolicy: domain.RetryPolicy{MaxAttempts: *r.maxAttempts, Backoff: domain.RetryBackoff(*r.backoff), IntervalSeconds: *r.interval},
		Migrated:    *r.migrated, DeletedAt: r.deletedAt, CreatedAt: *r.createdAt, UpdatedAt: *r.updatedAt,
	}
	if r.disabledReason != nil {
		reason := domain.WebhookDisabledReason(*r.disabledReason)
		e.DisabledReason = &reason
	}
	if e.Events == nil {
		e.Events = []string{}
	}
	e.HeadersHint = []domain.WebhookHeaderHint{}
	if len(r.headersHint) > 0 {
		if err := json.Unmarshal(r.headersHint, &e.HeadersHint); err != nil {
			return nil, fmt.Errorf("decode header hints: %w", err)
		}
	}
	return e, nil
}

func scanEndpoint(row pgx.Row) (*domain.WebhookEndpoint, error) {
	var r endpointRow
	if err := row.Scan(r.targets()...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan webhook endpoint: %w", err)
	}
	return r.endpoint()
}

func headerHintsJSON(h []domain.WebhookHeaderHint) ([]byte, error) {
	if h == nil {
		h = []domain.WebhookHeaderHint{}
	}
	b, err := json.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("encode header hints: %w", err)
	}
	return b, nil
}

func disabledReasonArg(r *domain.WebhookDisabledReason) any {
	if r == nil {
		return nil
	}
	return string(*r)
}

// Create inserts an endpoint and fills in its id and timestamps.
func (r *WebhookEndpointRepo) Create(ctx context.Context, e *domain.WebhookEndpoint) error {
	hints, err := headerHintsJSON(e.HeadersHint)
	if err != nil {
		return err
	}
	const q = `INSERT INTO webhook_endpoints (public_id, app_id, url, description, events, api_version,
		legacy_signature, secret_enc, secret_last4, previous_secret_enc, previous_secret_expires_at,
		headers_enc, headers_hint, status, disabled_reason, disabled_at, consecutive_failures,
		retry_max_attempts, retry_backoff, retry_interval_seconds, migrated)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		RETURNING id, created_at, updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, e.PublicID, e.AppID, e.URL, e.Description, e.Events, string(e.APIVersion),
		e.LegacySignature, e.SecretEnc, e.SecretLast4, e.PreviousSecretEnc, e.PreviousSecretExpiresAt,
		e.HeadersEnc, hints, string(e.Status), disabledReasonArg(e.DisabledReason), e.DisabledAt, e.ConsecutiveFailures,
		e.RetryPolicy.MaxAttempts, string(e.RetryPolicy.Backoff), e.RetryPolicy.IntervalSeconds, e.Migrated).
		Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert webhook endpoint: %w", err)
	}
	return nil
}

// Update writes every mutable column; the app and public id never change.
func (r *WebhookEndpointRepo) Update(ctx context.Context, e *domain.WebhookEndpoint) error {
	hints, err := headerHintsJSON(e.HeadersHint)
	if err != nil {
		return err
	}
	const q = `UPDATE webhook_endpoints SET url=$2, description=$3, events=$4, api_version=$5,
		legacy_signature=$6, secret_enc=$7, secret_last4=$8, previous_secret_enc=$9,
		previous_secret_expires_at=$10, headers_enc=$11, headers_hint=$12, status=$13, disabled_reason=$14,
		disabled_at=$15, consecutive_failures=$16, retry_max_attempts=$17, retry_backoff=$18,
		retry_interval_seconds=$19, deleted_at=$20, updated_at=now()
		WHERE id=$1 RETURNING updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, e.ID, e.URL, e.Description, e.Events, string(e.APIVersion),
		e.LegacySignature, e.SecretEnc, e.SecretLast4, e.PreviousSecretEnc,
		e.PreviousSecretExpiresAt, e.HeadersEnc, hints, string(e.Status), disabledReasonArg(e.DisabledReason),
		e.DisabledAt, e.ConsecutiveFailures, e.RetryPolicy.MaxAttempts, string(e.RetryPolicy.Backoff),
		e.RetryPolicy.IntervalSeconds, e.DeletedAt).Scan(&e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update webhook endpoint: %w", err)
	}
	return nil
}

// UpdateHealth writes the failure counter and status columns.
func (r *WebhookEndpointRepo) UpdateHealth(ctx context.Context, e *domain.WebhookEndpoint) error {
	const q = `UPDATE webhook_endpoints SET consecutive_failures=$2, status=$3, disabled_reason=$4, disabled_at=$5,
		updated_at = CASE WHEN status IS DISTINCT FROM $3 THEN now() ELSE updated_at END
		WHERE id=$1 RETURNING updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, e.ID, e.ConsecutiveFailures, string(e.Status),
		disabledReasonArg(e.DisabledReason), e.DisabledAt).Scan(&e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update webhook endpoint health: %w", err)
	}
	return nil
}

func (r *WebhookEndpointRepo) getOne(ctx context.Context, where string, args ...any) (*domain.WebhookEndpoint, error) {
	return scanEndpoint(conn(ctx, r.db).QueryRow(ctx, `SELECT `+endpointColumns+endpointFrom+` WHERE `+where, args...))
}

// GetByPublicID loads an endpoint that is not deleted by whe_ id.
func (r *WebhookEndpointRepo) GetByPublicID(ctx context.Context, publicID string) (*domain.WebhookEndpoint, error) {
	return r.getOne(ctx, `e.public_id=$1 AND e.deleted_at IS NULL`, publicID)
}

// GetByPublicIDForUpdate is GetByPublicID with a row lock until the
// transaction in ctx ends.
func (r *WebhookEndpointRepo) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.WebhookEndpoint, error) {
	return r.getOne(ctx, `e.public_id=$1 AND e.deleted_at IS NULL FOR UPDATE OF e`, publicID)
}

// GetByIDForUpdate loads an endpoint by internal id, deleted ones included,
// and locks its row until the transaction in ctx ends.
func (r *WebhookEndpointRepo) GetByIDForUpdate(ctx context.Context, id int64) (*domain.WebhookEndpoint, error) {
	return r.getOne(ctx, `e.id=$1 FOR UPDATE OF e`, id)
}

func (r *WebhookEndpointRepo) query(ctx context.Context, q string, args ...any) ([]domain.WebhookEndpoint, error) {
	rows, err := conn(ctx, r.db).Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list webhook endpoints: %w", err)
	}
	defer rows.Close()
	out := []domain.WebhookEndpoint{}
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list webhook endpoints: %w", err)
	}
	return out, nil
}

// List returns one page of the app's endpoints that are not deleted, newest
// first, using a keyset on (created_at, public_id).
func (r *WebhookEndpointRepo) List(ctx context.Context, appID int64, p domain.Page) (domain.PageResult[domain.WebhookEndpoint], error) {
	var w filters
	w.add("e.app_id = ?", appID)
	w.add("e.deleted_at IS NULL")
	if p.After != nil {
		w.add("(e.created_at, e.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := `SELECT ` + endpointColumns + endpointFrom + w.where() +
		` ORDER BY e.created_at DESC, e.public_id DESC LIMIT ` + w.arg(p.Limit+1)
	items, err := r.query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.WebhookEndpoint]{}, err
	}
	return keysetPage(items, p.Limit, func(e domain.WebhookEndpoint) domain.Cursor {
		return domain.Cursor{Time: e.CreatedAt, ID: e.PublicID}
	}), nil
}

// CountByApp counts the app's endpoints that are not deleted.
func (r *WebhookEndpointRepo) CountByApp(ctx context.Context, appID int64) (int, error) {
	var n int
	err := conn(ctx, r.db).QueryRow(ctx,
		`SELECT count(*) FROM webhook_endpoints WHERE app_id=$1 AND deleted_at IS NULL`, appID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count webhook endpoints: %w", err)
	}
	return n, nil
}

// ListActiveByApp returns the app's active endpoints that are not deleted,
// oldest first.
func (r *WebhookEndpointRepo) ListActiveByApp(ctx context.Context, appID int64) ([]domain.WebhookEndpoint, error) {
	return r.query(ctx, `SELECT `+endpointColumns+endpointFrom+
		` WHERE e.app_id=$1 AND e.deleted_at IS NULL AND e.status='active' ORDER BY e.created_at, e.id`, appID)
}
