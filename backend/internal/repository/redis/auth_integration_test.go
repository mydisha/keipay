package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mydisha/keipay/backend/internal/domain"
)

func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("KEI_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("KEI_TEST_REDIS_ADDR not set")
	}
	rdb, err := New(context.Background(), addr, "", 0)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func randomToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "it-" + hex.EncodeToString(b)
}

func TestIntegrationSessionStore(t *testing.T) {
	rdb := testRedis(t)
	store := NewSessionStore(rdb)
	ctx := context.Background()
	token := randomToken(t)
	t.Cleanup(func() { _ = store.Delete(context.Background(), token) })

	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	sess := &domain.Session{
		UserID: 7, UserPublicID: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", SessionVersion: 3, CSRFToken: "csrf-value",
		CreatedAt: now, LastSeenAt: now, IP: "203.0.113.5", UserAgent: "it-agent",
	}
	if err := store.Create(ctx, token, sess, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, token)
	if err != nil || *got != *sess {
		t.Fatalf("get = %+v, %v", got, err)
	}

	key := sessionKey(token)
	if !strings.HasPrefix(key, "keipay:session:") || len(key) != len("keipay:session:")+64 || strings.Contains(key, token) {
		t.Fatalf("key = %q", key)
	}
	keys, err := rdb.Keys(ctx, "keipay:session:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range keys {
		if strings.Contains(k, token) {
			t.Fatalf("raw token in key %q", k)
		}
		v, _ := rdb.Get(ctx, k).Result()
		if strings.Contains(v, token) {
			t.Fatalf("raw token in value of %q", k)
		}
		found = found || k == key
	}
	if !found {
		t.Fatal("session key not listed")
	}
	if ttl := rdb.TTL(ctx, key).Val(); ttl <= 0 || ttl > time.Minute {
		t.Fatalf("ttl = %v", ttl)
	}

	sess.LastSeenAt = now.Add(time.Minute)
	if err := store.Touch(ctx, token, sess, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if ttl := rdb.TTL(ctx, key).Val(); ttl <= 9*time.Minute {
		t.Fatalf("ttl after touch = %v", ttl)
	}
	if got, _ := store.Get(ctx, token); !got.LastSeenAt.Equal(sess.LastSeenAt) {
		t.Fatalf("last_seen_at = %v", got.LastSeenAt)
	}

	if err := store.Delete(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := store.Touch(ctx, token, sess, time.Minute); err != nil {
		t.Fatal(err)
	}
	if rdb.Exists(ctx, key).Val() != 0 {
		t.Fatal("touch recreated a deleted session")
	}
	if err := store.Delete(ctx, token); err != nil {
		t.Fatalf("deleting a missing session: %v", err)
	}
}

func TestIntegrationFailureCounter(t *testing.T) {
	rdb := testRedis(t)
	fc := NewFailureCounter(rdb)
	ctx := context.Background()
	key, other := "ip:"+randomToken(t), "email:"+randomToken(t)
	full := failureKeyPrefix + key
	t.Cleanup(func() {
		_ = fc.Reset(context.Background(), key)
		_ = fc.Reset(context.Background(), other)
	})
	count := func() int {
		n, err := rdb.Get(ctx, full).Int()
		if err != nil && !errors.Is(err, redis.Nil) {
			t.Fatal(err)
		}
		return n
	}
	limit := domain.FailureLimit{Key: key, Max: 2}

	if ok, wait, err := fc.Reserve(ctx, 3*time.Second, limit); err != nil || !ok || wait != 0 {
		t.Fatalf("first reserve = %v %v %v", ok, wait, err)
	}
	if !strings.HasPrefix(full, "keipay:loginfail:ip:") || count() != 1 {
		t.Fatalf("counter not stored under keipay:loginfail: (count %d)", count())
	}
	if ttl := rdb.PTTL(ctx, full).Val(); ttl <= 2*time.Second || ttl > 3*time.Second {
		t.Fatalf("ttl after first reserve = %v", ttl)
	}

	// Shorten the window to stand in for time passing: a later attempt must
	// not restart it.
	if err := rdb.PExpire(ctx, full, 1500*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := fc.Reserve(ctx, 3*time.Second, limit); err != nil || !ok || count() != 2 {
		t.Fatalf("second reserve = %v %v, count %d", ok, err, count())
	}
	if ttl := rdb.PTTL(ctx, full).Val(); ttl <= 0 || ttl > 1500*time.Millisecond {
		t.Fatalf("ttl = %v; the window must not restart", ttl)
	}

	// At the limit nothing is counted, under any key, and the wait is the
	// window left.
	ok, wait, err := fc.Reserve(ctx, 3*time.Second, domain.FailureLimit{Key: other, Max: 5}, limit)
	if err != nil || ok || wait <= 0 || wait > 1500*time.Millisecond {
		t.Fatalf("reserve over the limit = %v %v %v", ok, wait, err)
	}
	if count() != 2 || rdb.Exists(ctx, failureKeyPrefix+other).Val() != 0 {
		t.Fatalf("a refused reserve counted: count %d", count())
	}

	if err := fc.Release(ctx, key); err != nil || count() != 1 {
		t.Fatalf("release = %v, count %d", err, count())
	}
	if ttl := rdb.PTTL(ctx, full).Val(); ttl <= 0 {
		t.Fatalf("release dropped the expiry: ttl %v", ttl)
	}
	if err := fc.Release(ctx, key, other); err != nil || rdb.Exists(ctx, full, failureKeyPrefix+other).Val() != 0 {
		t.Fatalf("release to zero = %v; keys must be gone, missing ones not created", err)
	}

	// A counter left without an expiry gets one, whether or not it is over
	// the limit, so it can never lock a key forever.
	for _, n := range []int{1, 2} {
		if err := rdb.Set(ctx, full, n, 0).Err(); err != nil {
			t.Fatal(err)
		}
		ok, wait, _ := fc.Reserve(ctx, time.Minute, limit)
		if ttl := rdb.PTTL(ctx, full).Val(); ttl <= 0 {
			t.Fatalf("count %d: ttl = %v, want an expiry", n, ttl)
		}
		if ok != (n < 2) || (!ok && wait <= 0) {
			t.Fatalf("count %d: reserve = %v %v", n, ok, wait)
		}
	}

	if err := fc.Reset(ctx, key); err != nil || count() != 0 {
		t.Fatalf("reset = %v, count %d", err, count())
	}
}

// The check and the increment are one step in Redis, so parallel attempts
// never get past the limit.
func TestIntegrationFailureCounterConcurrentReserve(t *testing.T) {
	rdb := testRedis(t)
	fc := NewFailureCounter(rdb)
	key := "email:" + randomToken(t)
	t.Cleanup(func() { _ = fc.Reset(context.Background(), key) })

	const attempts, maxAttempts = 50, 5
	var wg sync.WaitGroup
	var reserved atomic.Int32
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, err := fc.Reserve(context.Background(), time.Minute, domain.FailureLimit{Key: key, Max: maxAttempts})
			if err != nil {
				t.Error(err)
			}
			if ok {
				reserved.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := reserved.Load(); got != maxAttempts {
		t.Fatalf("reserved %d of %d, want %d", got, attempts, maxAttempts)
	}
	if n := rdb.Get(context.Background(), failureKeyPrefix+key).Val(); n != "5" {
		t.Fatalf("count = %s", n)
	}
}
