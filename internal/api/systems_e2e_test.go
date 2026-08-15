package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSystemAPI drives the system surface over HTTP: an owner builds a system
// tree and runs CRUD, and a system-scoped viewer sees only its subtree, gets a
// non-disclosing 404 outside it, and is forbidden a write (capability
// fast-reject). Mirrors TestLocationAPI; reuses its helpers. Skipped under -short.
func TestSystemAPI(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner builds av (root, conforming to a standard) > av-sub; plus lab (root,
	// a one-off that conforms to none, since a standard is optional).
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av", "standard_id": "meeting-room"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av-sub", "standard_id": "huddle-room", "parent": "av"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "lab"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "bad", "standard_id": "galaxy"}, http.StatusUnprocessableEntity)

	// A conforming system converts back to a one-off over the wire: an omitted
	// standard_id leaves it alone, an explicit "" clears it. This is the path the
	// console takes, and without it "one-off" would only be reachable at create.
	standardOf := func(name string) string {
		var s struct {
			Standard string `json:"standard"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/"+name, nil, http.StatusOK), &s); err != nil {
			t.Fatalf("decode system %s: %v", name, err)
		}
		return s.Standard
	}
	c.do(ownerTok, http.MethodPatch, "/systems/av", map[string]any{"label": "AV"}, http.StatusOK)
	if got := standardOf("av"); got != "meeting-room" {
		t.Fatalf("omitted standard = %q, want meeting-room kept", got)
	}
	c.do(ownerTok, http.MethodPatch, "/systems/av", map[string]any{"standard_id": ""}, http.StatusOK)
	if got := standardOf("av"); got != "" {
		t.Fatalf("cleared standard_id = %q, want empty (a one-off system)", got)
	}

	// Owner lists all three.
	var listed struct {
		Systems []struct {
			ID, Name string
		} `json:"systems"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems", nil, http.StatusOK), &listed)
	if len(listed.Systems) != 3 {
		t.Fatalf("owner list = %d, want 3", len(listed.Systems))
	}
	var avID string
	for _, s := range listed.Systems {
		if s.Name == "av" {
			avID = s.ID
		}
	}

	// A viewer scoped to av: sees av + av-sub only, 404 on lab, 403 on write.
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-av", "viewer", "system", avID)
	var vlist struct {
		Systems []struct{ Name string } `json:"systems"`
	}
	json.Unmarshal(c.do(viewerTok, http.MethodGet, "/systems", nil, http.StatusOK), &vlist)
	if len(vlist.Systems) != 2 {
		t.Fatalf("viewer-av list = %d, want 2 (av subtree)", len(vlist.Systems))
	}
	c.do(viewerTok, http.MethodGet, "/systems/lab", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/systems/av-sub", nil, http.StatusOK)
	c.do(viewerTok, http.MethodPatch, "/systems/av-sub", map[string]any{"label": "nope"}, http.StatusForbidden)

	// Owner CRUD: patch, delete-occupied 409, leaf delete, then 404.
	c.do(ownerTok, http.MethodPatch, "/systems/av-sub", map[string]any{"label": "Subsystem"}, http.StatusOK)
	c.do(ownerTok, http.MethodDelete, "/systems/av", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/systems/av-sub", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/systems/av-sub", nil, http.StatusNotFound)
}

// TestSystemRenameAndCheckName drives the :rename custom method and the
// collection-level :checkName advisory over HTTP: checkName reports valid +
// available (against the unplaced/root bucket here), :rename moves the name, a rename onto a taken name is a
// 409, and a bad slug is rejected at the edge by the Huma pattern (422). Skipped
// under -short.
func TestSystemRenameAndCheckName(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Seed a system.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av-one"}, http.StatusCreated)

	type nameCheck struct {
		Valid     bool   `json:"valid"`
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	check := func(name string) nameCheck {
		out := c.do(ownerTok, http.MethodPost, "/systems:checkName", map[string]any{"name": name}, http.StatusOK)
		var nc nameCheck
		if err := json.Unmarshal(out, &nc); err != nil {
			t.Fatalf("decode checkName: %v", err)
		}
		return nc
	}

	// checkName: taken.
	if nc := check("av-one"); !nc.Valid || nc.Available {
		t.Fatalf("checkName(av-one) = %+v, want valid=true available=false", nc)
	}
	// checkName: available.
	if nc := check("av-free"); !nc.Valid || !nc.Available {
		t.Fatalf("checkName(av-free) = %+v, want valid=true available=true", nc)
	}
	// checkName: bad format -> valid:false, still 200.
	if nc := check("Bad Name"); nc.Valid {
		t.Fatalf("checkName(Bad Name) = %+v, want valid=false", nc)
	}

	// Rename via the custom method. It is not a PATCH: a rename breaks stored
	// external references, so it is an explicit act with its own permission.
	out := c.do(ownerTok, http.MethodPost, "/systems/av-one:rename", map[string]any{"name": "av-renamed"}, http.StatusOK)
	var renamed struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "av-renamed" {
		t.Fatalf("name = %q, want av-renamed", renamed.Name)
	}

	// Afterwards the system answers to the new name and to its uuid, and the old
	// name is gone.
	c.do(ownerTok, http.MethodGet, "/systems/av-renamed", nil, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/systems/"+renamed.ID, nil, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/systems/av-one", nil, http.StatusNotFound)

	// The name is no longer patchable: two ways to rename is what this method removed.
	c.do(ownerTok, http.MethodPatch, "/systems/av-renamed", map[string]any{"name": "av-sneaky"}, http.StatusUnprocessableEntity)

	// Dup rename -> 409.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av-two"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems/av-two:rename", map[string]any{"name": "av-renamed"}, http.StatusConflict)

	// Bad format -> 422 (Huma pattern rejects at the edge).
	c.do(ownerTok, http.MethodPost, "/systems/av-two:rename", map[string]any{"name": "Bad Name"}, http.StatusUnprocessableEntity)

	// A uuid-shaped name passes the slug pattern and is refused by the gateway, so
	// the name and the id can never be the same shape.
	c.do(ownerTok, http.MethodPost, "/systems/av-two:rename",
		map[string]any{"name": "019f8754-461f-7b82-b5f2-fc4bbe1c3765"}, http.StatusUnprocessableEntity)

	// Create-tightening: a bad name is rejected at create too, not just rename.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "Bad Name"}, http.StatusUnprocessableEntity)
}

// TestSystemCheckNameIsScopedToPlacement drives the #627 fix over HTTP:
// checkName now reports availability against the SAME placement bucket a
// create would land in, named by the Parent field, not a single global fact.
// A name taken under one parent is reported free under another (the exact
// false-positive that used to block a legal create), and the advisory stays
// blind to the caller's OWN grant scope, same as before. Skipped under -short.
func TestSystemCheckNameIsScopedToPlacement(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Two root systems, and a "sub-1" child under scope-av only.
	var av struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "scope-av"}, http.StatusCreated), &av); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "scope-lab"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sub-1", "parent": "scope-av"}, http.StatusCreated)

	// A deploy principal (system:update) scoped ONLY to scope-av.
	deployTok := setupScopedViewer(t, ctx, dsn, "deploy-av", "deploy", "system", av.ID)
	// It cannot read scope-lab (out of scope -> non-disclosing 404).
	c.do(deployTok, http.MethodGet, "/systems/scope-lab", nil, http.StatusNotFound)

	check := func(body map[string]any) (valid, available bool) {
		out := c.do(deployTok, http.MethodPost, "/systems:checkName", body, http.StatusOK)
		var nc struct {
			Valid     bool `json:"valid"`
			Available bool `json:"available"`
		}
		if err := json.Unmarshal(out, &nc); err != nil {
			t.Fatalf("decode checkName: %v", err)
		}
		return nc.Valid, nc.Available
	}

	// sub-1 is taken under scope-av, the same placement it was created at.
	if valid, available := check(map[string]any{"name": "sub-1", "parent": "scope-av"}); !valid || available {
		t.Fatalf("checkName(sub-1, parent=scope-av) = valid=%v available=%v, want true,false", valid, available)
	}
	// The identical name is FREE under scope-lab: this is the false positive
	// #627 fixes. The caller cannot even GET scope-lab, and the check still
	// answers correctly (blind to the caller's own grant, aware of the
	// placement bucket named in the request).
	if valid, available := check(map[string]any{"name": "sub-1", "parent": "scope-lab"}); !valid || !available {
		t.Fatalf("checkName(sub-1, parent=scope-lab) = valid=%v available=%v, want true,true", valid, available)
	}
	// scope-lab is taken at ROOT (no parent, no location): the advisory
	// remains scope-blind to the caller's own grant for this bucket too.
	if valid, available := check(map[string]any{"name": "scope-lab"}); !valid || available {
		t.Fatalf("checkName(scope-lab, root) = valid=%v available=%v, want true,false", valid, available)
	}
}
