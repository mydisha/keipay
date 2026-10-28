package domain

import (
	"context"
	"time"
)

// ConnectionStatus is whether a connection may take new checkouts.
// Disabled connections still accept callbacks.
type ConnectionStatus string

// Connection statuses.
const (
	ConnectionActive   ConnectionStatus = "active"
	ConnectionDisabled ConnectionStatus = "disabled"
)

// Valid reports whether s is a known connection status.
func (s ConnectionStatus) Valid() bool { return s == ConnectionActive || s == ConnectionDisabled }

// ConnectionTestStatus is the outcome of the last credential test.
type ConnectionTestStatus string

// Connection test outcomes.
const (
	ConnectionTestOK     ConnectionTestStatus = "ok"
	ConnectionTestFailed ConnectionTestStatus = "failed"
)

// Merchant owns apps and connections. Today there is exactly one, the
// default merchant, and it never leaves the server.
type Merchant struct {
	ID        int64
	Name      string
	IsDefault bool
	// LegacyConnectionID is the connection imported from the deprecated
	// XENDIT_* settings, the target of /webhooks/xendit; nil when none.
	LegacyConnectionID *int64
}

// Connection is one set of credentials for one gateway account. Credentials
// are stored sealed (envelope encryption bound to PublicID); only their last
// four characters are kept in the clear.
type Connection struct {
	ID          int64
	PublicID    string
	MerchantID  int64
	Provider    string
	Name        string
	Environment Environment
	Status      ConnectionStatus
	// CredentialsEnc and CredentialsDEKEnc are the sealed credentials and the
	// data key that seals them, itself encrypted with the master key.
	CredentialsEnc    string `json:"-"`
	CredentialsDEKEnc string `json:"-"`
	KeyVersion        int
	// CredentialsLast4 maps each credential key to its last four characters,
	// or nil when the value is too short to show any of it.
	CredentialsLast4 map[string]*string
	// Settings are the validated provider settings with defaults filled in.
	Settings       map[string]any
	LastTestedAt   *time.Time
	LastTestStatus *ConnectionTestStatus
	LastTestError  *string
	// LegacyWebhookAlias is filled by reads: /webhooks/xendit reaches this
	// connection.
	LegacyWebhookAlias bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Summary returns the short form of the connection that apps carry.
func (c *Connection) Summary() ConnectionSummary {
	return ConnectionSummary{PublicID: c.PublicID, Name: c.Name, Provider: c.Provider, Environment: c.Environment, Status: c.Status}
}

// ConnectionSummary identifies the connection an app or payment refers to.
type ConnectionSummary struct {
	PublicID    string
	Name        string
	Provider    string
	Environment Environment
	Status      ConnectionStatus
}

// ConnectionChannel is a stored channel choice of a connection.
type ConnectionChannel struct {
	Channel Channel
	Enabled bool
}

// ConnectionFilter narrows connection lists; nil fields match everything.
type ConnectionFilter struct {
	Environment *Environment
	Status      *ConnectionStatus
	Provider    *string
}

// MaxInUseApps caps the apps listed in a connection_in_use error.
const MaxInUseApps = 10

// ConnectionRepository persists connections and their channels. Every method
// joins the transaction carried by ctx. Lookups return ErrNotFound when no row
// matches; a duplicate name within the merchant returns ErrConflict.
type ConnectionRepository interface {
	Create(ctx context.Context, c *Connection) error
	// Update writes the mutable columns of c (name, status, settings, sealed
	// credentials and their hints, test result) and refreshes UpdatedAt.
	Update(ctx context.Context, c *Connection) error
	GetByID(ctx context.Context, id int64) (*Connection, error)
	GetByPublicID(ctx context.Context, publicID string) (*Connection, error)
	// GetByPublicIDForUpdate also row-locks the connection until the
	// transaction ends.
	GetByPublicIDForUpdate(ctx context.Context, publicID string) (*Connection, error)
	// GetByPublicIDForShare takes a shared row lock, so the connection cannot
	// be disabled before the transaction ends.
	GetByPublicIDForShare(ctx context.Context, publicID string) (*Connection, error)
	// List returns connections newest first (created_at, then public id).
	List(ctx context.Context, f ConnectionFilter, p Page) (PageResult[Connection], error)
	// NameTaken reports whether the merchant already has a connection named name.
	NameTaken(ctx context.Context, merchantID int64, name string) (bool, error)
	// Channels returns the stored channel rows of a connection; none means
	// every channel of the provider is enabled.
	Channels(ctx context.Context, connectionID int64) ([]ConnectionChannel, error)
	// ReplaceChannels deletes the connection's rows and stores chs instead.
	ReplaceChannels(ctx context.Context, connectionID int64, chs []ConnectionChannel) error
	// AppsUsing returns up to limit apps whose default connection is
	// connectionID, newest first, and how many there are in total.
	AppsUsing(ctx context.Context, connectionID int64, limit int) ([]AppSummary, int, error)
}

// MerchantRepository reads and updates the default merchant.
type MerchantRepository interface {
	GetDefault(ctx context.Context) (*Merchant, error)
	// GetDefaultForUpdate also row-locks the merchant until the transaction
	// ends; it serializes the one-time settings import across processes.
	GetDefaultForUpdate(ctx context.Context) (*Merchant, error)
	SetLegacyConnection(ctx context.Context, merchantID, connectionID int64) error
}
