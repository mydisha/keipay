package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
)

func fieldsOf(t *testing.T, err error) []domain.FieldError {
	t.Helper()
	var coded *domain.Error
	if !errors.As(err, &coded) || coded.Code != domain.CodeInvalidRequest {
		t.Fatalf("err = %v, want invalid_request", err)
	}
	return coded.Details["fields"].([]domain.FieldError)
}

// Each rule of the create request.
func TestCreateRunValidation(t *testing.T) {
	f := newReconFixture(t)
	from, to := f.window()
	noList := providertest.New()
	noList.Desc.Name = "nolist"
	noList.Caps.ListTransactions = false
	registry, err := provider.NewRegistry(f.fake, noList)
	if err != nil {
		t.Fatal(err)
	}
	f.svc.registry = registry
	unsupported := *f.conn
	unsupported.ID, unsupported.PublicID, unsupported.Name, unsupported.Provider = 0, id.New(id.Connection), "nolist", "nolist"
	if err := f.conns.Create(context.Background(), &unsupported); err != nil {
		t.Fatal(err)
	}
	rfc := func(t time.Time) string { return t.Format(time.RFC3339) }
	now := f.clock.Now()
	cases := []struct {
		name string
		in   CreateRunInput
		want []domain.FieldError
	}{
		{"all missing", CreateRunInput{}, []domain.FieldError{{Field: "connection_id", Rule: "required"}, {Field: "from", Rule: "required"}, {Field: "to", Rule: "required"}}},
		{"malformed id", CreateRunInput{ConnectionID: "con_123", From: rfc(from), To: rfc(to)}, []domain.FieldError{{Field: "connection_id", Rule: "public_id"}}},
		{"other prefix", CreateRunInput{ConnectionID: id.New(id.App), From: rfc(from), To: rfc(to)}, []domain.FieldError{{Field: "connection_id", Rule: "public_id"}}},
		{"unknown connection", CreateRunInput{ConnectionID: id.New(id.Connection), From: rfc(from), To: rfc(to)}, []domain.FieldError{{Field: "connection_id", Rule: "exists"}}},
		{"provider cannot list", CreateRunInput{ConnectionID: unsupported.PublicID, From: rfc(from), To: rfc(to)}, []domain.FieldError{{Field: "connection_id", Rule: "unsupported"}}},
		{"bad times", CreateRunInput{ConnectionID: f.conn.PublicID, From: "yesterday", To: "2026-10-01"}, []domain.FieldError{{Field: "from", Rule: "datetime"}, {Field: "to", Rule: "datetime"}}},
		{"to equals from", CreateRunInput{ConnectionID: f.conn.PublicID, From: rfc(from), To: rfc(from)}, []domain.FieldError{{Field: "to", Rule: "after"}}},
		{"to before from", CreateRunInput{ConnectionID: f.conn.PublicID, From: rfc(from), To: rfc(from.Add(-time.Second))}, []domain.FieldError{{Field: "to", Rule: "after"}}},
		{"31 days and a second", CreateRunInput{ConnectionID: f.conn.PublicID, From: rfc(to.Add(-31*24*time.Hour - time.Second)), To: rfc(to)},
			[]domain.FieldError{{Field: "to", Rule: "max_range"}}},
		{"from is now", CreateRunInput{ConnectionID: f.conn.PublicID, From: rfc(now), To: rfc(now.Add(time.Hour))}, []domain.FieldError{{Field: "from", Rule: "past"}}},
		{"from in the future", CreateRunInput{ConnectionID: f.conn.PublicID, From: rfc(now.Add(time.Hour)), To: rfc(now.Add(2 * time.Hour))},
			[]domain.FieldError{{Field: "from", Rule: "past"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.CreateRun(adminCtx(), tc.in)
			got := fieldsOf(t, err)
			if len(got) != len(tc.want) {
				t.Fatalf("fields = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("fields = %v, want %v", got, tc.want)
				}
			}
		})
	}
	if runs := f.runs.Runs(); len(runs) != 0 || len(f.audit.Logs()) != 0 || len(f.queue.calls) != 0 {
		t.Fatalf("invalid requests wrote: runs=%d audit=%d queued=%d", len(runs), len(f.audit.Logs()), len(f.queue.calls))
	}

	// Exactly 31 days, ending in the future, any offset: accepted.
	in := CreateRunInput{ConnectionID: strings.ToLower(f.conn.PublicID[:4]) + f.conn.PublicID[4:],
		From: now.Add(-31 * 24 * time.Hour).In(time.FixedZone("WIB", 7*3600)).Format(time.RFC3339Nano), To: rfc(now.Add(time.Nanosecond))}
	if _, err := f.svc.CreateRun(adminCtx(), in); err != nil {
		t.Fatalf("31 days: %v", err)
	}
}

// A run is stored queued with its creator, audited in the same
// transaction and queued; a disabled connection can be reconciled.
func TestCreateRun(t *testing.T) {
	f := newReconFixture(t)
	f.conn.Status = domain.ConnectionDisabled
	if err := f.conns.Update(context.Background(), f.conn); err != nil {
		t.Fatal(err)
	}
	from, to := f.window()
	run, err := f.svc.CreateRun(adminCtx(), CreateRunInput{ConnectionID: f.conn.PublicID,
		From: from.In(time.FixedZone("WIB", 7*3600)).Format(time.RFC3339), To: to.Format(time.RFC3339), CreatedByID: &f.admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !id.Valid(run.PublicID, id.Reconciliation) || run.Status != domain.RunQueued || run.RunCount != 1 ||
		!run.From.Equal(from) || run.From.Location() != time.UTC || !run.To.Equal(to) || run.Connection.PublicID != f.conn.PublicID ||
		run.CreatedBy == nil || run.CreatedBy.PublicID != f.admin.PublicID || run.Summary != nil || run.Error != nil {
		t.Fatalf("run = %+v", run)
	}
	if len(f.queue.calls) != 1 || f.queue.calls[0] != [2]int64{run.ID, 1} {
		t.Fatalf("queued = %v", f.queue.calls)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "reconciliation_run.create" ||
		logs[0].Target != (domain.AuditTarget{Type: "reconciliation_run", ID: run.PublicID}) {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "before", logs[0].Before, nil)
	assertJSON(t, "after", logs[0].After, map[string]any{"id": run.PublicID, "connection_id": f.conn.PublicID,
		"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339), "status": "queued"})
}

// The run and its audit entry roll back together.
func TestCreateRunAuditFailureRollsBack(t *testing.T) {
	f := newReconFixture(t)
	f.svc.audit = failingAuditor{}
	from, to := f.window()
	_, err := f.svc.CreateRun(adminCtx(), CreateRunInput{ConnectionID: f.conn.PublicID, From: from.Format(time.RFC3339), To: to.Format(time.RFC3339)})
	if !errors.Is(err, errAuditDown) || len(f.runs.Runs()) != 0 || len(f.queue.calls) != 0 {
		t.Fatalf("err = %v, runs = %d, queued = %d", err, len(f.runs.Runs()), len(f.queue.calls))
	}
	if _, err := f.svc.CreateRun(context.Background(), CreateRunInput{ConnectionID: f.conn.PublicID,
		From: from.Format(time.RFC3339), To: to.Format(time.RFC3339)}); err == nil {
		t.Fatal("a run without an actor was created")
	}
}

type failingAuditor struct{}

func (failingAuditor) Record(context.Context, string, domain.AuditTarget, any, any) error {
	return errAuditDown
}

// When the queue is down the run is returned failed/not_queued.
func TestCreateRunNotQueued(t *testing.T) {
	f := newReconFixture(t)
	f.queue.err = errors.New("redis down")
	run := f.createRun(t)
	if run.Status != domain.RunFailed || run.Error == nil || run.Error.Code != "not_queued" ||
		strings.Contains(run.Error.Message, "redis") || run.FinishedAt == nil {
		t.Fatalf("run = %+v (error %+v)", run, run.Error)
	}
	if len(f.audit.Logs()) != 1 {
		t.Fatal("the stored run was not audited")
	}
	// It can be re-run right away.
	f.queue.err = nil
	again, err := f.svc.Rerun(adminCtx(), run.PublicID)
	if err != nil || again.Status != domain.RunQueued || again.RunCount != 2 || again.Error != nil {
		t.Fatalf("rerun = %+v, %v", again, err)
	}
}

// A run stores its items, summary and unresolved count.
func TestExecuteStoresItemsAndSummary(t *testing.T) {
	f := newReconFixture(t)
	from, _ := f.window()
	mid := from.Add(6 * time.Hour)
	f.paidPayment(t, "kp_pay_match", 5000, i64(100), mid)
	f.paidPayment(t, "kp_pay_lost", 7000, nil, mid)
	f.paidPayment(t, "kp_pay_fee", 9000, i64(100), mid)
	f.gateway(
		gatewayRow("kp_pay_match", "tx_1", domain.PaymentStatusSettled, 5000, i64(100), mid.Add(time.Second)),
		gatewayRow("kp_pay_fee", "tx_2", domain.PaymentStatusPaid, 9000, i64(120), mid.Add(time.Second)),
		gatewayRow("kp_pay_stranger", "tx_3", domain.PaymentStatusPaid, 1000, nil, mid),
		gatewayRow("kp_pay_failed", "tx_4", domain.PaymentStatusFailed, 1000, nil, mid),
	)
	run := f.createRun(t)
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	got := f.run(t, run.PublicID)
	if got.Status != domain.RunSucceeded || got.FinishedAt == nil || got.StartedAt == nil || got.UnresolvedCount != 3 ||
		got.Summary == nil || got.Summary.Total != 4 || got.Summary.Results[domain.ResultMatched] != 1 ||
		got.Summary.Results[domain.ResultMissingInGateway] != 1 || got.Summary.Results[domain.ResultMissingInKeipay] != 1 ||
		got.Summary.Results[domain.ResultFeeMismatch] != 1 || got.Summary.Results[domain.ResultAmountMismatch] != 0 ||
		len(got.Summary.Results) != 6 {
		t.Fatalf("run = %+v summary %+v", got, got.Summary)
	}
	items := f.runs.Items(got.ID)
	results := map[string]domain.ReconciliationResult{}
	for _, it := range items {
		results[it.Reference] = it.Result
		if !id.Valid(it.PublicID, id.ReconciliationItem) || it.RunPublicID != run.PublicID {
			t.Fatalf("item = %+v", it)
		}
	}
	want := map[string]domain.ReconciliationResult{"kp_pay_match": domain.ResultMatched, "kp_pay_lost": domain.ResultMissingInGateway,
		"kp_pay_fee": domain.ResultFeeMismatch, "kp_pay_stranger": domain.ResultMissingInKeipay}
	if len(results) != len(want) {
		t.Fatalf("results = %v", results)
	}
	for ref, r := range want {
		if results[ref] != r {
			t.Fatalf("results = %v, want %v", results, want)
		}
	}
	// The gateway was read with an hour of padding on both sides.
	q := f.fake.Lists[0]
	if !q.From.Equal(got.From.Add(-time.Hour)) || !q.To.Equal(got.To.Add(time.Hour)) {
		t.Fatalf("gateway window = %v - %v", q.From, q.To)
	}
}

// Every failure fails the run with a fixed message and returns nil, so the
// task is never retried; anything unexpected is internal_error.
func TestExecuteFailureCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"rejected", provider.NewStatusError(provider.ErrRejected, 401, "invalid key xnd_secret"), "provider_rejected"},
		{"unavailable", provider.NewStatusError(provider.ErrRetryable, 503, "gateway exploded"), "provider_unavailable"},
		{"unknown outcome", provider.ErrUnknownOutcome, "provider_unavailable"},
		{"deadline", context.DeadlineExceeded, "provider_unavailable"},
		{"too many", ErrTooManyTransactions, "too_many_transactions"},
		{"internal", errors.New("disk on fire"), "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReconFixture(t)
			f.fake.ListErrs = []error{tc.err, tc.err, tc.err}
			run := f.createRun(t)
			if err := f.execute(t, run); err != nil {
				t.Fatalf("returned %v, want nil", err)
			}
			got := f.run(t, run.PublicID)
			if got.Status != domain.RunFailed || got.Error == nil || got.Error.Code != tc.code || got.FinishedAt == nil {
				t.Fatalf("run = %+v (error %+v)", got, got.Error)
			}
			if got.Error.Message != runErrorMessages[tc.code] || strings.Contains(got.Error.Message, "xnd_") ||
				strings.Contains(got.Error.Message, "exploded") || strings.Contains(got.Error.Message, "fire") {
				t.Fatalf("message = %q", got.Error.Message)
			}
			if len(f.runs.Items(got.ID)) != 0 {
				t.Fatal("a failed run stored items")
			}
		})
	}
}

// touchRecorder wraps the run store: it records when each heartbeat ran
// and can make TouchRun, ClaimRun or FailRun fail.
type touchRecorder struct {
	domain.ReconciliationRepository
	now      func() time.Time
	touches  []time.Time
	touchErr error
	claimErr error
	failErr  error
}

func (r *touchRecorder) TouchRun(ctx context.Context, id int64, runCount int) (bool, error) {
	r.touches = append(r.touches, r.now())
	if r.touchErr != nil {
		return false, r.touchErr
	}
	return r.ReconciliationRepository.TouchRun(ctx, id, runCount)
}

func (r *touchRecorder) ClaimRun(ctx context.Context, id int64, runCount int) (bool, error) {
	if r.claimErr != nil {
		return false, r.claimErr
	}
	return r.ReconciliationRepository.ClaimRun(ctx, id, runCount)
}

func (r *touchRecorder) FailRun(ctx context.Context, id int64, runCount int, e domain.RunError) (bool, error) {
	if r.failErr != nil {
		return false, r.failErr
	}
	return r.ReconciliationRepository.FailRun(ctx, id, runCount, e)
}

func (f *reconFixture) recordTouches() *touchRecorder {
	rec := &touchRecorder{ReconciliationRepository: f.runs, now: f.clock.Now}
	f.svc.runs = rec
	return rec
}

// pagedGateway makes the fake provider serve pages of perPage rows, each
// read taking step on the fixture's clock; before, when set, runs at the
// start of every read with its zero-based index.
func (f *reconFixture) pagedGateway(pages, perPage int, step time.Duration, before func(page int)) {
	at := f.clock.Now().Add(-5 * time.Hour)
	f.fake.ListFunc = func(q provider.TransactionQuery) (*provider.TransactionPage, error) {
		i := 0
		if q.Cursor != "" {
			i, _ = strconv.Atoi(q.Cursor)
		}
		if before != nil {
			before(i)
		}
		f.clock.Advance(step)
		page := &provider.TransactionPage{}
		for j := range perPage {
			ref := fmt.Sprintf("kp_pay_%d_%d", i, j)
			page.Transactions = append(page.Transactions, gatewayRow(ref, "tx_"+ref, domain.PaymentStatusPaid, 1000, nil, at))
		}
		if i+1 < pages {
			page.NextCursor = strconv.Itoa(i + 1)
		}
		return page, nil
	}
}

// While it pulls gateway rows a run refreshes its updated_at at most once a
// minute, however many rows arrive in between.
func TestExecuteHeartbeatThrottle(t *testing.T) {
	f := newReconFixture(t)
	rec := f.recordTouches()
	f.pagedGateway(5, 2, 40*time.Second, nil)
	run := f.createRun(t)
	claimedAt := f.clock.Now()
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	if len(rec.touches) != 2 {
		t.Fatalf("touched at %v, want twice over 200s", rec.touches)
	}
	last := claimedAt
	for _, at := range rec.touches {
		if at.Sub(last) < time.Minute {
			t.Fatalf("touched %v after the previous one", at.Sub(last))
		}
		last = at
	}
	got := f.run(t, run.PublicID)
	if got.Status != domain.RunSucceeded || got.Summary == nil || got.Summary.Total != 10 || f.fake.ListCalls() != 5 {
		t.Fatalf("run = %+v, %d reads", got, f.fake.ListCalls())
	}
}

// A run that reads the gateway quicker than a minute never touches it.
func TestExecuteHeartbeatNotNeeded(t *testing.T) {
	f := newReconFixture(t)
	rec := f.recordTouches()
	f.pagedGateway(3, 5, 10*time.Second, nil)
	run := f.createRun(t)
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	if len(rec.touches) != 0 || f.run(t, run.PublicID).Status != domain.RunSucceeded {
		t.Fatalf("touched at %v", rec.touches)
	}
}

// A run re-run while this run count pulls rows stops at the next heartbeat
// and writes nothing: no items, summary or failure.
func TestExecuteHeartbeatSeesRerun(t *testing.T) {
	f := newReconFixture(t)
	rec := f.recordTouches()
	run := f.createRun(t)
	f.pagedGateway(10, 3, 2*time.Minute, func(page int) {
		if page == 1 {
			f.runs.SetRunUpdatedAt(run.ID, f.clock.Now().Add(-time.Hour))
			if _, err := f.svc.Rerun(adminCtx(), run.PublicID); err != nil {
				t.Errorf("rerun: %v", err)
			}
		}
	})
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	if len(rec.touches) != 2 || f.fake.ListCalls() != 2 {
		t.Fatalf("touched at %v after %d reads, want 2 touches and 2 reads", rec.touches, f.fake.ListCalls())
	}
	got := f.run(t, run.PublicID)
	if got.Status != domain.RunQueued || got.RunCount != 2 || got.Summary != nil || got.Error != nil ||
		len(f.runs.Items(got.ID)) != 0 {
		t.Fatalf("superseded run count wrote: %+v (error %+v)", got, got.Error)
	}
}

// A heartbeat that cannot be stored fails the run like any other error.
func TestExecuteHeartbeatError(t *testing.T) {
	f := newReconFixture(t)
	rec := f.recordTouches()
	rec.touchErr = errors.New("connection reset")
	f.pagedGateway(3, 1, 2*time.Minute, nil)
	run := f.createRun(t)
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	got := f.run(t, run.PublicID)
	if got.Status != domain.RunFailed || got.Error == nil || got.Error.Code != domain.RunErrorInternal ||
		f.fake.ListCalls() != 1 || len(f.runs.Items(got.ID)) != 0 {
		t.Fatalf("run = %+v (error %+v), %d reads", got, got.Error, f.fake.ListCalls())
	}
}

// Only a claim or a failure that cannot be stored is returned to the
// worker.
func TestExecuteReturnsUnrecordedFailures(t *testing.T) {
	t.Run("claim", func(t *testing.T) {
		f := newReconFixture(t)
		rec := f.recordTouches()
		rec.claimErr = errors.New("pool closed")
		run := f.createRun(t)
		if err := f.execute(t, run); !errors.Is(err, rec.claimErr) {
			t.Fatalf("err = %v, want the claim error", err)
		}
		if f.fake.ListCalls() != 0 || f.run(t, run.PublicID).Status != domain.RunQueued {
			t.Fatal("an unclaimed run was executed")
		}
	})
	t.Run("fail run", func(t *testing.T) {
		f := newReconFixture(t)
		rec := f.recordTouches()
		rec.failErr = errors.New("pool closed")
		cause := provider.NewStatusError(provider.ErrRejected, 401, "bad key")
		f.fake.ListErrs = []error{cause}
		run := f.createRun(t)
		err := f.execute(t, run)
		if !errors.Is(err, rec.failErr) || !errors.Is(err, provider.ErrRejected) {
			t.Fatalf("err = %v, want the failure and its cause", err)
		}
		if f.run(t, run.PublicID).Status != domain.RunRunning {
			t.Fatal("the run changed although failing it did not")
		}
	})
}

// A task of an older run count writes nothing, and a second
// claim of the same count is a no-op.
func TestExecuteStaleAndDuplicateTasks(t *testing.T) {
	f := newReconFixture(t)
	f.gateway(gatewayRow("kp_pay_x", "tx_1", domain.PaymentStatusPaid, 1000, nil, f.clock.Now().Add(-5*time.Hour)))
	run := f.createRun(t)
	f.runs.SetRunUpdatedAt(run.ID, f.clock.Now().Add(-time.Hour))
	f.clock.Advance(time.Minute)
	if err := f.svc.Execute(context.Background(), run.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Execute(context.Background(), run.ID, 1); err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if calls := f.fake.ListCalls(); calls != 1 {
		t.Fatalf("gateway read %d times, want 1", calls)
	}
	if _, err := f.svc.Rerun(adminCtx(), run.PublicID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Execute(context.Background(), run.ID, 1); err != nil {
		t.Fatalf("stale task: %v", err)
	}
	got := f.run(t, run.PublicID)
	if got.Status != domain.RunQueued || got.RunCount != 2 || f.fake.ListCalls() != 1 {
		t.Fatalf("stale task changed the run: %+v, %d reads", got, f.fake.ListCalls())
	}
	if err := f.svc.Execute(context.Background(), run.ID, 3); err != nil || f.run(t, run.PublicID).Status != domain.RunQueued {
		t.Fatalf("future task: %v", err)
	}
}

// A run re-run while the worker was still busy keeps the newer run
// count; the older worker's result is dropped.
func TestExecuteDropsSupersededResult(t *testing.T) {
	f := newReconFixture(t)
	run := f.createRun(t)
	f.fake.ListFunc = func(provider.TransactionQuery) (*provider.TransactionPage, error) {
		// While this worker reads the gateway the run stalls and an admin
		// re-runs it.
		f.runs.SetRunUpdatedAt(run.ID, f.clock.Now().Add(-time.Hour))
		if _, err := f.svc.Rerun(adminCtx(), run.PublicID); err != nil {
			t.Errorf("rerun: %v", err)
		}
		f.fake.ListFunc = nil
		return &provider.TransactionPage{Transactions: []domain.GatewayTransaction{
			gatewayRow("kp_pay_x", "tx_1", domain.PaymentStatusPaid, 1000, nil, f.clock.Now().Add(-5*time.Hour))}}, nil
	}
	if err := f.svc.Execute(context.Background(), run.ID, 1); err != nil {
		t.Fatal(err)
	}
	got := f.run(t, run.PublicID)
	if got.Status != domain.RunQueued || got.RunCount != 2 || got.Summary != nil || len(f.runs.Items(got.ID)) != 0 {
		t.Fatalf("superseded result stored: %+v", got)
	}
}

// Re-running recomputes in place and keeps resolutions only for
// unchanged results.
func TestRerunRecomputesInPlace(t *testing.T) {
	f := newReconFixture(t)
	from, _ := f.window()
	mid := from.Add(time.Hour)
	f.paidPayment(t, "kp_pay_keep", 5000, nil, mid)
	f.paidPayment(t, "kp_pay_change", 6000, nil, mid)
	f.gateway(
		gatewayRow("kp_pay_change", "tx_1", domain.PaymentStatusPaid, 6500, nil, mid),
		gatewayRow("kp_pay_gone", "tx_2", domain.PaymentStatusPaid, 100, nil, mid),
	)
	run := f.createRun(t)
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	items := f.runs.Items(run.ID)
	byRef := map[string]domain.ReconciliationItem{}
	for _, it := range items {
		byRef[it.Reference] = it
	}
	for _, ref := range []string{"kp_pay_keep", "kp_pay_change"} {
		if _, err := f.svc.ResolveItem(adminCtx(), byRef[ref].PublicID, "checked "+ref, &f.admin.ID); err != nil {
			t.Fatalf("resolve %s: %v", ref, err)
		}
	}
	// The gateway now has the right amount for kp_pay_change (its result
	// changes) and no longer lists kp_pay_gone.
	f.gateway(gatewayRow("kp_pay_change", "tx_1", domain.PaymentStatusPaid, 6000, nil, mid))
	again, err := f.svc.Rerun(adminCtx(), run.PublicID)
	if err != nil || again.Status != domain.RunQueued || again.RunCount != 2 || again.Summary != nil {
		t.Fatalf("rerun = %+v, %v", again, err)
	}
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	after := map[string]domain.ReconciliationItem{}
	for _, it := range f.runs.Items(run.ID) {
		after[it.Reference] = it
	}
	keep, change := after["kp_pay_keep"], after["kp_pay_change"]
	if len(after) != 2 || keep.PublicID != byRef["kp_pay_keep"].PublicID || keep.Resolution == nil ||
		keep.Result != domain.ResultMissingInGateway {
		t.Fatalf("kept item = %+v", keep)
	}
	if change.PublicID != byRef["kp_pay_change"].PublicID || change.Result != domain.ResultMatched || change.Resolution != nil {
		t.Fatalf("changed item = %+v", change)
	}
	got := f.run(t, run.PublicID)
	if got.UnresolvedCount != 0 || got.Summary.Total != 2 {
		t.Fatalf("run = %+v", got)
	}
	logs := f.audit.Logs()
	last := logs[len(logs)-1]
	if last.Action != "reconciliation_run.rerun" {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "before", last.Before, map[string]any{"status": "succeeded", "run_count": 1})
	assertJSON(t, "after", last.After, map[string]any{"status": "queued", "run_count": 2})
}

// A run in progress cannot be re-run until it stalls for 15
// minutes.
func TestRerunInProgress(t *testing.T) {
	f := newReconFixture(t)
	run := f.createRun(t)
	if _, err := f.svc.Rerun(adminCtx(), run.PublicID); !errors.Is(err, domain.RunInProgress()) {
		t.Fatalf("queued: %v", err)
	}
	if ok, _ := f.runs.ClaimRun(context.Background(), run.ID, 1); !ok {
		t.Fatal("claim")
	}
	f.clock.Advance(14 * time.Minute)
	if _, err := f.svc.Rerun(adminCtx(), run.PublicID); !errors.Is(err, domain.RunInProgress()) {
		t.Fatalf("running: %v", err)
	}
	f.clock.Advance(time.Minute)
	got, err := f.svc.Rerun(adminCtx(), run.PublicID)
	if err != nil || got.RunCount != 2 || got.Status != domain.RunQueued {
		t.Fatalf("stalled: %+v, %v", got, err)
	}
	if _, err := f.svc.Rerun(adminCtx(), id.New(id.Reconciliation)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
}

// Resolving stores the note, user and time, takes the item
// off the unresolved counts and is audited.
func TestResolveItem(t *testing.T) {
	f := newReconFixture(t)
	from, _ := f.window()
	f.paidPayment(t, "kp_pay_a", 5000, nil, from.Add(time.Hour))
	f.paidPayment(t, "kp_pay_b", 5000, nil, from.Add(time.Hour))
	f.paidPayment(t, "kp_pay_m", 5000, nil, from.Add(time.Hour))
	f.gateway(gatewayRow("kp_pay_m", "tx_m", domain.PaymentStatusPaid, 5000, nil, from.Add(time.Hour)))
	run := f.createRun(t)
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	for _, it := range f.runs.Items(run.ID) {
		refs[it.Reference] = it.PublicID
	}
	if f.run(t, run.PublicID).UnresolvedCount != 2 {
		t.Fatal("unresolved count")
	}
	f.audit.Reset()

	long := strings.Repeat("ü", 1000)
	got, err := f.svc.ResolveItem(adminCtx(), refs["kp_pay_a"], "  "+long+" \n", &f.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resolution == nil || got.Resolution.Note != long || got.Resolution.ResolvedBy == nil ||
		got.Resolution.ResolvedBy.PublicID != f.admin.PublicID || !got.Resolution.ResolvedAt.Equal(f.clock.Now()) {
		t.Fatalf("resolution = %+v", got.Resolution)
	}
	if r := f.run(t, run.PublicID); r.UnresolvedCount != 1 {
		t.Fatalf("unresolved = %d", r.UnresolvedCount)
	}
	logs := f.audit.Logs()
	if len(logs) != 1 || logs[0].Action != "reconciliation_item.resolve" ||
		logs[0].Target != (domain.AuditTarget{Type: "reconciliation_item", ID: refs["kp_pay_a"]}) {
		t.Fatalf("audit = %+v", logs)
	}
	assertJSON(t, "before", logs[0].Before, map[string]any{"result": "missing_in_gateway", "resolved": false})
	assertJSON(t, "after", logs[0].After, map[string]any{"result": "missing_in_gateway", "resolved": true, "note": long})

	// Errors: already resolved, matched, bad notes, unknown item.
	if _, err := f.svc.ResolveItem(adminCtx(), refs["kp_pay_a"], "again", &f.admin.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("twice: %v", err)
	}
	var coded *domain.Error
	if _, err := f.svc.ResolveItem(adminCtx(), refs["kp_pay_m"], "fine", &f.admin.ID); !errors.As(err, &coded) ||
		coded.Code != domain.CodeValidationFailed {
		t.Fatalf("matched: %v", err)
	}
	for note, rule := range map[string]string{"": "required", "   \t\n": "required", strings.Repeat("x", 1001): "max", "a\x00b": "invalid_character"} {
		got := fieldsOf(t, func() error { _, err := f.svc.ResolveItem(adminCtx(), refs["kp_pay_b"], note, nil); return err }())
		if len(got) != 1 || got[0] != (domain.FieldError{Field: "note", Rule: rule}) {
			t.Fatalf("note %q: %v", note, got)
		}
	}
	if _, err := f.svc.ResolveItem(adminCtx(), id.New(id.ReconciliationItem), "x", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if len(f.audit.Logs()) != 1 || f.run(t, run.PublicID).UnresolvedCount != 1 {
		t.Fatal("a refused resolution wrote")
	}

	// While the run is re-run its items are frozen.
	f.clock.Advance(time.Minute)
	if _, err := f.svc.Rerun(adminCtx(), run.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ResolveItem(adminCtx(), refs["kp_pay_b"], "now", &f.admin.ID); !errors.Is(err, domain.RunInProgress()) {
		t.Fatalf("run in progress: %v", err)
	}

	// Regression: a failed re-run keeps the earlier run count's items, which
	// are stale; resolving one would change the count of a failed run.
	f.fake.ListErrs = []error{provider.ErrUnknownOutcome, provider.ErrUnknownOutcome, provider.ErrUnknownOutcome}
	if err := f.execute(t, f.run(t, run.PublicID)); err != nil {
		t.Fatal(err)
	}
	failed := f.run(t, run.PublicID)
	if failed.Status != domain.RunFailed {
		t.Fatalf("re-run status = %s", failed.Status)
	}
	audits := len(f.audit.Logs())
	if _, err := f.svc.ResolveItem(adminCtx(), refs["kp_pay_b"], "now", &f.admin.ID); !errors.As(err, &coded) ||
		coded.Code != domain.CodeRunNotSucceeded || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("failed run: %v", err)
	}
	if len(f.audit.Logs()) != audits || f.run(t, run.PublicID).UnresolvedCount != failed.UnresolvedCount {
		t.Fatal("a resolution on a failed run wrote")
	}
}

func TestGetItem(t *testing.T) {
	f := newReconFixture(t)
	from, _ := f.window()
	f.paidPayment(t, "kp_pay_a", 5000, nil, from.Add(time.Hour))
	run := f.createRun(t)
	if err := f.execute(t, run); err != nil {
		t.Fatal(err)
	}
	items := f.runs.Items(run.ID)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	got, err := f.svc.GetItem(adminCtx(), items[0].PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicID != items[0].PublicID || got.RunID != run.ID || got.Reference != "kp_pay_a" ||
		got.Result != domain.ResultMissingInGateway {
		t.Fatalf("item = %+v", got)
	}
	if _, err := f.svc.GetItem(adminCtx(), id.New(id.ReconciliationItem)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestListItemsUnknownRun(t *testing.T) {
	f := newReconFixture(t)
	if _, err := f.svc.ListItems(context.Background(), id.New(id.Reconciliation), domain.ReconciliationItemFilter{},
		domain.PageRequest{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("list: %v", err)
	}
	called := false
	err := f.svc.ExportItems(context.Background(), id.New(id.Reconciliation), domain.ReconciliationItemFilter{},
		func(*domain.ReconciliationItem) error { called = true; return nil })
	if !errors.Is(err, domain.ErrNotFound) || called {
		t.Fatalf("export: %v, called %v", err, called)
	}
}
