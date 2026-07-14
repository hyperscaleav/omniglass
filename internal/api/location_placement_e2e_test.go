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
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationPlacementAPI drives the allowed_parent_types enforcement over
// HTTP: an out-of-order create is a 422 naming both types, a valid move
// succeeds, an out-of-order move is the same 422, and a grandfathered
// placement is untouched by an unrelated update. Skipped under -short (it
// opens a real Postgres testcontainer via storagetest).
func TestLocationPlacementAPI(t *testing.T) {
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
	body = c.do(ownerTok, http.MethodPatch, "/locations/hq-b1", map[string]any{"parent": "lab"}, http.StatusOK)
	var moved locResp
	if err := json.Unmarshal(body, &moved); err != nil {
		t.Fatalf("decode moved location: %v", err)
	}
	if moved.Name != "hq-b1" {
		t.Fatalf("moved location name = %q, want hq-b1", moved.Name)
	}

	// Out of order on move: hq-b1 (building) under hq-r1 (room) is refused
	// (422), naming both types.
	body = c.do(ownerTok, http.MethodPatch, "/locations/hq-b1", map[string]any{"parent": "hq-r1"}, http.StatusUnprocessableEntity)
	if !bytes.Contains(body, []byte("building")) || !bytes.Contains(body, []byte("room")) {
		t.Errorf("move-placement 422 body = %s, want it to name building and room", body)
	}

	// Grandfathered: an unrelated field update on hq-r1 (a room, already
	// placed) succeeds untouched.
	body = c.do(ownerTok, http.MethodPatch, "/locations/hq-r1", map[string]any{"display_name": "Room 1"}, http.StatusOK)
	var renamed locResp
	if err := json.Unmarshal(body, &renamed); err != nil {
		t.Fatalf("decode renamed location: %v", err)
	}
	if renamed.DisplayName != "Room 1" {
		t.Errorf("renamed display_name = %q, want Room 1", renamed.DisplayName)
	}
}
