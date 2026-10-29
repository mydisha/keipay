package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

func collect(t *testing.T, f *reconFixture, conn *domain.Connection) ([]domain.GatewayTransaction, error) {
	t.Helper()
	from, to := f.window()
	var got []domain.GatewayTransaction
	err := f.source.Transactions(context.Background(), conn, from, to, func(tx domain.GatewayTransaction) error {
		got = append(got, tx)
		return nil
	})
	return got, err
}

// Pages are followed by cursor, 50 at a time, within the range.
func TestSourcePages(t *testing.T) {
	f := newReconFixture(t)
	from, to := f.window()
	f.fake.Pages = []provider.TransactionPage{
		{Transactions: []domain.GatewayTransaction{{Reference: "a"}, {Reference: "b"}}},
		{Transactions: []domain.GatewayTransaction{{Reference: "c"}}},
	}
	got, err := collect(t, f, f.conn)
	if err != nil || len(got) != 3 || got[2].Reference != "c" {
		t.Fatalf("transactions = %+v, %v", got, err)
	}
	if len(f.fake.Lists) != 2 || f.fake.Lists[0].Cursor != "" || f.fake.Lists[1].Cursor != "1" {
		t.Fatalf("queries = %+v", f.fake.Lists)
	}
	for _, q := range f.fake.Lists {
		if q.Limit != 50 || !q.From.Equal(from) || !q.To.Equal(to) {
			t.Fatalf("query = %+v", q)
		}
	}
	if len(f.sleeps) != 0 {
		t.Fatalf("slept %v without a failure", f.sleeps)
	}
}

// A retryable page is tried three times in total, waiting 1s then 2s.
func TestSourceRetriesRetryablePage(t *testing.T) {
	retryable := provider.NewStatusError(provider.ErrRetryable, 503, "busy")
	f := newReconFixture(t)
	f.gateway(domain.GatewayTransaction{Reference: "a"})
	f.fake.ListErrs = []error{retryable, retryable}
	got, err := collect(t, f, f.conn)
	if err != nil || len(got) != 1 || f.fake.ListCalls() != 3 {
		t.Fatalf("got %+v, %v after %d calls", got, err, f.fake.ListCalls())
	}
	if !slices.Equal(f.sleeps, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatalf("sleeps = %v", f.sleeps)
	}

	f = newReconFixture(t)
	f.gateway(domain.GatewayTransaction{Reference: "a"})
	f.fake.ListErrs = []error{retryable, retryable, retryable}
	if _, err := collect(t, f, f.conn); !errors.Is(err, provider.ErrRetryable) || f.fake.ListCalls() != 3 {
		t.Fatalf("three failures: %v after %d calls", err, f.fake.ListCalls())
	}
}

// A refused page is not retried.
func TestSourceDoesNotRetryRejected(t *testing.T) {
	f := newReconFixture(t)
	f.fake.ListErrs = []error{provider.NewStatusError(provider.ErrRejected, 401, "bad key")}
	if _, err := collect(t, f, f.conn); !errors.Is(err, provider.ErrRejected) || f.fake.ListCalls() != 1 || len(f.sleeps) != 0 {
		t.Fatalf("err = %v after %d calls, sleeps %v", err, f.fake.ListCalls(), f.sleeps)
	}
}

// More than 100000 rows stops the read.
func TestSourceStopsAfterMaxRows(t *testing.T) {
	f := newReconFixture(t)
	const pageSize = 1000
	f.fake.ListFunc = func(q provider.TransactionQuery) (*provider.TransactionPage, error) {
		n, _ := strconv.Atoi(q.Cursor)
		page := &provider.TransactionPage{Transactions: make([]domain.GatewayTransaction, pageSize), NextCursor: strconv.Itoa(n + 1)}
		return page, nil
	}
	yielded := 0
	from, to := f.window()
	err := f.source.Transactions(context.Background(), f.conn, from, to, func(domain.GatewayTransaction) error {
		yielded++
		return nil
	})
	if !errors.Is(err, ErrTooManyTransactions) || yielded != maxRunTransactions {
		t.Fatalf("err = %v after %d rows", err, yielded)
	}

	// Exactly the limit is fine.
	f = newReconFixture(t)
	f.fake.ListFunc = func(q provider.TransactionQuery) (*provider.TransactionPage, error) {
		n, _ := strconv.Atoi(q.Cursor)
		page := &provider.TransactionPage{Transactions: make([]domain.GatewayTransaction, pageSize)}
		if n+1 < maxRunTransactions/pageSize {
			page.NextCursor = strconv.Itoa(n + 1)
		}
		return page, nil
	}
	if got, err := collect(t, f, f.conn); err != nil || len(got) != maxRunTransactions {
		t.Fatalf("at the limit: %d rows, %v", len(got), err)
	}
}

func TestSourceErrors(t *testing.T) {
	f := newReconFixture(t)
	boom := errors.New("yield failed")
	f.gateway(domain.GatewayTransaction{Reference: "a"})
	from, to := f.window()
	if err := f.source.Transactions(context.Background(), f.conn, from, to, func(domain.GatewayTransaction) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("yield error = %v", err)
	}
	f.fake.Caps.ListTransactions = false
	if _, err := collect(t, f, f.conn); err == nil {
		t.Fatal("provider without the capability was read")
	}
	gone := *f.conn
	gone.Provider = "gone"
	if _, err := collect(t, f, &gone); !errors.Is(err, provider.ErrUnknownProvider) {
		t.Fatalf("unknown provider = %v", err)
	}
	broken := *f.conn
	broken.CredentialsEnc = "not sealed"
	if _, err := collect(t, newReconFixture(t), &broken); err == nil {
		t.Fatal("unreadable credentials accepted")
	}
}
