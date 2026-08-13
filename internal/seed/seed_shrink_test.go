package seed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSeedShrinksARolePermissionArray pins the #463 acceptance mechanic: the
// boot seed's upsert is authoritative, so a permission removed from roles.yaml
// disappears from an already-seeded role on the next boot rather than
// lingering. Simulated by seeding, appending a retired grant to the stored
// row, and seeding again. The retired grant is deliberately a permission no
// route will ever stamp: the mechanic under test is the upsert's authority, and
// borrowing a real resource's vocabulary for it once left a phantom capability
// in the repo that read as prior art (#728).
func TestSeedShrinksARolePermissionArray(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	admin := roleByID(t, ctx, gw, "admin")
	admin.Permissions = append(append([]string{}, admin.Permissions...), "widget:frobnicate,polish")
	if err := gw.UpsertRole(ctx, admin); err != nil {
		t.Fatalf("widen admin: %v", err)
	}

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	after := roleByID(t, ctx, gw, "admin")
	for _, p := range after.Permissions {
		if p == "widget:frobnicate,polish" {
			t.Fatalf("re-seed left the retired grant behind: %v", after.Permissions)
		}
	}
}

func roleByID(t *testing.T, ctx context.Context, gw storage.Gateway, id string) storage.Role {
	t.Helper()
	roles, err := gw.ListRoles(ctx)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	for _, r := range roles {
		if r.Name == id {
			return r
		}
	}
	t.Fatalf("role %s not seeded", id)
	return storage.Role{}
}
