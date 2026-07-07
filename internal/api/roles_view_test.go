package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestRolesViewAPI drives GET /roles: each official role carries its display name
// and description, and its effective (flattened) permissions resolve inheritance,
// wildcards, and the :read floor. The owner effective set is the global wildcard;
// admin's is broad but does NOT include *:* (the capability firewall that makes it
// bounded), and viewer's includes *:read. Skipped under -short.
func TestRolesViewAPI(t *testing.T) {
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

	var out struct {
		Roles []struct {
			ID                   string   `json:"id"`
			DisplayName          string   `json:"display_name"`
			Description          string   `json:"description"`
			Official             bool     `json:"official"`
			EffectivePermissions []string `json:"effective_permissions"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/roles", nil, http.StatusOK), &out); err != nil {
		t.Fatalf("decode roles: %v", err)
	}
	byID := map[string]struct {
		display, desc string
		eff           []string
	}{}
	for _, r := range out.Roles {
		byID[r.ID] = struct {
			display, desc string
			eff           []string
		}{r.DisplayName, r.Description, r.EffectivePermissions}
	}

	owner, ok := byID["owner"]
	if !ok || owner.display != "Owner" || owner.desc == "" || !slices.Contains(owner.eff, "*:*") {
		t.Fatalf("owner role = %+v, want display Owner, a description, and *:* effective", owner)
	}
	admin, ok := byID["admin"]
	if !ok || admin.display != "Administrator" || slices.Contains(admin.eff, "*:*") {
		t.Fatalf("admin role = %+v, want display Administrator and NO *:* (bounded)", admin)
	}
	// admin still holds the broad management wildcards it enumerates.
	if !slices.Contains(admin.eff, "principal:*") {
		t.Fatalf("admin effective perms = %v, want principal:*", admin.eff)
	}
	viewer, ok := byID["viewer"]
	if !ok || viewer.display != "Viewer" || !slices.Contains(viewer.eff, "*:read") {
		t.Fatalf("viewer role = %+v, want display Viewer and *:read effective", viewer)
	}
	// operator inherits viewer, so its effective set includes the read floor.
	if op := byID["operator"]; !slices.Contains(op.eff, "*:read") {
		t.Fatalf("operator effective perms = %v, want inherited *:read", op.eff)
	}
}
