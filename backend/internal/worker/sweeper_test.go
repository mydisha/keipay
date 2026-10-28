package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/domain"
)

type unsentRepo struct {
	domain.WebhookRepository
	unsent    []domain.WebhookDelivery
	olderThan time.Time
	limit     int
}

func (r *unsentRepo) ListUnsentDeliveries(_ context.Context, olderThan time.Time, limit int) ([]domain.WebhookDelivery, error) {
	r.olderThan, r.limit = olderThan, limit
	return r.unsent, nil
}

type recordingEnqueuer struct {
	ids []int64
	err error
}

func (e *recordingEnqueuer) EnqueueWebhookForward(_ context.Context, id int64) error {
	if e.err != nil {
		return e.err
	}
	e.ids = append(e.ids, id)
	return nil
}

func TestSweepOnceEnqueuesUnsent(t *testing.T) {
	repo := &unsentRepo{unsent: []domain.WebhookDelivery{{ID: 3}, {ID: 7}}}
	enq := &recordingEnqueuer{}
	s := NewSweeper(repo, enq, zap.NewNop())

	n, err := s.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 2 || len(enq.ids) != 2 || enq.ids[0] != 3 || enq.ids[1] != 7 {
		t.Fatalf("expected deliveries 3,7 enqueued, got n=%d ids=%v", n, enq.ids)
	}
	// Only deliveries older than minAge, so it never races the post-commit enqueue.
	if age := time.Since(repo.olderThan); age < s.minAge {
		t.Fatalf("cutoff too recent: %v", age)
	}
	if repo.limit != s.batch {
		t.Fatalf("expected batch limit %d, got %d", s.batch, repo.limit)
	}
}

func TestSweepOnceNothingToDo(t *testing.T) {
	enq := &recordingEnqueuer{}
	n, err := NewSweeper(&unsentRepo{}, enq, zap.NewNop()).SweepOnce(context.Background())
	if err != nil || n != 0 || len(enq.ids) != 0 {
		t.Fatalf("expected no-op, got n=%d err=%v ids=%v", n, err, enq.ids)
	}
}

func TestSweepOnceStopsOnEnqueueError(t *testing.T) {
	repo := &unsentRepo{unsent: []domain.WebhookDelivery{{ID: 3}, {ID: 7}}}
	enq := &recordingEnqueuer{err: errors.New("redis down")}
	n, err := NewSweeper(repo, enq, zap.NewNop()).SweepOnce(context.Background())
	if !errors.Is(err, enq.err) || n != 0 {
		t.Fatalf("expected enqueue error after 0, got n=%d err=%v", n, err)
	}
}
