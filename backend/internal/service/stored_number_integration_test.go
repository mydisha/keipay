package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mydisha/keipay/backend/internal/testutil/pgtest"
)

// storedNumber must produce exactly what Postgres JSONB returns, or the
// create response and later reads of the same metadata disagree.
func TestIntegrationStoredNumberMatchesPostgres(t *testing.T) {
	dsn := os.Getenv(pgtest.DSNEnv)
	if dsn == "" {
		t.Skip(pgtest.DSNEnv + " not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	for _, tc := range storedNumberCases {
		var pg string
		if err := conn.QueryRow(ctx, `SELECT ('{"n":' || $1 || '}')::jsonb ->> 'n'`, tc.in).Scan(&pg); err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		got, ok := storedNumber(json.Number(tc.in), maxMetadataBytes)
		if !ok || string(got) != pg {
			t.Errorf("storedNumber(%s) = %q, %v; Postgres stores %q", tc.in, got, ok, pg)
		}
	}

	// Numbers numeric cannot hold are refused by storedNumber too.
	for _, in := range []string{"1e200000", "-1e-200000", "1e1073741823"} {
		var pg string
		if err := conn.QueryRow(ctx, `SELECT ('{"n":' || $1 || '}')::jsonb ->> 'n'`, in).Scan(&pg); err == nil {
			t.Errorf("Postgres stored %s", in)
		}
		if _, ok := storedNumber(json.Number(in), maxMetadataBytes); ok {
			t.Errorf("storedNumber(%s) accepted a number Postgres refuses", in)
		}
	}
}
