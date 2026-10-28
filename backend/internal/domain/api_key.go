package domain

import (
	"context"
	"time"
)

// APIKeyStatus is derived from an API key's revoked_at and expires_at.
type APIKeyStatus string

// API key statuses.
const (
	APIKeyActive  APIKeyStatus = "active"
	APIKeyExpired APIKeyStatus = "expired"
	APIKeyRevoked APIKeyStatus = "revoked"
)

// MaxUsableAPIKeys caps the keys an app may have that are neither revoked
// nor expired.
const MaxUsableAPIKeys = 10

// APIKey is one credential of an app. Only a hash of the key is stored, with
// its prefix and last four characters for display; the HMAC secret is stored
// encrypted.
type APIKey struct {
	ID          int64
	PublicID    string
	AppID       int64
	Environment Environment
	Label       string
	// Prefix is "kp_test_", "kp_live_", or "kp_" for keys issued before apps.
	Prefix string
	Last4  string
	// Hash is the lower-case hex SHA-256 of the full key.
	Hash       string `json:"-"`
	SecretEnc  string `json:"-"`
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Status is revoked once RevokedAt is set, else expired from ExpiresAt on,
// else active.
func (k *APIKey) Status(now time.Time) APIKeyStatus {
	switch {
	case k.RevokedAt != nil:
		return APIKeyRevoked
	case k.ExpiresAt != nil && !now.Before(*k.ExpiresAt):
		return APIKeyExpired
	default:
		return APIKeyActive
	}
}

// Hint is the loggable, displayable form of the key: prefix, an ellipsis and
// the last four characters.
func (k *APIKey) Hint() string { return k.Prefix + "…" + k.Last4 }

// APIKeyRepository persists API keys. Every method joins the transaction
// carried by ctx and returns ErrNotFound when no row matches.
type APIKeyRepository interface {
	Create(ctx context.Context, k *APIKey) error
	// GetByHashWithApp loads a key and its app in one query.
	GetByHashWithApp(ctx context.Context, hash string) (*APIKey, *App, error)
	GetByPublicID(ctx context.Context, appID int64, publicID string) (*APIKey, error)
	// GetByPublicIDForUpdate also row-locks the key until the transaction ends.
	GetByPublicIDForUpdate(ctx context.Context, appID int64, publicID string) (*APIKey, error)
	// List returns the app's keys newest first (created_at, then public id),
	// revoked and expired ones included.
	List(ctx context.Context, appID int64, p Page) (PageResult[APIKey], error)
	// CountUsable counts the app's keys that are neither revoked nor expired at now.
	CountUsable(ctx context.Context, appID int64, now time.Time) (int, error)
	// Revoke stores k.RevokedAt and refreshes UpdatedAt.
	Revoke(ctx context.Context, k *APIKey) error
	// TouchLastUsed sets last_used_at to now unless it was set less than a
	// minute before, so busy keys cost at most one write a minute.
	TouchLastUsed(ctx context.Context, id int64, now time.Time) error
}
