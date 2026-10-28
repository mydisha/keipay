// Package redis provides a Redis client plus tenant cache and idempotency helpers.
package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// New constructs and pings a Redis client.
func New(ctx context.Context, addr, password string, db int) (*redis.Client, error) {
	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Ping(pingCtx).Err(); err != nil {
		return nil, err
	}
	return c, nil
}
