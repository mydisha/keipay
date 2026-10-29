package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type recordingExecutor struct {
	calls [][2]int64
	err   error
}

func (e *recordingExecutor) Execute(_ context.Context, runID int64, runCount int) error {
	e.calls = append(e.calls, [2]int64{runID, int64(runCount)})
	return e.err
}

// The task names one run count; its id makes it unique.
func TestReconciliationTask(t *testing.T) {
	task, err := NewReconciliationTask(42, 3)
	if err != nil {
		t.Fatal(err)
	}
	var p ReconciliationPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil || task.Type() != "reconciliation:run" || p.RunID != 42 || p.RunCount != 3 {
		t.Fatalf("task = %s %s (%v)", task.Type(), task.Payload(), err)
	}
	if got := ReconciliationTaskID(42, 3); got != "reconciliation:run:42:3" {
		t.Fatalf("task id = %s", got)
	}

	exec := &recordingExecutor{}
	if err := NewReconciler(exec).HandleReconciliationRun(context.Background(), task); err != nil || len(exec.calls) != 1 ||
		exec.calls[0] != [2]int64{42, 3} {
		t.Fatalf("handled: %v %v", exec.calls, err)
	}
	exec.err = errors.New("db down")
	if err := NewReconciler(exec).HandleReconciliationRun(context.Background(), task); !errors.Is(err, exec.err) {
		t.Fatalf("executor error = %v", err)
	}
	for _, payload := range []string{`not json`, `{"run_id":0,"run_count":1}`, `{"run_id":1,"run_count":0}`} {
		err := NewReconciler(exec).HandleReconciliationRun(context.Background(), asynq.NewTask(TypeReconciliationRun, []byte(payload)))
		if !errors.Is(err, asynq.SkipRetry) {
			t.Fatalf("%s: err = %v, want SkipRetry", payload, err)
		}
	}
}

// Queuing the same run count twice is a no-op; the task is on its own
// queue, is never retried and may run for an hour.
func TestEnqueueReconciliationIntegration(t *testing.T) {
	addr := os.Getenv("KEI_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("KEI_TEST_REDIS_ADDR not set")
	}
	opt := asynq.RedisClientOpt{Addr: addr}
	e := NewEnqueuer(opt)
	t.Cleanup(func() { _ = e.Close() })
	inspector := asynq.NewInspector(opt)
	t.Cleanup(func() { _ = inspector.Close() })

	// A run id unique to this test run, so reruns never see an old task.
	runID := time.Now().UnixNano()
	ctx := context.Background()
	for range 2 {
		if err := e.EnqueueReconciliation(ctx, runID, 1); err != nil {
			t.Fatal(err)
		}
	}
	taskID := ReconciliationTaskID(runID, 1)
	t.Cleanup(func() { _ = inspector.DeleteTask(QueueReconciliation, taskID) })
	info, err := inspector.GetTaskInfo(QueueReconciliation, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Queue != "reconciliation" || info.MaxRetry != 0 || info.Timeout != 60*time.Minute || info.Type != TypeReconciliationRun {
		t.Fatalf("task = %+v", info)
	}
	pending, err := inspector.ListPendingTasks(QueueReconciliation, asynq.PageSize(1000))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range pending {
		if p.ID == taskID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d tasks with id %s", n, taskID)
	}
	if err := e.EnqueueReconciliation(ctx, runID, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inspector.DeleteTask(QueueReconciliation, ReconciliationTaskID(runID, 2)) })
}
