package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// ConnectionRepo is a pgx-backed domain.ConnectionRepository.
type ConnectionRepo struct {
	db *pgxpool.Pool
}

// NewConnectionRepo constructs a ConnectionRepo.
func NewConnectionRepo(db *pgxpool.Pool) *ConnectionRepo { return &ConnectionRepo{db: db} }

// connectionSelect joins the merchant so every read knows whether the
// connection is the target of /webhooks/xendit.
const connectionSelect = `SELECT c.id, c.public_id, c.merchant_id, c.provider, c.name, c.environment, c.status,
	c.credentials_enc, c.credentials_dek_enc, c.key_version, c.credentials_last4, c.settings,
	c.last_tested_at, c.last_test_status, c.last_test_error,
	COALESCE(m.legacy_connection_id = c.id, false), c.created_at, c.updated_at
	FROM connections c JOIN merchants m ON m.id = c.merchant_id`

func scanConnection(row pgx.Row) (*domain.Connection, error) {
	var c domain.Connection
	var env, status string
	var testStatus *string
	var last4, settings []byte
	err := row.Scan(&c.ID, &c.PublicID, &c.MerchantID, &c.Provider, &c.Name, &env, &status,
		&c.CredentialsEnc, &c.CredentialsDEKEnc, &c.KeyVersion, &last4, &settings,
		&c.LastTestedAt, &testStatus, &c.LastTestError, &c.LegacyWebhookAlias, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan connection: %w", err)
	}
	c.Environment = domain.Environment(env)
	c.Status = domain.ConnectionStatus(status)
	if testStatus != nil {
		s := domain.ConnectionTestStatus(*testStatus)
		c.LastTestStatus = &s
	}
	if err := json.Unmarshal(last4, &c.CredentialsLast4); err != nil {
		return nil, fmt.Errorf("decode credential hints: %w", err)
	}
	if c.Settings, err = decodeObject(settings); err != nil {
		return nil, fmt.Errorf("decode connection settings: %w", err)
	}
	return &c, nil
}

// decodeObject reads a JSONB object keeping numbers as json.Number.
func decodeObject(raw []byte) (map[string]any, error) {
	m := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// connectionJSON encodes the JSONB columns of c.
func connectionJSON(c *domain.Connection) (last4, settings []byte, err error) {
	hints := c.CredentialsLast4
	if hints == nil {
		hints = map[string]*string{}
	}
	if last4, err = json.Marshal(hints); err != nil {
		return nil, nil, fmt.Errorf("encode credential hints: %w", err)
	}
	s := c.Settings
	if s == nil {
		s = map[string]any{}
	}
	if settings, err = json.Marshal(s); err != nil {
		return nil, nil, fmt.Errorf("encode connection settings: %w", err)
	}
	return last4, settings, nil
}

func testStatusArg(s *domain.ConnectionTestStatus) any {
	if s == nil {
		return nil
	}
	return string(*s)
}

// Create inserts a connection and fills in its id and timestamps.
func (r *ConnectionRepo) Create(ctx context.Context, c *domain.Connection) error {
	last4, settings, err := connectionJSON(c)
	if err != nil {
		return err
	}
	const q = `INSERT INTO connections (public_id, merchant_id, provider, name, environment, status,
		credentials_enc, credentials_dek_enc, key_version, credentials_last4, settings,
		last_tested_at, last_test_status, last_test_error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, created_at, updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, c.PublicID, c.MerchantID, c.Provider, c.Name, string(c.Environment),
		string(c.Status), c.CredentialsEnc, c.CredentialsDEKEnc, c.KeyVersion, last4, settings,
		c.LastTestedAt, testStatusArg(c.LastTestStatus), c.LastTestError).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if uniqueConstraint(err) != "" {
		return fmt.Errorf("%w: connection name already used", domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert connection: %w", err)
	}
	return nil
}

// Update writes the mutable columns; provider, environment and merchant
// never change.
func (r *ConnectionRepo) Update(ctx context.Context, c *domain.Connection) error {
	last4, settings, err := connectionJSON(c)
	if err != nil {
		return err
	}
	const q = `UPDATE connections SET name=$2, status=$3, credentials_enc=$4, credentials_dek_enc=$5,
		key_version=$6, credentials_last4=$7, settings=$8, last_tested_at=$9, last_test_status=$10,
		last_test_error=$11, updated_at=now()
		WHERE id=$1 RETURNING updated_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, c.ID, c.Name, string(c.Status), c.CredentialsEnc, c.CredentialsDEKEnc,
		c.KeyVersion, last4, settings, c.LastTestedAt, testStatusArg(c.LastTestStatus), c.LastTestError).
		Scan(&c.UpdatedAt)
	if uniqueConstraint(err) != "" {
		return fmt.Errorf("%w: connection name already used", domain.ErrConflict)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update connection: %w", err)
	}
	return nil
}

func (r *ConnectionRepo) getOne(ctx context.Context, where string, args ...any) (*domain.Connection, error) {
	return scanConnection(conn(ctx, r.db).QueryRow(ctx, connectionSelect+` WHERE `+where, args...))
}

// GetByID loads a connection by internal id.
func (r *ConnectionRepo) GetByID(ctx context.Context, id int64) (*domain.Connection, error) {
	return r.getOne(ctx, `c.id=$1`, id)
}

// GetByPublicID loads a connection by con_ id.
func (r *ConnectionRepo) GetByPublicID(ctx context.Context, publicID string) (*domain.Connection, error) {
	return r.getOne(ctx, `c.public_id=$1`, publicID)
}

// GetByPublicIDForUpdate loads a connection and locks its row until the
// transaction in ctx ends.
func (r *ConnectionRepo) GetByPublicIDForUpdate(ctx context.Context, publicID string) (*domain.Connection, error) {
	return r.getOne(ctx, `c.public_id=$1 FOR UPDATE OF c`, publicID)
}

// GetByPublicIDForShare loads a connection with a shared row lock, which
// blocks a concurrent disable until the transaction in ctx ends.
func (r *ConnectionRepo) GetByPublicIDForShare(ctx context.Context, publicID string) (*domain.Connection, error) {
	return r.getOne(ctx, `c.public_id=$1 FOR SHARE OF c`, publicID)
}

// List returns one page of connections, newest first, using a keyset on
// (created_at, public_id).
func (r *ConnectionRepo) List(ctx context.Context, f domain.ConnectionFilter, p domain.Page) (domain.PageResult[domain.Connection], error) {
	var w filters
	if f.Environment != nil {
		w.add("c.environment = ?", string(*f.Environment))
	}
	if f.Status != nil {
		w.add("c.status = ?", string(*f.Status))
	}
	if f.Provider != nil {
		w.add("c.provider = ?", *f.Provider)
	}
	if p.After != nil {
		w.add("(c.created_at, c.public_id) < (?, ?)", p.After.Time, p.After.ID)
	}
	q := connectionSelect + w.where() + ` ORDER BY c.created_at DESC, c.public_id DESC LIMIT ` + w.arg(p.Limit+1)
	rows, err := conn(ctx, r.db).Query(ctx, q, w.args...)
	if err != nil {
		return domain.PageResult[domain.Connection]{}, fmt.Errorf("list connections: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Connection, 0, p.Limit+1)
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return domain.PageResult[domain.Connection]{}, err
		}
		items = append(items, *c)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[domain.Connection]{}, fmt.Errorf("list connections: %w", err)
	}
	return keysetPage(items, p.Limit, func(c domain.Connection) domain.Cursor {
		return domain.Cursor{Time: c.CreatedAt, ID: c.PublicID}
	}), nil
}

// NameTaken reports whether the merchant has a connection named name.
func (r *ConnectionRepo) NameTaken(ctx context.Context, merchantID int64, name string) (bool, error) {
	var taken bool
	err := conn(ctx, r.db).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM connections WHERE merchant_id=$1 AND name=$2)`, merchantID, name).Scan(&taken)
	if err != nil {
		return false, fmt.Errorf("check connection name: %w", err)
	}
	return taken, nil
}

// Channels returns the connection's stored channel rows.
func (r *ConnectionRepo) Channels(ctx context.Context, connectionID int64) ([]domain.ConnectionChannel, error) {
	rows, err := conn(ctx, r.db).Query(ctx,
		`SELECT channel, enabled FROM connection_channels WHERE connection_id=$1 ORDER BY channel`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list connection channels: %w", err)
	}
	defer rows.Close()
	var out []domain.ConnectionChannel
	for rows.Next() {
		var ch domain.ConnectionChannel
		var code string
		if err := rows.Scan(&code, &ch.Enabled); err != nil {
			return nil, fmt.Errorf("scan connection channel: %w", err)
		}
		ch.Channel = domain.Channel(code)
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list connection channels: %w", err)
	}
	return out, nil
}

// ReplaceChannels deletes the connection's rows and inserts chs. Callers run
// it in a transaction so readers never see a half-written list.
func (r *ConnectionRepo) ReplaceChannels(ctx context.Context, connectionID int64, chs []domain.ConnectionChannel) error {
	db := conn(ctx, r.db)
	if _, err := db.Exec(ctx, `DELETE FROM connection_channels WHERE connection_id=$1`, connectionID); err != nil {
		return fmt.Errorf("delete connection channels: %w", err)
	}
	if len(chs) == 0 {
		return nil
	}
	codes := make([]string, 0, len(chs))
	enabled := make([]bool, 0, len(chs))
	for _, ch := range chs {
		codes = append(codes, string(ch.Channel))
		enabled = append(enabled, ch.Enabled)
	}
	_, err := db.Exec(ctx, `INSERT INTO connection_channels (connection_id, channel, enabled)
		SELECT $1, u.channel, u.enabled FROM unnest($2::text[], $3::boolean[]) AS u(channel, enabled)`,
		connectionID, codes, enabled)
	if err != nil {
		return fmt.Errorf("insert connection channels: %w", err)
	}
	return nil
}

// AppsUsing returns up to limit apps whose default connection is
// connectionID, newest first, and their total count.
func (r *ConnectionRepo) AppsUsing(ctx context.Context, connectionID int64, limit int) ([]domain.AppSummary, int, error) {
	rows, err := conn(ctx, r.db).Query(ctx, `SELECT public_id, slug, name, environment, count(*) OVER ()
		FROM apps WHERE default_connection_id=$1
		ORDER BY created_at DESC, public_id DESC LIMIT $2`, connectionID, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list apps using connection: %w", err)
	}
	defer rows.Close()
	var out []domain.AppSummary
	total := 0
	for rows.Next() {
		var a domain.AppSummary
		var env string
		if err := rows.Scan(&a.PublicID, &a.Slug, &a.Name, &env, &total); err != nil {
			return nil, 0, fmt.Errorf("scan app using connection: %w", err)
		}
		a.Environment = domain.Environment(env)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list apps using connection: %w", err)
	}
	return out, total, nil
}

// MerchantRepo is a pgx-backed domain.MerchantRepository.
type MerchantRepo struct {
	db *pgxpool.Pool
}

// NewMerchantRepo constructs a MerchantRepo.
func NewMerchantRepo(db *pgxpool.Pool) *MerchantRepo { return &MerchantRepo{db: db} }

func (r *MerchantRepo) getDefault(ctx context.Context, lock string) (*domain.Merchant, error) {
	var m domain.Merchant
	err := conn(ctx, r.db).QueryRow(ctx,
		`SELECT id, name, is_default, legacy_connection_id FROM merchants WHERE is_default`+lock).
		Scan(&m.ID, &m.Name, &m.IsDefault, &m.LegacyConnectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load default merchant: %w", err)
	}
	return &m, nil
}

// GetDefault loads the default merchant.
func (r *MerchantRepo) GetDefault(ctx context.Context) (*domain.Merchant, error) {
	return r.getDefault(ctx, "")
}

// GetDefaultForUpdate loads the default merchant and locks its row until the
// transaction in ctx ends.
func (r *MerchantRepo) GetDefaultForUpdate(ctx context.Context) (*domain.Merchant, error) {
	return r.getDefault(ctx, " FOR UPDATE")
}

// SetLegacyConnection points /webhooks/xendit at connectionID.
func (r *MerchantRepo) SetLegacyConnection(ctx context.Context, merchantID, connectionID int64) error {
	ct, err := conn(ctx, r.db).Exec(ctx,
		`UPDATE merchants SET legacy_connection_id=$2, updated_at=now() WHERE id=$1`, merchantID, connectionID)
	if err != nil {
		return fmt.Errorf("set legacy connection: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
