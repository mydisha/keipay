package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeQueue holds task ids like asynq: scheduling a held id is ErrTaskExists;
// deleting an active one fails.
type fakeQueue struct {
	held      map[string]bool
	active    map[string]bool
	scheduled []scheduled
	deleted   []string
	err       error
}

func (q *fakeQueue) Schedule(_ context.Context, id int64, attempt int, manual bool, at time.Time) error {
	if q.err != nil {
		return q.err
	}
	tid := ForwardTaskID(id, attempt)
	if q.held[tid] {
		return ErrTaskExists
	}
	q.held[tid] = true
	q.scheduled = append(q.scheduled, scheduled{id: id, attempt: attempt, manual: manual, at: at})
	return nil
}

func (q *fakeQueue) DeleteTask(queue, id string) error {
	if queue != QueueWebhooks || q.active[id] {
		return errors.New("cannot delete active task")
	}
	delete(q.held, id)
	q.deleted = append(q.deleted, id)
	return nil
}

// Overdue deliveries get their next attempt queued now; a task
// still holding the id (archived) is replaced; an active one is left alone.
func TestSweeper(t *testing.T) {
	x := newFwdFixture(t, 5)
	ep := x.addEndpoint(t, nil)
	overdue := x.addDelivery(t, ep, false, 8)
	archived := x.addDelivery(t, ep, false, 8)
	running := x.addDelivery(t, ep, false, 8)
	x.clock.Advance(time.Minute)
	fresh := x.addDelivery(t, ep, false, 8)
	x.recv.status(500)
	if err := x.run(t, archived, 1, false); err != nil {
		t.Fatal(err)
	}
	// archived failed once and waits for attempt 2, whose task is archived.
	x.clock.Advance(3 * time.Minute)
	finished := x.addDelivery(t, ep, false, 8)
	x.recv.status(200)
	if err := x.run(t, finished, 1, false); err != nil {
		t.Fatal(err)
	}

	q := &fakeQueue{held: map[string]bool{ForwardTaskID(archived.ID, 2): true, ForwardTaskID(running.ID, 1): true},
		active: map[string]bool{ForwardTaskID(running.ID, 1): true}}
	s := NewSweeper(x.hooks.Deliveries(), q, q, zap.NewNop())
	s.now = x.clock.Now
	n, err := s.SweepOnce(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("swept %d, %v", n, err)
	}
	now := x.clock.Now()
	want := []scheduled{{id: overdue.ID, attempt: 1, at: now}, {id: fresh.ID, attempt: 1, at: now}, {id: archived.ID, attempt: 2, at: now}}
	got := map[int64]scheduled{}
	for _, s := range q.scheduled {
		got[s.id] = s
	}
	for _, w := range want {
		if got[w.id] != w {
			t.Fatalf("scheduled = %+v, want %+v", q.scheduled, want)
		}
	}
	if _, ok := got[running.ID]; ok || len(q.deleted) != 1 || q.deleted[0] != ForwardTaskID(archived.ID, 2) {
		t.Fatalf("deleted = %v scheduled = %+v", q.deleted, q.scheduled)
	}
	if _, ok := got[finished.ID]; ok {
		t.Fatal("a delivered delivery was swept")
	}
}

// Deliveries due less than two minutes ago are left to their own task.
func TestSweeperLeavesRecentDeliveries(t *testing.T) {
	x := newFwdFixture(t, 5)
	x.addDelivery(t, x.addEndpoint(t, nil), false, 8)
	x.clock.Advance(119 * time.Second)
	q := &fakeQueue{held: map[string]bool{}}
	s := NewSweeper(x.hooks.Deliveries(), q, q, zap.NewNop())
	s.now = x.clock.Now
	if n, err := s.SweepOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("swept %d, %v", n, err)
	}
}

func TestSweeperQueueDown(t *testing.T) {
	x := newFwdFixture(t, 5)
	x.addDelivery(t, x.addEndpoint(t, nil), false, 8)
	x.clock.Advance(time.Hour)
	q := &fakeQueue{held: map[string]bool{}, err: errors.New("redis down")}
	s := NewSweeper(x.hooks.Deliveries(), q, q, zap.NewNop())
	s.now = x.clock.Now
	if n, err := s.SweepOnce(context.Background()); err == nil || n != 0 {
		t.Fatalf("swept %d, %v", n, err)
	}
}

func TestSweeperRunStopsOnCancel(t *testing.T) {
	x := newFwdFixture(t, 5)
	q := &fakeQueue{held: map[string]bool{}}
	s := NewSweeper(x.hooks.Deliveries(), q, q, zap.NewNop())
	s.interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sweeper did not stop")
	}
}
