package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
)

// AuthConfig holds the session and sign-in limits.
type AuthConfig struct {
	IdleTimeout         time.Duration
	AbsoluteTimeout     time.Duration
	MaxFailuresPerIP    int
	MaxFailuresPerEmail int
	FailureWindow       time.Duration
	// SetupToken, when set, must accompany first-run setup.
	SetupToken string
}

// AuthDeps are the ports AuthService uses.
type AuthDeps struct {
	Users    domain.UserRepository
	Sessions domain.SessionStore
	Failures domain.FailureCounter
	Tx       domain.TxManager
	Audit    Auditor
	Hasher   PasswordHasher
	Log      *zap.Logger
}

// ClientMeta describes where a request came from.
type ClientMeta struct {
	IP        string
	UserAgent string
	RequestID string
}

// AuthResult is a signed-in user with a fresh session. Token is the cookie
// value; it is never stored or logged.
type AuthResult struct {
	User    *domain.User
	Session *domain.Session
	Token   string
}

// SetupStatus tells the console whether first-run setup is still open.
type SetupStatus struct {
	Required      bool
	TokenRequired bool
}

// SetupInput is the first owner to create.
type SetupInput struct {
	Email      string
	Name       string
	Password   string
	SetupToken string
}

// LoginInput is a sign-in attempt. PreviousToken is the session cookie the
// browser already had, if any; it is replaced.
type LoginInput struct {
	Email         string
	Password      string
	PreviousToken string
}

// ChangePasswordInput is a signed-in user changing their own password.
type ChangePasswordInput struct {
	User            *domain.User
	Token           string
	CurrentPassword string
	NewPassword     string
}

// Reasons recorded on user.login_failed.
const (
	reasonInvalidCredentials = "invalid_credentials"
	reasonUserDisabled       = "user_disabled"
)

// touchInterval bounds how often an active session is written back to Redis.
const touchInterval = time.Minute

// maxStoredUserAgent caps what a client can make us store per session.
const maxStoredUserAgent = 512

// AuthService runs first-run setup, sign-in, sessions and password changes.
type AuthService struct {
	users    domain.UserRepository
	sessions domain.SessionStore
	failures domain.FailureCounter
	tx       domain.TxManager
	audit    Auditor
	hasher   PasswordHasher
	log      *zap.Logger
	cfg      AuthConfig

	// dummyHash is compared against for unknown emails, so a sign-in takes
	// the same time whether or not the account exists.
	dummyHash string
	now       func() time.Time
	newToken  func() (string, error)
}

// NewAuthService constructs an AuthService. It hashes a random dummy password
// with the configured hasher, which takes a moment at bcrypt's production cost.
func NewAuthService(deps AuthDeps, cfg AuthConfig) (*AuthService, error) {
	random, err := NewToken()
	if err != nil {
		return nil, err
	}
	dummy, err := deps.Hasher.Hash(random)
	if err != nil {
		return nil, fmt.Errorf("dummy hash: %w", err)
	}
	log := deps.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &AuthService{
		users:     deps.Users,
		sessions:  deps.Sessions,
		failures:  deps.Failures,
		tx:        deps.Tx,
		audit:     deps.Audit,
		hasher:    deps.Hasher,
		log:       log,
		cfg:       cfg,
		dummyHash: dummy,
		now:       time.Now,
		newToken:  NewToken,
	}, nil
}

// SetupStatus reports whether no user exists yet.
func (s *AuthService) SetupStatus(ctx context.Context) (SetupStatus, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return SetupStatus{}, fmt.Errorf("count users: %w", err)
	}
	return SetupStatus{Required: n == 0, TokenRequired: s.cfg.SetupToken != ""}, nil
}

// Setup creates the first owner and signs them in. It fails with
// setup_completed once any user exists; concurrent calls are serialized by
// the users lock so exactly one succeeds. Only a wrong setup token counts as
// a failure against the IP limit.
func (s *AuthService) Setup(ctx context.Context, in SetupInput, meta ClientMeta) (*AuthResult, error) {
	email := CanonicalEmail(in.Email)
	name, nameRule := normalizeName(in.Name)
	var fe fieldErrors
	fe.check("email", emailRule(email))
	fe.check("name", nameRule)
	fe.check("password", passwordRule(in.Password))
	if err := fe.err(); err != nil {
		return nil, err
	}

	ipKey := ipFailureKey(meta.IP)
	if err := s.reserveAttempt(ctx, meta, logger.EventSetupRateLimited,
		domain.FailureLimit{Key: ipKey, Max: s.cfg.MaxFailuresPerIP}); err != nil {
		return nil, err
	}
	u, err := s.createOwner(ctx, email, name, in)
	if errors.Is(err, domain.ErrInvalidSetupToken) {
		logger.Security(s.log, logger.EventSetupTokenInvalid, logger.SeverityHigh, meta.IP, meta.RequestID, "invalid setup token")
		return nil, domain.ErrInvalidSetupToken
	}
	s.releaseAttempt(ctx, ipKey)
	if err != nil {
		return nil, err
	}
	return s.startSession(ctx, u, meta)
}

// createOwner inserts the first owner. The unlocked count runs before bcrypt
// so that requests to a console that is already set up stay cheap; the
// locked count inside the transaction is the one that decides.
func (s *AuthService) createOwner(ctx context.Context, email, name string, in SetupInput) (*domain.User, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil, domain.ErrSetupCompleted
	}
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, err
	}
	u := &domain.User{
		PublicID:       id.New(id.User),
		Email:          email,
		Name:           name,
		Role:           domain.RoleOwner,
		PasswordHash:   hash,
		SessionVersion: 1,
	}
	err = s.tx.WithinTx(domain.WithActor(ctx, domain.SystemActor("setup")), func(ctx context.Context) error {
		if err := s.users.LockUsers(ctx); err != nil {
			return err
		}
		n, err := s.users.Count(ctx)
		if err != nil {
			return fmt.Errorf("count users: %w", err)
		}
		if n > 0 {
			return domain.ErrSetupCompleted
		}
		// Checked only now, so a wrong token cannot probe whether setup happened.
		if s.cfg.SetupToken != "" && !SecureEqual(in.SetupToken, s.cfg.SetupToken) {
			return domain.ErrInvalidSetupToken
		}
		if err := s.users.Create(ctx, u); err != nil {
			return fmt.Errorf("create owner: %w", err)
		}
		return s.audit.Record(ctx, ActionUserSetup, userTarget(u.PublicID), nil, snapshotNewUser(u, false))
	})
	if err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}
	return u, nil
}

// Login signs a user in by email and password. Wrong passwords, unknown
// emails and disabled users all return ErrInvalidCredentials; over the
// failure limit it returns rate_limited without checking the password.
// Every attempt is counted before the password check and taken back on
// success, so parallel guesses cannot overrun the limits.
func (s *AuthService) Login(ctx context.Context, in LoginInput, meta ClientMeta) (*AuthResult, error) {
	email := CanonicalEmail(in.Email)
	ipKey, emailKey := ipFailureKey(meta.IP), emailFailureKey(email)
	if err := s.reserveAttempt(ctx, meta, logger.EventLoginRateLimited,
		domain.FailureLimit{Key: ipKey, Max: s.cfg.MaxFailuresPerIP},
		domain.FailureLimit{Key: emailKey, Max: s.cfg.MaxFailuresPerEmail}); err != nil {
		return nil, err
	}

	u, err := s.users.GetByEmail(ctx, email)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		s.hasher.Matches(s.dummyHash, in.Password)
		return nil, s.loginFailed(ctx, email, nil, reasonInvalidCredentials, meta)
	case err != nil:
		s.releaseAttempt(ctx, ipKey, emailKey)
		return nil, fmt.Errorf("load user: %w", err)
	case !s.hasher.Matches(u.PasswordHash, in.Password):
		return nil, s.loginFailed(ctx, email, u, reasonInvalidCredentials, meta)
	case u.DisabledAt != nil:
		return nil, s.loginFailed(ctx, email, u, reasonUserDisabled, meta)
	}

	s.releaseAttempt(ctx, ipKey)
	if err := s.failures.Reset(ctx, emailKey); err != nil {
		return nil, fmt.Errorf("reset failed sign-ins: %w", err)
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	err = s.tx.WithinTx(withUserActor(ctx, u, meta), func(ctx context.Context) error {
		if err := s.users.SetLastLogin(ctx, u.ID, now); err != nil {
			return fmt.Errorf("set last login: %w", err)
		}
		return s.audit.Record(ctx, ActionUserLogin, userTarget(u.PublicID), nil, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("record sign-in: %w", err)
	}
	u.LastLoginAt = &now

	if in.PreviousToken != "" {
		if err := s.sessions.Delete(ctx, in.PreviousToken); err != nil {
			return nil, fmt.Errorf("delete previous session: %w", err)
		}
	}
	return s.startSession(ctx, u, meta)
}

// loginFailed audits and logs a failed sign-in, then returns the generic
// invalid credentials error. The attempt stays counted from its reservation.
func (s *AuthService) loginFailed(ctx context.Context, email string, u *domain.User, reason string, meta ClientMeta) error {
	userID := ""
	if u != nil {
		userID = u.PublicID
	}
	actorCtx := domain.WithActor(ctx, domain.AnonymousActor(meta.IP, meta.UserAgent, meta.RequestID))
	if err := s.audit.Record(actorCtx, ActionUserLoginFailed, userTarget(userID), nil,
		loginFailedSnapshot{Email: truncateBytes(email, maxEmailBytes), Reason: reason}); err != nil {
		return fmt.Errorf("record failed sign-in: %w", err)
	}
	logger.Security(s.log, logger.EventLoginFailed, logger.SeverityMedium, meta.IP, meta.RequestID, reason,
		zap.String("user_id", userID))
	return domain.ErrInvalidCredentials
}

// Authenticate resolves a session token to its user. Any reason the session
// cannot be used (unknown, timed out, revoked, user gone or disabled) returns
// ErrSessionExpired, and the stale session is deleted.
func (s *AuthService) Authenticate(ctx context.Context, token string) (*domain.User, *domain.Session, error) {
	sess, err := s.sessions.Get(ctx, token)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil, domain.ErrSessionExpired
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load session: %w", err)
	}
	now := s.now().UTC()
	age := now.Sub(sess.CreatedAt)
	idle := now.Sub(sess.LastSeenAt)
	if age >= s.cfg.AbsoluteTimeout || idle >= s.cfg.IdleTimeout {
		return nil, nil, s.endSession(ctx, token)
	}
	u, err := s.users.GetByID(ctx, sess.UserID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil, s.endSession(ctx, token)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load session user: %w", err)
	}
	if u.DisabledAt != nil || u.SessionVersion != sess.SessionVersion {
		return nil, nil, s.endSession(ctx, token)
	}
	if idle >= touchInterval {
		sess.LastSeenAt = now
		ttl := min(s.cfg.IdleTimeout, s.cfg.AbsoluteTimeout-age)
		if err := s.sessions.Touch(ctx, token, sess, ttl); err != nil {
			return nil, nil, fmt.Errorf("touch session: %w", err)
		}
	}
	return u, sess, nil
}

// endSession deletes an unusable session and returns ErrSessionExpired.
func (s *AuthService) endSession(ctx context.Context, token string) error {
	if err := s.sessions.Delete(ctx, token); err != nil {
		return fmt.Errorf("delete expired session: %w", err)
	}
	return domain.ErrSessionExpired
}

// Logout ends the session named by token when it is valid and records it.
// Without a valid session it does nothing.
func (s *AuthService) Logout(ctx context.Context, token string, meta ClientMeta) error {
	if token == "" {
		return nil
	}
	u, _, err := s.Authenticate(ctx, token)
	if errors.Is(err, domain.ErrSessionExpired) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.sessions.Delete(ctx, token); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return s.audit.Record(withUserActor(ctx, u, meta), ActionUserLogout, userTarget(u.PublicID), nil, nil)
}

// ChangePassword replaces the signed-in user's password, ends every other
// session (by bumping the session version) and starts a new session for this
// browser.
func (s *AuthService) ChangePassword(ctx context.Context, in ChangePasswordInput, meta ClientMeta) (*AuthResult, error) {
	if !s.hasher.Matches(in.User.PasswordHash, in.CurrentPassword) {
		return nil, domain.ErrInvalidCredentials
	}
	if rule := passwordRule(in.NewPassword); rule != "" {
		return nil, domain.InvalidFields(domain.FieldError{Field: "new_password", Rule: rule})
	}
	if in.NewPassword == in.CurrentPassword {
		return nil, domain.NewError(domain.CodeValidationFailed, "the new password must differ from the current one")
	}
	hash, err := s.hasher.Hash(in.NewPassword)
	if err != nil {
		return nil, err
	}

	var u *domain.User
	err = s.tx.WithinTx(withUserActor(ctx, in.User, meta), func(ctx context.Context) error {
		var err error
		u, err = s.users.GetByIDForUpdate(ctx, in.User.ID)
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		before := passwordFlagSnapshot{MustChangePassword: u.MustChangePassword}
		u.PasswordHash = hash
		u.MustChangePassword = false
		u.SessionVersion++
		if err := s.users.Update(ctx, u); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		return s.audit.Record(ctx, ActionUserChangePassword, userTarget(u.PublicID),
			before, passwordFlagSnapshot{MustChangePassword: false})
	})
	if err != nil {
		return nil, fmt.Errorf("change password: %w", err)
	}
	if err := s.sessions.Delete(ctx, in.Token); err != nil {
		return nil, fmt.Errorf("delete old session: %w", err)
	}
	return s.startSession(ctx, u, meta)
}

// startSession stores a new session for u with fresh session and CSRF tokens.
func (s *AuthService) startSession(ctx context.Context, u *domain.User, meta ClientMeta) (*AuthResult, error) {
	token, err := s.newToken()
	if err != nil {
		return nil, err
	}
	csrf, err := s.newToken()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	sess := &domain.Session{
		UserID:         u.ID,
		UserPublicID:   u.PublicID,
		SessionVersion: u.SessionVersion,
		CSRFToken:      csrf,
		CreatedAt:      now,
		LastSeenAt:     now,
		IP:             meta.IP,
		UserAgent:      truncateBytes(meta.UserAgent, maxStoredUserAgent),
	}
	if err := s.sessions.Create(ctx, token, sess, s.cfg.IdleTimeout); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &AuthResult{User: u, Session: sess, Token: token}, nil
}

// reserveAttempt counts an attempt under every limit, or returns rate_limited
// when one is already reached. The retry delay is the longest remaining
// window among the exceeded counters.
func (s *AuthService) reserveAttempt(ctx context.Context, meta ClientMeta, event string, limits ...domain.FailureLimit) error {
	ok, wait, err := s.failures.Reserve(ctx, s.cfg.FailureWindow, limits...)
	if err != nil {
		return fmt.Errorf("count attempt: %w", err)
	}
	if ok {
		return nil
	}
	logger.Security(s.log, event, logger.SeverityMedium, meta.IP, meta.RequestID, "too many failed attempts")
	return domain.RateLimited(int(math.Ceil(wait.Seconds())))
}

// releaseAttempt takes back a reserved attempt that was not a failure. It
// survives a cancelled request, and a release that still fails only leaves
// the attempt counted, which errs on the strict side, so it is logged rather
// than turned into an error for an otherwise finished request.
func (s *AuthService) releaseAttempt(ctx context.Context, keys ...string) {
	if err := s.failures.Release(context.WithoutCancel(ctx), keys...); err != nil {
		s.log.Warn("release counted attempt", zap.Error(err))
	}
}

func ipFailureKey(ip string) string { return "ip:" + ip }

// emailFailureKey hashes the email so Redis never holds addresses.
func emailFailureKey(email string) string { return "email:" + sha256Hex(email) }

func userTarget(publicID string) domain.AuditTarget {
	return domain.AuditTarget{Type: TargetUser, ID: publicID}
}

func withUserActor(ctx context.Context, u *domain.User, meta ClientMeta) context.Context {
	return domain.WithActor(ctx, domain.UserActor(u.PublicID, meta.IP, meta.UserAgent, meta.RequestID))
}

// createdUserSnapshot is the audited view of a created user; it never holds a
// password or hash.
type createdUserSnapshot struct {
	Email              string      `json:"email"`
	Name               string      `json:"name"`
	Role               domain.Role `json:"role"`
	MustChangePassword *bool       `json:"must_change_password,omitempty"`
}

func snapshotNewUser(u *domain.User, withFlag bool) createdUserSnapshot {
	snap := createdUserSnapshot{Email: u.Email, Name: u.Name, Role: u.Role}
	if withFlag {
		flag := u.MustChangePassword
		snap.MustChangePassword = &flag
	}
	return snap
}

type loginFailedSnapshot struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type passwordFlagSnapshot struct {
	MustChangePassword bool `json:"must_change_password"`
}
