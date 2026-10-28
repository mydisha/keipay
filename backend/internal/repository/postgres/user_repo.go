package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// usersLockKey is the advisory lock that serializes first-run setup and every
// user mutation, so owner counts cannot race.
const usersLockKey int64 = 0x6b65697573657273

// UserRepo is a pgx-backed domain.UserRepository.
type UserRepo struct {
	db *pgxpool.Pool
}

// NewUserRepo constructs a UserRepo.
func NewUserRepo(db *pgxpool.Pool) *UserRepo { return &UserRepo{db: db} }

const userColumns = `id, public_id, email, name, role, password_hash, must_change_password,
	disabled_at, last_login_at, session_version, created_at, updated_at`

func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	var role string
	err := row.Scan(&u.ID, &u.PublicID, &u.Email, &u.Name, &role, &u.PasswordHash, &u.MustChangePassword,
		&u.DisabledAt, &u.LastLoginAt, &u.SessionVersion, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.Role = domain.Role(role)
	return &u, nil
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Create inserts a user and fills in its id and timestamps.
func (r *UserRepo) Create(ctx context.Context, u *domain.User) error {
	const q = `INSERT INTO users (public_id, email, name, role, password_hash, must_change_password,
		disabled_at, last_login_at, session_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at, updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, u.PublicID, u.Email, u.Name, string(u.Role), u.PasswordHash,
		u.MustChangePassword, u.DisabledAt, u.LastLoginAt, u.SessionVersion).
		Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if uniqueViolation(err) {
		return fmt.Errorf("%w: email already in use", domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// Update writes every mutable column of u.
func (r *UserRepo) Update(ctx context.Context, u *domain.User) error {
	const q = `UPDATE users SET email=$2, name=$3, role=$4, password_hash=$5, must_change_password=$6,
		disabled_at=$7, last_login_at=$8, session_version=$9, updated_at=now()
		WHERE id=$1 RETURNING updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, u.ID, u.Email, u.Name, string(u.Role), u.PasswordHash,
		u.MustChangePassword, u.DisabledAt, u.LastLoginAt, u.SessionVersion).Scan(&u.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrNotFound
	case uniqueViolation(err):
		return fmt.Errorf("%w: email already in use", domain.ErrConflict)
	case err != nil:
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// SetLastLogin stores the sign-in time without touching updated_at, which
// tracks changes to the account itself.
func (r *UserRepo) SetLastLogin(ctx context.Context, id int64, at time.Time) error {
	tag, err := conn(ctx, r.db).Exec(ctx, `UPDATE users SET last_login_at=$2 WHERE id=$1`, id, at)
	if err != nil {
		return fmt.Errorf("set last login: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *UserRepo) getOne(ctx context.Context, where string, arg any) (*domain.User, error) {
	return scanUser(conn(ctx, r.db).QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE `+where, arg))
}

// GetByID loads a user by internal id.
func (r *UserRepo) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	return r.getOne(ctx, `id=$1`, id)
}

// GetByIDForUpdate loads a user and locks its row until the transaction in
// ctx ends.
func (r *UserRepo) GetByIDForUpdate(ctx context.Context, id int64) (*domain.User, error) {
	return r.getOne(ctx, `id=$1 FOR UPDATE`, id)
}

// GetByPublicID loads a user by usr_ id.
func (r *UserRepo) GetByPublicID(ctx context.Context, publicID string) (*domain.User, error) {
	return r.getOne(ctx, `public_id=$1`, publicID)
}

// GetByPublicIDForUpdate loads a user by usr_ id and locks its row until the
// transaction in ctx ends.
func (r *UserRepo) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.User, error) {
	return r.getOne(ctx, `public_id=$1 FOR UPDATE`, publicID)
}

// GetByEmail loads a user by canonical email.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.getOne(ctx, `email=$1`, email)
}

// OldestOwner returns the first owner created, disabled or not.
func (r *UserRepo) OldestOwner(ctx context.Context) (*domain.User, error) {
	return scanUser(conn(ctx, r.db).QueryRow(ctx, `SELECT `+userColumns+` FROM users
		WHERE role='owner' ORDER BY created_at, id LIMIT 1`))
}

// Count returns the number of users, disabled ones included.
func (r *UserRepo) Count(ctx context.Context) (int, error) {
	var n int
	if err := conn(ctx, r.db).QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CountActiveOwners returns the number of owners that are not disabled.
func (r *UserRepo) CountActiveOwners(ctx context.Context) (int, error) {
	var n int
	err := conn(ctx, r.db).QueryRow(ctx, `SELECT count(*) FROM users WHERE role='owner' AND disabled_at IS NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count active owners: %w", err)
	}
	return n, nil
}

// List returns one page of users, newest first, using a keyset on
// (created_at, public_id).
func (r *UserRepo) List(ctx context.Context, p domain.Page) (domain.PageResult[domain.User], error) {
	var (
		rows pgx.Rows
		err  error
	)
	// One extra row tells whether another page exists.
	if p.After == nil {
		rows, err = conn(ctx, r.db).Query(ctx, `SELECT `+userColumns+` FROM users
			ORDER BY created_at DESC, public_id DESC LIMIT $1`, p.Limit+1)
	} else {
		rows, err = conn(ctx, r.db).Query(ctx, `SELECT `+userColumns+` FROM users
			WHERE (created_at, public_id) < ($1, $2)
			ORDER BY created_at DESC, public_id DESC LIMIT $3`, p.After.Time, p.After.ID, p.Limit+1)
	}
	if err != nil {
		return domain.PageResult[domain.User]{}, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	items := make([]domain.User, 0, p.Limit+1)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return domain.PageResult[domain.User]{}, err
		}
		items = append(items, *u)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.User]{}, fmt.Errorf("list users: %w", err)
	}

	res := domain.PageResult[domain.User]{Items: items}
	if len(items) > p.Limit {
		res.Items = items[:p.Limit]
		last := res.Items[p.Limit-1]
		res.NextCursor = domain.EncodeCursor(domain.Cursor{Time: last.CreatedAt, ID: last.PublicID})
	}
	return res, nil
}

// errNoTx guards LockUsers: outside a transaction the lock would be released
// as soon as it was taken.
var errNoTx = errors.New("lock users: no transaction in context")

// LockUsers takes the users advisory lock for the rest of the transaction.
func (r *UserRepo) LockUsers(ctx context.Context) error {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return errNoTx
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, usersLockKey); err != nil {
		return fmt.Errorf("lock users: %w", err)
	}
	return nil
}
