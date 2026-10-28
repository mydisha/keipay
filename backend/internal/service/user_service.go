package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

// UserService manages console users. Authorization (users:manage) is checked
// by the transport layer; the service enforces the business rules. Every
// mutation takes the users lock, row-locks its target and records one audit
// entry in the same transaction.
type UserService struct {
	users       domain.UserRepository
	tx          domain.TxManager
	audit       Auditor
	hasher      PasswordHasher
	now         func() time.Time
	newPassword func() (string, error)
}

// NewUserService constructs a UserService.
func NewUserService(users domain.UserRepository, tx domain.TxManager, audit Auditor, hasher PasswordHasher) *UserService {
	return &UserService{
		users:       users,
		tx:          tx,
		audit:       audit,
		hasher:      hasher,
		now:         time.Now,
		newPassword: TemporaryPassword,
	}
}

// CreateUserInput is a user to create with a temporary password.
type CreateUserInput struct {
	Email string
	Name  string
	Role  domain.Role
}

// UpdateUserInput holds the fields to change; nil means unchanged.
type UpdateUserInput struct {
	Email *string
	Name  *string
	Role  *domain.Role
}

// List returns one page of users, newest first.
func (s *UserService) List(ctx context.Context, req domain.PageRequest) (domain.PageResult[domain.User], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.User]{}, err
	}
	res, err := s.users.List(ctx, p)
	if err != nil {
		return domain.PageResult[domain.User]{}, fmt.Errorf("list users: %w", err)
	}
	return res, nil
}

// Get returns a user by public id.
func (s *UserService) Get(ctx context.Context, publicID string) (*domain.User, error) {
	u, err := s.users.GetByPublicID(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// Create adds an active user with a generated temporary password, which is
// returned once and must be changed at first sign-in.
func (s *UserService) Create(ctx context.Context, in CreateUserInput) (*domain.User, string, error) {
	email := CanonicalEmail(in.Email)
	name, nameRule := normalizeName(in.Name)
	var fe fieldErrors
	fe.check("email", emailRule(email))
	fe.check("name", nameRule)
	fe.check("role", roleRule(in.Role))
	if err := fe.err(); err != nil {
		return nil, "", err
	}
	password, hash, err := s.temporaryPassword()
	if err != nil {
		return nil, "", err
	}
	u := &domain.User{
		PublicID:           id.New(id.User),
		Email:              email,
		Name:               name,
		Role:               in.Role,
		PasswordHash:       hash,
		MustChangePassword: true,
		SessionVersion:     1,
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.users.LockUsers(ctx); err != nil {
			return err
		}
		if err := s.users.Create(ctx, u); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionUserCreate, userTarget(u.PublicID), nil, snapshotNewUser(u, true))
	})
	if err != nil {
		return nil, "", fmt.Errorf("create user: %w", err)
	}
	return u, password, nil
}

// Update changes the given fields. A request that changes nothing writes and
// records nothing. Demoting the last active owner returns ErrLastOwner.
func (s *UserService) Update(ctx context.Context, publicID string, in UpdateUserInput) (*domain.User, error) {
	if in.Email == nil && in.Name == nil && in.Role == nil {
		return nil, domain.InvalidFields(domain.FieldError{Field: "body", Rule: RuleRequired})
	}
	var (
		fe          fieldErrors
		email, name string
	)
	if in.Email != nil {
		email = CanonicalEmail(*in.Email)
		fe.check("email", emailRule(email))
	}
	if in.Name != nil {
		var rule string
		name, rule = normalizeName(*in.Name)
		fe.check("name", rule)
	}
	if in.Role != nil {
		fe.check("role", roleRule(*in.Role))
	}
	if err := fe.err(); err != nil {
		return nil, err
	}

	return s.mutate(ctx, publicID, func(ctx context.Context, u *domain.User) error {
		before, after := map[string]any{}, map[string]any{}
		demotesOwner := false
		if in.Email != nil && email != u.Email {
			before["email"], after["email"] = u.Email, email
			u.Email = email
		}
		if in.Name != nil && name != u.Name {
			before["name"], after["name"] = u.Name, name
			u.Name = name
		}
		if in.Role != nil && *in.Role != u.Role {
			demotesOwner = u.IsActiveOwner()
			before["role"], after["role"] = u.Role, *in.Role
			u.Role = *in.Role
		}
		if len(after) == 0 {
			return nil
		}
		if demotesOwner {
			if err := s.requireAnotherOwner(ctx); err != nil {
				return err
			}
		}
		if err := s.users.Update(ctx, u); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionUserUpdate, userTarget(u.PublicID), before, after)
	})
}

// Disable blocks sign-in and ends every session of the user. Disabling an
// already disabled user changes and records nothing.
func (s *UserService) Disable(ctx context.Context, publicID, actorID string) (*domain.User, error) {
	if publicID == actorID {
		return nil, domain.ErrCannotModifySelf
	}
	return s.mutate(ctx, publicID, func(ctx context.Context, u *domain.User) error {
		if u.DisabledAt != nil {
			return nil
		}
		if u.IsActiveOwner() {
			if err := s.requireAnotherOwner(ctx); err != nil {
				return err
			}
		}
		now := s.timestamp()
		u.DisabledAt = &now
		u.SessionVersion++
		if err := s.users.Update(ctx, u); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionUserDisable, userTarget(u.PublicID),
			disabledSnapshot{DisabledAt: nil}, disabledSnapshot{DisabledAt: &now})
	})
}

// Enable lets a disabled user sign in again with their existing password.
// Enabling an active user changes and records nothing.
func (s *UserService) Enable(ctx context.Context, publicID string) (*domain.User, error) {
	return s.mutate(ctx, publicID, func(ctx context.Context, u *domain.User) error {
		if u.DisabledAt == nil {
			return nil
		}
		before := disabledSnapshot{DisabledAt: u.DisabledAt}
		u.DisabledAt = nil
		if err := s.users.Update(ctx, u); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionUserEnable, userTarget(u.PublicID), before, disabledSnapshot{DisabledAt: nil})
	})
}

// ResetPassword replaces the user's password with a temporary one, returned
// once, and ends every session of the user.
func (s *UserService) ResetPassword(ctx context.Context, publicID, actorID string) (*domain.User, string, error) {
	if publicID == actorID {
		return nil, "", domain.ErrCannotModifySelf
	}
	password, hash, err := s.temporaryPassword()
	if err != nil {
		return nil, "", err
	}
	u, err := s.mutate(ctx, publicID, func(ctx context.Context, u *domain.User) error {
		before := passwordFlagSnapshot{MustChangePassword: u.MustChangePassword}
		u.PasswordHash = hash
		u.MustChangePassword = true
		u.SessionVersion++
		if err := s.users.Update(ctx, u); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionUserResetPassword, userTarget(u.PublicID),
			before, passwordFlagSnapshot{MustChangePassword: true})
	})
	if err != nil {
		return nil, "", err
	}
	return u, password, nil
}

// RecoverResult is the outcome of Recover. Password is set only when it was
// generated.
type RecoverResult struct {
	User     *domain.User
	Password string
	Created  bool
}

// ErrNoOwner means recovery without an email found no owner to reset.
var ErrNoOwner = errors.New("no owner exists; pass an email to create one")

// Recover is the shell-only recovery path behind `migrate -reset-admin`. With
// an email it resets that user (enabling them again), or creates the first
// owner when no user exists at all; without an email it resets the oldest
// owner. An empty password generates a temporary one that must be changed at
// sign-in. ctx must carry the CLI's system actor.
func (s *UserService) Recover(ctx context.Context, email, password string) (*RecoverResult, error) {
	email = CanonicalEmail(email)
	var fe fieldErrors
	if email != "" {
		fe.check("email", emailRule(email))
	}
	if password != "" {
		fe.check("password", passwordRule(password))
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	generated := password == ""
	if generated {
		var err error
		if password, err = s.newPassword(); err != nil {
			return nil, err
		}
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	res := &RecoverResult{}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.users.LockUsers(ctx); err != nil {
			return err
		}
		target, err := s.recoveryTarget(ctx, email)
		if errors.Is(err, errCreateFirstOwner) {
			res.User, res.Created = s.firstOwner(email, hash, generated), true
			if err := s.users.Create(ctx, res.User); err != nil {
				return err
			}
			return s.audit.Record(ctx, ActionUserSetup, userTarget(res.User.PublicID), nil, snapshotNewUser(res.User, true))
		}
		if err != nil {
			return err
		}
		u, err := s.users.GetByIDForUpdate(ctx, target.ID)
		if err != nil {
			return err
		}
		before := recoverSnapshot{MustChangePassword: u.MustChangePassword, DisabledAt: u.DisabledAt}
		u.PasswordHash = hash
		u.MustChangePassword = generated
		u.DisabledAt = nil
		u.SessionVersion++
		if err := s.users.Update(ctx, u); err != nil {
			return err
		}
		res.User = u
		return s.audit.Record(ctx, ActionUserResetPassword, userTarget(u.PublicID),
			before, recoverSnapshot{MustChangePassword: generated})
	})
	if err != nil {
		return nil, fmt.Errorf("recover access: %w", err)
	}
	if generated {
		res.Password = password
	}
	return res, nil
}

var errCreateFirstOwner = errors.New("no user exists")

// recoveryTarget picks the user Recover resets, or errCreateFirstOwner when
// the email is given and no user exists yet.
func (s *UserService) recoveryTarget(ctx context.Context, email string) (*domain.User, error) {
	if email == "" {
		u, err := s.users.OldestOwner(ctx)
		if errors.Is(err, domain.ErrNotFound) {
			return nil, ErrNoOwner
		}
		return u, err
	}
	u, err := s.users.GetByEmail(ctx, email)
	if !errors.Is(err, domain.ErrNotFound) {
		return u, err
	}
	n, err := s.users.Count(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errCreateFirstOwner
	}
	return nil, fmt.Errorf("%w: no user has the email %s", domain.ErrNotFound, email)
}

func (s *UserService) firstOwner(email, hash string, mustChange bool) *domain.User {
	local, _, _ := strings.Cut(email, "@")
	name, _ := normalizeName(local)
	return &domain.User{
		PublicID:           id.New(id.User),
		Email:              email,
		Name:               name,
		Role:               domain.RoleOwner,
		PasswordHash:       hash,
		MustChangePassword: mustChange,
		SessionVersion:     1,
	}
}

// mutate runs fn on the row-locked user inside a transaction that holds the
// users lock, so owner counts cannot change underneath it.
func (s *UserService) mutate(ctx context.Context, publicID string, fn func(ctx context.Context, u *domain.User) error) (*domain.User, error) {
	var u *domain.User
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.users.LockUsers(ctx); err != nil {
			return err
		}
		var err error
		if u, err = s.users.GetByPublicIDForUpdate(ctx, publicID); err != nil {
			return err
		}
		return fn(ctx, u)
	})
	if err != nil {
		return nil, fmt.Errorf("update user %s: %w", publicID, err)
	}
	return u, nil
}

// requireAnotherOwner fails when at most one active owner exists, i.e. the
// change at hand would remove the last one.
func (s *UserService) requireAnotherOwner(ctx context.Context) error {
	n, err := s.users.CountActiveOwners(ctx)
	if err != nil {
		return fmt.Errorf("count owners: %w", err)
	}
	if n <= 1 {
		return domain.ErrLastOwner
	}
	return nil
}

func (s *UserService) temporaryPassword() (password, hash string, err error) {
	if password, err = s.newPassword(); err != nil {
		return "", "", err
	}
	if hash, err = s.hasher.Hash(password); err != nil {
		return "", "", err
	}
	return password, hash, nil
}

// timestamp is the current time at the precision Postgres stores.
func (s *UserService) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Microsecond)
}

type disabledSnapshot struct {
	DisabledAt *time.Time `json:"disabled_at"`
}

type recoverSnapshot struct {
	MustChangePassword bool       `json:"must_change_password"`
	DisabledAt         *time.Time `json:"disabled_at"`
}
