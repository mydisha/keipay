package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/service"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

func freshPool(t *testing.T, db *pgtest.Database) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.NewPool(context.Background(), db.DSN)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newUser(email string, role domain.Role) *domain.User {
	return &domain.User{PublicID: id.New(id.User), Email: email, Name: email, Role: role, PasswordHash: "hash", SessionVersion: 1}
}

// T-23 / AC-1
func TestIntegrationUsersMigrationBackfill(t *testing.T) {
	db := pgtest.New(t, 5)
	pool := freshPool(t, db)
	ctx := context.Background()
	created := time.Date(2025, 3, 4, 5, 6, 7, 123456000, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO admin_users (username, password_hash, must_change_password, created_at, updated_at)
		VALUES ('admin', 'hash-one', true, $1, $1), ('Ops@Example.com', 'hash-two', false, $1, $1)`, created); err != nil {
		t.Fatalf("seed admin_users: %v", err)
	}
	adminUsers := func() string {
		var dump string
		if err := pool.QueryRow(ctx, `SELECT string_agg(username || ':' || password_hash || ':' || must_change_password, ',' ORDER BY id)
			FROM admin_users`).Scan(&dump); err != nil {
			t.Fatalf("dump admin_users: %v", err)
		}
		return dump
	}
	before := adminUsers()

	for round := 1; round <= 2; round++ {
		if err := db.Migrate.Migrate(6); err != nil {
			t.Fatalf("round %d up: %v", round, err)
		}
		repo := postgres.NewUserRepo(pool)
		for _, tc := range []struct {
			email, name, hash string
			must              bool
		}{
			{"admin@keipay.invalid", "admin", "hash-one", true},
			{"ops@example.com", "Ops@Example.com", "hash-two", false},
		} {
			u, err := repo.GetByEmail(ctx, tc.email)
			if err != nil {
				t.Fatalf("round %d: %s: %v", round, tc.email, err)
			}
			if !id.Valid(u.PublicID, id.User) {
				t.Fatalf("backfilled id %q is not a valid usr_ id", u.PublicID)
			}
			if canonical, _ := id.ParseWithPrefix(u.PublicID, id.User); canonical != u.PublicID {
				t.Fatalf("backfilled id %q is not canonical", u.PublicID)
			}
			if u.Name != tc.name || u.Role != domain.RoleOwner || u.PasswordHash != tc.hash || u.MustChangePassword != tc.must ||
				u.DisabledAt != nil || u.SessionVersion != 1 || !u.CreatedAt.Equal(created) {
				t.Fatalf("round %d: user = %+v", round, u)
			}
		}
		if n, _ := repo.Count(ctx); n != 2 {
			t.Fatalf("round %d: %d users", round, n)
		}
		if adminUsers() != before {
			t.Fatal("admin_users changed")
		}

		if err := db.Migrate.Steps(-1); err != nil {
			t.Fatalf("round %d down: %v", round, err)
		}
		var exists bool
		_ = pool.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&exists)
		if exists || adminUsers() != before {
			t.Fatalf("round %d down: users exists=%v, admin_users intact=%v", round, exists, adminUsers() == before)
		}
	}
}

// T-24
func TestIntegrationUserRepo(t *testing.T) {
	pool := freshPool(t, pgtest.New(t, 0))
	repo := postgres.NewUserRepo(pool)
	ctx := context.Background()

	owner := newUser("owner@example.com", domain.RoleOwner)
	if err := repo.Create(ctx, owner); err != nil || owner.ID == 0 || owner.CreatedAt.IsZero() {
		t.Fatalf("create = %+v, %v", owner, err)
	}
	if err := repo.Create(ctx, newUser("owner@example.com", domain.RoleAdmin)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate email: %v", err)
	}
	second := newUser("second@example.com", domain.RoleOwner)
	_ = repo.Create(ctx, second)
	second.Email = "owner@example.com"
	if err := repo.Update(ctx, second); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("update to a taken email: %v", err)
	}
	if err := repo.Create(ctx, &domain.User{PublicID: id.New(id.User), Email: "x@example.com", Name: "x", Role: "root", PasswordHash: "h", SessionVersion: 1}); err == nil {
		t.Fatal("role CHECK not enforced")
	}

	if n, _ := repo.CountActiveOwners(ctx); n != 2 {
		t.Fatalf("active owners = %d", n)
	}
	second, _ = repo.GetByPublicID(ctx, second.PublicID)
	at := time.Now().UTC().Truncate(time.Microsecond)
	second.DisabledAt = &at
	if err := repo.Update(ctx, second); err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.CountActiveOwners(ctx); n != 1 {
		t.Fatalf("active owners after disable = %d", n)
	}
	if got, _ := repo.GetByID(ctx, second.ID); got.DisabledAt == nil || !got.DisabledAt.Equal(at) {
		t.Fatalf("disabled_at = %v", got.DisabledAt)
	}
	if err := repo.SetLastLogin(ctx, owner.ID, at); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByEmail(ctx, "owner@example.com"); got.LastLoginAt == nil || !got.LastLoginAt.Equal(at) || !got.UpdatedAt.Equal(owner.UpdatedAt) {
		t.Fatalf("last login = %+v", got)
	}
	if oldest, err := repo.OldestOwner(ctx); err != nil || oldest.ID != owner.ID {
		t.Fatalf("oldest owner = %+v, %v", oldest, err)
	}
	for _, err := range []error{
		func() error { _, err := repo.GetByID(ctx, 999999); return err }(),
		func() error { _, err := repo.GetByPublicID(ctx, id.New(id.User)); return err }(),
		func() error { _, err := repo.GetByEmail(ctx, "nobody@example.com"); return err }(),
		repo.SetLastLogin(ctx, 999999, at),
		repo.Update(ctx, &domain.User{ID: 999999, Email: "ghost@example.com", Role: domain.RoleAdmin}),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("missing row: %v", err)
		}
	}
	if err := repo.LockUsers(ctx); err == nil {
		t.Fatal("LockUsers outside a transaction succeeded")
	}
	tx := postgres.NewTxManager(pool)
	err := tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := repo.LockUsers(ctx); err != nil {
			return err
		}
		_, err := repo.GetByPublicIDForUpdate(ctx, owner.PublicID)
		return err
	})
	if err != nil {
		t.Fatalf("lock in tx: %v", err)
	}
}

// T-24: keyset pagination over rows with the same created_at.
func TestIntegrationUserListKeyset(t *testing.T) {
	pool := freshPool(t, pgtest.New(t, 0))
	repo := postgres.NewUserRepo(pool)
	ctx := context.Background()
	same := time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)
	var want []string
	for i := range 5 {
		u := newUser(fmt.Sprintf("u%d@example.com", i), domain.RoleViewer)
		if _, err := pool.Exec(ctx, `INSERT INTO users (public_id, email, name, role, password_hash, created_at, updated_at)
			VALUES ($1,$2,$3,'viewer','h',$4,$4)`, u.PublicID, u.Email, u.Name, same); err != nil {
			t.Fatal(err)
		}
		want = append(want, u.PublicID)
	}
	slices.Sort(want)
	slices.Reverse(want)

	var got []string
	var sizes []int
	page := domain.Page{Limit: 2}
	for i := 0; ; i++ {
		if i > 5 {
			t.Fatal("pagination did not terminate")
		}
		res, err := repo.List(ctx, page)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(res.Items))
		for _, u := range res.Items {
			got = append(got, u.PublicID)
		}
		if res.NextCursor == "" {
			break
		}
		c, err := domain.DecodeCursor(res.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		page.After = &c
	}
	if !slices.Equal(sizes, []int{2, 2, 1}) || !slices.Equal(got, want) {
		t.Fatalf("sizes = %v, order = %v, want %v", sizes, got, want)
	}
}

type userServices struct {
	pool  *pgxpool.Pool
	repo  *postgres.UserRepo
	users *service.UserService
	auth  *service.AuthService
	audit *service.AuditService
}

func newUserServices(t *testing.T) *userServices {
	t.Helper()
	pool := freshPool(t, pgtest.New(t, 0))
	repo := postgres.NewUserRepo(pool)
	tx := postgres.NewTxManager(pool)
	audit := service.NewAuditService(postgres.NewAuditRepo(pool))
	hasher := service.NewBcryptHasher(bcrypt.MinCost)
	auth, err := service.NewAuthService(service.AuthDeps{
		Users: repo, Sessions: memstore.NewSessions(time.Now), Failures: memstore.NewFailures(time.Now),
		Tx: tx, Audit: audit, Hasher: hasher,
	}, service.AuthConfig{
		IdleTimeout: time.Hour, AbsoluteTimeout: time.Hour, MaxFailuresPerIP: 100, MaxFailuresPerEmail: 100, FailureWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &userServices{pool: pool, repo: repo, users: service.NewUserService(repo, tx, audit, hasher), auth: auth, audit: audit}
}

func (s *userServices) entries(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action=$1`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// T-25 / AC-3: concurrent first-run setups create exactly one owner.
func TestIntegrationConcurrentSetup(t *testing.T) {
	s := newUserServices(t)
	ctx := context.Background()
	const n = 8
	for round := range 5 {
		if _, err := s.pool.Exec(ctx, `DELETE FROM users; DELETE FROM audit_logs`); err != nil {
			t.Fatal(err)
		}
		errs := make([]error, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, errs[i] = s.auth.Setup(ctx, service.SetupInput{
					Email: fmt.Sprintf("owner%d-%d@example.com", round, i), Name: "Owner", Password: "setup-password-1",
				}, service.ClientMeta{IP: "203.0.113.5"})
			}()
		}
		close(start)
		wg.Wait()

		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, domain.ErrSetupCompleted):
				t.Fatalf("round %d: unexpected error: %v", round, err)
			}
		}
		if count, _ := s.repo.Count(ctx); ok != 1 || count != 1 || s.entries(t, "user.setup") != 1 {
			t.Fatalf("round %d: successes = %d, users = %d, entries = %d", round, ok, count, s.entries(t, "user.setup"))
		}
	}
}

// T-25 / AC-18: two owners disabling or demoting each other at the same time
// never leave zero active owners.
func TestIntegrationConcurrentLastOwner(t *testing.T) {
	s := newUserServices(t)
	ctx := context.Background()
	for round := range 10 {
		if _, err := s.pool.Exec(ctx, `UPDATE users SET disabled_at = now() WHERE disabled_at IS NULL`); err != nil {
			t.Fatal(err)
		}
		a, b := newUser(fmt.Sprintf("a%d@example.com", round), domain.RoleOwner), newUser(fmt.Sprintf("b%d@example.com", round), domain.RoleOwner)
		_ = s.repo.Create(ctx, a)
		_ = s.repo.Create(ctx, b)
		ctxA := domain.WithActor(ctx, domain.UserActor(a.PublicID, "", "", ""))
		ctxB := domain.WithActor(ctx, domain.UserActor(b.PublicID, "", "", ""))

		var errA, errB error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		if round%2 == 0 {
			go func() { defer wg.Done(); <-start; _, errA = s.users.Disable(ctxA, b.PublicID, a.PublicID) }()
			go func() { defer wg.Done(); <-start; _, errB = s.users.Disable(ctxB, a.PublicID, b.PublicID) }()
		} else {
			viewer := domain.RoleViewer
			go func() {
				defer wg.Done()
				<-start
				_, errA = s.users.Update(ctxA, b.PublicID, service.UpdateUserInput{Role: &viewer})
			}()
			go func() {
				defer wg.Done()
				<-start
				_, errB = s.users.Update(ctxB, a.PublicID, service.UpdateUserInput{Role: &viewer})
			}()
		}
		close(start)
		wg.Wait()

		if (errA == nil) == (errB == nil) {
			t.Fatalf("round %d: errA=%v errB=%v, want exactly one success", round, errA, errB)
		}
		failed := errA
		if failed == nil {
			failed = errB
		}
		if !errors.Is(failed, domain.ErrLastOwner) {
			t.Fatalf("round %d: %v, want last_owner", round, failed)
		}
		if n, _ := s.repo.CountActiveOwners(ctx); n != 1 {
			t.Fatalf("round %d: active owners = %d", round, n)
		}
	}
}

// T-26 / AC-19: a failing audit insert rolls back the user change.
func TestIntegrationUserUpdateRollsBackWithAudit(t *testing.T) {
	s := newUserServices(t)
	ctx := context.Background()
	owner := newUser("owner@example.com", domain.RoleOwner)
	target := newUser("ops@example.com", domain.RoleAdmin)
	_ = s.repo.Create(ctx, owner)
	_ = s.repo.Create(ctx, target)
	actor := domain.WithActor(ctx, domain.UserActor(owner.PublicID, "203.0.113.5", "it", "req"))

	failing := service.NewUserService(s.repo, postgres.NewTxManager(s.pool), failAfter{s.audit}, service.NewBcryptHasher(bcrypt.MinCost))
	name := "Changed"
	if _, err := failing.Update(actor, target.PublicID, service.UpdateUserInput{Name: &name}); !errors.Is(err, errAfterAudit) {
		t.Fatalf("err = %v", err)
	}
	if _, err := failing.Disable(actor, target.PublicID, owner.PublicID); !errors.Is(err, errAfterAudit) {
		t.Fatalf("err = %v", err)
	}
	stored, _ := s.repo.GetByPublicID(ctx, target.PublicID)
	if stored.Name != "ops@example.com" || stored.DisabledAt != nil || stored.SessionVersion != 1 {
		t.Fatalf("change survived the rollback: %+v", stored)
	}
	if s.entries(t, "user.update")+s.entries(t, "user.disable") != 0 {
		t.Fatal("audit entry survived the rollback")
	}

	if _, err := s.users.Update(actor, target.PublicID, service.UpdateUserInput{Name: &name}); err != nil {
		t.Fatal(err)
	}
	var actorType, actorID, before, after string
	err := s.pool.QueryRow(ctx, `SELECT actor_type, actor_id, before::text, after::text FROM audit_logs WHERE action='user.update'`).
		Scan(&actorType, &actorID, &before, &after)
	if err != nil || actorType != "admin" || actorID != owner.PublicID {
		t.Fatalf("entry: %s %s %v", actorType, actorID, err)
	}
	jsonEqual(t, "before", []byte(before), map[string]any{"name": "ops@example.com"})
	jsonEqual(t, "after", []byte(after), map[string]any{"name": "Changed"})
}

// Sign-in against Postgres: login_failed for an unknown email is stored with
// the anonymous actor and an empty target id.
func TestIntegrationLoginAudit(t *testing.T) {
	s := newUserServices(t)
	ctx := context.Background()
	meta := service.ClientMeta{IP: "203.0.113.5", UserAgent: "it", RequestID: "req-it"}
	if _, err := s.auth.Setup(ctx, service.SetupInput{Email: "owner@example.com", Name: "Owner", Password: "setup-password-1"}, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := s.auth.Login(ctx, service.LoginInput{Email: "ghost@example.com", Password: "x"}, meta); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
	res, err := s.auth.Login(ctx, service.LoginInput{Email: "OWNER@example.com", Password: "setup-password-1"}, meta)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := s.repo.GetByID(ctx, res.User.ID)
	if stored.LastLoginAt == nil || !stored.LastLoginAt.Equal(*res.User.LastLoginAt) {
		t.Fatalf("last_login_at = %v, returned %v", stored.LastLoginAt, res.User.LastLoginAt)
	}
	var actorType, actorID, targetID, ip, after string
	err = s.pool.QueryRow(ctx, `SELECT actor_type, actor_id, target_id, ip, after::text FROM audit_logs WHERE action='user.login_failed'`).
		Scan(&actorType, &actorID, &targetID, &ip, &after)
	if err != nil || actorType != "anonymous" || actorID != "anonymous" || targetID != "" || ip != "203.0.113.5" {
		t.Fatalf("entry: %s %s %q %s %v", actorType, actorID, targetID, ip, err)
	}
	jsonEqual(t, "after", []byte(after), map[string]any{"email": "ghost@example.com", "reason": "invalid_credentials"})
}
