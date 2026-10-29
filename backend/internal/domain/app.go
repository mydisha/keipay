package domain

import (
	"context"
	"time"
)

// Environment separates test from live traffic. An app's environment is fixed
// at creation and every key of the app carries it.
type Environment string

// Environments.
const (
	EnvTest Environment = "test"
	EnvLive Environment = "live"
)

// Valid reports whether e is a known environment.
func (e Environment) Valid() bool { return e == EnvTest || e == EnvLive }

// AppStatus is whether an app may call /v1.
type AppStatus string

// App statuses.
const (
	AppActive   AppStatus = "active"
	AppDisabled AppStatus = "disabled"
)

// Valid reports whether s is a known app status.
func (s AppStatus) Valid() bool { return s == AppActive || s == AppDisabled }

// App is a client application that calls KeiPay. Slug is unique within an
// environment; neither slug nor environment ever changes.
type App struct {
	ID                 int64
	PublicID           string
	Slug               string
	Name               string
	Environment        Environment
	Status             AppStatus
	Metadata           Metadata
	WebhookURL         string
	RedirectSuccessURL string
	RedirectFailureURL string
	// WebhookSecretEnc is the encrypted secret that signs outbound webhooks.
	WebhookSecretEnc string `json:"-"`
	MerchantID       int64
	// DefaultConnectionID is the connection /v1/payments uses; nil when none.
	// DefaultConnection is its summary, filled by every read.
	DefaultConnectionID *int64
	DefaultConnection   *ConnectionSummary
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Summary returns the short form of the app that list rows carry.
func (a *App) Summary() AppSummary {
	return AppSummary{PublicID: a.PublicID, Slug: a.Slug, Name: a.Name, Environment: a.Environment}
}

// AppSummary identifies the app a payment, payment report or delivery belongs to.
type AppSummary struct {
	PublicID    string
	Slug        string
	Name        string
	Environment Environment
}

// AppFilter narrows app lists; nil fields match everything.
type AppFilter struct {
	Environment *Environment
	Status      *AppStatus
}

// AppRepository persists apps. Every method joins the transaction carried by
// ctx. Lookups return ErrNotFound when no row matches; a duplicate
// (environment, slug) returns ErrConflict.
type AppRepository interface {
	Create(ctx context.Context, a *App) error
	// Update writes the mutable columns of a, its default connection
	// included, and refreshes UpdatedAt.
	Update(ctx context.Context, a *App) error
	GetByID(ctx context.Context, id int64) (*App, error)
	GetByPublicID(ctx context.Context, publicID string) (*App, error)
	// GetByPublicIDForUpdate also row-locks the app until the transaction ends.
	GetByPublicIDForUpdate(ctx context.Context, publicID string) (*App, error)
	GetBySlug(ctx context.Context, env Environment, slug string) (*App, error)
	// List returns apps newest first (created_at, then public id).
	List(ctx context.Context, f AppFilter, p Page) (PageResult[App], error)
	// ListWithoutDefaultConnection returns every app without a default
	// connection, oldest first, row-locked until the transaction ends.
	ListWithoutDefaultConnection(ctx context.Context) ([]App, error)
}
