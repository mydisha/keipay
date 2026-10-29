package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// TypeReconciliationRun is the asynq task type for one run count of a
// reconciliation run.
const TypeReconciliationRun = "reconciliation:run"

// QueueReconciliation is the queue reconciliation runs take, so a long run
// never holds up webhook forwarding.
const QueueReconciliation = "reconciliation"

// Limits of one reconciliation task. A retry could never claim the run
// again, since the failure is recorded on it, so there is none; an admin
// re-runs a failed or stalled run instead. The timeout leaves room for the
// largest range a run reads, and the run's heartbeat keeps it from looking
// stalled meanwhile.
const (
	reconciliationMaxRetry = 0
	reconciliationTimeout  = 60 * time.Minute
)

// ReconciliationPayload names the run count a task performs; the handler
// loads the run from Postgres.
type ReconciliationPayload struct {
	RunID    int64 `json:"run_id"`
	RunCount int   `json:"run_count"`
}

// NewReconciliationTask builds the task of one run count.
func NewReconciliationTask(runID int64, runCount int) (*asynq.Task, error) {
	b, err := json.Marshal(ReconciliationPayload{RunID: runID, RunCount: runCount})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeReconciliationRun, b), nil
}

// ReconciliationTaskID is the task id of one run count, so it is queued at
// most once.
func ReconciliationTaskID(runID int64, runCount int) string {
	return fmt.Sprintf("%s:%d:%d", TypeReconciliationRun, runID, runCount)
}

// EnqueueReconciliation queues one run count; one that is already queued is
// a no-op.
func (e *Enqueuer) EnqueueReconciliation(ctx context.Context, runID int64, runCount int) error {
	task, err := NewReconciliationTask(runID, runCount)
	if err != nil {
		return err
	}
	_, err = e.client.EnqueueContext(ctx, task, asynq.TaskID(ReconciliationTaskID(runID, runCount)),
		asynq.Queue(QueueReconciliation), asynq.MaxRetry(reconciliationMaxRetry), asynq.Timeout(reconciliationTimeout))
	if errors.Is(err, ErrTaskExists) {
		return nil
	}
	return err
}

// RunExecutor performs one run count of a reconciliation run.
type RunExecutor interface {
	Execute(ctx context.Context, runID int64, runCount int) error
}

// Reconciler executes reconciliation:run tasks.
type Reconciler struct {
	runs RunExecutor
}

// NewReconciler constructs a Reconciler.
func NewReconciler(runs RunExecutor) *Reconciler { return &Reconciler{runs: runs} }

// HandleReconciliationRun implements asynq.HandlerFunc for
// TypeReconciliationRun. A payload that cannot be read is never retried.
func (r *Reconciler) HandleReconciliationRun(ctx context.Context, task *asynq.Task) error {
	var p ReconciliationPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil || p.RunID <= 0 || p.RunCount < 1 {
		return fmt.Errorf("reconciliation task payload %q: %w", task.Payload(), asynq.SkipRetry)
	}
	return r.runs.Execute(ctx, p.RunID, p.RunCount)
}
