package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
			Name                 string   `json:"name"`
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
		byID[r.Name] = struct {
			display, desc string
			eff           []string
		}{r.DisplayName, r.Description, r.EffectivePermissions}
	}

	owner, ok := byID["owner"]
	if !ok || owner.display != "Owner" || owner.desc == "" || !slices.Contains(owner.eff, ">") {
		t.Fatalf("owner role = %+v, want display Owner, a description, and > effective (the superuser tail)", owner)
	}
	admin, ok := byID["admin"]
	if !ok || admin.display != "Administrator" || slices.Contains(admin.eff, ">") || slices.Contains(admin.eff, "*:*") {
		t.Fatalf("admin role = %+v, want display Administrator and NO > or *:* (bounded)", admin)
	}
	// admin reads the audit trail via the explicit admin-tier grant, not a wildcard.
	if !slices.Contains(admin.eff, "audit:read:admin") {
		t.Fatalf("admin effective perms = %v, want audit:read:admin", admin.eff)
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

// TestRolesViewNetPermissions drives GET /roles for the net (held-vs-missing)
// surface: the payload carries permission_universe (every capability the route
// surface enforces, concrete and sorted), and each role carries held (the subset
// of that universe it covers, resolved through the same rbac matcher as the
// effective set). Owner holds the whole universe; viewer holds the non-sensitive
// two-token reads via *:read but not the admin-tier reads nor the sensitive
// secret:read; admin holds the admin-tier reads it is explicitly granted. Skipped
// under -short.
func TestRolesViewNetPermissions(t *testing.T) {
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
		PermissionUniverse []string `json:"permission_universe"`
		Roles              []struct {
			ID   string   `json:"id"`
			Name string   `json:"name"`
			Held []string `json:"held"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/roles", nil, http.StatusOK), &out); err != nil {
		t.Fatalf("decode roles: %v", err)
	}

	uni := out.PermissionUniverse
	if len(uni) < 30 {
		t.Fatalf("permission_universe has %d entries, want the full routed surface (>=30)", len(uni))
	}
	if !slices.IsSorted(uni) {
		t.Fatalf("permission_universe is not sorted: %v", uni)
	}
	// The universe is concrete: it is derived from route declarations, never grants,
	// so it must contain no wildcard or tail tokens.
	for _, p := range uni {
		if strings.ContainsAny(p, "*>") {
			t.Fatalf("permission_universe entry %q contains a wildcard/tail; the universe must be concrete", p)
		}
	}
	// Sample entries that must be present (a spread across resources and tiers).
	// platform:<action> rides in on platformGated, so the tier gate a write at the
	// least-specific cascade level needs is in the universe like any primary gate,
	// and the roles view can report it held or missing.
	for _, want := range []string{"component:read", "component:delete", "audit:read:admin", "role:read:admin", "secret:reveal", "platform:create", "platform:update", "platform:delete"} {
		if !slices.Contains(uni, want) {
			t.Errorf("permission_universe missing %q; got %v", want, uni)
		}
	}
	uniSet := map[string]bool{}
	for _, p := range uni {
		uniSet[p] = true
	}

	held := map[string][]string{}
	for _, r := range out.Roles {
		for _, h := range r.Held {
			if !uniSet[h] {
				t.Errorf("role %s held %q is not in the permission_universe", r.Name, h)
			}
		}
		held[r.Name] = r.Held
	}

	// Owner (>) holds the entire universe.
	if len(held["owner"]) != len(uni) {
		t.Errorf("owner held %d of %d universe permissions, want all", len(held["owner"]), len(uni))
	}
	// Viewer (*:read) holds non-sensitive two-token reads, but not admin-tier reads
	// and not the sensitive secret:read.
	if !slices.Contains(held["viewer"], "component:read") {
		t.Errorf("viewer held = %v, want component:read (via *:read)", held["viewer"])
	}
	if slices.Contains(held["viewer"], "audit:read:admin") {
		t.Errorf("viewer must not hold audit:read:admin (admin-tier fences the read floor)")
	}
	if slices.Contains(held["viewer"], "secret:read") {
		t.Errorf("viewer must not hold secret:read (secret is a sensitive resource)")
	}
	// Admin holds the admin-tier reads it is explicitly granted.
	for _, want := range []string{"audit:read:admin", "role:read:admin", "component:delete"} {
		if !slices.Contains(held["admin"], want) {
			t.Errorf("admin held = %v, want %q", held["admin"], want)
		}
	}
	// Operator holds system:read ONLY through the inherited viewer *:read floor, not
	// a direct grant, so this proves held is computed from the flattened (inheritance-
	// resolved) set, not from the role's own raw permissions.
	if !slices.Contains(held["operator"], "system:read") {
		t.Errorf("operator held = %v, want inherited system:read (held via the viewer floor, not a direct grant)", held["operator"])
	}
}
