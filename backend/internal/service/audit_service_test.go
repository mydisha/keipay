package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

func newAuditSvc() (*AuditService, *memAuditRepo) {
	repo := &memAuditRepo{}
	svc := NewAuditService(repo)
	svc.now = func() time.Time { return time.Date(2026, 1, 2, 10, 0, 0, 123456000, time.FixedZone("WIB", 7*3600)) }
	return svc, repo
}

func TestAuditRecordWithoutActorFailsClosed(t *testing.T) {
	svc, repo := newAuditSvc()
	err := svc.Record(context.Background(), ActionTenantUpdate, domain.AuditTarget{Type: "tenant", ID: "1"}, nil, nil)
	if !errors.Is(err, ErrNoActor) {
		t.Fatalf("err = %v, want ErrNoActor", err)
	}
	if len(repo.logs) != 0 {
		t.Fatalf("entry written without actor: %+v", repo.logs)
	}
}

func TestAuditRecordRejectsInvalidActor(t *testing.T) {
	for _, a := range []domain.Actor{{Type: "robot", ID: "x"}, {Type: domain.ActorAdmin}, {}} {
		svc, repo := newAuditSvc()
		ctx := domain.WithActor(context.Background(), a)
		if err := svc.Record(ctx, ActionTenantUpdate, domain.AuditTarget{Type: "tenant", ID: "1"}, nil, nil); err == nil {
			t.Fatalf("actor %+v accepted", a)
		}
		if len(repo.logs) != 0 {
			t.Fatalf("actor %+v wrote an entry", a)
		}
	}
}

func TestAuditRecordStoresActorTargetAndSnapshots(t *testing.T) {
	svc, repo := newAuditSvc()
	err := svc.Record(adminCtx(), ActionTenantUpdate, domain.AuditTarget{Type: "tenant", ID: "7"},
		map[string]any{"name": "Old"}, map[string]any{"name": "Acme Ünïcode"})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(repo.logs) != 1 {
		t.Fatalf("got %d entries, want 1", len(repo.logs))
	}
	l := repo.logs[0]
	if !id.Valid(l.PublicID, id.Audit) {
		t.Fatalf("public id %q is not an aud_ id", l.PublicID)
	}
	if l.OccurredAt.Location() != time.UTC || !l.OccurredAt.Equal(svc.now()) {
		t.Fatalf("occurred_at = %v, want %v in UTC", l.OccurredAt, svc.now())
	}
	want := domain.Actor{Type: domain.ActorAdmin, ID: "admin", IP: "203.0.113.7", UserAgent: "test-agent", RequestID: "req-1"}
	if l.Actor != want {
		t.Fatalf("actor = %+v, want %+v", l.Actor, want)
	}
	if l.Action != ActionTenantUpdate || l.Target != (domain.AuditTarget{Type: "tenant", ID: "7"}) {
		t.Fatalf("action/target = %s %+v", l.Action, l.Target)
	}
	assertJSON(t, "before", l.Before, map[string]any{"name": "Old"})
	assertJSON(t, "after", l.After, map[string]any{"name": "Acme Ünïcode"})
}

func TestAuditRecordNilSnapshotsAreNull(t *testing.T) {
	svc, repo := newAuditSvc()
	var typedNil *tenantSnapshot
	if err := svc.Record(adminCtx(), ActionTenantCreate, domain.AuditTarget{Type: "tenant", ID: "1"}, nil, typedNil); err != nil {
		t.Fatalf("record: %v", err)
	}
	if repo.logs[0].Before != nil || repo.logs[0].After != nil {
		t.Fatalf("before/after = %s / %s, want nil", repo.logs[0].Before, repo.logs[0].After)
	}
}

func TestAuditRedaction(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"password", map[string]any{"password": "hunter2", "name": "a"}, map[string]any{"password": redacted, "name": "a"}},
		{"api_secret", map[string]any{"api_secret": "sk_x"}, map[string]any{"api_secret": redacted}},
		{"mixed case", map[string]any{"New_Password": "x", "AccessToken": "y"}, map[string]any{"New_Password": redacted, "AccessToken": redacted}},
		{"enc suffix", map[string]any{"api_secret_enc": "zz", "key_enc": "zz", "encoding": "utf8"},
			map[string]any{"api_secret_enc": redacted, "key_enc": redacted, "encoding": "utf8"}},
		{"nested", map[string]any{"a": map[string]any{"token": "t", "b": 1}}, map[string]any{"a": map[string]any{"token": redacted, "b": 1}}},
		{"arrays", map[string]any{"items": []any{map[string]any{"secret": "s"}, map[string]any{"ok": "v"}}},
			map[string]any{"items": []any{map[string]any{"secret": redacted}, map[string]any{"ok": "v"}}}},
		{"secret object", map[string]any{"secret": map[string]any{"k": "v"}}, map[string]any{"secret": redacted}},
		{"flags stay", map[string]any{"must_change_password": true, "token": nil}, map[string]any{"must_change_password": true, "token": nil}},
		{"untouched", map[string]any{"api_key_last4": "abcd", "slug": "acme", "count": 3}, map[string]any{"api_key_last4": "abcd", "slug": "acme", "count": 3}},
		{"top-level array", []any{map[string]any{"password": "p"}}, []any{map[string]any{"password": redacted}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := snapshot(tc.in)
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			assertJSON(t, "snapshot", got, tc.want)
		})
	}
}

func TestAuditSnapshotKeepsLargeIntegers(t *testing.T) {
	got, err := snapshot(map[string]any{"id": int64(9007199254740993)})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(string(got), "9007199254740993") {
		t.Fatalf("large integer changed: %s", got)
	}
}

func TestAuditListValidatesPage(t *testing.T) {
	svc, repo := newAuditSvc()
	if _, err := svc.List(context.Background(), domain.PageRequest{}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if repo.page.Limit != domain.DefaultPageLimit || repo.page.After != nil {
		t.Fatalf("page = %+v, want default limit and no cursor", repo.page)
	}

	cur := domain.Cursor{Time: time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC), ID: id.New(id.Audit)}
	if _, err := svc.List(context.Background(), domain.PageRequest{Limit: 2, Cursor: domain.EncodeCursor(cur)}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if repo.page.Limit != 2 || repo.page.After == nil || !repo.page.After.Time.Equal(cur.Time) || repo.page.After.ID != cur.ID {
		t.Fatalf("page = %+v, want limit 2 after %+v", repo.page, cur)
	}

	if _, err := svc.List(context.Background(), domain.PageRequest{Cursor: "%%%"}); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v, want ErrInvalidCursor", err)
	}
	if _, err := svc.List(context.Background(), domain.PageRequest{Limit: 201}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("limit 201 err = %v, want ErrInvalidInput", err)
	}
}
