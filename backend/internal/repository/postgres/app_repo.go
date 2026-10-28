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

const appColumns = `a.id, a.public_id, a.slug, a.name, a.environment, a.status, a.metadata,
	a.webhook_url, a.redirect_success_url, a.redirect_failure_url, a.webhook_secret_enc,
	a.created_at, a.updated_at`

// appScanTargets returns scan destinations for appColumns; finish decodes the
// metadata once the row is scanned.
func appScanTargets(a *domain.App) (targets []any, finish func() error) {
	var env, status string
	var meta []byte
	targets = []any{&a.ID, &a.PublicID, &a.Slug, &a.Name, &env, &status, &meta,
		&a.WebhookURL, &a.RedirectSuccessURL, &a.RedirectFailureURL, &a.WebhookSecretEnc,
		&a.CreatedAt, &a.UpdatedAt}
	finish = func() error {
		a.Environment = domain.Environment(env)
		a.Status = domain.AppStatus(status)
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

// Create inserts an app and fills in its id and timestamps.
func (r *AppRepo) Create(ctx context.Context, a *domain.App) error {
	meta, err := encodeMetadata(a.Metadata)
	if err != nil {
		return err
	}
	const q = `INSERT INTO apps (public_id, slug, name, environment, status, metadata, webhook_url,
		redirect_success_url, redirect_failure_url, webhook_secret_enc)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, created_at, updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, a.PublicID, a.Slug, a.Name, string(a.Environment), string(a.Status),
		meta, a.WebhookURL, a.RedirectSuccessURL, a.RedirectFailureURL, a.WebhookSecretEnc).
		Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	if uniqueConstraint(err) != "" {
		return fmt.Errorf("%w: slug already used in the %s environment", domain.ErrConflict, a.Environment)
	}
	if err != nil {
		return fmt.Errorf("insert app: %w", err)
	}
	return nil
}

// Update writes the mutable columns; slug and environment never change.
func (r *AppRepo) Update(ctx context.Context, a *domain.App) error {
	meta, err := encodeMetadata(a.Metadata)
	if err != nil {
		return err
	}
	const q = `UPDATE apps SET name=$2, status=$3, metadata=$4, webhook_url=$5,
		redirect_success_url=$6, redirect_failure_url=$7, updated_at=now()
		WHERE id=$1 RETURNING updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, a.ID, a.Name, string(a.Status), meta,
		a.WebhookURL, a.RedirectSuccessURL, a.RedirectFailureURL).Scan(&a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update app: %w", err)
	}
	return nil
}

func (r *AppRepo) getOne(ctx context.Context, where string, args ...any) (*domain.App, error) {
	return scanApp(conn(ctx, r.db).QueryRow(ctx, `SELECT `+appColumns+` FROM apps a WHERE `+where, args...))
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
	return r.getOne(ctx, `a.public_id=$1 FOR UPDATE`, publicID)
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
	q := `SELECT ` + appColumns + ` FROM apps a` + w.where() +
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
