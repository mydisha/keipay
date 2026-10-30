package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type countingChecker struct {
	calls atomic.Int32
	err   error
}

func (c *countingChecker) CheckOverdue(context.Context) (int, error) {
	c.calls.Add(1)
	return 0, c.err
}

// The checker runs on every tick, logs a failed listing and stops with its
// context.
func TestCheckoutCheckerRunsUntilCancelled(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	checks := &countingChecker{err: errors.New("pool closed")}
	c := NewCheckoutChecker(checks, zap.New(core))
	c.interval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for checks.calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("ran %d times", checks.calls.Load())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	if logs.FilterMessage("checkout check failed").Len() == 0 {
		t.Fatal("the failure was not logged")
	}
}
