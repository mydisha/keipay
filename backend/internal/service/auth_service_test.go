package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

// recordingHasher records how often it hashed and which hashes were
// compared, so tests can prove the dummy hash is checked for unknown emails
// and that cheap rejections never reach bcrypt.
type recordingHasher struct {
	inner    PasswordHasher
	mu       sync.Mutex
	hashed   int
	compared []string
}

func (h *recordingHasher) Hash(p string) (string, error) {
	h.mu.Lock()
	h.hashed++
	h.mu.Unlock()
	return h.inner.Hash(p)
}

func (h *recordingHasher) Matches(hash, p string) bool {
	h.mu.Lock()
	h.compared = append(h.compared, hash)
	h.mu.Unlock()
	return h.inner.Matches(hash, p)
}

func (h *recordingHasher) hashCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hashed
}

type authFixture struct {
	svc      *AuthService
	clock    *memstore.Clock
	users    *memstore.Users
	sessions *memstore.Sessions
	failures *memstore.Failures
	audit    *memstore.Audit
	tx       *memstore.Tx
	hasher   *recordingHasher
	logs     *observer.ObservedLogs
}

var testMeta = ClientMeta{IP: "203.0.113.5", UserAgent: "test-agent/1.0", RequestID: "req-1"}

func testAuthConfig() AuthConfig {
	return AuthConfig{
		IdleTimeout:         30 * time.Minute,
		AbsoluteTimeout:     12 * time.Hour,
		MaxFailuresPerIP:    20,
		MaxFailuresPerEmail: 5,
		FailureWindow:       15 * time.Minute,
	}
}

func newAuthFixture(t *testing.T, cfg AuthConfig) *authFixture {
	t.Helper()
	clock := memstore.NewClock(time.Date(2026, 5, 6, 7, 0, 0, 0, time.UTC))
	core, logs := observer.New(zap.DebugLevel)
	f := &authFixture{
		clock:    clock,
		users:    memstore.NewUsers(clock.Now),
		sessions: memstore.NewSessions(clock.Now),
		failures: memstore.NewFailures(clock.Now),
		audit:    &memstore.Audit{},
		hasher:   &recordingHasher{inner: NewBcryptHasher(bcrypt.MinCost)},
		logs:     logs,
	}
	f.tx = memstore.NewTx(f.users, f.audit)
	auditSvc := NewAuditService(f.audit)
	auditSvc.now = clock.Now
	svc, err := NewAuthService(AuthDeps{
		Users: f.users, Sessions: f.sessions, Failures: f.failures, Tx: f.tx,
		Audit: auditSvc, Hasher: f.hasher, Log: zap.New(core),
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc.now = clock.Now
	f.svc = svc
	return f
}

func (f *authFixture) addUser(t *testing.T, email, password string, role domain.Role) *domain.User {
	t.Helper()
	hash, err := f.hasher.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	u := &domain.User{PublicID: id.New(id.User), Email: email, Name: "Test " + string(role), Role: role, PasswordHash: hash}
	f.users.Add(u)
	return u
}

func (f *authFixture) login(t *testing.T, email, password string) *AuthResult {
	t.Helper()
	res, err := f.svc.Login(context.Background(), LoginInput{Email: email, Password: password}, testMeta)
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}
	return res
}

func actions(logs []domain.AuditLog) []string {
	out := make([]string, 0, len(logs))
	for _, l := range logs {
		out = append(out, l.Action)
	}
	return out
}

const ownerPassword = "owner-password-1"

// --- T-7 setup ---

func TestSetupCreatesOwnerAndSession(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	ctx := context.Background()

	st, err := f.svc.SetupStatus(ctx)
	if err != nil || !st.Required || st.TokenRequired {
		t.Fatalf("status = %+v, %v", st, err)
	}
	res, err := f.svc.Setup(ctx, SetupInput{Email: "  Owner@Example.COM ", Name: " Ops Lead ", Password: ownerPassword}, testMeta)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	u := res.User
	if u.Email != "owner@example.com" || u.Name != "Ops Lead" || u.Role != domain.RoleOwner || u.MustChangePassword || u.DisabledAt != nil {
		t.Fatalf("user = %+v", u)
	}
	if !id.Valid(u.PublicID, id.User) || !f.hasher.inner.Matches(u.PasswordHash, ownerPassword) {
		t.Fatalf("id %q or hash wrong", u.PublicID)
	}
	if res.Token == "" || res.Session.CSRFToken == "" || res.Token == res.Session.CSRFToken || !f.sessions.Has(res.Token) {
		t.Fatalf("session not created: %+v", res.Session)
	}
	if f.sessions.TTL(res.Token) != 30*time.Minute || res.Session.UserID != u.ID || res.Session.SessionVersion != u.SessionVersion {
		t.Fatalf("session = %+v ttl=%v", res.Session, f.sessions.TTL(res.Token))
	}
	logs := f.audit.Logs()
	if len(logs) != 1 {
		t.Fatalf("entries = %v", actions(logs))
	}
	l := logs[0]
	if l.Action != ActionUserSetup || l.Actor != domain.SystemActor("setup") || l.Target != (domain.AuditTarget{Type: "user", ID: u.PublicID}) {
		t.Fatalf("entry = %+v", l)
	}
	assertJSON(t, "before", l.Before, nil)
	assertJSON(t, "after", l.After, map[string]any{"email": "owner@example.com", "name": "Ops Lead", "role": "owner"})
	if f.users.Locks != 1 {
		t.Fatalf("users lock taken %d times", f.users.Locks)
	}

	st, _ = f.svc.SetupStatus(ctx)
	if st.Required {
		t.Fatal("setup still required after the owner exists")
	}
	hashedBefore := f.hasher.hashCount()
	_, err = f.svc.Setup(ctx, SetupInput{Email: "second@example.com", Name: "Second", Password: ownerPassword}, testMeta)
	if !errors.Is(err, domain.ErrSetupCompleted) {
		t.Fatalf("second setup err = %v", err)
	}
	if n, _ := f.users.Count(ctx); n != 1 || len(f.audit.Logs()) != 1 {
		t.Fatalf("second setup wrote: users=%d entries=%d", n, len(f.audit.Logs()))
	}
	// Regression: setup stays public after the first owner, so rejecting it
	// must not cost a bcrypt hash.
	if f.hasher.hashCount() != hashedBefore {
		t.Fatal("setup after completion hashed the password")
	}
	if f.failures.Value(ipFailureKey(testMeta.IP)) != 0 {
		t.Fatal("setup left attempts counted")
	}
}

func TestSetupValidatesInput(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	_, err := f.svc.Setup(context.Background(), SetupInput{Email: "a@b", Name: "  ", Password: "short"}, testMeta)
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeInvalidRequest {
		t.Fatalf("err = %v", err)
	}
	want := []domain.FieldError{{Field: "email", Rule: "email"}, {Field: "name", Rule: "required"}, {Field: "password", Rule: "min"}}
	if got := coded.Details["fields"].([]domain.FieldError); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("fields = %+v", got)
	}
	if n, _ := f.users.Count(context.Background()); n != 0 {
		t.Fatal("invalid setup created a user")
	}
}

func TestSetupToken(t *testing.T) {
	cfg := testAuthConfig()
	cfg.SetupToken = "s3tup-tøken"
	f := newAuthFixture(t, cfg)
	ctx := context.Background()
	if st, _ := f.svc.SetupStatus(ctx); !st.TokenRequired {
		t.Fatal("token not reported as required")
	}
	in := SetupInput{Email: "owner@example.com", Name: "Owner", Password: ownerPassword}
	for i, token := range []string{"", "wrong", "s3tup-tøken "} {
		in.SetupToken = token
		if _, err := f.svc.Setup(ctx, in, testMeta); !errors.Is(err, domain.ErrInvalidSetupToken) {
			t.Fatalf("token %q: err = %v", token, err)
		}
		if got := f.failures.Value(ipFailureKey(testMeta.IP)); got != i+1 {
			t.Fatalf("ip failures = %d, want %d", got, i+1)
		}
	}
	if n, _ := f.users.Count(ctx); n != 0 || len(f.audit.Logs()) != 0 {
		t.Fatal("wrong token created a user or an entry")
	}
	if f.logs.FilterField(zap.String("event", "auth.setup_token_invalid")).Len() != 3 {
		t.Fatalf("security logs = %v", f.logs.All())
	}
	for _, e := range f.logs.All() {
		for _, v := range e.ContextMap() {
			if s, ok := v.(string); ok && strings.Contains(s, "wrong") {
				t.Fatalf("setup token logged: %v", e.ContextMap())
			}
		}
	}

	in.SetupToken = "s3tup-tøken"
	if _, err := f.svc.Setup(ctx, in, testMeta); err != nil {
		t.Fatalf("right token: %v", err)
	}
	if got := f.failures.Value(ipFailureKey(testMeta.IP)); got != 3 {
		t.Fatalf("ip failures after a good setup = %d, want the 3 wrong tokens only", got)
	}
	// Once set up, a wrong token gets setup_completed: the token check runs
	// after the existence check and counts nothing.
	in.SetupToken = "wrong"
	before := f.failures.Value(ipFailureKey(testMeta.IP))
	if _, err := f.svc.Setup(ctx, in, testMeta); !errors.Is(err, domain.ErrSetupCompleted) {
		t.Fatalf("after setup err = %v, want setup_completed", err)
	}
	if f.failures.Value(ipFailureKey(testMeta.IP)) != before {
		t.Fatal("setup_completed counted as a failure")
	}
}

func TestSetupRateLimitedByIP(t *testing.T) {
	cfg := testAuthConfig()
	cfg.SetupToken = "right"
	cfg.MaxFailuresPerIP = 2
	f := newAuthFixture(t, cfg)
	in := SetupInput{Email: "owner@example.com", Name: "Owner", Password: ownerPassword, SetupToken: "wrong"}
	for range 2 {
		_, _ = f.svc.Setup(context.Background(), in, testMeta)
	}
	in.SetupToken = "right"
	_, err := f.svc.Setup(context.Background(), in, testMeta)
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeRateLimited || coded.Details["retry_after_seconds"] != 900 {
		t.Fatalf("err = %v (%+v)", err, coded)
	}
	f.clock.Advance(15 * time.Minute)
	if _, err := f.svc.Setup(context.Background(), in, testMeta); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

// --- T-8 login ---

func TestLoginSuccess(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	u := f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	ipKey, emailKey := ipFailureKey(testMeta.IP), emailFailureKey("ops@example.com")
	f.failures.Inc(ipKey, time.Hour)
	f.failures.Inc(emailKey, time.Hour)

	res := f.login(t, "  OPS@Example.com", ownerPassword)
	if res.User.ID != u.ID || res.User.LastLoginAt == nil || !res.User.LastLoginAt.Equal(f.clock.Now()) {
		t.Fatalf("user = %+v", res.User)
	}
	if stored := f.users.Get(u.PublicID); stored.LastLoginAt == nil {
		t.Fatal("last_login_at not stored")
	}
	if f.failures.Value(emailKey) != 0 || f.failures.Value(ipKey) != 1 {
		t.Fatalf("counters email=%d ip=%d; want email reset, ip untouched", f.failures.Value(emailKey), f.failures.Value(ipKey))
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserLogin {
		t.Fatalf("entries = %v", actions(logs))
	}
	want := domain.Actor{Type: domain.ActorAdmin, ID: u.PublicID, IP: testMeta.IP, UserAgent: testMeta.UserAgent, RequestID: testMeta.RequestID}
	if logs[0].Actor != want || logs[0].Target.ID != u.PublicID {
		t.Fatalf("entry = %+v", logs[0])
	}
	if res.Session.IP != testMeta.IP || res.Session.UserAgent != testMeta.UserAgent {
		t.Fatalf("session origin = %+v", res.Session)
	}
}

func TestLoginReplacesPreviousSession(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	first := f.login(t, "ops@example.com", ownerPassword)
	second, err := f.svc.Login(context.Background(), LoginInput{Email: "ops@example.com", Password: ownerPassword, PreviousToken: first.Token}, testMeta)
	if err != nil {
		t.Fatal(err)
	}
	if f.sessions.Has(first.Token) || !f.sessions.Has(second.Token) {
		t.Fatal("previous session not replaced")
	}
}

func TestLoginMustChangePasswordUserSignsIn(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	u := f.addUser(t, "new@example.com", ownerPassword, domain.RoleViewer)
	u.MustChangePassword = true
	_ = f.users.Update(context.Background(), u)
	if res := f.login(t, "new@example.com", ownerPassword); !res.User.MustChangePassword {
		t.Fatal("flag lost")
	}
}

func TestLoginFailuresAreIdentical(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	active := f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	disabled := f.addUser(t, "gone@example.com", ownerPassword, domain.RoleAdmin)
	at := f.clock.Now()
	disabled.DisabledAt = &at
	_ = f.users.Update(context.Background(), disabled)

	cases := []struct {
		name, email, password, target, reason string
	}{
		{"wrong password", "OPS@example.com", "wrong-password-1", active.PublicID, "invalid_credentials"},
		{"unknown email", "nobody@example.com", ownerPassword, "", "invalid_credentials"},
		{"disabled user", "gone@example.com", ownerPassword, disabled.PublicID, "user_disabled"},
		{"malformed email", "not an email", ownerPassword, "", "invalid_credentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.audit.Reset()
			f.hasher.compared = nil
			ctx := context.Background()
			email := CanonicalEmail(tc.email)
			ipBefore := f.failures.Value(ipFailureKey(testMeta.IP))
			emailBefore := f.failures.Value(emailFailureKey(email))

			_, err := f.svc.Login(ctx, LoginInput{Email: tc.email, Password: tc.password}, testMeta)
			if err != domain.ErrInvalidCredentials {
				t.Fatalf("err = %#v, want the shared ErrInvalidCredentials", err)
			}
			if f.failures.Value(ipFailureKey(testMeta.IP)) != ipBefore+1 || f.failures.Value(emailFailureKey(email)) != emailBefore+1 {
				t.Fatal("counters not incremented")
			}
			if len(f.hasher.compared) != 1 {
				t.Fatalf("bcrypt ran %d times, want 1", len(f.hasher.compared))
			}
			if tc.target == "" && f.hasher.compared[0] != f.svc.dummyHash {
				t.Fatal("unknown email did not compare against the dummy hash")
			}
			logs := f.audit.Logs()
			if len(logs) != 1 || logs[0].Action != ActionUserLoginFailed {
				t.Fatalf("entries = %v", actions(logs))
			}
			l := logs[0]
			wantActor := domain.AnonymousActor(testMeta.IP, testMeta.UserAgent, testMeta.RequestID)
			if l.Actor != wantActor || l.Target != (domain.AuditTarget{Type: "user", ID: tc.target}) {
				t.Fatalf("entry = %+v", l)
			}
			assertJSON(t, "after", l.After, map[string]any{"email": email, "reason": tc.reason})
			if strings.Contains(string(l.After), tc.password) {
				t.Fatal("password audited")
			}
		})
	}
	events := f.logs.FilterField(zap.String("event", "auth.login_failed")).All()
	if len(events) != len(cases) {
		t.Fatalf("security events = %d", len(events))
	}
	for _, e := range events {
		for k, v := range e.ContextMap() {
			s, _ := v.(string)
			if strings.Contains(s, "@") || strings.Contains(s, ownerPassword) {
				t.Fatalf("security log field %s = %q leaks the email or password", k, s)
			}
		}
	}
}

func TestLoginLongEmailIsTruncatedInAudit(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	long := strings.Repeat("é", 200) + "@example.com"
	_, _ = f.svc.Login(context.Background(), LoginInput{Email: long, Password: "x"}, testMeta)
	logs := f.audit.Logs()
	if len(logs) != 1 || !strings.Contains(string(logs[0].After), "éé") {
		t.Fatalf("entries = %+v", logs)
	}
	var email string
	for _, part := range strings.Split(string(logs[0].After), `"`) {
		if strings.HasPrefix(part, "éé") {
			email = part
		}
	}
	if len(email) > 254 || len(email) == 0 {
		t.Fatalf("audited email is %d bytes", len(email))
	}
}

func TestLoginStoreErrorsAreInternal(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	f.users.GetErr = errors.New("db down")
	_, err := f.svc.Login(context.Background(), LoginInput{Email: "ops@example.com", Password: ownerPassword}, testMeta)
	if err == nil || errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("err = %v, want internal", err)
	}
	if f.failures.Value(ipFailureKey(testMeta.IP)) != 0 || f.failures.Value(emailFailureKey("ops@example.com")) != 0 {
		t.Fatal("an attempt that never checked the password stayed counted")
	}
	f.users.GetErr = nil
	f.failures.Err = errors.New("redis down")
	if _, err := f.svc.Login(context.Background(), LoginInput{Email: "ops@example.com", Password: ownerPassword}, testMeta); err == nil {
		t.Fatal("login succeeded without the failure counter (fails open)")
	}
}

// --- T-9 rate limits ---

func TestLoginRateLimitPerEmail(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	ctx := context.Background()
	for i := range 5 {
		if _, err := f.svc.Login(ctx, LoginInput{Email: "ops@example.com", Password: "wrong-password"}, testMeta); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		f.clock.Advance(time.Minute)
	}
	f.audit.Reset()
	f.hasher.compared = nil

	// The window opened with the first failure 5 minutes ago: 10 minutes left.
	_, err := f.svc.Login(ctx, LoginInput{Email: "OPS@example.com", Password: ownerPassword}, testMeta)
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeRateLimited || coded.Details["retry_after_seconds"] != 600 {
		t.Fatalf("err = %v (%+v)", err, coded)
	}
	if len(f.audit.Logs()) != 0 || len(f.hasher.compared) != 0 {
		t.Fatal("rate-limited attempt was audited or checked the password")
	}
	if f.failures.Value(emailFailureKey("ops@example.com")) != 5 {
		t.Fatal("rate-limited attempt was counted")
	}
	if f.logs.FilterField(zap.String("event", "auth.login_rate_limited")).Len() != 1 {
		t.Fatal("no rate-limit security log")
	}

	f.clock.Advance(10 * time.Minute)
	if res := f.login(t, "ops@example.com", ownerPassword); res == nil {
		t.Fatal("login after the window failed")
	}
}

func TestLoginRateLimitPerIP(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	ctx := context.Background()
	for i := range 20 {
		email := "user" + string(rune('a'+i)) + "@example.com"
		if _, err := f.svc.Login(ctx, LoginInput{Email: email, Password: "x"}, testMeta); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	_, err := f.svc.Login(ctx, LoginInput{Email: "ops@example.com", Password: ownerPassword}, testMeta)
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeRateLimited || coded.Details["retry_after_seconds"] != 900 {
		t.Fatalf("err = %v", err)
	}
	other := testMeta
	other.IP = "198.51.100.1"
	if _, err := f.svc.Login(ctx, LoginInput{Email: "ops@example.com", Password: ownerPassword}, other); err != nil {
		t.Fatalf("another IP is blocked too: %v", err)
	}
}

// gatedHasher holds every password check until the test opens the gate, so
// all concurrent sign-ins are in flight at the same time.
type gatedHasher struct {
	PasswordHasher
	arrived *sync.WaitGroup
	gate    chan struct{}
	mu      sync.Mutex
	checked int
}

func (h *gatedHasher) Matches(hash, p string) bool {
	h.mu.Lock()
	h.checked++
	h.mu.Unlock()
	h.arrived.Done()
	<-h.gate
	return h.PasswordHasher.Matches(hash, p)
}

// Regression: the limits used to be read before the password check and
// counted after it, so parallel guesses all passed the check.
func TestLoginLimitsHoldUnderConcurrency(t *testing.T) {
	cases := []struct {
		name     string
		attempts int
		email    func(i int) string
		want     int
	}{
		{"one email", 12, func(int) string { return "ops@example.com" }, 5},
		{"one ip", 30, func(i int) string { return fmt.Sprintf("user%d@example.com", i) }, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthFixture(t, testAuthConfig())
			f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
			var arrived, done sync.WaitGroup
			h := &gatedHasher{PasswordHasher: f.hasher, arrived: &arrived, gate: make(chan struct{})}
			f.svc.hasher = h
			arrived.Add(tc.attempts)
			errs := make([]error, tc.attempts)
			for i := range tc.attempts {
				done.Add(1)
				go func() {
					defer done.Done()
					_, errs[i] = f.svc.Login(context.Background(), LoginInput{Email: tc.email(i), Password: "wrong-password"}, testMeta)
					if !errors.Is(errs[i], domain.ErrInvalidCredentials) {
						arrived.Done()
					}
				}()
			}
			arrived.Wait()
			close(h.gate)
			done.Wait()

			if h.checked != tc.want {
				t.Fatalf("password checked %d times, want %d", h.checked, tc.want)
			}
			limited := 0
			for _, err := range errs {
				var coded *domain.Error
				switch {
				case errors.As(err, &coded) && coded.Code == domain.CodeRateLimited:
					limited++
				case !errors.Is(err, domain.ErrInvalidCredentials):
					t.Fatalf("unexpected err = %v", err)
				}
			}
			if limited != tc.attempts-tc.want {
				t.Fatalf("rate limited %d, want %d", limited, tc.attempts-tc.want)
			}
		})
	}
}

func TestRetryAfterIsLargestExceededWindow(t *testing.T) {
	cfg := testAuthConfig()
	cfg.MaxFailuresPerIP, cfg.MaxFailuresPerEmail = 1, 1
	f := newAuthFixture(t, cfg)
	ctx := context.Background()
	f.failures.Inc(emailFailureKey("ops@example.com"), 100*time.Second)
	f.failures.Inc(ipFailureKey(testMeta.IP), 300*time.Millisecond)
	_, err := f.svc.Login(ctx, LoginInput{Email: "ops@example.com", Password: "x"}, testMeta)
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Details["retry_after_seconds"] != 100 {
		t.Fatalf("err = %+v", coded)
	}
	_ = f.failures.Reset(ctx, emailFailureKey("ops@example.com"))
	_, err = f.svc.Login(ctx, LoginInput{Email: "ops@example.com", Password: "x"}, testMeta)
	if !errors.As(err, &coded) || coded.Details["retry_after_seconds"] != 1 {
		t.Fatalf("sub-second window: %+v", coded)
	}
}

// --- T-10 authenticate ---

func TestAuthenticate(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	u := f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	res := f.login(t, "ops@example.com", ownerPassword)
	ctx := context.Background()

	got, sess, err := f.svc.Authenticate(ctx, res.Token)
	if err != nil || got.ID != u.ID || sess.CSRFToken != res.Session.CSRFToken {
		t.Fatalf("authenticate = %+v, %v", got, err)
	}
	if f.sessions.Touches != 0 {
		t.Fatal("touched within the first minute")
	}
	f.clock.Advance(59 * time.Second)
	if _, _, err := f.svc.Authenticate(ctx, res.Token); err != nil || f.sessions.Touches != 0 {
		t.Fatalf("59s: err=%v touches=%d", err, f.sessions.Touches)
	}
	f.clock.Advance(time.Second)
	if _, sess, err := f.svc.Authenticate(ctx, res.Token); err != nil || f.sessions.Touches != 1 || !sess.LastSeenAt.Equal(f.clock.Now()) {
		t.Fatalf("60s: err=%v touches=%d", err, f.sessions.Touches)
	}
	if ttl := f.sessions.TTL(res.Token); ttl != 30*time.Minute {
		t.Fatalf("touch ttl = %v, want the idle timeout", ttl)
	}
	if _, _, err := f.svc.Authenticate(ctx, "unknown-token"); !errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("unknown token: %v", err)
	}
}

func TestAuthenticateIdleTimeout(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	res := f.login(t, "ops@example.com", ownerPassword)
	ctx := context.Background()

	// A request inside the idle window extends the session.
	f.clock.Advance(29 * time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, res.Token); err != nil {
		t.Fatalf("29m: %v", err)
	}
	f.clock.Advance(29 * time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, res.Token); err != nil {
		t.Fatalf("58m with activity: %v", err)
	}
	// The stored TTL is longer than the idle limit here, so the check in the
	// service (not Redis expiry) must end the session.
	f.sessions.Touches = 0
	_ = f.sessions.Touch(ctx, res.Token, mustSession(t, f, res.Token), time.Hour)
	f.clock.Advance(30 * time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, res.Token); !errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("idle 30m: %v", err)
	}
	if f.sessions.Has(res.Token) {
		t.Fatal("expired session not deleted")
	}
}

func mustSession(t *testing.T, f *authFixture, token string) *domain.Session {
	t.Helper()
	s, err := f.sessions.Get(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAuthenticateAbsoluteTimeout(t *testing.T) {
	cfg := testAuthConfig()
	cfg.AbsoluteTimeout = 2 * time.Hour
	f := newAuthFixture(t, cfg)
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	res := f.login(t, "ops@example.com", ownerPassword)
	ctx := context.Background()

	for elapsed := time.Duration(0); elapsed < 110*time.Minute; elapsed += 10 * time.Minute {
		f.clock.Advance(10 * time.Minute)
		if _, _, err := f.svc.Authenticate(ctx, res.Token); err != nil {
			t.Fatalf("at %v: %v", elapsed+10*time.Minute, err)
		}
	}
	// 110 minutes in: the touch TTL is capped by the 10 minutes left.
	if ttl := f.sessions.TTL(res.Token); ttl != 10*time.Minute {
		t.Fatalf("ttl = %v, want min(idle, absolute - age) = 10m", ttl)
	}
	_ = f.sessions.Touch(ctx, res.Token, mustSession(t, f, res.Token), time.Hour)
	f.clock.Advance(10 * time.Minute)
	if _, _, err := f.svc.Authenticate(ctx, res.Token); !errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("past absolute: %v", err)
	}
	if f.sessions.Has(res.Token) {
		t.Fatal("expired session not deleted")
	}
}

func TestAuthenticateRevocation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f *authFixture, u *domain.User)
	}{
		{"version bumped", func(f *authFixture, u *domain.User) {
			u.SessionVersion++
			_ = f.users.Update(context.Background(), u)
		}},
		{"user disabled", func(f *authFixture, u *domain.User) {
			at := f.clock.Now()
			u.DisabledAt = &at
			_ = f.users.Update(context.Background(), u)
		}},
		{"user deleted", func(f *authFixture, u *domain.User) {
			f.users.Remove(u.PublicID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthFixture(t, testAuthConfig())
			u := f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
			res := f.login(t, "ops@example.com", ownerPassword)
			u = f.users.Get(u.PublicID)
			tc.mutate(f, u)
			if _, _, err := f.svc.Authenticate(context.Background(), res.Token); !errors.Is(err, domain.ErrSessionExpired) {
				t.Fatalf("err = %v", err)
			}
			if f.sessions.Has(res.Token) {
				t.Fatal("revoked session not deleted")
			}
		})
	}
}

func TestAuthenticateRoleChangeAppliesImmediately(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	u := f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	res := f.login(t, "ops@example.com", ownerPassword)
	u = f.users.Get(u.PublicID)
	u.Role = domain.RoleViewer
	_ = f.users.Update(context.Background(), u)
	got, _, err := f.svc.Authenticate(context.Background(), res.Token)
	if err != nil || got.Role != domain.RoleViewer {
		t.Fatalf("role = %v, %v", got, err)
	}
}

func TestAuthenticateStoreErrorIsInternal(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.sessions.Err = errors.New("redis down")
	if _, _, err := f.svc.Authenticate(context.Background(), "x"); err == nil || errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("err = %v, want internal", err)
	}
}

// --- T-11 logout ---

func TestLogout(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	u := f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	res := f.login(t, "ops@example.com", ownerPassword)
	f.audit.Reset()
	ctx := context.Background()

	if err := f.svc.Logout(ctx, res.Token, testMeta); err != nil {
		t.Fatal(err)
	}
	if f.sessions.Has(res.Token) {
		t.Fatal("session survived logout")
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserLogout || logs[0].Actor.ID != u.PublicID || logs[0].Actor.Type != domain.ActorAdmin {
		t.Fatalf("entries = %+v", logs)
	}
	for _, token := range []string{"", res.Token, "garbage"} {
		if err := f.svc.Logout(ctx, token, testMeta); err != nil {
			t.Fatalf("logout(%q) = %v", token, err)
		}
	}
	if len(f.audit.Logs()) != 1 {
		t.Fatal("logout without a session was audited")
	}
}

// --- T-12 change password ---

func TestChangePassword(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	u := f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	u.MustChangePassword = true
	_ = f.users.Update(context.Background(), u)
	current := f.login(t, "ops@example.com", ownerPassword)
	other := f.login(t, "ops@example.com", ownerPassword)
	f.audit.Reset()
	ctx := context.Background()
	in := ChangePasswordInput{User: current.User, Token: current.Token, CurrentPassword: ownerPassword, NewPassword: "brand-new-pass-1"}

	res, err := f.svc.ChangePassword(ctx, in, testMeta)
	if err != nil {
		t.Fatal(err)
	}
	stored := f.users.Get(u.PublicID)
	if !f.hasher.inner.Matches(stored.PasswordHash, "brand-new-pass-1") || stored.MustChangePassword || stored.SessionVersion != 2 {
		t.Fatalf("stored = %+v", stored)
	}
	if f.sessions.Has(current.Token) || !f.sessions.Has(res.Token) || res.Session.SessionVersion != 2 {
		t.Fatal("session not replaced with the new version")
	}
	if res.Session.CSRFToken == current.Session.CSRFToken {
		t.Fatal("csrf token not rotated")
	}
	if _, _, err := f.svc.Authenticate(ctx, other.Token); !errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("other session still valid: %v", err)
	}
	if _, _, err := f.svc.Authenticate(ctx, res.Token); err != nil {
		t.Fatalf("new session invalid: %v", err)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserChangePassword || logs[0].Actor.ID != u.PublicID {
		t.Fatalf("entries = %+v", logs)
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"must_change_password": true})
	assertJSON(t, "after", logs[0].After, map[string]any{"must_change_password": false})
}

func TestChangePasswordRejections(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	res := f.login(t, "ops@example.com", ownerPassword)
	f.audit.Reset()
	ctx := context.Background()
	base := ChangePasswordInput{User: res.User, Token: res.Token, CurrentPassword: ownerPassword}

	wrong := base
	wrong.CurrentPassword, wrong.NewPassword = "not-the-password", "brand-new-pass-1"
	if _, err := f.svc.ChangePassword(ctx, wrong, testMeta); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("wrong current: %v", err)
	}
	same := base
	same.NewPassword = ownerPassword
	var coded *domain.Error
	if _, err := f.svc.ChangePassword(ctx, same, testMeta); !errors.As(err, &coded) || coded.Code != domain.CodeValidationFailed {
		t.Fatalf("same password: %v", err)
	}
	short := base
	short.NewPassword = "short"
	if _, err := f.svc.ChangePassword(ctx, short, testMeta); !errors.As(err, &coded) || coded.Code != domain.CodeInvalidRequest ||
		coded.Details["fields"].([]domain.FieldError)[0] != (domain.FieldError{Field: "new_password", Rule: "min"}) {
		t.Fatalf("short: %+v", coded)
	}
	if !f.sessions.Has(res.Token) || len(f.audit.Logs()) != 0 || f.users.Get(res.User.PublicID).SessionVersion != 1 {
		t.Fatal("a rejected change wrote something")
	}
}

func TestChangePasswordAuditFailureRollsBack(t *testing.T) {
	f := newAuthFixture(t, testAuthConfig())
	f.addUser(t, "ops@example.com", ownerPassword, domain.RoleAdmin)
	res := f.login(t, "ops@example.com", ownerPassword)
	f.audit.Err = errAuditDown
	_, err := f.svc.ChangePassword(context.Background(), ChangePasswordInput{
		User: res.User, Token: res.Token, CurrentPassword: ownerPassword, NewPassword: "brand-new-pass-1",
	}, testMeta)
	if !errors.Is(err, errAuditDown) {
		t.Fatalf("err = %v", err)
	}
	stored := f.users.Get(res.User.PublicID)
	if !f.hasher.inner.Matches(stored.PasswordHash, ownerPassword) || stored.SessionVersion != 1 || !f.sessions.Has(res.Token) {
		t.Fatal("change not rolled back")
	}
}
