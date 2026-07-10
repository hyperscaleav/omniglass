package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func scopeAll() scope.Set { return scope.Set{All: true} }

func newAvatarGW(t *testing.T) (context.Context, storage.Gateway, string) {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("new pg: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: zeros, Prefix: "abcd1234"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pr, err := gw.AuthenticateBearer(ctx, zeros)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	return ctx, gw, pr.ID
}

func TestLoadPrincipal_NoAvatarByDefault(t *testing.T) {
	ctx, gw, pid := newAvatarGW(t)
	pr, err := gw.GetPrincipal(ctx, pid, scopeAll())
	if err != nil {
		t.Fatalf("get principal: %v", err)
	}
	if pr.Human == nil || pr.Human.HasAvatar {
		t.Errorf("HasAvatar = %v, want false", pr.Human.HasAvatar)
	}
	if pr.Human.AvatarUpdatedAt != nil {
		t.Errorf("AvatarUpdatedAt = %v, want nil", pr.Human.AvatarUpdatedAt)
	}
}
