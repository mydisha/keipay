package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// APIKeyRepo is a pgx-backed domain.APIKeyRepository.
type APIKeyRepo struct {
	db *pgxpool.Pool
}

// NewAPIKeyRepo constructs an APIKeyRepo.
func NewAPIKeyRepo(db *pgxpool.Pool) *APIKeyRepo { return &APIKeyRepo{db: db} }

const apiKeyColumns = `k.id, k.public_id, k.app_id, k.environment, k.label, k.key_prefix, k.key_last4,
	k.key_hash, k.secret_enc, k.last_used_at, k.expires_at, k.revoked_at, k.created_at, k.updated_at`

func apiKeyScanTargets(k *domain.APIKey) (targets []any, finish func()) {
	var env string
	targets = []any{&k.ID, &k.PublicID, &k.AppID, &env, &k.Label, &k.Prefix, &k.Last4,
		&k.Hash, &k.SecretEnc, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt, &k.UpdatedAt}
	return targets, func() { k.Environment = domain.Environment(env) }
}

func scanAPIKey(row pgx.Row) (*domain.APIKey, error) {
	var k domain.APIKey
	targets, finish := apiKeyScanTargets(&k)
	if err := row.Scan(targets...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan api key: %w", err)
	}
	finish()
	return &k, nil
}

// Create inserts a key and fills in its id and timestamps.
func (r *APIKeyRepo) Create(ctx context.Context, k *domain.APIKey) error {
	const q = `INSERT INTO app_api_keys (public_id, app_id, environment, label, key_prefix, key_last4,
		key_hash, secret_enc, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at, updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, k.PublicID, k.AppID, string(k.Environment), k.Label, k.Prefix,
		k.Last4, k.Hash, k.SecretEnc, k.ExpiresAt).Scan(&k.ID, &k.CreatedAt, &k.UpdatedAt)
	if uniqueConstraint(err) != "" {
		return fmt.Errorf("%w: api key exists", domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert api key: %w", err)
	}
	return nil
}

// GetByHashWithApp loads a key by its hash together with its app, in one query.
func (r *APIKeyRepo) GetByHashWithApp(ctx context.Context, hash string) (*domain.APIKey, *domain.App, error) {
	q := `SELECT ` + apiKeyColumns + `, ` + appColumns + `
		FROM app_api_keys k JOIN apps a ON a.id = k.app_id` + appJoins + `
		WHERE k.key_hash = $1`
	var k domain.APIKey
	var a domain.App
	keyTargets, finishKey := apiKeyScanTargets(&k)
	appTargets, finishApp := appScanTargets(&a)
	err := conn(ctx, r.db).QueryRow(ctx, q, hash).Scan(append(keyTargets, appTargets...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("lookup api key: %w", err)
	}
	finishKey()
	if err := finishApp(); err != nil {
		return nil, nil, err
	}
	return &k, &a, nil
}

// GetByPublicID loads one of the app's keys by key_ id.
func (r *APIKeyRepo) GetByPublicID(ctx context.Context, appID int64, publicID string) (*domain.APIKey, error) {
	q := `SELECT ` + apiKeyColumns + ` FROM app_api_keys k WHERE k.app_id=$1 AND k.public_id=$2`
	return scanAPIKey(conn(ctx, r.db).QueryRow(ctx, q, appID, publicID))
}

// GetByPublicIDForUpdate is GetByPublicID that also locks the row until the
// transaction in ctx ends.
func (r *APIKeyRepo) GetByPublicIDForUpdate(ctx context.Context, appID int64, publicID string) (*domain.APIKey, error) {
	q := `SELECT ` + apiKeyColumns + ` FROM app_api_keys k WHERE k.app_id=$1 AND k.public_id=$2 FOR UPDATE`
	return scanAPIKey(conn(ctx, r.db).QueryRow(ctx, q, appID, publicID))
}

// List returns one page of the app's keys, newest first.
func (r *APIKeyRepo) List(ctx context.Context, appID int64, p domain.Page) (domain.PageResult[domain.APIKey], error) {
	var w filters
	w.add("k.app_id = ?", appID)
	if p.After != nil {
		w.add("(k.created_at, k.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := `SELECT ` + apiKeyColumns + ` FROM app_api_keys k` + w.where() +
		` ORDER BY k.created_at DESC, k.public_id DESC LIMIT ` + w.arg(p.Limit+1)
	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.APIKey]{}, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	items := make([]domain.APIKey, 0, p.Limit+1)
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return domain.PageResult[domain.APIKey]{}, err
		}
		items = append(items, *k)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.APIKey]{}, fmt.Errorf("list api keys: %w", err)
	}
	return keysetPage(items, p.Limit, func(k domain.APIKey) domain.Cursor {
		return domain.Cursor{Time: k.CreatedAt, ID: k.PublicID}
	}), nil
}

// CountUsable counts the app's keys that are neither revoked nor expired at now.
func (r *APIKeyRepo) CountUsable(ctx context.Context, appID int64, now time.Time) (int, error) {
	var n int
	err := conn(ctx, r.db).QueryRow(ctx, `SELECT count(*) FROM app_api_keys
		WHERE app_id=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $2)`, appID, now).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count usable api keys: %w", err)
	}
	return n, nil
}

// Revoke stores k.RevokedAt.
func (r *APIKeyRepo) Revoke(ctx context.Context, k *domain.APIKey) error {
	err := conn(ctx, r.db).QueryRow(ctx, `UPDATE app_api_keys SET revoked_at=$2, updated_at=now()
		WHERE id=$1 RETURNING updated_at`, k.ID, k.RevokedAt).Scan(&k.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// TouchLastUsed records a use at most once a minute per key. It leaves
// updated_at alone, which tracks changes to the key itself.
func (r *APIKeyRepo) TouchLastUsed(ctx context.Context, id int64, now time.Time) error {
	_, err := conn(ctx, r.db).Exec(ctx, `UPDATE app_api_keys SET last_used_at=$2
		WHERE id=$1 AND (last_used_at IS NULL OR last_used_at <= $2::timestamptz - interval '1 minute')`, id, now)
	if err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
