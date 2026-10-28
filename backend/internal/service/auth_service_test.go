package service

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
)

type memAdminRepo struct {
	user   *domain.AdminUser
	getErr error
}

func (m *memAdminRepo) GetByUsername(_ context.Context, username string) (*domain.AdminUser, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.user == nil || m.user.Username != username {
		return nil, nil
	}
	cp := *m.user
	return &cp, nil
}

func (m *memAdminRepo) Create(context.Context, *domain.AdminUser) error { return nil }

func (m *memAdminRepo) UpdatePassword(ctx context.Context, _ int64, hash string) error {
	prev := *m.user
	m.user.PasswordHash = hash
	m.user.MustChangePassword = false
	onRollback(ctx, func() { *m.user = prev })
	return nil
}

func newAuthFixture(t *testing.T) (*AuthService, *memAdminRepo, *memAuditRepo) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("current-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memAdminRepo{user: &domain.AdminUser{ID: 1, Username: "admin", PasswordHash: string(hash), MustChangePassword: true}}
	audit := &memAuditRepo{}
	return NewAuthService(repo, "test-jwt-secret", &fakeTxManager{}, NewAuditService(audit)), repo, audit
}

func TestChangePasswordRecordsEntry(t *testing.T) {
	svc, repo, audit := newAuthFixture(t)
	if err := svc.ChangePassword(adminCtx(), "admin", "current-pass", "new-password-1"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(repo.user.PasswordHash), []byte("new-password-1")) != nil {
		t.Fatal("password not updated")
	}
	if len(audit.logs) != 1 {
		t.Fatalf("got %d entries, want 1", len(audit.logs))
	}
	l := audit.logs[0]
	if l.Action != "admin_user.change_password" || l.Target != (domain.AuditTarget{Type: "admin_user", ID: "admin"}) {
		t.Fatalf("action/target = %s %+v", l.Action, l.Target)
	}
	assertJSON(t, "before", l.Before, map[string]any{"must_change_password": true})
	assertJSON(t, "after", l.After, map[string]any{"must_change_password": false})
}

func TestChangePasswordWrongCurrentRecordsNothing(t *testing.T) {
	svc, repo, audit := newAuthFixture(t)
	before := repo.user.PasswordHash
	err := svc.ChangePassword(adminCtx(), "admin", "wrong", "new-password-1")
	if !errors.Is(err, domain.ErrInvalidCredentials) || !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrInvalidCredentials wrapping ErrUnauthorized", err)
	}
	if len(audit.logs) != 0 || repo.user.PasswordHash != before {
		t.Fatalf("wrong password changed state: entries=%d", len(audit.logs))
	}
}

func TestChangePasswordAuditFailureKeepsPassword(t *testing.T) {
	svc, repo, audit := newAuthFixture(t)
	audit.err = errAuditDown
	before := repo.user.PasswordHash
	if err := svc.ChangePassword(adminCtx(), "admin", "current-pass", "new-password-1"); !errors.Is(err, errAuditDown) {
		t.Fatalf("err = %v, want audit failure", err)
	}
	if repo.user.PasswordHash != before || !repo.user.MustChangePassword {
		t.Fatal("password change not rolled back")
	}
}

func TestChangePasswordRepoErrorIsInternal(t *testing.T) {
	svc, repo, _ := newAuthFixture(t)
	repo.getErr = errors.New("db down")
	err := svc.ChangePassword(adminCtx(), "admin", "current-pass", "new-password-1")
	if err == nil || errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("err = %v, want a non-auth error", err)
	}
}

func TestLoginOutcomes(t *testing.T) {
	svc, repo, _ := newAuthFixture(t)
	ctx := context.Background()

	res, err := svc.Login(ctx, "admin", "current-pass")
	if err != nil || res.Token == "" || !res.MustChangePassword {
		t.Fatalf("login = %+v, %v", res, err)
	}
	if sub, err := svc.Verify(res.Token); err != nil || sub != "admin" {
		t.Fatalf("verify = %q, %v", sub, err)
	}
	for _, c := range [][2]string{{"admin", "wrong"}, {"nobody", "current-pass"}, {"", ""}} {
		if _, err := svc.Login(ctx, c[0], c[1]); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("login(%q, %q) err = %v, want ErrInvalidCredentials", c[0], c[1], err)
		}
	}
	repo.getErr = errors.New("db down")
	if _, err := svc.Login(ctx, "admin", "current-pass"); err == nil || errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("repo failure err = %v, want a non-auth error", err)
	}
}
