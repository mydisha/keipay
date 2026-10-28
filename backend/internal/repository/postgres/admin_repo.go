package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

type adminRepo struct {
	pool *pgxpool.Pool
}

// NewAdminRepo creates a PostgreSQL-backed AdminRepository.
func NewAdminRepo(pool *pgxpool.Pool) domain.AdminRepository {
	return &adminRepo{pool: pool}
}

func (r *adminRepo) GetByUsername(ctx context.Context, username string) (*domain.AdminUser, error) {
	q := `SELECT id, username, password_hash, must_change_password, created_at, updated_at
	      FROM admin_users WHERE username = $1`
	var u domain.AdminUser
	err := conn(ctx, r.pool).QueryRow(ctx, q, username).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *adminRepo) Create(ctx context.Context, user *domain.AdminUser) error {
	q := `INSERT INTO admin_users (username, password_hash, must_change_password)
	      VALUES ($1, $2, $3) RETURNING id, created_at, updated_at`
	return conn(ctx, r.pool).QueryRow(ctx, q, user.Username, user.PasswordHash, user.MustChangePassword).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

func (r *adminRepo) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	q := `UPDATE admin_users SET password_hash = $1, must_change_password = false, updated_at = now()
	      WHERE id = $2`
	_, err := conn(ctx, r.pool).Exec(ctx, q, passwordHash, id)
	return err
}
