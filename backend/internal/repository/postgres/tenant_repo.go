package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// TenantRepo is a pgx-backed domain.TenantRepository.
type TenantRepo struct {
	db *pgxpool.Pool
}

// NewTenantRepo constructs a TenantRepo.
func NewTenantRepo(db *pgxpool.Pool) *TenantRepo { return &TenantRepo{db: db} }

const tenantColumns = `id, name, slug, api_key, api_secret_enc, webhook_url,
	redirect_success_url, redirect_failure_url, active, created_at, updated_at`

func scanTenant(row pgx.Row) (*domain.Tenant, error) {
	var t domain.Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.APIKey, &t.APISecretEnc,
		&t.WebhookURL, &t.RedirectSuccessURL, &t.RedirectFailureURL,
		&t.Active, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

// Create inserts a tenant and populates generated fields.
func (r *TenantRepo) Create(ctx context.Context, t *domain.Tenant) error {
	const q = `INSERT INTO tenants (name, slug, api_key, api_secret_enc, webhook_url,
		redirect_success_url, redirect_failure_url, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at, updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, t.Name, t.Slug, t.APIKey, t.APISecretEnc,
		t.WebhookURL, t.RedirectSuccessURL, t.RedirectFailureURL, t.Active).
		Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: tenant slug or api_key exists", domain.ErrConflict)
		}
		return err
	}
	return nil
}

// Update persists mutable tenant fields.
func (r *TenantRepo) Update(ctx context.Context, t *domain.Tenant) error {
	const q = `UPDATE tenants SET name=$2, api_key=$3, api_secret_enc=$4, webhook_url=$5,
		redirect_success_url=$6, redirect_failure_url=$7, active=$8, updated_at=now()
		WHERE id=$1 RETURNING updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, t.ID, t.Name, t.APIKey, t.APISecretEnc,
		t.WebhookURL, t.RedirectSuccessURL, t.RedirectFailureURL, t.Active).
		Scan(&t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

// GetByID loads a tenant by primary key.
func (r *TenantRepo) GetByID(ctx context.Context, id int64) (*domain.Tenant, error) {
	q := `SELECT ` + tenantColumns + ` FROM tenants WHERE id=$1`
	return scanTenant(conn(ctx, r.db).QueryRow(ctx, q, id))
}

// GetByIDForUpdate loads a tenant and locks its row until the transaction in
// ctx ends.
func (r *TenantRepo) GetByIDForUpdate(ctx context.Context, id int64) (*domain.Tenant, error) {
	q := `SELECT ` + tenantColumns + ` FROM tenants WHERE id=$1 FOR UPDATE`
	return scanTenant(conn(ctx, r.db).QueryRow(ctx, q, id))
}

// GetByAPIKey loads a tenant by its public API key.
func (r *TenantRepo) GetByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error) {
	q := `SELECT ` + tenantColumns + ` FROM tenants WHERE api_key=$1`
	return scanTenant(conn(ctx, r.db).QueryRow(ctx, q, apiKey))
}

// GetBySlug loads a tenant by slug.
func (r *TenantRepo) GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	q := `SELECT ` + tenantColumns + ` FROM tenants WHERE slug=$1`
	return scanTenant(conn(ctx, r.db).QueryRow(ctx, q, slug))
}

// List returns all tenants ordered by id.
func (r *TenantRepo) List(ctx context.Context) ([]domain.Tenant, error) {
	q := `SELECT ` + tenantColumns + ` FROM tenants ORDER BY id`
	rows, err := conn(ctx, r.db).Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}
