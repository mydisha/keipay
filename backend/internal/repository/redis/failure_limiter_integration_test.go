package redis

import (
	"context"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// Blocked reads the counters Fail writes, under the scope's own prefix.
func TestIntegrationFailureLimiter(t *testing.T) {
	rdb := testRedis(t)
	fc := NewScopedFailureCounter(rdb, "v1fail")
	ctx := context.Background()
	key, other := randomToken(t), randomToken(t)
	t.Cleanup(func() {
		_ = fc.Reset(context.Background(), key)
		_ = fc.Reset(context.Background(), other)
	})
	window := time.Minute
	limits := []domain.FailureLimit{{Key: key, Max: 2}, {Key: other, Max: 10}}

	if blocked, _, err := fc.Blocked(ctx, window, limits...); err != nil || blocked {
		t.Fatalf("fresh: blocked %v, %v", blocked, err)
	}
	for i := 0; i < 2; i++ {
		if err := fc.Fail(ctx, window, key, other); err != nil {
			t.Fatal(err)
		}
	}
	full := failureKeyPrefix("v1fail") + key
	if n := rdb.Get(ctx, full).Val(); n != "2" {
		t.Fatalf("%s = %q, want 2 under the scope's prefix", full, n)
	}
	if ttl := rdb.PTTL(ctx, full).Val(); ttl <= 0 || ttl > window {
		t.Fatalf("ttl = %v", ttl)
	}
	blocked, wait, err := fc.Blocked(ctx, window, limits...)
	if err != nil || !blocked || wait <= 0 || wait > window {
		t.Fatalf("at the limit: blocked %v, wait %v, %v", blocked, wait, err)
	}
	// Blocked counts nothing, and the other key is not over its limit.
	if n := rdb.Get(ctx, full).Val(); n != "2" {
		t.Fatalf("Blocked changed the counter to %q", n)
	}
	if blocked, _, _ := fc.Blocked(ctx, window, domain.FailureLimit{Key: other, Max: 10}); blocked {
		t.Fatal("the other key is blocked")
	}

	// A counter left without an expiry gets one rather than blocking forever.
	if err := rdb.Persist(ctx, full).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fc.Blocked(ctx, window, limits...); err != nil {
		t.Fatal(err)
	}
	if ttl := rdb.PTTL(ctx, full).Val(); ttl <= 0 {
		t.Fatalf("ttl after Blocked = %v, want an expiry", ttl)
	}

	// The login counter lives under its own prefix: the same key is free there.
	if blocked, _, _ := NewFailureCounter(rdb).Blocked(ctx, window, domain.FailureLimit{Key: key, Max: 1}); blocked {
		t.Fatal("scopes share counters")
	}
	if err := fc.Fail(ctx, window); err != nil {
		t.Fatalf("Fail with no keys: %v", err)
	}
}
