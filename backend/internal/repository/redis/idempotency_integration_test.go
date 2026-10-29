package redis

import (
	"context"
	"testing"
	"time"
)

// A nonce is new once and remembered for the TTL; a store that cannot answer
// says so instead of letting the request through.
func TestIntegrationIdempotencyAcquire(t *testing.T) {
	rdb := testRedis(t)
	ttl := 2 * time.Minute
	store := NewIdempotency(rdb, ttl)
	ctx := context.Background()
	key := "nonce:" + randomToken(t)
	t.Cleanup(func() { _ = rdb.Del(context.Background(), "idemp:"+key) })

	if fresh, err := store.Acquire(ctx, key); err != nil || !fresh {
		t.Fatalf("first: fresh %v, %v", fresh, err)
	}
	if fresh, err := store.Acquire(ctx, key); err != nil || fresh {
		t.Fatalf("second: fresh %v, %v", fresh, err)
	}
	if got := rdb.PTTL(ctx, "idemp:"+key).Val(); got <= 0 || got > ttl {
		t.Fatalf("ttl = %v, want within %v", got, ttl)
	}

	closed, err := New(ctx, rdb.Options().Addr, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	if fresh, err := NewIdempotency(closed, ttl).Acquire(ctx, key+"-down"); err == nil || fresh {
		t.Fatalf("closed client: fresh %v, err %v, want an error", fresh, err)
	}
}
