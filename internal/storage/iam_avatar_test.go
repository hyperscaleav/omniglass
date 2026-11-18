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
	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
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

func TestLoadUser_NoAvatarByDefault(t *testing.T) {
	t.Parallel()
	ctx, gw, pid := newAvatarGW(t)
	pr, err := gw.GetUser(ctx, pid, scopeAll())
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if pr.Person == nil || pr.Person.HasAvatar {
		t.Errorf("HasAvatar = %v, want false", pr.Person.HasAvatar)
	}
	if pr.Person.AvatarUpdatedAt != nil {
		t.Errorf("AvatarUpdatedAt = %v, want nil", pr.Person.AvatarUpdatedAt)
	}
}

func TestSetGetClearOwnAvatar(t *testing.T) {
	t.Parallel()
	ctx, gw, pid := newAvatarGW(t)
	if err := gw.SetOwnAvatar(ctx, pid, "AAAA"); err != nil {
		t.Fatalf("set own: %v", err)
	}
	b64, ok, err := gw.GetPersonAvatar(ctx, pid)
	if err != nil || !ok || b64 != "AAAA" {
		t.Fatalf("get = (%q,%v,%v), want (AAAA,true,nil)", b64, ok, err)
	}
	pr, _ := gw.GetUser(ctx, pid, scopeAll())
	if !pr.Person.HasAvatar || pr.Person.AvatarUpdatedAt == nil {
		t.Errorf("HasAvatar=%v updatedAt=%v, want true/non-nil", pr.Person.HasAvatar, pr.Person.AvatarUpdatedAt)
	}
	if err := gw.ClearOwnAvatar(ctx, pid); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok, _ := gw.GetPersonAvatar(ctx, pid); ok {
		t.Errorf("ok = true after clear, want false")
	}
}

func TestSetUserAvatar_RequiresAllScope(t *testing.T) {
	t.Parallel()
	ctx, gw, pid := newAvatarGW(t)
	if err := gw.SetUserAvatar(ctx, pid, pid, "AAAA", scope.Set{}); err != storage.ErrUserForbidden {
		t.Errorf("err = %v, want ErrUserForbidden", err)
	}
	if err := gw.SetUserAvatar(ctx, pid, pid, "AAAA", scope.Set{All: true}); err != nil {
		t.Errorf("admin set with all-scope: %v", err)
	}
}
