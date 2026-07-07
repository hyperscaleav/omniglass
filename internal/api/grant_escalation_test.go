package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestGrantEscalationGuard proves a grant cannot confer a capability the granter
// lacks at all-scope: an admin (an enumerated role, no *:*) cannot grant owner
// (*:*) to itself or anyone, so it cannot self-promote to the owner tier. An admin
// can still grant roles it covers, and an owner (which covers everything) can grant
// anything. Skipped under -short.
func TestGrantEscalationGuard(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	adminTok := principalWithGrants(t, ctx, dsn, "an-admin", []grant{{role: "admin", scopeKind: "all"}})
	adminID := meID(t, c, adminTok)

	// A plain target to receive grants.
	targetTok := principalWithGrants(t, ctx, dsn, "a-target", nil)
	targetID := meID(t, c, targetTok)

	// An admin CANNOT grant owner (*:*), which it does not cover, to anyone...
	if code, b := c.send(adminTok, http.MethodPost, "/principals/"+targetID+"/grants", map[string]any{"role": "owner", "scope_kind": "all"}); code != http.StatusForbidden {
		t.Fatalf("admin granting owner to a user: want 403 (escalation), got %d (%s)", code, b)
	}
	// ...nor to itself (the self-promotion path).
	if code, b := c.send(adminTok, http.MethodPost, "/principals/"+adminID+"/grants", map[string]any{"role": "owner", "scope_kind": "all"}); code != http.StatusForbidden {
		t.Fatalf("admin self-granting owner: want 403 (escalation), got %d (%s)", code, b)
	}
	// An admin CAN grant a role it covers (viewer, via the *:read floor it inherits).
	c.do(adminTok, http.MethodPost, "/principals/"+targetID+"/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)
	// An admin CAN grant a peer admin (it covers itself).
	c.do(adminTok, http.MethodPost, "/principals/"+targetID+"/grants", map[string]any{"role": "operator", "scope_kind": "all"}, http.StatusCreated)

	// The owner covers everything, so it may still grant owner.
	other := principalWithGrants(t, ctx, dsn, "owner-recipient", nil)
	otherID := meID(t, c, other)
	c.do(ownerTok, http.MethodPost, "/principals/"+otherID+"/grants", map[string]any{"role": "owner", "scope_kind": "all"}, http.StatusCreated)
}
