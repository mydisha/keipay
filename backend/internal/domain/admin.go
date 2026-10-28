package domain

import (
	"context"
	"time"
)

// AdminUser represents an admin user stored in the database.
type AdminUser struct {
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	PasswordHash       string    `json:"-"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// AdminRepository defines admin user persistence operations.
type AdminRepository interface {
	GetByUsername(ctx context.Context, username string) (*AdminUser, error)
	Create(ctx context.Context, user *AdminUser) error
	UpdatePassword(ctx context.Context, id int64, passwordHash string) error
}
