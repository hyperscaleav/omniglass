package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestGrantsAndOwnerInvariant proves grant assignment and the owner-invariant
// trigger against a real Postgres: an admin grants and revokes a role x scope, bad
// inputs are refused, the last owner grant cannot be stripped, but a swap (grant a
// second owner, then revoke the first) is allowed. Skipped under -short.
func TestGrantsAndOwnerInvariant(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	for _, r := range []storage.Role{
		{Name: "owner", Official: true, Permissions: []string{"*:*"}},
		{Name: "viewer", Official: true, Permissions: []string{"*:read"}},
	} {
		if err := gw.UpsertRole(ctx, r); err != nil {
			t.Fatalf("seed role %s: %v", r.Name, err)
		}
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, _ := gw.AuthenticateBearer(ctx, zeros)
	all := scope.Set{All: true}

	alice, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: "alice"}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Grant viewer @ all, then it shows on the principal.
	g, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, all)
	if err != nil || g.ID == "" {
		t.Fatalf("create grant: %+v err %v", g, err)
	}
	if got, _ := gw.GetPrincipal(ctx, alice.ID, all); len(got.Grants) != 1 || got.Grants[0].Role != "viewer" {
		t.Fatalf("grant not visible: %+v", got.Grants)
	}

	// Bad inputs are refused.
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, all); !errors.Is(err, storage.ErrGrantExists) {
		t.Fatalf("duplicate grant: want ErrGrantExists, got %v", err)
	}
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location"}, all); !errors.Is(err, storage.ErrBadScope) {
		t.Fatalf("scoped grant without id: want ErrBadScope, got %v", err)
	}
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "nope", ScopeKind: "all"}, all); !errors.Is(err, storage.ErrUnknownRole) {
		t.Fatalf("unknown role: want ErrUnknownRole, got %v", err)
	}
	if _, err := gw.CreateGrant(ctx, owner.ID, "00000000-0000-0000-0000-000000000000", storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, all); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("unknown principal: want ErrPrincipalNotFound, got %v", err)
	}
	// A scoped grant targets a real entity by id: a name or unknown id is refused,
	// a valid location id is fine.
	if err := gw.UpsertLocationType(ctx, storage.LocationType{Name: "campus", DisplayName: "Campus", Official: true}); err != nil {
		t.Fatalf("seed location type: %v", err)
	}
	hq, err := gw.CreateLocation(ctx, owner.ID, storage.LocationSpec{Name: "hq", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create hq: %v", err)
	}
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: "hq"}, all); !errors.Is(err, storage.ErrBadScope) {
		t.Fatalf("scoped grant to a name: want ErrBadScope, got %v", err)
	}
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: hq.ID}, all); err != nil {
		t.Fatalf("scoped grant by id: %v", err)
	}

	// An unknown scope operator is refused.
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: hq.ID, ScopeOp: "bogus"}, all); !errors.Is(err, storage.ErrBadScope) {
		t.Fatalf("bad scope_op: want ErrBadScope, got %v", err)
	}
	// The operator is part of a grant's identity: viewer @ hq with a DIFFERENT op is a
	// distinct grant, not a duplicate (the dedup index includes scope_op). It persists
	// and round-trips.
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: hq.ID, ScopeOp: "self"}, all); err != nil {
		t.Fatalf("viewer @ hq (self) alongside viewer @ hq (subtree): %v", err)
	}
	got, _ := gw.GetPrincipal(ctx, alice.ID, all)
	var sawSelf bool
	for _, gr := range got.Grants {
		if gr.Role == "viewer" && gr.ScopeKind == "location" && gr.ScopeOp == "self" {
			sawSelf = true
		}
	}
	if !sawSelf {
		t.Fatalf("self-op grant did not round-trip: %+v", got.Grants)
	}

	// Revoke the viewer@all grant; it disappears, leaving the two scoped grants
	// (viewer @ hq subtree and viewer @ hq self).
	if err := gw.RevokeGrant(ctx, owner.ID, alice.ID, g.ID, all); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got, _ := gw.GetPrincipal(ctx, alice.ID, all); len(got.Grants) != 2 {
		t.Fatalf("expected the two scoped grants to remain: %+v", got.Grants)
	}

	// The owner invariant: root's single owner grant cannot be revoked.
	rootPr, _ := gw.GetPrincipal(ctx, owner.ID, all)
	var ownerGrantID string
	for _, gr := range rootPr.Grants {
		if gr.Role == "owner" && gr.ScopeKind == "all" {
			ownerGrantID = gr.ID
		}
	}
	if ownerGrantID == "" {
		t.Fatal("root should hold an owner@all grant")
	}
	if err := gw.RevokeGrant(ctx, owner.ID, owner.ID, ownerGrantID, all); !errors.Is(err, storage.ErrLastOwner) {
		t.Fatalf("revoke last owner: want ErrLastOwner, got %v", err)
	}

	// A swap is allowed: grant alice owner@all, then root's owner can be revoked.
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "owner", ScopeKind: "all"}, all); err != nil {
		t.Fatalf("grant alice owner: %v", err)
	}
	if err := gw.RevokeGrant(ctx, owner.ID, owner.ID, ownerGrantID, all); err != nil {
		t.Fatalf("revoke root owner after swap should succeed: %v", err)
	}

	// A non-all scope is refused for both grant and revoke.
	if _, err := gw.CreateGrant(ctx, owner.ID, alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, scope.Set{}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("scoped create grant: want ErrPrincipalForbidden, got %v", err)
	}
	if err := gw.RevokeGrant(ctx, owner.ID, alice.ID, g.ID, scope.Set{}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("scoped revoke: want ErrPrincipalForbidden, got %v", err)
	}
	// Revoking an unknown grant is a clean not-found.
	if err := gw.RevokeGrant(ctx, owner.ID, alice.ID, "00000000-0000-0000-0000-000000000000", all); !errors.Is(err, storage.ErrGrantNotFound) {
		t.Fatalf("revoke unknown: want ErrGrantNotFound, got %v", err)
	}
}
