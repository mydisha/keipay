package postgres

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
)

// isolateAuditLogs empties audit_logs inside tx, which the caller rolls back.
// The table lock is taken before the snapshot, so writers in other test
// packages wait instead of making the delete fail with a serialization error.
func isolateAuditLogs(ctx context.Context, t *testing.T, tx pgx.Tx) {
	t.Helper()
	if _, err := tx.Exec(ctx, `LOCK TABLE audit_logs IN EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM audit_logs`); err != nil {
		t.Fatalf("clear: %v", err)
	}
}

// Rows written by other tests cannot change the pages: see isolateAuditLogs.
func TestIntegrationAuditListKeyset(t *testing.T) {
	dsn := os.Getenv("KEI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("KEI_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ctx = context.WithValue(ctx, txKey{}, tx)
	isolateAuditLogs(ctx, t, tx)

	repo := NewAuditRepo(pool)
	same := time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)
	times := []time.Time{same.Add(-time.Second), same, same, same, same.Add(time.Second)}
	var want []domain.AuditLog
	for _, at := range times {
		l := domain.AuditLog{
			PublicID: id.New(id.Audit), OccurredAt: at, Action: "tenant.update",
			Actor:  domain.Actor{Type: domain.ActorAdmin, ID: "admin"},
			Target: domain.AuditTarget{Type: "tenant", ID: "1"},
		}
		if err := repo.Create(ctx, &l); err != nil {
			t.Fatalf("create: %v", err)
		}
		if l.ID == 0 {
			t.Fatal("create did not return the row id")
		}
		want = append(want, l)
	}
	sort.Slice(want, func(i, j int) bool {
		if !want[i].OccurredAt.Equal(want[j].OccurredAt) {
			return want[i].OccurredAt.After(want[j].OccurredAt)
		}
		return want[i].PublicID > want[j].PublicID
	})

	var got []string
	var sizes []int
	page := domain.Page{Limit: 2}
	for i := 0; ; i++ {
		if i > 5 {
			t.Fatal("pagination did not terminate")
		}
		res, err := repo.List(ctx, page)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		sizes = append(sizes, len(res.Items))
		for _, l := range res.Items {
			if !id.Valid(l.PublicID, id.Audit) {
				t.Fatalf("item public id %q", l.PublicID)
			}
			got = append(got, l.PublicID)
		}
		if res.NextCursor == "" {
			break
		}
		c, err := domain.DecodeCursor(res.NextCursor)
		if err != nil {
			t.Fatalf("decode next cursor: %v", err)
		}
		if !id.Valid(c.ID, id.Audit) {
			t.Fatalf("cursor id %q is not a public id", c.ID)
		}
		page.After = &c
	}

	if len(sizes) != 3 || sizes[0] != 2 || sizes[1] != 2 || sizes[2] != 1 {
		t.Fatalf("page sizes = %v, want [2 2 1]", sizes)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d ids, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i].PublicID {
			t.Fatalf("position %d = %s, want %s (order %v)", i, got[i], want[i].PublicID, got)
		}
	}

	// An exact multiple of the page size still ends with no cursor.
	res, err := repo.List(ctx, domain.Page{Limit: 5})
	if err != nil || len(res.Items) != 5 || res.NextCursor != "" {
		t.Fatalf("limit 5: %d items, cursor %q, %v", len(res.Items), res.NextCursor, err)
	}
	// Snapshots stored as NULL come back as nil.
	if res.Items[0].Before != nil || res.Items[0].After != nil {
		t.Fatalf("before/after = %s / %s, want nil", res.Items[0].Before, res.Items[0].After)
	}
}

func TestIntegrationAuditRoundTripsSnapshots(t *testing.T) {
	dsn := os.Getenv("KEI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("KEI_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ctx = context.WithValue(ctx, txKey{}, tx)
	isolateAuditLogs(ctx, t, tx)

	repo := NewAuditRepo(pool)
	in := domain.AuditLog{
		PublicID: id.New(id.Audit), OccurredAt: time.Now().UTC(), Action: "tenant.update",
		Actor:  domain.Actor{Type: domain.ActorAdmin, ID: "admin", IP: "203.0.113.7", UserAgent: "ua/1", RequestID: "rid-1"},
		Target: domain.AuditTarget{Type: "tenant", ID: "7"},
		Before: []byte(`{"name":"Old"}`), After: []byte(`{"name":"Ünïcode"}`),
	}
	if err := repo.Create(ctx, &in); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := repo.List(ctx, domain.Page{Limit: 1})
	if err != nil || len(res.Items) != 1 {
		t.Fatalf("list: %+v, %v", res, err)
	}
	out := res.Items[0]
	if out.Actor != in.Actor || out.Target != in.Target || out.Action != in.Action ||
		string(out.Before) != `{"name": "Old"}` || string(out.After) != `{"name": "Ünïcode"}` ||
		!out.OccurredAt.Equal(in.OccurredAt.Truncate(time.Microsecond)) {
		t.Fatalf("round trip = %+v (before %s after %s)", out, out.Before, out.After)
	}
}
