package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// LoginResult holds the JWT token and whether the user must change password.
type LoginResult struct {
	Token              string `json:"token"`
	MustChangePassword bool   `json:"must_change_password"`
}

// AuthService authenticates admin users from the database and issues JWT sessions.
type AuthService struct {
	repo      domain.AdminRepository
	jwtSecret []byte
	ttl       time.Duration
	tx        domain.TxManager
	audit     Auditor
}

// NewAuthService constructs an AuthService backed by a database repository.
func NewAuthService(repo domain.AdminRepository, jwtSecret string, tx domain.TxManager, audit Auditor) *AuthService {
	return &AuthService{
		repo:      repo,
		jwtSecret: []byte(jwtSecret),
		ttl:       12 * time.Hour,
		tx:        tx,
		audit:     audit,
	}
}

// passwordChangeSnapshot is the audited view of a password change. It never
// holds a password or hash.
type passwordChangeSnapshot struct {
	MustChangePassword bool `json:"must_change_password"`
}

// Login verifies credentials against the database and returns a JWT + must_change_password flag.
// Wrong credentials return domain.ErrInvalidCredentials; anything else is an
// internal failure.
func (s *AuthService) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	user, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("load admin user: %w", err)
	}
	if user == nil {
		return nil, domain.ErrInvalidCredentials
	}
	if subtle.ConstantTimeCompare([]byte(username), []byte(user.Username)) != 1 {
		return nil, domain.ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	claims := jwt.RegisteredClaims{
		Subject:   username,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Issuer:    "keipay",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}
	return &LoginResult{
		Token:              signed,
		MustChangePassword: user.MustChangePassword,
	}, nil
}

// ChangePassword verifies the current password and updates to a new one. A
// wrong current password returns domain.ErrInvalidCredentials and records
// nothing.
func (s *AuthService) ChangePassword(ctx context.Context, username, currentPwd, newPwd string) error {
	user, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("load admin user: %w", err)
	}
	if user == nil {
		return domain.ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPwd)); err != nil {
		return domain.ErrInvalidCredentials
	}

	// Hashing is slow, so it runs before the transaction opens.
	hash, err := bcrypt.GenerateFromPassword([]byte(newPwd), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdatePassword(ctx, user.ID, string(hash)); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		return s.audit.Record(ctx, ActionAdminChangePassword,
			domain.AuditTarget{Type: TargetAdminUser, ID: user.Username},
			passwordChangeSnapshot{MustChangePassword: user.MustChangePassword},
			passwordChangeSnapshot{MustChangePassword: false})
	})
}

// Verify validates a JWT and returns the subject (admin username).
func (s *AuthService) Verify(tokenStr string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return "", domain.ErrUnauthorized
	}
	return claims.Subject, nil
}
