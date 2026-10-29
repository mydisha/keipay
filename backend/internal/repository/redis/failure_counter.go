package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// loginScope is the key scope of the sign-in and setup counters.
const loginScope = "loginfail"

// failureKeyPrefix builds the Redis key prefix of a scope.
func failureKeyPrefix(scope string) string { return "keipay:" + scope + ":" }

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

// blockedAttempt is the check half of reserveAttempt: it returns the longest
// PTTL among the counters at or over their maximum (ARGV[i]), or -1 when
// none is, and counts nothing.
var blockedAttempt = redis.NewScript(`
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
return wait
`)

// countFailure is the increment half of reserveAttempt.
var countFailure = redis.NewScript(`
for _, key in ipairs(KEYS) do
  redis.call('INCR', key)
  if redis.call('PTTL', key) < 0 then
    redis.call('PEXPIRE', key, ARGV[1])
  end
end
return 0
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

// FailureCounter is a Redis-backed domain.FailureCounter and
// domain.FailureLimiter with fixed windows. Keys are prefixed with
// keipay:<scope>:, so the counters of one use never meet another's.
type FailureCounter struct {
	rdb    *redis.Client
	prefix string
}

// NewFailureCounter constructs the sign-in and setup counter
// (keipay:loginfail:).
func NewFailureCounter(rdb *redis.Client) *FailureCounter {
	return NewScopedFailureCounter(rdb, loginScope)
}

// NewScopedFailureCounter constructs a counter under keipay:<scope>:.
func NewScopedFailureCounter(rdb *redis.Client, scope string) *FailureCounter {
	return &FailureCounter{rdb: rdb, prefix: failureKeyPrefix(scope)}
}

// limitArgs are the keys and arguments of the reserve and blocked scripts.
func (f *FailureCounter) limitArgs(window time.Duration, limits []domain.FailureLimit) ([]string, []any) {
	keys := make([]string, len(limits))
	args := make([]any, 0, len(limits)+1)
	args = append(args, window.Milliseconds())
	for i, l := range limits {
		keys[i] = f.prefix + l.Key
		args = append(args, l.Max)
	}
	return keys, args
}

func (f *FailureCounter) fullKeys(keys []string) []string {
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = f.prefix + k
	}
	return full
}

// Reserve counts one attempt under every limit, or none when a limit is
// already reached.
func (f *FailureCounter) Reserve(ctx context.Context, window time.Duration, limits ...domain.FailureLimit) (bool, time.Duration, error) {
	keys, args := f.limitArgs(window, limits)
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
	if err := releaseAttempt.Run(ctx, f.rdb, f.fullKeys(keys)).Err(); err != nil {
		return fmt.Errorf("release attempt: %w", err)
	}
	return nil
}

// Blocked reports whether any limit is reached, counting nothing. A counter
// found without an expiry is given one of a window, so it cannot block
// forever; the window only matters in that case.
func (f *FailureCounter) Blocked(ctx context.Context, limits ...domain.FailureLimit) (bool, time.Duration, error) {
	keys, args := f.limitArgs(blockedWindow, limits)
	wait, err := blockedAttempt.Run(ctx, f.rdb, keys, args...).Int64()
	if err != nil {
		return false, 0, fmt.Errorf("check limits: %w", err)
	}
	if wait < 0 {
		return false, 0, nil
	}
	return true, time.Duration(wait) * time.Millisecond, nil
}

// blockedWindow is the expiry Blocked gives a counter that has none.
const blockedWindow = 15 * time.Minute

// Fail counts one failure under every key.
func (f *FailureCounter) Fail(ctx context.Context, window time.Duration, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := countFailure.Run(ctx, f.rdb, f.fullKeys(keys), window.Milliseconds()).Err(); err != nil {
		return fmt.Errorf("count failure: %w", err)
	}
	return nil
}

// Reset clears the counter.
func (f *FailureCounter) Reset(ctx context.Context, key string) error {
	if err := f.rdb.Del(ctx, f.prefix+key).Err(); err != nil {
		return fmt.Errorf("reset failures: %w", err)
	}
	return nil
}
