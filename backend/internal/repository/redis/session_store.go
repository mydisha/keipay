package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mydisha/keipay/backend/internal/domain"
)

const sessionKeyPrefix = "keipay:session:"

// SessionStore is a Redis-backed domain.SessionStore. Sessions are keyed by
// the SHA-256 of their token, so neither Redis nor an error message ever
// holds a usable token.
type SessionStore struct {
	rdb *redis.Client
}

// NewSessionStore constructs a SessionStore.
func NewSessionStore(rdb *redis.Client) *SessionStore { return &SessionStore{rdb: rdb} }

func sessionKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return sessionKeyPrefix + hex.EncodeToString(h[:])
}

// Create stores s under token for ttl.
func (s *SessionStore) Create(ctx context.Context, token string, sess *domain.Session, ttl time.Duration) error {
	raw, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	if err := s.rdb.Set(ctx, sessionKey(token), raw, ttl).Err(); err != nil {
		return fmt.Errorf("store session: %w", err)
	}
	return nil
}

// Get loads the session for token, or domain.ErrNotFound.
func (s *SessionStore) Get(ctx context.Context, token string) (*domain.Session, error) {
	raw, err := s.rdb.Get(ctx, sessionKey(token)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	var sess domain.Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &sess, nil
}

// Touch rewrites an existing session with a new TTL. A session deleted in the
// meantime (sign-out) stays deleted.
func (s *SessionStore) Touch(ctx context.Context, token string, sess *domain.Session, ttl time.Duration) error {
	raw, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	err = s.rdb.SetArgs(ctx, sessionKey(token), raw, redis.SetArgs{Mode: "XX", TTL: ttl}).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// Delete removes the session; deleting an unknown token is not an error.
func (s *SessionStore) Delete(ctx context.Context, token string) error {
	if err := s.rdb.Del(ctx, sessionKey(token)).Err(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
