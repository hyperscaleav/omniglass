package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A move is its own verb (#627 Task 13). Placement (parent, location) is no
// longer a PATCH field on component, system, or location: it is a POST
// .../{name}:move custom method, gated by its own <resource>:move permission,
// distinct from <resource>:update the same way a rename is distinct from
// update.

// TestPatchRefusesContainment proves the containment fields left the PATCH
// body: sending "parent" (component, system) or "parent" (location) to the
// PATCH route is a 422, Huma's additionalProperties rejection, the same
// mechanism that already refuses a PATCH "name" now that rename is its own
// method. This is a property of the removed struct field, not code this test
// exercises directly, but it is exactly the contract an operator who still
// posts the old PATCH shape needs to see fail loudly rather than silently no-op.
func TestPatchRefusesContainment(t *testing.T) {
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

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "patch-refuses-comp", "product": "generic-device"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "patch-refuses-sys"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "patch-refuses-loc", "location_type": "campus"}, http.StatusCreated)

	c.do(ownerTok, http.MethodPatch, "/components/patch-refuses-comp", map[string]any{"parent": ""}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/components/patch-refuses-comp", map[string]any{"location": ""}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/systems/patch-refuses-sys", map[string]any{"parent": ""}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/systems/patch-refuses-sys", map[string]any{"location": ""}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/locations/patch-refuses-loc", map[string]any{"parent": ""}, http.StatusUnprocessableEntity)
}

// TestMoveIsItsOwnPermission mirrors TestRenameIsItsOwnPermission exactly: a
// caller holding <resource>:update, the permission that used to carry
// placement, is refused :move; a caller holding <resource>:move is allowed.
func TestMoveIsItsOwnPermission(t *testing.T) {
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

	moveables := []struct {
		resource string
		create   func(c *apiClient, tok, name string)
		movePath func(name string) string
		// moveBody is a function of name: component and system clear-to-root
		// (a fixed body, independent of name), but location has no
		// clear-to-root capability at all (ADR-0088), so its leg needs an
		// actual resolvable destination, a second fixture parented off name.
		moveBody func(name string) map[string]any
	}{
		{
			resource: "component",
			create: func(c *apiClient, tok, name string) {
				c.do(tok, http.MethodPost, "/components", map[string]any{"name": name, "product": "generic-device"}, http.StatusCreated)
			},
			movePath: func(name string) string { return "/components/" + name + ":move" },
			moveBody: func(string) map[string]any { return map[string]any{"parent": ""} },
		},
		{
			resource: "system",
			create: func(c *apiClient, tok, name string) {
				c.do(tok, http.MethodPost, "/systems", map[string]any{"name": name}, http.StatusCreated)
			},
			movePath: func(name string) string { return "/systems/" + name + ":move" },
			moveBody: func(string) map[string]any { return map[string]any{"parent": ""} },
		},
		{
			resource: "location",
			create: func(c *apiClient, tok, name string) {
				// campus is root-only (allowed_parent_types={root}); building is
				// allowed under campus, so the "before" fixture must be a
				// building, not another campus, or the move itself 422s on
				// placement before the permission question is ever reached.
				c.do(tok, http.MethodPost, "/locations", map[string]any{"name": name + "-dest", "location_type": "campus"}, http.StatusCreated)
				c.do(tok, http.MethodPost, "/locations", map[string]any{"name": name, "location_type": "building"}, http.StatusCreated)
			},
			movePath: func(name string) string { return "/locations/" + name + ":move" },
			moveBody: func(name string) map[string]any { return map[string]any{"parent": name + "-dest"} },
		},
	}

	for _, r := range moveables {
		insertRole(t, ctx, dsn, roleSlug(r.resource)+"-mv-updater", []string{r.resource + ":read,update"})
		insertRole(t, ctx, dsn, roleSlug(r.resource)+"-mv-mover", []string{r.resource + ":read,move"})
	}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	for _, r := range moveables {
		t.Run(r.resource, func(t *testing.T) {
			slug := roleSlug(r.resource)
			updaterTok := principalWithGrants(t, ctx, dsn, slug+"-mv-updater-svc",
				[]grant{{role: slug + "-mv-updater", scopeKind: "all"}})
			moverTok := principalWithGrants(t, ctx, dsn, slug+"-mv-mover-svc",
				[]grant{{role: slug + "-mv-mover", scopeKind: "all"}})

			// Both custom roles grant scopeKind "all", so a no-op clear-to-root
			// on an already-root row (component, system) isolates the
			// permission-token question from the scope-guard question
			// TestScopedPrincipalCannotLiftToRoot covers. Location has no
			// clear-to-root at all, so its body targets a real destination
			// instead (see moveBody above); the permission question is the
			// same either way.
			name := slug + "-mv-before"
			r.create(c, ownerTok, name)
			body := r.moveBody(name)

			// An update grant does not reach :move.
			if code, respBody := c.send(updaterTok, http.MethodPost, r.movePath(name), body); code != http.StatusForbidden {
				t.Fatalf("%s:update moved (%d, body %s), want 403: update must not imply move", r.resource, code, respBody)
			}
			// A move grant does.
			c.do(moverTok, http.MethodPost, r.movePath(name), body, http.StatusOK)
		})
	}
}

// TestMoveCollisionNamesBothParties proves the 409 a name collision at the
// move destination produces, over HTTP, names both the moved entity and the
// destination it collided at, using the per-index sentinels (Task 11's 23505
// mapper split) rather than a generic "name already exists". A move can
// never rename, so a real repro needs two ALREADY-same-named components in
// different placement buckets (#627: name uniqueness is scoped to placement,
// so this is legal), one moving into the other's bucket. Addressed by uuid
// (returned from its create), never by the now-shared bare name "dup", which
// would be ambiguous fleet-wide for an all-scoped owner; the 409 message
// still names it by its actual name (componentMoverName resolves the ref back
// to the row's own name), proving the message is correct under uuid
// addressing too, not just when the caller happens to type the plain name.
func TestMoveCollisionNamesBothParties(t *testing.T) {
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

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "collide-dest", "product": "generic-device"}, http.StatusCreated)
	// An operator-owned duplicate already sits at the destination.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "dup", "parent": "collide-dest", "product": "generic-device"}, http.StatusCreated)
	// The mover: a second, distinct "dup" elsewhere (unplaced), a legal
	// create since the unplaced bucket and collide-dest's bucket are separate
	// under #627's scoped-uniqueness rule.
	moverBody := c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "dup", "product": "generic-device"}, http.StatusCreated)
	moverID := createdID(t, moverBody)

	status, body := c.send(ownerTok, http.MethodPost, "/components/"+moverID+":move", map[string]any{"parent": "collide-dest"})
	if status != http.StatusConflict {
		t.Fatalf("move onto a name collision = %d (%s), want 409", status, body)
	}
	if !bytes.Contains(body, []byte("dup")) || !bytes.Contains(body, []byte("collide-dest")) {
		t.Errorf("409 body = %s, want it to name both the mover (dup) and the destination (collide-dest)", body)
	}
}
