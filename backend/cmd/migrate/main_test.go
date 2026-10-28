package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
)

// The seeded tenant is recorded as tenant.create by system:seed, and a second
// seed run skips it without another entry.
func TestIntegrationSeedTenantIsAuditedAsSystem(t *testing.T) {
	dsn := os.Getenv("KEI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("KEI_TEST_POSTGRES_DSN not set")
	}
	ctx := domain.WithActor(context.Background(), domain.SystemActor("seed"))
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cipher, err := crypto.New("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	slug := fmt.Sprintf("seed-it-%d", time.Now().UnixNano())
	tenant, creds, err := seedTenant(ctx, pool, cipher, "Seed IT", slug)
	if err != nil || tenant == nil || creds == nil {
		t.Fatalf("seed = %+v, %v", tenant, err)
	}
	target := strconv.FormatInt(tenant.ID, 10)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM audit_logs WHERE target_type='tenant' AND target_id=$1`, target)
		_, _ = pool.Exec(bg, `DELETE FROM tenants WHERE id=$1`, tenant.ID)
	})

	again, _, err := seedTenant(ctx, pool, cipher, "Seed IT", slug)
	if err != nil || again != nil {
		t.Fatalf("second seed = %+v, %v; want skip", again, err)
	}

	var n int
	var actorType, actorID, action, ip string
	err = pool.QueryRow(context.Background(), `SELECT count(*), min(actor_type), min(actor_id), min(action), min(ip)
		FROM audit_logs WHERE target_type='tenant' AND target_id=$1`, target).Scan(&n, &actorType, &actorID, &action, &ip)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 || actorType != "system" || actorID != "seed" || action != "tenant.create" || ip != "" {
		t.Fatalf("entries=%d actor=%s:%s action=%s ip=%q", n, actorType, actorID, action, ip)
	}
}
