package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mydisha/keipay/backend/internal/domain"
)

const failureKeyPrefix = "keipay:loginfail:"

// reserveAttempt checks every counter against its maximum (ARGV[i+1]) and,
// only when none is exceeded, increments them all. It returns -1 when the
// attempt was counted, else the longest PTTL among the exceeded counters.
// Running check and increment in one script is what makes the limit hold
// under concurrency. Any counter it touches without an expiry gets one, so a
// crash can never lock a key forever.
var reserveAttempt = redis.NewScript(`
local window = tonumber(ARGV[1])
local wait = -1
for i, key in ipairs(KEYS) do
  local n = tonumber(redis.call('GET', key) or '0')
  if n >= tonumber(ARGV[i + 1]) then
    local ttl = redis.call('PTTL', key)
    if ttl < 0 then
      redis.call('PEXPIRE', key, window)
      ttl = window
    end
    if ttl > wait then wait = ttl end
  end
end
if wait >= 0 then
  return wait
end
for _, key in ipairs(KEYS) do
  redis.call('INCR', key)
  if redis.call('PTTL', key) < 0 then
    redis.call('PEXPIRE', key, window)
  end
end
return -1
`)

// releaseAttempt decrements existing counters and deletes those that reach
// zero. A counter whose window already ended is left alone rather than being
// recreated without an expiry.
var releaseAttempt = redis.NewScript(`
for _, key in ipairs(KEYS) do
  if redis.call('EXISTS', key) == 1 and redis.call('DECR', key) <= 0 then
    redis.call('DEL', key)
  end
end
return 0
`)

// FailureCounter is a Redis-backed domain.FailureCounter with fixed windows.
// Keys are prefixed with keipay:loginfail:.
type FailureCounter struct {
	rdb *redis.Client
}

// NewFailureCounter constructs a FailureCounter.
func NewFailureCounter(rdb *redis.Client) *FailureCounter { return &FailureCounter{rdb: rdb} }

// Reserve counts one attempt under every limit, or none when a limit is
// already reached.
func (f *FailureCounter) Reserve(ctx context.Context, window time.Duration, limits ...domain.FailureLimit) (bool, time.Duration, error) {
	keys := make([]string, len(limits))
	args := make([]any, 0, len(limits)+1)
	args = append(args, window.Milliseconds())
	for i, l := range limits {
		keys[i] = failureKeyPrefix + l.Key
		args = append(args, l.Max)
	}
	wait, err := reserveAttempt.Run(ctx, f.rdb, keys, args...).Int64()
	if err != nil {
		return false, 0, fmt.Errorf("reserve attempt: %w", err)
	}
	if wait < 0 {
		return true, 0, nil
	}
	return false, time.Duration(wait) * time.Millisecond, nil
}

// Release takes back one reserved attempt per key.
func (f *FailureCounter) Release(ctx context.Context, keys ...string) error {
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = failureKeyPrefix + k
	}
	if err := releaseAttempt.Run(ctx, f.rdb, full).Err(); err != nil {
		return fmt.Errorf("release attempt: %w", err)
	}
	return nil
}

// Reset clears the counter.
func (f *FailureCounter) Reset(ctx context.Context, key string) error {
	if err := f.rdb.Del(ctx, failureKeyPrefix+key).Err(); err != nil {
		return fmt.Errorf("reset failures: %w", err)
	}
	return nil
}
