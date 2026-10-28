package dto

import (
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// SetupRequest is the request body for POST /admin/setup. Field rules are
// checked by the service so they match the contract exactly.
type SetupRequest struct {
	Email      string `json:"email" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Password   string `json:"password" binding:"required"`
	SetupToken string `json:"setup_token" binding:"max=256"`
}

// LoginRequest is the request body for POST /admin/login. The email is not
// format-checked: a malformed email simply does not match.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,max=254"`
	Password string `json:"password" binding:"required,max=1024"`
}

// ChangePasswordRequest is the request body for POST /admin/me/password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required,max=1024"`
	NewPassword     string `json:"new_password" binding:"required"`
}

// CreateUserRequest is the request body for POST /admin/users.
type CreateUserRequest struct {
	Email string      `json:"email" binding:"required"`
	Name  string      `json:"name" binding:"required"`
	Role  domain.Role `json:"role" binding:"required"`
}

// UpdateUserRequest is the request body for PATCH /admin/users/{id}; absent
// fields are left unchanged.
type UpdateUserRequest struct {
	Email *string      `json:"email"`
	Name  *string      `json:"name"`
	Role  *domain.Role `json:"role"`
}

// SetupStatusResponse is the response for GET /admin/setup.
type SetupStatusResponse struct {
	SetupRequired      bool `json:"setup_required"`
	SetupTokenRequired bool `json:"setup_token_required"`
}

// User is a console user as the API shows it. The password hash and internal
// id never leave the server.
type User struct {
	ID                 string            `json:"id"`
	Email              string            `json:"email"`
	Name               string            `json:"name"`
	Role               domain.Role       `json:"role"`
	Status             domain.UserStatus `json:"status"`
	MustChangePassword bool              `json:"must_change_password"`
	DisabledAt         *time.Time        `json:"disabled_at"`
	LastLoginAt        *time.Time        `json:"last_login_at"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

// UserFromDomain converts a user; timestamps are sent in UTC.
func UserFromDomain(u *domain.User) User {
	return User{
		ID:                 u.PublicID,
		Email:              u.Email,
		Name:               u.Name,
		Role:               u.Role,
		Status:             u.Status(),
		MustChangePassword: u.MustChangePassword,
		DisabledAt:         utcPtr(u.DisabledAt),
		LastLoginAt:        utcPtr(u.LastLoginAt),
		CreatedAt:          u.CreatedAt.UTC(),
		UpdatedAt:          u.UpdatedAt.UTC(),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

// MeResponse is the signed-in user and everything their role allows.
type MeResponse struct {
	User        User                `json:"user"`
	Permissions []domain.Permission `json:"permissions"`
}

// MeFromDomain builds a MeResponse; permissions are sorted and never null.
func MeFromDomain(u *domain.User) MeResponse {
	return MeResponse{User: UserFromDomain(u), Permissions: u.Role.Permissions()}
}

// UserResponse wraps one user.
type UserResponse struct {
	User User `json:"user"`
}

// UserWithTemporaryPasswordResponse carries a temporary password shown once.
type UserWithTemporaryPasswordResponse struct {
	User              User   `json:"user"`
	TemporaryPassword string `json:"temporary_password"`
}

// UserListResponse is the response for GET /admin/users.
type UserListResponse struct {
	Data       []User  `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// UserListFromDomain converts a page; data is [] when empty and next_cursor
// null on the last page.
func UserListFromDomain(page domain.PageResult[domain.User]) UserListResponse {
	out := UserListResponse{Data: make([]User, 0, len(page.Items))}
	for i := range page.Items {
		out.Data = append(out.Data, UserFromDomain(&page.Items[i]))
	}
	if page.NextCursor != "" {
		next := page.NextCursor
		out.NextCursor = &next
	}
	return out
}
