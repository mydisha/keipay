package worker

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

// Regression: reconciliation runs shared the webhook workers, so enough
// long runs held back every webhook forward. Runs now fill only their own
// workers and a forward is still handled at once.
func TestPoolKeepsReconciliationOffWebhookWorkersIntegration(t *testing.T) {
	addr := os.Getenv("KEI_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("KEI_TEST_REDIS_ADDR not set")
	}
	// A database of its own, so no other test's tasks reach these handlers.
	opt := asynq.RedisClientOpt{Addr: addr, DB: 7}
	inspector := asynq.NewInspector(opt)
	t.Cleanup(func() { _ = inspector.Close() })
	for _, q := range []string{QueueWebhooks, QueueReconciliation} {
		_ = inspector.DeleteQueue(q, true)
	}

	release := make(chan struct{})
	var running, peak atomic.Int32
	forwarded := make(chan struct{}, 1)
	pool, err := StartPool(opt, Handlers{
		WebhookForward: func(context.Context, *asynq.Task) error {
			forwarded <- struct{}{}
			return nil
		},
		ReconciliationRun: func(ctx context.Context, _ *asynq.Task) error {
			n := running.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			defer running.Add(-1)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(release)
		pool.Shutdown()
		for _, q := range []string{QueueWebhooks, QueueReconciliation} {
			_ = inspector.DeleteQueue(q, true)
		}
	})

	client := asynq.NewClient(opt)
	t.Cleanup(func() { _ = client.Close() })
	// More runs than all workers together.
	runs := WebhookConcurrency + ReconciliationConcurrency + 1
	for i := range runs {
		task := asynq.NewTask(TypeReconciliationRun, []byte(fmt.Sprintf(`{"run_id":%d}`, i)))
		if _, err := client.Enqueue(task, asynq.Queue(QueueReconciliation), asynq.MaxRetry(0)); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for running.Load() < ReconciliationConcurrency {
		if time.Now().After(deadline) {
			t.Fatalf("%d runs started, want %d", running.Load(), ReconciliationConcurrency)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := client.Enqueue(asynq.NewTask(TypeWebhookForward, []byte(`{"delivery_id":1}`)), asynq.Queue(QueueWebhooks)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-forwarded:
	case <-time.After(10 * time.Second):
		t.Fatal("webhook forward was not handled while runs held their workers")
	}
	if p := peak.Load(); p != ReconciliationConcurrency {
		t.Fatalf("%d runs ran at once, want %d", p, ReconciliationConcurrency)
	}
}
