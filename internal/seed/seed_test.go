package seed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestSeedRolesIdempotent proves the boot-seed installs exactly the four
// official roles and that running it twice does not duplicate or drift them.
// Skipped under -short.
func TestSeedRolesIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	// Run twice: idempotency is the property under test.
	for i := 0; i < 2; i++ {
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatalf("seed run %d: %v", i, err)
		}
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var count int
	if err := conn.QueryRow(ctx, `select count(*) from role where official`).Scan(&count); err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if count != 4 {
		t.Errorf("official roles = %d, want 4 (seed not idempotent or incomplete)", count)
	}

	var ownerPerms []string
	if err := conn.QueryRow(ctx, `select permissions from role where id = 'owner'`).Scan(&ownerPerms); err != nil {
		t.Fatalf("read owner role: %v", err)
	}
	if len(ownerPerms) != 1 || ownerPerms[0] != "*:*" {
		t.Errorf("owner permissions = %v, want [*:*]", ownerPerms)
	}
}
