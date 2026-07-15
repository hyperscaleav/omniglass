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

// TestIAMReadsAreAdminTier pins that the Users, Roles, and Groups directories are
// admin-tier reads: a viewer@all (the read floor deploy also inherits) is refused,
// while admin@all and owner are allowed. This is the fix for the reported bug where
// viewer's *:read reached principal/role/principal_group read.
func TestIAMReadsAreAdminTier(t *testing.T) {
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
	// viewer@all carries *:read, the same floor deploy inherits. If it cannot read
	// these, neither can deploy (which holds no broader grant on them).
	viewerTok := principalWithGrants(t, ctx, dsn, "az-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	adminTok := principalWithGrants(t, ctx, dsn, "az-admin", []grant{{role: "admin", scopeKind: "all"}})

	reads := []string{"/principals", "/roles", "/principal-groups"}
	for _, p := range reads {
		c.do(viewerTok, http.MethodGet, p, nil, http.StatusForbidden)
		c.do(adminTok, http.MethodGet, p, nil, http.StatusOK)
		c.do(ownerTok, http.MethodGet, p, nil, http.StatusOK)
	}
}
