package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Idempotency remembers keys for a TTL with Redis SET NX. The /v1 API uses it
// as its signature nonce store; gateway callbacks are deduplicated in
// Postgres instead.
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
// already existed. Errors fall through as true so requests aren't blocked by a
// transient Redis issue.
func (i *Idempotency) Acquire(ctx context.Context, key string) bool {
	ok, err := i.rdb.SetNX(ctx, "idemp:"+key, 1, i.ttl).Result()
	if err != nil {
		return true
	}
	return ok
}
