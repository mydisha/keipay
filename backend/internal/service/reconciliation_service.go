package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/id"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/provider"
)

// Reconciliation limits.
const (
	maxRunRange = 31 * 24 * time.Hour
	// stallAfter is how long a queued or running run may go unchanged before
	// it can be re-run: the worker is gone or its task was lost.
	stallAfter = 15 * time.Minute
	// readPadding widens the window a run reads on both sides, so a payment
	// KeiPay booked just inside the range and the gateway just outside it
	// (or the other way round) is still compared.
	readPadding       = time.Hour
	maxResolutionNote = 1000
	// failTimeout bounds marking a run failed after its own context ended.
	failTimeout = 10 * time.Second
	// heartbeatEvery is how often a run refreshes its updated_at while it
	// pulls gateway rows, well inside stallAfter.
	heartbeatEvery = time.Minute
)

// errRunSuperseded stops a run count whose run was re-run or failed while it
// was pulling gateway rows.
var errRunSuperseded = errors.New("reconciliation run superseded")

// Fixed messages of a failed run; gateway text only goes to the logs.
var runErrorMessages = map[string]string{
	domain.RunErrorProviderRejected:    "the payment provider rejected the request; check the connection's credentials",
	domain.RunErrorProviderUnavailable: "the payment provider could not be reached; re-run later",
	domain.RunErrorTooManyTransactions: "the range has more than 100000 gateway transactions; use a shorter range",
	domain.RunErrorNotQueued:           "the run could not be queued; re-run it",
	domain.RunErrorInternal:            "the run failed unexpectedly; re-run it",
}

// Refusals of a resolution, with messages for the console.
var (
	errMatchedItem = &domain.Error{Code: domain.CodeValidationFailed,
		Message: "a matched item has nothing to resolve", Err: domain.ErrInvalidInput}
	errItemResolved = &domain.Error{Code: domain.CodeConflict, Message: "the item is already resolved", Err: domain.ErrConflict}
)

// RunEnqueuer queues one run of a reconciliation. Queuing a run count that is
// already queued is a no-op.
type RunEnqueuer interface {
	EnqueueReconciliation(ctx context.Context, runID int64, runCount int) error
}

// ReconciliationDeps are the collaborators of a ReconciliationService.
type ReconciliationDeps struct {
	Runs        domain.ReconciliationRepository
	Connections domain.ConnectionRepository
	Registry    *provider.Registry
	Source      TransactionSource
	// Reports, when set, receives the fees a run learns from the gateway.
	Reports  domain.PaymentReportRepository
	Tx       domain.TxManager
	Enqueuer RunEnqueuer
	Audit    Auditor
	Log      *zap.Logger
}

// ReconciliationService starts, runs and shows reconciliation runs and
// resolves their items.
type ReconciliationService struct {
	runs     domain.ReconciliationRepository
	conns    domain.ConnectionRepository
	registry *provider.Registry
	source   TransactionSource
	reports  domain.PaymentReportRepository
	tx       domain.TxManager
	enqueuer RunEnqueuer
	audit    Auditor
	log      *zap.Logger
	now      func() time.Time
}

// NewReconciliationService constructs a ReconciliationService.
func NewReconciliationService(d ReconciliationDeps) *ReconciliationService {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &ReconciliationService{runs: d.Runs, conns: d.Connections, registry: d.Registry, source: d.Source,
		reports: d.Reports, tx: d.Tx, enqueuer: d.Enqueuer, audit: d.Audit, log: log, now: time.Now}
}

// CreateRunInput asks for a run of one connection over [From, To), both RFC
// 3339. CreatedByID is the console user starting it.
type CreateRunInput struct {
	ConnectionID string
	From         string
	To           string
	CreatedByID  *int64
}

// runCreated is the audited state of a new run.
type runCreated struct {
	ID           string                         `json:"id"`
	ConnectionID string                         `json:"connection_id"`
	From         time.Time                      `json:"from"`
	To           time.Time                      `json:"to"`
	Status       domain.ReconciliationRunStatus `json:"status"`
}

// runState is the audited state of a run around a re-run.
type runState struct {
	Status   domain.ReconciliationRunStatus `json:"status"`
	RunCount int                            `json:"run_count"`
}

// itemState is the audited state of an item around its resolution.
type itemState struct {
	Result   domain.ReconciliationResult `json:"result"`
	Resolved bool                        `json:"resolved"`
	Note     *string                     `json:"note,omitempty"`
}

// CreateRun stores a queued run and queues it. A run that cannot be queued
// is returned failed with error code not_queued.
func (s *ReconciliationService) CreateRun(ctx context.Context, in CreateRunInput) (*domain.ReconciliationRun, error) {
	conn, from, to, err := s.validateRun(ctx, in)
	if err != nil {
		return nil, err
	}
	run := &domain.ReconciliationRun{
		PublicID: id.New(id.Reconciliation), ConnectionID: conn.ID, From: from, To: to,
		Status: domain.RunQueued, RunCount: 1, CreatedByID: in.CreatedByID,
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.runs.CreateRun(ctx, run); err != nil {
			return err
		}
		after := runCreated{ID: run.PublicID, ConnectionID: conn.PublicID, From: from, To: to, Status: run.Status}
		return s.audit.Record(ctx, ActionReconciliationRunCreate,
			domain.AuditTarget{Type: TargetReconciliationRun, ID: run.PublicID}, nil, after)
	})
	if err != nil {
		return nil, fmt.Errorf("create reconciliation run: %w", err)
	}
	return s.enqueue(ctx, run)
}

// validateRun checks the input and returns the connection and the range in
// UTC.
func (s *ReconciliationService) validateRun(ctx context.Context, in CreateRunInput) (*domain.Connection, time.Time, time.Time, error) {
	var errs fieldErrors
	conn, rule, err := s.runConnection(ctx, in.ConnectionID)
	if err != nil {
		return nil, time.Time{}, time.Time{}, err
	}
	errs.check("connection_id", rule)
	from, fromRule := parseInstant(in.From)
	to, toRule := parseInstant(in.To)
	if fromRule == "" && toRule == "" {
		switch {
		case !to.After(from):
			toRule = RuleAfter
		case to.Sub(from) > maxRunRange:
			toRule = RuleMaxRange
		}
	}
	if fromRule == "" && !from.Before(s.now()) {
		fromRule = RulePast
	}
	errs.check("from", fromRule)
	errs.check("to", toRule)
	if err := errs.err(); err != nil {
		return nil, time.Time{}, time.Time{}, err
	}
	return conn, from, to, nil
}

// runConnection finds the connection a run is for and returns the failed
// rule, or "". Disabled connections can be reconciled.
func (s *ReconciliationService) runConnection(ctx context.Context, raw string) (*domain.Connection, string, error) {
	if raw == "" {
		return nil, RuleRequired, nil
	}
	publicID, err := id.ParseWithPrefix(raw, id.Connection)
	if err != nil {
		return nil, RulePublicID, nil
	}
	conn, err := s.conns.GetByPublicID(ctx, publicID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, RuleExists, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("load connection: %w", err)
	}
	prov, err := s.registry.Get(provider.Name(conn.Provider))
	if err != nil || !prov.Capabilities().ListTransactions {
		return nil, RuleUnsupported, nil
	}
	return conn, "", nil
}

// parseInstant reads an RFC 3339 time as stored: UTC, microseconds.
func parseInstant(s string) (time.Time, string) {
	if s == "" {
		return time.Time{}, RuleRequired
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, RuleDatetime
	}
	return t.UTC().Truncate(time.Microsecond), ""
}

// Rerun queues a run that is not in progress again, as the next run count.
// A run queued or running without a change for 15 minutes counts as stalled
// and can be re-run too.
func (s *ReconciliationService) Rerun(ctx context.Context, publicID string) (*domain.ReconciliationRun, error) {
	var run *domain.ReconciliationRun
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		run, err = s.runs.GetRunForUpdate(ctx, publicID)
		if err != nil {
			return err
		}
		if run.Status.Active() && s.now().Sub(run.UpdatedAt) < stallAfter {
			return domain.RunInProgress()
		}
		before := runState{Status: run.Status, RunCount: run.RunCount}
		run.RunCount++
		run.Status, run.Summary, run.Error, run.FinishedAt = domain.RunQueued, nil, nil, nil
		if err := s.runs.UpdateRun(ctx, run); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionReconciliationRunRerun, domain.AuditTarget{Type: TargetReconciliationRun, ID: run.PublicID},
			before, runState{Status: run.Status, RunCount: run.RunCount})
	})
	if err != nil {
		return nil, fmt.Errorf("rerun reconciliation run: %w", err)
	}
	return s.enqueue(ctx, run)
}

// enqueue queues the committed run and returns it as stored. When the queue
// is unavailable the run is failed with not_queued, so it can be re-run
// instead of waiting for a worker that will never see it.
func (s *ReconciliationService) enqueue(ctx context.Context, run *domain.ReconciliationRun) (*domain.ReconciliationRun, error) {
	if err := s.enqueuer.EnqueueReconciliation(ctx, run.ID, run.RunCount); err != nil {
		logger.Ctx(ctx, s.log).Warn("reconciliation run not queued",
			zap.String(logger.FieldLogType, logger.LogTypeApp),
			zap.String(logger.FieldEvent, "reconciliation.run.not_queued"),
			zap.String("run_id", run.PublicID),
			zap.Error(err),
		)
		if _, err := s.runs.FailRun(ctx, run.ID, run.RunCount, runError(domain.RunErrorNotQueued)); err != nil {
			return nil, fmt.Errorf("fail unqueued run: %w", err)
		}
	}
	stored, err := s.runs.GetRun(ctx, run.PublicID)
	if err != nil {
		return nil, fmt.Errorf("load reconciliation run: %w", err)
	}
	return stored, nil
}

func runError(code string) domain.RunError {
	return domain.RunError{Code: code, Message: runErrorMessages[code]}
}

// Get returns a run.
func (s *ReconciliationService) Get(ctx context.Context, publicID string) (*domain.ReconciliationRun, error) {
	run, err := s.runs.GetRun(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("get reconciliation run: %w", err)
	}
	return run, nil
}

// GetItem returns an item by its rci_ id, whatever run it belongs to.
func (s *ReconciliationService) GetItem(ctx context.Context, publicID string) (*domain.ReconciliationItem, error) {
	item, err := s.runs.GetItem(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("get reconciliation item: %w", err)
	}
	return item, nil
}

// List returns one page of runs, newest first.
func (s *ReconciliationService) List(ctx context.Context, f domain.ReconciliationRunFilter, req domain.PageRequest) (domain.PageResult[domain.ReconciliationRun], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.ReconciliationRun]{}, err
	}
	res, err := s.runs.ListRuns(ctx, f, p)
	if err != nil {
		return domain.PageResult[domain.ReconciliationRun]{}, fmt.Errorf("list reconciliation runs: %w", err)
	}
	return res, nil
}

// ListItems returns one page of a run's items, newest first.
func (s *ReconciliationService) ListItems(ctx context.Context, runPublicID string, f domain.ReconciliationItemFilter,
	req domain.PageRequest) (domain.PageResult[domain.ReconciliationItem], error) {
	p, err := req.Page()
	if err != nil {
		return domain.PageResult[domain.ReconciliationItem]{}, err
	}
	run, err := s.Get(ctx, runPublicID)
	if err != nil {
		return domain.PageResult[domain.ReconciliationItem]{}, err
	}
	res, err := s.runs.ListItems(ctx, run.ID, f, p)
	if err != nil {
		return domain.PageResult[domain.ReconciliationItem]{}, fmt.Errorf("list reconciliation items: %w", err)
	}
	return res, nil
}

// ExportItems calls fn with every matching item of a run. An unknown run
// fails before fn is called.
func (s *ReconciliationService) ExportItems(ctx context.Context, runPublicID string, f domain.ReconciliationItemFilter,
	fn func(*domain.ReconciliationItem) error) error {
	run, err := s.Get(ctx, runPublicID)
	if err != nil {
		return err
	}
	return s.runs.StreamItems(ctx, run.ID, f, fn)
}

// ResolveItem marks a difference as resolved by resolvedByID with a note,
// and takes it off the run's unresolved count.
func (s *ReconciliationService) ResolveItem(ctx context.Context, publicID, note string, resolvedByID *int64) (*domain.ReconciliationItem, error) {
	note, rule := normalizeNote(note)
	if rule != "" {
		return nil, domain.InvalidFields(domain.FieldError{Field: "note", Rule: rule})
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// The run is locked before the item, in the order the worker takes
		// them, so a resolution and a run finishing never deadlock.
		item, err := s.runs.GetItem(ctx, publicID)
		if err != nil {
			return err
		}
		run, err := s.runs.GetRunByIDForUpdate(ctx, item.RunID)
		if err != nil {
			return err
		}
		if item, err = s.runs.GetItemForUpdate(ctx, publicID); err != nil {
			return err
		}
		switch {
		case run.Status.Active():
			return domain.RunInProgress()
		case run.Status != domain.RunSucceeded:
			return domain.RunNotSucceeded()
		case item.Result == domain.ResultMatched:
			return errMatchedItem
		case item.Resolution != nil:
			return errItemResolved
		}
		item.Resolution = &domain.Resolution{Note: note, ResolvedByID: resolvedByID, ResolvedAt: s.now().UTC().Truncate(time.Microsecond)}
		if err := s.runs.ResolveItem(ctx, item); err != nil {
			return err
		}
		if run.UnresolvedCount > 0 {
			run.UnresolvedCount--
		}
		if err := s.runs.UpdateRun(ctx, run); err != nil {
			return err
		}
		return s.audit.Record(ctx, ActionReconciliationItemResolve, domain.AuditTarget{Type: TargetReconciliationItem, ID: item.PublicID},
			itemState{Result: item.Result}, itemState{Result: item.Result, Resolved: true, Note: &note})
	})
	if err != nil {
		return nil, fmt.Errorf("resolve reconciliation item: %w", err)
	}
	item, err := s.runs.GetItem(ctx, publicID)
	if err != nil {
		return nil, fmt.Errorf("load reconciliation item: %w", err)
	}
	return item, nil
}

// normalizeNote trims a resolution note and returns it with the failed rule,
// or "". Postgres text cannot hold U+0000.
func normalizeNote(s string) (string, string) {
	note := strings.TrimSpace(s)
	switch {
	case note == "":
		return "", RuleRequired
	case utf8.RuneCountInString(note) > maxResolutionNote:
		return note, RuleMax
	case !utf8.ValidString(note) || strings.ContainsRune(note, 0):
		return note, RuleInvalidCharacter
	}
	return note, ""
}

// Execute performs run count runCount of a run: it pulls the gateway's
// transactions, compares them with KeiPay's payments and stores the items
// and summary. A task for an older run count, or for a run another worker
// already claimed, does nothing, and so does one whose run is re-run or
// failed while it works. Every failure is recorded on the run and returns
// nil: the task is never retried, a re-run is the recovery. Only a failed
// claim or a failure that cannot be recorded is returned.
func (s *ReconciliationService) Execute(ctx context.Context, runID int64, runCount int) error {
	claimed, err := s.runs.ClaimRun(ctx, runID, runCount)
	if err != nil {
		return fmt.Errorf("claim reconciliation run %d: %w", runID, err)
	}
	if !claimed {
		logger.Ctx(ctx, s.log).Info("stale reconciliation task skipped",
			zap.String(logger.FieldLogType, logger.LogTypeApp),
			zap.String(logger.FieldEvent, "reconciliation.run.stale"),
			zap.Int64("run", runID), zap.Int("run_count", runCount))
		return nil
	}
	run, items, err := s.compute(ctx, runID, runCount)
	if err == nil {
		err = s.finish(ctx, run, runCount, items)
	}
	if errors.Is(err, errRunSuperseded) {
		s.logSuperseded(ctx, runID, runCount)
		return nil
	}
	if err != nil {
		return s.fail(ctx, runID, runCount, err)
	}
	return nil
}

// compute loads the run and returns its items. It keeps the run's
// updated_at fresh while it pulls the gateway rows.
func (s *ReconciliationService) compute(ctx context.Context, runID int64, runCount int) (*domain.ReconciliationRun, []domain.ReconciliationItem, error) {
	run, err := s.runs.GetRunByID(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("load run: %w", err)
	}
	conn, err := s.conns.GetByID(ctx, run.ConnectionID)
	if err != nil {
		return nil, nil, fmt.Errorf("load connection: %w", err)
	}
	from, to := run.From.Add(-readPadding), run.To.Add(readPadding)

	var gateway []domain.GatewayTransaction
	refs := map[string]bool{}
	beat := heartbeat{runs: s.runs, now: s.now, runID: runID, runCount: runCount, last: s.now()}
	err = s.source.Transactions(ctx, conn, from, to, func(t domain.GatewayTransaction) error {
		if err := beat.tick(ctx); err != nil {
			return err
		}
		gateway = append(gateway, t)
		refs[t.Reference] = true
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read gateway transactions: %w", err)
	}
	refList := make([]string, 0, len(refs))
	for ref := range refs {
		refList = append(refList, ref)
	}
	keipay, err := s.runs.KeipayPayments(ctx, conn.ID, from, to, refList)
	if err != nil {
		return nil, nil, fmt.Errorf("load keipay payments: %w", err)
	}
	items := matchReconciliation(run.From, run.To, keipay, gateway)
	for i := range items {
		items[i].PublicID = id.New(id.ReconciliationItem)
	}
	return run, items, nil
}

// finish stores the items and the summary, unless the run was re-run or
// failed meanwhile: then the newer run count owns it and this result is
// dropped.
func (s *ReconciliationService) finish(ctx context.Context, loaded *domain.ReconciliationRun, runCount int, items []domain.ReconciliationItem) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		run, err := s.runs.GetRunByIDForUpdate(ctx, loaded.ID)
		if err != nil {
			return err
		}
		if run.RunCount != runCount || run.Status != domain.RunRunning {
			return errRunSuperseded
		}
		unresolved, err := s.runs.SaveItems(ctx, run.ID, items)
		if err != nil {
			return err
		}
		if err := s.fillFees(ctx, items); err != nil {
			return err
		}
		finished := s.now().UTC().Truncate(time.Microsecond)
		run.Status, run.Summary, run.UnresolvedCount, run.Error, run.FinishedAt =
			domain.RunSucceeded, domain.NewReconciliationSummary(items), unresolved, nil, &finished
		return s.runs.UpdateRun(ctx, run)
	})
}

// fillFees stores the gateway's fee on reports whose fee the callback did
// not carry. Only a matched item with exactly one gateway transaction
// qualifies, so the fee is that payment's own and never a sum over a
// duplicate; a fee above the gross is left alone. The items keep the
// snapshot they were compared with.
func (s *ReconciliationService) fillFees(ctx context.Context, items []domain.ReconciliationItem) error {
	if s.reports == nil {
		return nil
	}
	for _, it := range items {
		if it.Result != domain.ResultMatched || it.PaymentID == nil || it.Keipay == nil || it.Keipay.Fee != nil ||
			it.Gateway == nil || it.Gateway.Fee == nil || it.Gateway.TransactionCount != 1 {
			continue
		}
		fee := *it.Gateway.Fee
		if fee < 0 || fee > it.Keipay.Amount {
			continue
		}
		if _, err := s.reports.FillFee(ctx, *it.PaymentID, fee); err != nil {
			return fmt.Errorf("fill fee of %s: %w", it.Reference, err)
		}
	}
	return nil
}

func (s *ReconciliationService) logSuperseded(ctx context.Context, runID int64, runCount int) {
	logger.Ctx(ctx, s.log).Info("superseded reconciliation result dropped",
		zap.String(logger.FieldLogType, logger.LogTypeApp),
		zap.String(logger.FieldEvent, "reconciliation.run.superseded"),
		zap.Int64("run", runID), zap.Int("run_count", runCount))
}

// heartbeat refreshes a running run's updated_at at most once per
// heartbeatEvery. last starts at the claim, which set updated_at.
type heartbeat struct {
	runs     domain.ReconciliationRepository
	now      func() time.Time
	runID    int64
	runCount int
	last     time.Time
}

// tick touches the run when heartbeatEvery has passed since the last touch.
// It returns errRunSuperseded when the run count no longer runs.
func (h *heartbeat) tick(ctx context.Context) error {
	now := h.now()
	if now.Sub(h.last) < heartbeatEvery {
		return nil
	}
	alive, err := h.runs.TouchRun(ctx, h.runID, h.runCount)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	if !alive {
		return errRunSuperseded
	}
	h.last = now
	return nil
}

// fail marks the run failed with the code of cause and returns nil, or the
// error of recording it. The run's own context may have ended (a task
// timeout is a cause too), so it is written with a fresh deadline.
func (s *ReconciliationService) fail(ctx context.Context, runID int64, runCount int, cause error) error {
	code := runErrorCode(cause)
	logger.Ctx(ctx, s.log).Warn("reconciliation run failed",
		zap.String(logger.FieldLogType, logger.LogTypeApp),
		zap.String(logger.FieldEvent, "reconciliation.run.failed"),
		zap.Int64("run", runID), zap.Int("run_count", runCount), zap.String("code", code),
		zap.Error(cause))
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failTimeout)
	defer cancel()
	if _, err := s.runs.FailRun(fctx, runID, runCount, runError(code)); err != nil {
		return fmt.Errorf("fail reconciliation run %d: %w (cause: %w)", runID, err, cause)
	}
	return nil
}

// runErrorCode maps a run failure to the code shown on the run.
func runErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrTooManyTransactions):
		return domain.RunErrorTooManyTransactions
	case errors.Is(err, provider.ErrRejected):
		return domain.RunErrorProviderRejected
	case errors.Is(err, provider.ErrRetryable), errors.Is(err, provider.ErrUnknownOutcome),
		errors.Is(err, context.DeadlineExceeded):
		return domain.RunErrorProviderUnavailable
	default:
		return domain.RunErrorInternal
	}
}
