package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

// ListOverdue applies every filter in SQL, oldest expiry first, and
// MarkChecked holds a payment back until the recheck time.
func TestIntegrationCheckoutCheckListOverdue(t *testing.T) {
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	app := newTestApp(t, pool, domain.EnvTest)
	conn := newTestConnection(t, pool, domain.EnvTest)
	payments := postgres.NewPaymentRepo(pool)
	repo := postgres.NewCheckoutCheckRepo(pool)

	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	add := func(ref string, expired time.Duration, status domain.PaymentStatus, withConn bool, invoice string) *domain.Payment {
		exp := now.Add(-expired)
		p := &domain.Payment{PublicID: id.New(id.Payment), AppID: app.ID, OrderID: ref, ExternalID: ref, XenditInvoiceID: invoice,
			Amount: 1000, Currency: "IDR", Status: domain.PaymentStatusPending, ExpiresAt: &exp}
		if withConn {
			p.ConnectionID, p.Provider = &conn.ID, conn.Provider
		}
		if err := payments.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		if status != domain.PaymentStatusPending {
			if err := payments.UpdateStatus(ctx, p.ID, status, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	older := add("ref-older", 30*time.Minute, domain.PaymentStatusPending, true, "inv-older")
	due := add("ref-due", 5*time.Minute, domain.PaymentStatusPending, true, "inv-due")
	add("ref-recent", time.Minute, domain.PaymentStatusPending, true, "inv-recent")
	add("ref-ancient", 80*time.Hour, domain.PaymentStatusPending, true, "inv-ancient")
	add("ref-paid", 5*time.Minute, domain.PaymentStatusPaid, true, "inv-paid")
	add("ref-noconn", 5*time.Minute, domain.PaymentStatusPending, false, "inv-noconn")
	add("ref-noinvoice", 5*time.Minute, domain.PaymentStatusPending, true, "")

	q := domain.OverdueCheckoutQuery{ExpiredBefore: now.Add(-2 * time.Minute), NotBefore: now.Add(-72 * time.Hour),
		CheckedBefore: now.Add(-10 * time.Minute), Limit: 10}
	got, err := repo.ListOverdue(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].PaymentID != older.ID || got[1].PaymentID != due.ID {
		t.Fatalf("overdue = %+v", got)
	}
	if c := got[1]; c.PaymentPublicID != due.PublicID || c.ConnectionID != conn.ID || c.ProviderPaymentID != "inv-due" ||
		c.Reference != "ref-due" || !c.ExpiresAt.Equal(*due.ExpiresAt) {
		t.Fatalf("row = %+v", c)
	}

	if err := repo.MarkChecked(ctx, older.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err = repo.ListOverdue(ctx, q)
	if err != nil || len(got) != 1 || got[0].PaymentID != due.ID {
		t.Fatalf("after a recent check: %+v, %v", got, err)
	}
	if err := repo.MarkChecked(ctx, older.ID, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, err = repo.ListOverdue(ctx, q); err != nil || len(got) != 2 {
		t.Fatalf("after the recheck time: %+v, %v", got, err)
	}
	q.Limit = 1
	if got, err = repo.ListOverdue(ctx, q); err != nil || len(got) != 1 || got[0].PaymentID != older.ID {
		t.Fatalf("limit: %+v, %v", got, err)
	}
}

// Rows read back are stored with source poll; the down migration folds them
// back into webhook so the old constraint can return.
func TestIntegrationMigration11UpDownUp(t *testing.T) {
	db := pgtest.New(t, 0)
	pool := openPool(t, db.DSN)
	ctx := context.Background()
	conn := newTestConnection(t, pool, domain.EnvTest)
	insert := func(source string) error {
		_, err := pool.Exec(ctx, `INSERT INTO inbound_webhooks (public_id, connection_id, provider, source, signature_valid, result, raw_body)
			VALUES ($1, $2, 'xendit', $3, true, 'applied', '\x7b7d')`, id.New(id.Inbound), conn.ID, source)
		return err
	}
	if err := insert(domain.InboundSourcePoll); err != nil {
		t.Fatalf("poll source refused: %v", err)
	}
	if err := insert("carrier-pigeon"); err == nil {
		t.Fatal("an unknown source was accepted")
	}

	if err := db.Migrate.Steps(-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	var polls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inbound_webhooks WHERE source = 'poll'`).Scan(&polls); err != nil || polls != 0 {
		t.Fatalf("poll rows after down: %d, %v", polls, err)
	}
	if err := insert(domain.InboundSourcePoll); err == nil {
		t.Fatal("the old constraint accepted poll")
	}
	var col int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'payments' AND column_name = 'status_checked_at'`).Scan(&col); err != nil || col != 0 {
		t.Fatalf("status_checked_at after down: %d, %v", col, err)
	}
	if err := db.Migrate.Steps(1); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if err := insert(domain.InboundSourcePoll); err != nil {
		t.Fatalf("poll after up again: %v", err)
	}
}
