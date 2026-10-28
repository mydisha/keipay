package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// AppRepo is a pgx-backed domain.AppRepository.
type AppRepo struct {
	db *pgxpool.Pool
}

// NewAppRepo constructs an AppRepo.
func NewAppRepo(db *pgxpool.Pool) *AppRepo { return &AppRepo{db: db} }

// appColumns needs appJoins, which brings the default connection's summary
// into the same row, so no list loads connections one by one.
const appColumns = `a.id, a.public_id, a.slug, a.name, a.environment, a.status, a.metadata,
	a.webhook_url, a.redirect_success_url, a.redirect_failure_url, a.webhook_secret_enc,
	a.merchant_id, a.default_connection_id, dc.public_id, dc.name, dc.provider, dc.environment, dc.status,
	a.created_at, a.updated_at`

const appJoins = ` LEFT JOIN connections dc ON dc.id = a.default_connection_id`

// appScanTargets returns scan destinations for appColumns; finish decodes the
// metadata and the connection summary once the row is scanned.
func appScanTargets(a *domain.App) (targets []any, finish func() error) {
	var env, status string
	var meta []byte
	var connID, connName, connProvider, connEnv, connStatus *string
	targets = []any{&a.ID, &a.PublicID, &a.Slug, &a.Name, &env, &status, &meta,
		&a.WebhookURL, &a.RedirectSuccessURL, &a.RedirectFailureURL, &a.WebhookSecretEnc,
		&a.MerchantID, &a.DefaultConnectionID, &connID, &connName, &connProvider, &connEnv, &connStatus,
		&a.CreatedAt, &a.UpdatedAt}
	finish = func() error {
		a.Environment = domain.Environment(env)
		a.Status = domain.AppStatus(status)
		a.DefaultConnection = nil
		if connID != nil {
			a.DefaultConnection = &domain.ConnectionSummary{PublicID: *connID, Name: *connName, Provider: *connProvider,
				Environment: domain.Environment(*connEnv), Status: domain.ConnectionStatus(*connStatus)}
		}
		m, err := decodeMetadata(meta)
		a.Metadata = m
		return err
	}
	return targets, finish
}

func scanApp(row pgx.Row) (*domain.App, error) {
	var a domain.App
	targets, finish := appScanTargets(&a)
	if err := row.Scan(targets...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan app: %w", err)
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return &a, nil
}

// Create inserts an app of the default merchant and fills in its id, merchant
// and timestamps. New apps have no default connection.
func (r *AppRepo) Create(ctx context.Context, a *domain.App) error {
	meta, err := encodeMetadata(a.Metadata)
	if err != nil {
		return err
	}
	const q = `INSERT INTO apps (public_id, slug, name, environment, status, metadata, webhook_url,
		redirect_success_url, redirect_failure_url, webhook_secret_enc, merchant_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,(SELECT id FROM merchants WHERE is_default))
		RETURNING id, merchant_id, created_at, updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, a.PublicID, a.Slug, a.Name, string(a.Environment), string(a.Status),
		meta, a.WebhookURL, a.RedirectSuccessURL, a.RedirectFailureURL, a.WebhookSecretEnc).
		Scan(&a.ID, &a.MerchantID, &a.CreatedAt, &a.UpdatedAt)
	if uniqueConstraint(err) != "" {
		return fmt.Errorf("%w: slug already used in the %s environment", domain.ErrConflict, a.Environment)
	}
	if err != nil {
		return fmt.Errorf("insert app: %w", err)
	}
	return nil
}

// Update writes the mutable columns, the default connection included; slug,
// environment and merchant never change. The composite foreign key refuses a
// default connection of another merchant or environment.
func (r *AppRepo) Update(ctx context.Context, a *domain.App) error {
	meta, err := encodeMetadata(a.Metadata)
	if err != nil {
		return err
	}
	const q = `UPDATE apps SET name=$2, status=$3, metadata=$4, webhook_url=$5,
		redirect_success_url=$6, redirect_failure_url=$7, default_connection_id=$8, updated_at=now()
		WHERE id=$1 RETURNING updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, a.ID, a.Name, string(a.Status), meta,
		a.WebhookURL, a.RedirectSuccessURL, a.RedirectFailureURL, a.DefaultConnectionID).Scan(&a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update app: %w", err)
	}
	return nil
}

func (r *AppRepo) getOne(ctx context.Context, where string, args ...any) (*domain.App, error) {
	return scanApp(conn(ctx, r.db).QueryRow(ctx, `SELECT `+appColumns+` FROM apps a`+appJoins+` WHERE `+where, args...))
}

// GetByID loads an app by internal id.
func (r *AppRepo) GetByID(ctx context.Context, id int64) (*domain.App, error) {
	return r.getOne(ctx, `a.id=$1`, id)
}

// GetByPublicID loads an app by app_ id.
func (r *AppRepo) GetByPublicID(ctx context.Context, publicID string) (*domain.App, error) {
	return r.getOne(ctx, `a.public_id=$1`, publicID)
}

// GetByPublicIDForUpdate loads an app by app_ id and locks its row until the
// transaction in ctx ends.
func (r *AppRepo) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.App, error) {
	return r.getOne(ctx, `a.public_id=$1 FOR UPDATE OF a`, publicID)
}

// GetBySlug loads an app by environment and slug.
func (r *AppRepo) GetBySlug(ctx context.Context, env domain.Environment, slug string) (*domain.App, error) {
	return r.getOne(ctx, `a.environment=$1 AND a.slug=$2`, string(env), slug)
}

// List returns one page of apps, newest first, using a keyset on
// (created_at, public_id).
func (r *AppRepo) List(ctx context.Context, f domain.AppFilter, p domain.Page) (domain.PageResult[domain.App], error) {
	var w filters
	if f.Environment != nil {
		w.add("a.environment = ?", string(*f.Environment))
	}
	if f.Status != nil {
		w.add("a.status = ?", string(*f.Status))
	}
	if p.After != nil {
		w.add("(a.created_at, a.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := `SELECT ` + appColumns + ` FROM apps a` + appJoins + w.where() +
		` ORDER BY a.created_at DESC, a.public_id DESC LIMIT ` + w.arg(p.Limit+1)

	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.App]{}, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	items := make([]domain.App, 0, p.Limit+1)
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return domain.PageResult[domain.App]{}, err
		}
		items = append(items, *a)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.App]{}, fmt.Errorf("list apps: %w", err)
	}
	return keysetPage(items, p.Limit, func(a domain.App) domain.Cursor {
		return domain.Cursor{Time: a.CreatedAt, ID: a.PublicID}
	}), nil
}

// ListWithoutDefaultConnection returns every app without a default
// connection, oldest first, locking the rows until the transaction ends.
func (r *AppRepo) ListWithoutDefaultConnection(ctx context.Context) ([]domain.App, error) {
	q := `SELECT ` + appColumns + ` FROM apps a` + appJoins +
		` WHERE a.default_connection_id IS NULL ORDER BY a.id FOR UPDATE OF a`
	rows, err := conn(ctx, r.db).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list apps without default connection: %w", err)
	}
	defer rows.Close()
	var out []domain.App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list apps without default connection: %w", err)
	}
	return out, nil
}
