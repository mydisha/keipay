package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/testutil/memstore"
)

type userFixture struct {
	svc    *UserService
	clock  *memstore.Clock
	users  *memstore.Users
	audit  *memstore.Audit
	hasher PasswordHasher
	owner  *domain.User
	ctx    context.Context
}

const fixedTemp = "TempPassword23456789"

func newUserFixture(t *testing.T) *userFixture {
	t.Helper()
	clock := memstore.NewClock(time.Date(2026, 5, 6, 7, 0, 0, 0, time.UTC))
	f := &userFixture{
		clock:  clock,
		users:  memstore.NewUsers(clock.Now),
		audit:  &memstore.Audit{},
		hasher: NewBcryptHasher(bcrypt.MinCost),
	}
	auditSvc := NewAuditService(f.audit)
	auditSvc.now = clock.Now
	f.svc = NewUserService(f.users, memstore.NewTx(f.users, f.audit), auditSvc, f.hasher)
	f.svc.now = clock.Now
	f.svc.newPassword = func() (string, error) { return fixedTemp, nil }
	f.owner = f.add(t, "owner@example.com", domain.RoleOwner)
	f.ctx = domain.WithActor(context.Background(), domain.UserActor(f.owner.PublicID, "203.0.113.5", "ua", "req-1"))
	return f
}

func (f *userFixture) add(t *testing.T, email string, role domain.Role) *domain.User {
	t.Helper()
	hash, err := f.hasher.Hash("initial-password")
	if err != nil {
		t.Fatal(err)
	}
	u := &domain.User{PublicID: id.New(id.User), Email: email, Name: "User " + email, Role: role, PasswordHash: hash}
	f.users.Add(u)
	f.clock.Advance(time.Second)
	return u
}

func (f *userFixture) disable(u *domain.User) {
	stored := f.users.Get(u.PublicID)
	at := f.clock.Now()
	stored.DisabledAt = &at
	_ = f.users.Update(context.Background(), stored)
}

func ptr[T any](v T) *T { return &v }

func requireCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != code {
		t.Fatalf("err = %v, want %s", err, code)
	}
}

func requireFields(t *testing.T, err error, want ...domain.FieldError) {
	t.Helper()
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeInvalidRequest {
		t.Fatalf("err = %v, want invalid_request", err)
	}
	got := coded.Details["fields"].([]domain.FieldError)
	if len(got) != len(want) {
		t.Fatalf("fields = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fields = %+v, want %+v", got, want)
		}
	}
}

// assertNoSecrets fails when any audit snapshot holds a password or hash.
func (f *userFixture) assertNoSecrets(t *testing.T, secrets ...string) {
	t.Helper()
	for _, l := range f.audit.Logs() {
		for _, raw := range []string{string(l.Before), string(l.After)} {
			if strings.Contains(raw, "$2a$") || strings.Contains(raw, "password_hash") {
				t.Fatalf("%s snapshot holds a hash: %s", l.Action, raw)
			}
			for _, s := range secrets {
				if s != "" && strings.Contains(raw, s) {
					t.Fatalf("%s snapshot holds a secret: %s", l.Action, raw)
				}
			}
		}
	}
}

func TestCreateUser(t *testing.T) {
	f := newUserFixture(t)
	u, temp, err := f.svc.Create(f.ctx, CreateUserInput{Email: " Viewer@Example.COM ", Name: " Dewi Ü ", Role: domain.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	if temp != fixedTemp || !f.hasher.Matches(u.PasswordHash, fixedTemp) {
		t.Fatal("temporary password not returned or not hashed")
	}
	if u.Email != "viewer@example.com" || u.Name != "Dewi Ü" || u.Role != domain.RoleViewer || !u.MustChangePassword || u.DisabledAt != nil {
		t.Fatalf("user = %+v", u)
	}
	if !id.Valid(u.PublicID, id.User) {
		t.Fatalf("public id %q", u.PublicID)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserCreate || logs[0].Actor.ID != f.owner.PublicID || logs[0].Target.ID != u.PublicID {
		t.Fatalf("entries = %+v", logs)
	}
	assertJSON(t, "before", logs[0].Before, nil)
	assertJSON(t, "after", logs[0].After, map[string]any{
		"email": "viewer@example.com", "name": "Dewi Ü", "role": "viewer", "must_change_password": true,
	})
	f.assertNoSecrets(t, fixedTemp)
}

func TestCreateUserRejections(t *testing.T) {
	f := newUserFixture(t)
	f.add(t, "üser@example.com", domain.RoleAdmin)
	f.audit.Reset()

	for _, email := range []string{"OWNER@example.com", "ÜSER@EXAMPLE.COM"} {
		_, _, err := f.svc.Create(f.ctx, CreateUserInput{Email: email, Name: "Dup", Role: domain.RoleAdmin})
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("%s: err = %v, want conflict", email, err)
		}
	}
	_, _, err := f.svc.Create(f.ctx, CreateUserInput{Email: "bad", Name: "", Role: "boss"})
	requireFields(t, err,
		domain.FieldError{Field: "email", Rule: "email"},
		domain.FieldError{Field: "name", Rule: "required"},
		domain.FieldError{Field: "role", Rule: "oneof"})
	_, _, err = f.svc.Create(f.ctx, CreateUserInput{Email: "x@example.com", Name: "X"})
	requireFields(t, err, domain.FieldError{Field: "role", Rule: "required"})
	if len(f.audit.Logs()) != 0 {
		t.Fatal("rejected creates were audited")
	}
}

func TestUpdateUser(t *testing.T) {
	f := newUserFixture(t)
	u := f.add(t, "ops@example.com", domain.RoleAdmin)
	f.audit.Reset()

	got, err := f.svc.Update(f.ctx, u.PublicID, UpdateUserInput{Name: ptr("Ops Lead"), Role: ptr(domain.RoleViewer), Email: ptr("OPS@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Ops Lead" || got.Role != domain.RoleViewer || got.Email != "ops@example.com" {
		t.Fatalf("user = %+v", got)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserUpdate {
		t.Fatalf("entries = %v", actions(logs))
	}
	// Email was sent but did not change, so it is not in the snapshots.
	assertJSON(t, "before", logs[0].Before, map[string]any{"name": "User ops@example.com", "role": "admin"})
	assertJSON(t, "after", logs[0].After, map[string]any{"name": "Ops Lead", "role": "viewer"})

	f.audit.Reset()
	unchanged, err := f.svc.Update(f.ctx, u.PublicID, UpdateUserInput{Name: ptr("  Ops Lead "), Role: ptr(domain.RoleViewer)})
	if err != nil || unchanged.UpdatedAt != got.UpdatedAt {
		t.Fatalf("no-op update = %+v, %v", unchanged, err)
	}
	if len(f.audit.Logs()) != 0 {
		t.Fatal("no-op update was audited")
	}
}

func TestUpdateUserRejections(t *testing.T) {
	f := newUserFixture(t)
	u := f.add(t, "ops@example.com", domain.RoleAdmin)
	f.audit.Reset()

	_, err := f.svc.Update(f.ctx, u.PublicID, UpdateUserInput{})
	requireFields(t, err, domain.FieldError{Field: "body", Rule: "required"})
	_, err = f.svc.Update(f.ctx, u.PublicID, UpdateUserInput{Email: ptr("a@b"), Name: ptr(strings.Repeat("x", 101)), Role: ptr(domain.Role("root"))})
	requireFields(t, err,
		domain.FieldError{Field: "email", Rule: "email"},
		domain.FieldError{Field: "name", Rule: "max"},
		domain.FieldError{Field: "role", Rule: "oneof"})
	if _, err := f.svc.Update(f.ctx, u.PublicID, UpdateUserInput{Email: ptr("Owner@Example.com")}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate email: %v", err)
	}
	if _, err := f.svc.Update(f.ctx, id.New(id.User), UpdateUserInput{Name: ptr("x")}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	if stored := f.users.Get(u.PublicID); stored.Email != "ops@example.com" || len(f.audit.Logs()) != 0 {
		t.Fatal("a rejected update wrote something")
	}
}

func TestLastOwnerRules(t *testing.T) {
	t.Run("demote the only owner", func(t *testing.T) {
		f := newUserFixture(t)
		_, err := f.svc.Update(f.ctx, f.owner.PublicID, UpdateUserInput{Role: ptr(domain.RoleAdmin)})
		requireCode(t, err, domain.CodeLastOwner)
		if f.users.Get(f.owner.PublicID).Role != domain.RoleOwner {
			t.Fatal("role changed")
		}
	})
	t.Run("demote self with another owner", func(t *testing.T) {
		f := newUserFixture(t)
		f.add(t, "second@example.com", domain.RoleOwner)
		if _, err := f.svc.Update(f.ctx, f.owner.PublicID, UpdateUserInput{Role: ptr(domain.RoleAdmin)}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("demote while the other owner is disabled", func(t *testing.T) {
		f := newUserFixture(t)
		f.disable(f.add(t, "second@example.com", domain.RoleOwner))
		_, err := f.svc.Update(f.ctx, f.owner.PublicID, UpdateUserInput{Role: ptr(domain.RoleViewer)})
		requireCode(t, err, domain.CodeLastOwner)
	})
	t.Run("demote a disabled owner", func(t *testing.T) {
		f := newUserFixture(t)
		other := f.add(t, "second@example.com", domain.RoleOwner)
		f.disable(other)
		if _, err := f.svc.Update(f.ctx, other.PublicID, UpdateUserInput{Role: ptr(domain.RoleViewer)}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("disable the last active owner", func(t *testing.T) {
		f := newUserFixture(t)
		other := f.add(t, "second@example.com", domain.RoleOwner)
		// The acting owner was disabled by someone else meanwhile, so other
		// is now the only active owner.
		f.disable(f.owner)
		_, err := f.svc.Disable(f.ctx, other.PublicID, f.owner.PublicID)
		requireCode(t, err, domain.CodeLastOwner)
		if f.users.Get(other.PublicID).DisabledAt != nil {
			t.Fatal("last owner disabled")
		}
	})
}

func TestDisableEnable(t *testing.T) {
	f := newUserFixture(t)
	u := f.add(t, "ops@example.com", domain.RoleAdmin)
	f.audit.Reset()

	_, err := f.svc.Disable(f.ctx, f.owner.PublicID, f.owner.PublicID)
	requireCode(t, err, domain.CodeCannotModifySelf)

	got, err := f.svc.Disable(f.ctx, u.PublicID, f.owner.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt == nil || !got.DisabledAt.Equal(f.clock.Now()) || got.SessionVersion != 2 || got.Status() != domain.UserDisabled {
		t.Fatalf("user = %+v", got)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserDisable {
		t.Fatalf("entries = %v", actions(logs))
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"disabled_at": nil})
	assertJSON(t, "after", logs[0].After, map[string]any{"disabled_at": f.clock.Now().Format(time.RFC3339)})

	again, err := f.svc.Disable(f.ctx, u.PublicID, f.owner.PublicID)
	if err != nil || again.SessionVersion != 2 || len(f.audit.Logs()) != 1 {
		t.Fatalf("second disable: %+v, %v, entries=%d", again, err, len(f.audit.Logs()))
	}

	f.audit.Reset()
	enabled, err := f.svc.Enable(f.ctx, u.PublicID)
	if err != nil || enabled.DisabledAt != nil || enabled.SessionVersion != 2 {
		t.Fatalf("enable = %+v, %v", enabled, err)
	}
	logs = f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserEnable {
		t.Fatalf("entries = %v", actions(logs))
	}
	assertJSON(t, "after", logs[0].After, map[string]any{"disabled_at": nil})
	if _, err := f.svc.Enable(f.ctx, u.PublicID); err != nil || len(f.audit.Logs()) != 1 {
		t.Fatalf("enabling an active user: %v, entries=%d", err, len(f.audit.Logs()))
	}
	if _, err := f.svc.Enable(f.ctx, id.New(id.User)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
}

func TestResetPassword(t *testing.T) {
	f := newUserFixture(t)
	u := f.add(t, "ops@example.com", domain.RoleAdmin)
	f.disable(u)
	f.audit.Reset()

	_, _, err := f.svc.ResetPassword(f.ctx, f.owner.PublicID, f.owner.PublicID)
	requireCode(t, err, domain.CodeCannotModifySelf)

	got, temp, err := f.svc.ResetPassword(f.ctx, u.PublicID, f.owner.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if temp != fixedTemp || !f.hasher.Matches(got.PasswordHash, fixedTemp) || !got.MustChangePassword || got.SessionVersion != 2 {
		t.Fatalf("user = %+v", got)
	}
	if got.DisabledAt == nil {
		t.Fatal("reset enabled a disabled user")
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserResetPassword {
		t.Fatalf("entries = %v", actions(logs))
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"must_change_password": false})
	assertJSON(t, "after", logs[0].After, map[string]any{"must_change_password": true})
	f.assertNoSecrets(t, fixedTemp)
}

func TestUserMutationsRollBackWhenAuditFails(t *testing.T) {
	f := newUserFixture(t)
	u := f.add(t, "ops@example.com", domain.RoleAdmin)
	f.audit.Err = errAuditDown

	if _, _, err := f.svc.Create(f.ctx, CreateUserInput{Email: "new@example.com", Name: "New", Role: domain.RoleViewer}); !errors.Is(err, errAuditDown) {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.Update(f.ctx, u.PublicID, UpdateUserInput{Name: ptr("Changed")}); !errors.Is(err, errAuditDown) {
		t.Fatalf("update: %v", err)
	}
	if _, err := f.svc.Disable(f.ctx, u.PublicID, f.owner.PublicID); !errors.Is(err, errAuditDown) {
		t.Fatalf("disable: %v", err)
	}
	if _, _, err := f.svc.ResetPassword(f.ctx, u.PublicID, f.owner.PublicID); !errors.Is(err, errAuditDown) {
		t.Fatalf("reset: %v", err)
	}
	stored := f.users.Get(u.PublicID)
	if stored.Name != "User ops@example.com" || stored.DisabledAt != nil || stored.SessionVersion != 1 || stored.MustChangePassword {
		t.Fatalf("user changed despite rollback: %+v", stored)
	}
	if n, _ := f.users.Count(context.Background()); n != 2 {
		t.Fatalf("users = %d, create not rolled back", n)
	}
}

func TestUserMutationsRequireActor(t *testing.T) {
	f := newUserFixture(t)
	_, _, err := f.svc.Create(context.Background(), CreateUserInput{Email: "new@example.com", Name: "New", Role: domain.RoleViewer})
	if !errors.Is(err, ErrNoActor) {
		t.Fatalf("err = %v", err)
	}
}

func TestListAndGetUsers(t *testing.T) {
	f := newUserFixture(t)
	a := f.add(t, "a@example.com", domain.RoleAdmin)
	b := f.add(t, "b@example.com", domain.RoleViewer)

	page, err := f.svc.List(f.ctx, domain.PageRequest{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != b.ID || page.Items[1].ID != a.ID || page.NextCursor == "" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	page, err = f.svc.List(f.ctx, domain.PageRequest{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != f.owner.ID || page.NextCursor != "" {
		t.Fatalf("second page = %+v, %v", page, err)
	}
	if _, err := f.svc.List(f.ctx, domain.PageRequest{Cursor: "nope"}); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
	if got, err := f.svc.Get(f.ctx, a.PublicID); err != nil || got.ID != a.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := f.svc.Get(f.ctx, id.New(id.User)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func cliCtx() context.Context {
	return domain.WithActor(context.Background(), domain.SystemActor("cli"))
}

func TestRecoverExistingEmail(t *testing.T) {
	f := newUserFixture(t)
	u := f.add(t, "ops@example.com", domain.RoleAdmin)
	f.disable(u)
	f.audit.Reset()

	res, err := f.svc.Recover(cliCtx(), "  OPS@Example.com ", "chosen-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || res.Password != "" || res.User.ID != u.ID {
		t.Fatalf("result = %+v", res)
	}
	stored := f.users.Get(u.PublicID)
	if !f.hasher.Matches(stored.PasswordHash, "chosen-password-1") || stored.MustChangePassword || stored.DisabledAt != nil || stored.SessionVersion != 2 {
		t.Fatalf("stored = %+v", stored)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserResetPassword || logs[0].Actor != domain.SystemActor("cli") {
		t.Fatalf("entries = %+v", logs)
	}
	assertJSON(t, "after", logs[0].After, map[string]any{"must_change_password": false, "disabled_at": nil})
	f.assertNoSecrets(t, "chosen-password-1")
}

func TestRecoverGeneratedPasswordOnOldestOwner(t *testing.T) {
	f := newUserFixture(t)
	f.add(t, "second-owner@example.com", domain.RoleOwner)
	f.audit.Reset()

	res, err := f.svc.Recover(cliCtx(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.ID != f.owner.ID || res.Password != fixedTemp || !res.User.MustChangePassword {
		t.Fatalf("result = %+v", res)
	}
	f.assertNoSecrets(t, fixedTemp)
}

func TestRecoverCreatesFirstOwner(t *testing.T) {
	f := newUserFixture(t)
	f.users.Remove(f.owner.PublicID)
	res, err := f.svc.Recover(cliCtx(), "Boss@Example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	u := res.User
	if !res.Created || u.Email != "boss@example.com" || u.Name != "boss" || u.Role != domain.RoleOwner || !u.MustChangePassword || res.Password != fixedTemp {
		t.Fatalf("result = %+v user = %+v", res, u)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != ActionUserSetup || logs[0].Actor != domain.SystemActor("cli") {
		t.Fatalf("entries = %+v", logs)
	}

	f2 := newUserFixture(t)
	f2.users.Remove(f2.owner.PublicID)
	res, err = f2.svc.Recover(cliCtx(), "boss@example.com", "chosen-password-1")
	if err != nil || res.User.MustChangePassword || res.Password != "" {
		t.Fatalf("provided password: %+v, %v", res, err)
	}
}

func TestRecoverRejections(t *testing.T) {
	f := newUserFixture(t)
	f.audit.Reset()
	before := *f.users.Get(f.owner.PublicID)

	if _, err := f.svc.Recover(cliCtx(), "nobody@example.com", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown email: %v", err)
	}
	_, err := f.svc.Recover(cliCtx(), "owner@example.com", "short-pass1")
	requireFields(t, err, domain.FieldError{Field: "password", Rule: "min"})
	_, err = f.svc.Recover(cliCtx(), "not-an-email", "")
	requireFields(t, err, domain.FieldError{Field: "email", Rule: "email"})
	if after := *f.users.Get(f.owner.PublicID); after != before || len(f.audit.Logs()) != 0 {
		t.Fatal("a rejected recovery changed something")
	}

	empty := newUserFixture(t)
	empty.users.Remove(empty.owner.PublicID)
	if _, err := empty.svc.Recover(cliCtx(), "", ""); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("no users, no email: %v", err)
	}
}
