package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestLocationPlacementAPI drives the allowed_parent_types enforcement over
// HTTP: an out-of-order create is a 422 naming both types, a valid move
// succeeds, an out-of-order move is the same 422, and a grandfathered
// placement is untouched by an unrelated update. Skipped under -short (it
// opens a real Postgres testcontainer via storagetest).
func TestLocationPlacementAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

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

	c.create(ownerTok, locReq{Name: "hq", LocationType: "campus"}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "lab", LocationType: "campus"}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "hq-b1", LocationType: "building", Parent: ptr("hq")}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "hq-r1", LocationType: "room", Parent: ptr("hq-b1")}, http.StatusCreated)

	// Out of order on create: a floor under a room is refused (422), naming
	// both types.
	body := c.do(ownerTok, http.MethodPost, "/locations",
		map[string]any{"name": "bad-floor", "location_type": "floor", "parent": "hq-r1"}, http.StatusUnprocessableEntity)
	if !bytes.Contains(body, []byte("floor")) || !bytes.Contains(body, []byte("room")) {
		t.Errorf("create-placement 422 body = %s, want it to name floor and room", body)
	}

	// A valid move: hq-b1 (building, allowed={root,campus}) moves from hq to
	// lab, both campuses.
	body = c.do(ownerTok, http.MethodPost, "/locations/hq-b1:move", map[string]any{"parent": "lab"}, http.StatusOK)
	var moved locResp
	if err := json.Unmarshal(body, &moved); err != nil {
		t.Fatalf("decode moved location: %v", err)
	}
	if moved.Name != "hq-b1" {
		t.Fatalf("moved location name = %q, want hq-b1", moved.Name)
	}

	// Out of order on move: hq-b1 (building) under hq-r1 (room) is refused
	// (422), naming both types.
	body = c.do(ownerTok, http.MethodPost, "/locations/hq-b1:move", map[string]any{"parent": "hq-r1"}, http.StatusUnprocessableEntity)
	if !bytes.Contains(body, []byte("building")) || !bytes.Contains(body, []byte("room")) {
		t.Errorf("move-placement 422 body = %s, want it to name building and room", body)
	}

	// Grandfathered: an unrelated field update on hq-r1 (a room, already
	// placed) succeeds untouched.
	body = c.do(ownerTok, http.MethodPatch, "/locations/hq-r1", map[string]any{"label": "Room 1"}, http.StatusOK)
	var renamed locResp
	if err := json.Unmarshal(body, &renamed); err != nil {
		t.Fatalf("decode renamed location: %v", err)
	}
	if renamed.Label != "Room 1" {
		t.Errorf("renamed label = %q, want Room 1", renamed.Label)
	}
}

// TestLocationReclassifyPlacementAPI drives the reclassify-in-place enforcement
// over HTTP: PATCHing location_type to a type whose allowed_parent_types
// excludes the current parent is the same 422 as a move, naming the type and
// the parent's type, while resending the current type (what the console does on
// every save) succeeds.
func TestLocationReclassifyPlacementAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

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

	c.create(ownerTok, locReq{Name: "hq", LocationType: "campus"}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "hq-b1", LocationType: "building", Parent: ptr("hq")}, http.StatusCreated)

	// A campus allows only root, so retyping a building under hq into one is
	// refused (422), naming the type and the parent's type.
	body := c.do(ownerTok, http.MethodPatch, "/locations/hq-b1", map[string]any{"location_type": "campus"}, http.StatusUnprocessableEntity)
	if !bytes.Contains(body, []byte("campus is not allowed under campus")) {
		t.Errorf("reclassify 422 body = %s, want it to say campus is not allowed under campus", body)
	}

	// A floor is allowed under a campus: the reclassify succeeds.
	body = c.do(ownerTok, http.MethodPatch, "/locations/hq-b1", map[string]any{"location_type": "floor"}, http.StatusOK)
	var retyped locResp
	if err := json.Unmarshal(body, &retyped); err != nil {
		t.Fatalf("decode retyped location: %v", err)
	}
	if retyped.LocationType != "floor" {
		t.Errorf("retyped location_type = %q, want floor", retyped.LocationType)
	}

	// Resending the current type alongside a label edit is not a change.
	c.do(ownerTok, http.MethodPatch, "/locations/hq-b1", map[string]any{"location_type": "floor", "label": "Floor 1"}, http.StatusOK)
}
