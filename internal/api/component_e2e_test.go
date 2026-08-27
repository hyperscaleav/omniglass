package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestComponentRenameAndCheckName drives the :rename custom method and the
// collection-level :checkName advisory over HTTP: checkName reports valid +
// available (against the unplaced/root bucket here), :rename moves the name, a rename onto a taken name is a
// 409, and a bad slug is rejected at the edge by the Huma pattern (422). Skipped
// under -short.
func TestComponentRenameAndCheckName(t *testing.T) {
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

	// Seed a component.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "cmp-one", "product": "generic-device"}, http.StatusCreated)

	type nameCheck struct {
		Valid     bool   `json:"valid"`
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	check := func(name string) nameCheck {
		out := c.do(ownerTok, http.MethodPost, "/components:checkName", map[string]any{"name": name}, http.StatusOK)
		var nc nameCheck
		if err := json.Unmarshal(out, &nc); err != nil {
			t.Fatalf("decode checkName: %v", err)
		}
		return nc
	}

	// checkName: taken.
	if nc := check("cmp-one"); !nc.Valid || nc.Available {
		t.Fatalf("checkName(cmp-one) = %+v, want valid=true available=false", nc)
	}
	// checkName: available.
	if nc := check("cmp-free"); !nc.Valid || !nc.Available {
		t.Fatalf("checkName(cmp-free) = %+v, want valid=true available=true", nc)
	}
	// checkName: bad format -> valid:false, still 200.
	if nc := check("Bad Name"); nc.Valid {
		t.Fatalf("checkName(Bad Name) = %+v, want valid=false", nc)
	}

	// Rename via the custom method. It is not a PATCH: a rename breaks stored
	// external references, so it is an explicit act with its own permission.
	out := c.do(ownerTok, http.MethodPost, "/components/cmp-one:rename", map[string]any{"name": "cmp-renamed"}, http.StatusOK)
	var renamed struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "cmp-renamed" {
		t.Fatalf("name = %q, want cmp-renamed", renamed.Name)
	}

	// Afterwards the component answers to the new name and to its uuid, and the old
	// name is gone.
	c.do(ownerTok, http.MethodGet, "/components/cmp-renamed", nil, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/components/"+renamed.ID, nil, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/components/cmp-one", nil, http.StatusNotFound)

	// The name is no longer patchable: two ways to rename is what this method removed.
	c.do(ownerTok, http.MethodPatch, "/components/cmp-renamed", map[string]any{"name": "cmp-sneaky"}, http.StatusUnprocessableEntity)

	// Dup rename -> 409.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "cmp-two", "product": "generic-device"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components/cmp-two:rename", map[string]any{"name": "cmp-renamed"}, http.StatusConflict)

	// Bad format -> 422 (Huma pattern rejects at the edge).
	c.do(ownerTok, http.MethodPost, "/components/cmp-two:rename", map[string]any{"name": "Bad Name"}, http.StatusUnprocessableEntity)

	// A uuid-shaped name passes the slug pattern and is refused by the gateway, so
	// the name and the id can never be the same shape.
	c.do(ownerTok, http.MethodPost, "/components/cmp-two:rename",
		map[string]any{"name": "019f8754-461f-7b82-b5f2-fc4bbe1c3765"}, http.StatusUnprocessableEntity)

	// Create-tightening: a bad name is rejected at create too, not just rename.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "Bad Name", "product": "generic-device"}, http.StatusUnprocessableEntity)
}

// TestComponentCheckNameIsScopedToPlacement drives the #627 fix over HTTP:
// checkName now reports availability against the SAME placement bucket a
// create would land in, named by the Parent field, not a single global fact.
// A name taken under one parent is reported free under another (the exact
// false-positive that used to block a legal create), and the advisory stays
// blind to the caller's OWN grant scope, same as before. Skipped under -short.
func TestComponentCheckNameIsScopedToPlacement(t *testing.T) {
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

	// Two root components, and a "sub-1" child under scope-disp only.
	var disp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "scope-disp", "product": "generic-device"}, http.StatusCreated), &disp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "scope-cam", "product": "generic-device"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "sub-1", "product": "generic-device", "parent": "scope-disp"}, http.StatusCreated)

	// A deploy principal (component:update) scoped ONLY to scope-disp.
	deployTok := setupScopedViewer(t, ctx, dsn, "deploy-disp", "deploy", "component", disp.ID)
	// It cannot read scope-cam (out of scope -> non-disclosing 404).
	c.do(deployTok, http.MethodGet, "/components/scope-cam", nil, http.StatusNotFound)

	check := func(body map[string]any) (valid, available bool) {
		out := c.do(deployTok, http.MethodPost, "/components:checkName", body, http.StatusOK)
		var nc struct {
			Valid     bool `json:"valid"`
			Available bool `json:"available"`
		}
		if err := json.Unmarshal(out, &nc); err != nil {
			t.Fatalf("decode checkName: %v", err)
		}
		return nc.Valid, nc.Available
	}

	// sub-1 is taken under scope-disp, the same placement it was created at.
	if valid, available := check(map[string]any{"name": "sub-1", "parent": "scope-disp"}); !valid || available {
		t.Fatalf("checkName(sub-1, parent=scope-disp) = valid=%v available=%v, want true,false", valid, available)
	}
	// The identical name is FREE unplaced: this is the false positive #627
	// fixes. The caller cannot even GET scope-cam, and the check still
	// answers correctly for a bucket it names explicitly (blind to the
	// caller's own grant, aware of the placement bucket named in the
	// request).
	if valid, available := check(map[string]any{"name": "sub-1"}); !valid || !available {
		t.Fatalf("checkName(sub-1, unplaced) = valid=%v available=%v, want true,true", valid, available)
	}
	// scope-cam is taken unplaced (no parent, no location): the advisory
	// remains scope-blind to the caller's own grant for this bucket too.
	if valid, available := check(map[string]any{"name": "scope-cam"}); !valid || available {
		t.Fatalf("checkName(scope-cam, unplaced) = valid=%v available=%v, want true,false", valid, available)
	}
}

// TestComponentRequiresProduct drives the #614 component floor over HTTP: a
// create with no product is a 422 whose message names generic-device (the
// escape hatch, an operator can act on the message immediately), a create
// with a product succeeds, patch still reclassifies, and patch no longer
// treats an explicit empty string as a clear (also a 422). Skipped under
// -short.
func TestComponentRequiresProduct(t *testing.T) {
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

	// No product at all: 422, naming generic-device.
	status, body := c.send(ownerTok, http.MethodPost, "/components", map[string]any{"name": "no-product"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("create with no product status = %d, want 422\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "generic-device") {
		t.Fatalf("create with no product body = %s, want it to name generic-device", body)
	}

	// An explicit empty-string product is the same 422, not a silent no-op.
	status, body = c.send(ownerTok, http.MethodPost, "/components", map[string]any{"name": "empty-product", "product": ""})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("create with empty product status = %d, want 422\nbody: %s", status, body)
	}

	// A named product succeeds.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "has-product", "product": "generic-device"}, http.StatusCreated)

	// PATCH still reclassifies to a real product (minted, not a seeded SKU).
	p := storagetest.MintProduct(t, ctx, gw, "")
	c.do(ownerTok, http.MethodPatch, "/components/has-product", map[string]any{"product": p.Name}, http.StatusOK)
	reread := struct {
		Product string `json:"product"`
	}{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/has-product", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if reread.Product != p.Name {
		t.Fatalf("product after reclassify = %q, want %q", reread.Product, p.Name)
	}

	// PATCH no longer clears with an explicit empty string: 422, and the
	// product is left exactly as the reclassify left it.
	c.do(ownerTok, http.MethodPatch, "/components/has-product", map[string]any{"product": ""}, http.StatusUnprocessableEntity)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/has-product", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get after refused clear: %v", err)
	}
	if reread.Product != p.Name {
		t.Fatalf("product after refused clear = %q, want it unchanged (%q)", reread.Product, p.Name)
	}
}

// TestGetComponentByAmbiguousNameIs409 is the DDL-and-refusal-ship-together
// half of #627 proved over HTTP: mapComponentErr must translate
// storage.ErrAmbiguousName to a 409 (via the shared mapRefErr, moved into this
// task from its original later slot) rather than falling through to its
// default 500. Landing the migration without this wiring would turn a
// routine duplicate-name lookup into an unmapped internal error the moment two
// rooms hold the same name, which is exactly the DDL's own effect. dup-1 is
// created twice in different placement buckets (unplaced, and under a
// parent), which #627 makes legal, so GET by the now-ambiguous bare name has
// no single answer and must be refused, not guessed. Skipped under -short.
func TestGetComponentByAmbiguousNameIs409(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "dup-1", "product": "generic-device"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "holder", "product": "generic-device"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "dup-1", "product": "generic-device", "parent": "holder"}, http.StatusCreated)

	status, body := c.send(ownerTok, http.MethodGet, "/components/dup-1", nil)
	if status != http.StatusConflict {
		t.Fatalf("get ambiguous bare name status = %d, want 409\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "dup-1") {
		t.Fatalf("409 body = %s, want it to name the ambiguous reference", body)
	}
}

// TestComponentCreateResolvesParentWithinScopeAndNeverLeaksCandidates drives
// ruling 2 (scope decides before ambiguity) over the CREATE path, not just
// the read path TestGetComponentByAmbiguousNameIs409 already covers: a
// deploy principal scoped to one component's subtree posts a bare parent
// name that is ambiguous FLEET-WIDE but unique within its own scope, and the
// create must resolve to the in-scope row cleanly (not 409, not a coin-flip
// onto the wrong "shared") rather than the pre-fix scope-blind resolve that
// would either land on the wrong parent or refuse a create that should
// succeed. It then drives the true 409: two in-scope matches for the same
// bare name, and asserts the response body carries neither in-scope uuid it
// cannot yet distinguish between NOR the out-of-scope uuid the caller was
// never allowed to see, closing the disclosure C2 named (mapRefErr's
// Candidates list, not just this one caller's principal grant).
func TestComponentCreateResolvesParentWithinScopeAndNeverLeaksCandidates(t *testing.T) {
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

	// container is the scope root. wingA and wingB are two of its children,
	// each holding a child of its own named "shared" (different parent
	// buckets, so the DDL legalizes both). A THIRD "shared", entirely
	// unrelated and outside container's subtree, is what the caller must
	// never see the uuid of.
	var container struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "container", "product": "generic-device"}, http.StatusCreated), &container); err != nil {
		t.Fatalf("decode container: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "wing-a", "product": "generic-device", "parent": "container"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "wing-b", "product": "generic-device", "parent": "container"}, http.StatusCreated)
	var wingAShared struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "shared", "product": "generic-device", "parent": "wing-a"}, http.StatusCreated), &wingAShared); err != nil {
		t.Fatalf("decode wingA/shared: %v", err)
	}
	var outOfScopeShared struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "shared", "product": "generic-device"}, http.StatusCreated), &outOfScopeShared); err != nil {
		t.Fatalf("decode unplaced shared: %v", err)
	}

	deployTok := setupScopedViewer(t, ctx, dsn, "deploy-container", "deploy", "component", container.ID)
	// Confirmed out of reach for a direct read: the create below must not
	// silently resolve onto it either.
	c.do(deployTok, http.MethodGet, "/components/"+outOfScopeShared.ID, nil, http.StatusNotFound)

	// One in-scope "shared" (wingA/shared): the bare name is ambiguous
	// fleet-wide (this one plus the out-of-scope root), but unique within
	// the caller's own scope, so the create resolves cleanly onto it.
	var leaf struct {
		ID       string `json:"id"`
		ParentID string `json:"parent_id"`
	}
	if err := json.Unmarshal(c.do(deployTok, http.MethodPost, "/components", map[string]any{"name": "leaf-1", "product": "generic-device", "parent": "shared"}, http.StatusCreated), &leaf); err != nil {
		t.Fatalf("decode leaf-1: %v", err)
	}
	if leaf.ParentID != wingAShared.ID {
		t.Fatalf("leaf-1 parent_id = %s, want the in-scope wingA/shared %s (not the out-of-scope shared or a guess)", leaf.ParentID, wingAShared.ID)
	}

	// A second "shared", under wingB this time, makes the bare name
	// ambiguous WITHIN the caller's own scope too: now there is no single
	// answer, and the create must 409, not guess wingA's.
	var wingBShared struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "shared", "product": "generic-device", "parent": "wing-b"}, http.StatusCreated), &wingBShared); err != nil {
		t.Fatalf("decode wingB/shared: %v", err)
	}
	status, body := c.send(deployTok, http.MethodPost, "/components", map[string]any{"name": "leaf-2", "product": "generic-device", "parent": "shared"})
	if status != http.StatusConflict {
		t.Fatalf("create with in-scope-ambiguous parent status = %d, want 409\nbody: %s", status, body)
	}
	// The out-of-scope uuid must never appear in the body (the disclosure
	// C2 named); the two in-scope candidates MAY appear, per mapRefErr's
	// contract of listing only what the caller's own scope proved exists.
	if strings.Contains(string(body), outOfScopeShared.ID) {
		t.Fatalf("409 body = %s, leaks the out-of-scope shared's uuid %s", body, outOfScopeShared.ID)
	}
}

// TestComponentCreateWithoutNameGenerates drives #627 Task 14 over HTTP: name
// is optional on create (a dropped Huma "required", not just a Go zero value),
// the platform mints "<stem>-<n>" from the named product's component_type,
// and the read side reports name_generated=true. Supplying a name still works
// exactly as before, reporting name_generated=false.
func TestComponentCreateWithoutNameGenerates(t *testing.T) {
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

	// A product classified display, so the platform mints "display-<n>".
	disp := storagetest.MintProduct(t, ctx, gw, "display")

	type comp struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		NameGenerated bool   `json:"name_generated"`
	}

	// No "name" key in the body at all: the omission itself, not an empty
	// string, is what the Huma pattern/minLength guards must tolerate.
	var first comp
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"product": disp.Name}, http.StatusCreated), &first); err != nil {
		t.Fatalf("decode create without name: %v", err)
	}
	if first.Name != "display-1" || !first.NameGenerated {
		t.Fatalf("create without name = %+v, want name=display-1 name_generated=true", first)
	}

	// The read side (a plain GET) reports the same flag: it survives a round
	// trip through the API, not just the create response.
	var got comp
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/"+first.ID, nil, http.StatusOK), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if !got.NameGenerated {
		t.Fatalf("get after create = %+v, want name_generated=true on the read side too", got)
	}

	// Supplying a name still behaves exactly as before.
	var typed comp
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "operator-typed", "product": disp.Name}, http.StatusCreated), &typed); err != nil {
		t.Fatalf("decode create with name: %v", err)
	}
	if typed.Name != "operator-typed" || typed.NameGenerated {
		t.Fatalf("create with an explicit name = %+v, want name=operator-typed name_generated=false", typed)
	}
}

// TestComponentResetName drives :resetName over HTTP: it regenerates the
// name and flips name_generated to true, and it is gated by the SAME
// component:rename token :rename uses (a scoped principal holding rename but
// not the all-scope create grant can still call it).
func TestComponentResetName(t *testing.T) {
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

	// A product classified display, so :resetName mints "display-1".
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "reset-me", "product": disp.Name}, http.StatusCreated)

	var reset struct {
		Name          string `json:"name"`
		NameGenerated bool   `json:"name_generated"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/reset-me:resetName", nil, http.StatusOK), &reset); err != nil {
		t.Fatalf("decode resetName: %v", err)
	}
	if reset.Name != "display-1" || !reset.NameGenerated {
		t.Fatalf("resetName = %+v, want name=display-1 name_generated=true", reset)
	}

	// A missing component is the ordinary 404, same as :rename.
	c.do(ownerTok, http.MethodPost, "/components/no-such-component:resetName", nil, http.StatusNotFound)
}
