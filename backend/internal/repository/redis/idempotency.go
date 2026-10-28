package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Idempotency provides fast first-line dedup using Redis SET NX, backed by the
// durable webhook_events table for the source of truth.
type Idempotency struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewIdempotency constructs an Idempotency helper with the given key TTL.
func NewIdempotency(rdb *redis.Client, ttl time.Duration) *Idempotency {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Idempotency{rdb: rdb, ttl: ttl}
}

// Acquire returns true if key was newly set (first time seen), false if it
// already existed. Errors fall through as true so processing isn't blocked by a
// transient Redis issue; the DB unique constraint remains the final guard.
func (i *Idempotency) Acquire(ctx context.Context, key string) bool {
	ok, err := i.rdb.SetNX(ctx, "idemp:"+key, 1, i.ttl).Result()
	if err != nil {
		return true
	}
	return ok
}

// Seen reports whether key was marked as processed. Errors read as not seen so
// a transient Redis issue falls through to the durable DB guard.
func (i *Idempotency) Seen(ctx context.Context, key string) bool {
	n, err := i.rdb.Exists(ctx, "idemp:"+key).Result()
	return err == nil && n > 0
}

// Mark records key as processed. Call it only after the work has committed, so a
// failed attempt never short-circuits the retry. Errors are ignored: the DB
// guard still rejects the duplicate.
func (i *Idempotency) Mark(ctx context.Context, key string) {
	_ = i.rdb.Set(ctx, "idemp:"+key, 1, i.ttl).Err()
}
