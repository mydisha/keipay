package domain

import (
	"context"
	"slices"
	"time"
)

// Role is a console user's role. Each role maps to a fixed set of permissions
// in RolePermissions.
type Role string

// Roles, from most to least privileged.
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	_, ok := RolePermissions[r]
	return ok
}

// Permission names one guarded admin capability. The set must equal the
// Permission enum in api/openapi.yaml.
type Permission string

// Permissions checked by the admin API.
const (
	PermAppsRead         Permission = "apps:read"
	PermAppsWrite        Permission = "apps:write"
	PermPaymentsRead     Permission = "payments:read"
	PermDeliveriesRead   Permission = "deliveries:read"
	PermDeliveriesReplay Permission = "deliveries:replay"
	PermAuditRead        Permission = "audit:read"
	PermUsersManage      Permission = "users:manage"
)

// AllPermissions returns every defined permission, sorted.
func AllPermissions() []Permission {
	return sortedPermissions(
		PermAppsRead, PermAppsWrite, PermPaymentsRead, PermDeliveriesRead,
		PermDeliveriesReplay, PermAuditRead, PermUsersManage,
	)
}

// RolePermissions is the only definition of what each role may do. Each list
// is sorted. Callers must treat it as read-only; use Role.Permissions for a
// copy.
var RolePermissions = map[Role][]Permission{
	RoleOwner: AllPermissions(),
	RoleAdmin: sortedPermissions(
		PermAppsRead, PermAppsWrite, PermPaymentsRead, PermDeliveriesRead,
		PermDeliveriesReplay, PermAuditRead,
	),
	RoleViewer: sortedPermissions(PermAppsRead, PermPaymentsRead, PermDeliveriesRead),
}

func sortedPermissions(ps ...Permission) []Permission {
	slices.Sort(ps)
	return ps
}

// Can reports whether the role grants p. Unknown roles grant nothing.
func (r Role) Can(p Permission) bool {
	return slices.Contains(RolePermissions[r], p)
}

// Permissions returns a sorted copy of the role's permissions, never nil.
func (r Role) Permissions() []Permission {
	return append([]Permission{}, RolePermissions[r]...)
}

// UserStatus is derived from User.DisabledAt.
type UserStatus string

// User statuses.
const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

// User is a console user. Email is stored in canonical form (trimmed, NFC,
// lower case). SessionVersion is copied into every session; bumping it ends
// all of the user's sessions.
type User struct {
	ID                 int64
	PublicID           string
	Email              string
	Name               string
	Role               Role
	PasswordHash       string `json:"-"`
	MustChangePassword bool
	DisabledAt         *time.Time
	LastLoginAt        *time.Time
	SessionVersion     int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Status is disabled exactly when DisabledAt is set.
func (u *User) Status() UserStatus {
	if u.DisabledAt != nil {
		return UserDisabled
	}
	return UserActive
}

// IsActiveOwner reports whether u counts towards the active-owner minimum.
func (u *User) IsActiveOwner() bool {
	return u.Role == RoleOwner && u.DisabledAt == nil
}

// UserRepository persists users. Every method joins the transaction carried
// by ctx. Lookups return ErrNotFound when no row matches; a duplicate email
// returns ErrConflict.
type UserRepository interface {
	Create(ctx context.Context, u *User) error
	// Update writes every mutable column of u and refreshes UpdatedAt.
	Update(ctx context.Context, u *User) error
	// SetLastLogin stores only last_login_at, so a sign-in never overwrites a
	// concurrent change to the rest of the row.
	SetLastLogin(ctx context.Context, id int64, at time.Time) error
	GetByID(ctx context.Context, id int64) (*User, error)
	// GetByIDForUpdate also row-locks the user until the transaction ends.
	GetByIDForUpdate(ctx context.Context, id int64) (*User, error)
	GetByPublicID(ctx context.Context, publicID string) (*User, error)
	// GetByPublicIDForUpdate also row-locks the user until the transaction ends.
	GetByPublicIDForUpdate(ctx context.Context, publicID string) (*User, error)
	// GetByEmail matches the canonical email exactly.
	GetByEmail(ctx context.Context, email string) (*User, error)
	// OldestOwner returns the owner created first, disabled or not.
	OldestOwner(ctx context.Context) (*User, error)
	Count(ctx context.Context) (int, error)
	CountActiveOwners(ctx context.Context) (int, error)
	// List returns users newest first (created_at, then public id).
	List(ctx context.Context, p Page) (PageResult[User], error)
	// LockUsers takes the transaction-scoped lock that serializes setup and
	// every user mutation. It fails outside a transaction.
	LockUsers(ctx context.Context) error
}

// Session is a signed-in browser. It is stored under a hash of its token, so
// the token itself is never persisted.
type Session struct {
	UserID         int64     `json:"user_id"`
	UserPublicID   string    `json:"user_public_id"`
	SessionVersion int       `json:"session_version"`
	CSRFToken      string    `json:"csrf_token"`
	CreatedAt      time.Time `json:"created_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	IP             string    `json:"ip"`
	UserAgent      string    `json:"user_agent"`
}

// SessionStore keeps sessions by token. Get returns ErrNotFound for an
// unknown or expired token.
type SessionStore interface {
	Create(ctx context.Context, token string, s *Session, ttl time.Duration) error
	Get(ctx context.Context, token string) (*Session, error)
	// Touch overwrites an existing session and its TTL; it never recreates
	// a session that was deleted meanwhile.
	Touch(ctx context.Context, token string, s *Session, ttl time.Duration) error
	Delete(ctx context.Context, token string) error
}

// FailureLimit caps the attempts counted under one key.
type FailureLimit struct {
	Key string
	Max int
}

// FailureCounter counts attempts per key in fixed windows. An attempt is
// counted before it runs and taken back when it turns out not to be a
// failure, so concurrent attempts cannot all pass a limit that was only read.
type FailureCounter interface {
	// Reserve counts one attempt under every limit at once. When any counter
	// has already reached its Max it counts nothing and returns false with the
	// longest time left among the exceeded windows. A counter's first attempt
	// starts a window of the given length.
	Reserve(ctx context.Context, window time.Duration, limits ...FailureLimit) (bool, time.Duration, error)
	// Release takes back one reserved attempt per key.
	Release(ctx context.Context, keys ...string) error
	Reset(ctx context.Context, key string) error
}
