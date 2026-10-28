package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mydisha/keipay/backend/internal/domain"
)

const tenantCacheTTL = 5 * time.Minute

// TenantCache caches tenant lookups by API key on top of a TenantRepository.
type TenantCache struct {
	rdb  *redis.Client
	repo domain.TenantRepository
}

// NewTenantCache wraps repo with a Redis read-through cache.
func NewTenantCache(rdb *redis.Client, repo domain.TenantRepository) *TenantCache {
	return &TenantCache{rdb: rdb, repo: repo}
}

func tenantKey(apiKey string) string { return "tenant:apikey:" + apiKey }

// GetByAPIKey returns a tenant from cache or falls back to the repository.
func (c *TenantCache) GetByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error) {
	key := tenantKey(apiKey)
	if raw, err := c.rdb.Get(ctx, key).Bytes(); err == nil {
		var t domain.Tenant
		if json.Unmarshal(raw, &t) == nil {
			return &t, nil
		}
	}
	t, err := c.repo.GetByAPIKey(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	if b, err := json.Marshal(t); err == nil {
		_ = c.rdb.Set(ctx, key, b, tenantCacheTTL).Err()
	}
	return t, nil
}

// Invalidate drops the cache entry for an API key (call after tenant updates).
func (c *TenantCache) Invalidate(ctx context.Context, apiKey string) error {
	return c.rdb.Del(ctx, tenantKey(apiKey)).Err()
}
