package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationTypesAPI drives the location_type registry read endpoint: an owner
// lists the seeded official types in alphabetical order (by display_name), each
// with its display_name, so a form can populate a type picker (value = id, label
// = display_name). The 403 for a principal without type:read is covered
// generically by TestEveryRouteIsGated.
func TestLocationTypesAPI(t *testing.T) {
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

	out := c.do(ownerTok, http.MethodGet, "/types/location", nil, http.StatusOK)
	var body struct {
		LocationTypes []struct {
			ID                 string   `json:"id"`
			DisplayName        string   `json:"display_name"`
			Icon               string   `json:"icon"`
			Official           bool     `json:"official"`
			AllowedParentTypes []string `json:"allowed_parent_types"`
		} `json:"location_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The four seeded official types, in alphabetical order by display_name
	// (Building, Campus, Floor, Room), each labelled and official.
	want := []string{"building", "campus", "floor", "room"}
	gotIDs := make([]string, len(body.LocationTypes))
	for i, lt := range body.LocationTypes {
		gotIDs[i] = lt.ID
	}
	if len(gotIDs) != len(want) {
		t.Fatalf("location_types = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("location_types order = %v, want %v", gotIDs, want)
		}
	}
	for _, lt := range body.LocationTypes {
		if lt.DisplayName == "" || !lt.Official {
			t.Errorf("type %q: display_name=%q official=%v, want non-empty label + official", lt.ID, lt.DisplayName, lt.Official)
		}
	}
	// The icon travels the wire so the console can render each type's leading tree
	// glyph without a second lookup.
	wantIcons := map[string]string{"campus": "landmark", "building": "building", "floor": "layers", "room": "door-open"}
	for _, lt := range body.LocationTypes {
		if lt.Icon != wantIcons[lt.ID] {
			t.Errorf("type %q: icon=%q, want %q", lt.ID, lt.Icon, wantIcons[lt.ID])
		}
	}

	// allowed_parent_types travels the wire, matching the seeded hierarchy.
	wantParents := map[string][]string{
		"campus": {"root"}, "building": {"root", "campus"},
		"floor": {"building", "campus"}, "room": {"floor", "building", "campus"},
	}
	for _, lt := range body.LocationTypes {
		want := wantParents[lt.ID]
		if len(lt.AllowedParentTypes) != len(want) {
			t.Errorf("type %q: allowed_parent_types = %v, want %v", lt.ID, lt.AllowedParentTypes, want)
			continue
		}
		for i := range want {
			if lt.AllowedParentTypes[i] != want[i] {
				t.Errorf("type %q: allowed_parent_types = %v, want %v", lt.ID, lt.AllowedParentTypes, want)
			}
		}
	}
}

func TestLocationTypeCRUDAPI(t *testing.T) {
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

	// Create a custom type (201), then it appears in the list.
	c.do(ownerTok, http.MethodPost, "/types/location",
		map[string]any{"id": "wing", "display_name": "Wing", "icon": "layers"}, http.StatusCreated)

	// Update it (200).
	c.do(ownerTok, http.MethodPatch, "/types/location/wing",
		map[string]any{"display_name": "West Wing"}, http.StatusOK)

	// Official rows are read-only (422 on update and delete).
	c.do(ownerTok, http.MethodPatch, "/types/location/campus",
		map[string]any{"display_name": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/types/location/campus", nil, http.StatusUnprocessableEntity)

	// "root" is reserved: creating a type with that id is refused (422).
	c.do(ownerTok, http.MethodPost, "/types/location",
		map[string]any{"id": "root", "display_name": "Root"}, http.StatusUnprocessableEntity)

	// allowed_parent_types round-trips through create and update.
	c.do(ownerTok, http.MethodPost, "/types/location",
		map[string]any{"id": "annex", "display_name": "Annex", "allowed_parent_types": []string{"wing", "root"}}, http.StatusCreated)
	out := c.do(ownerTok, http.MethodGet, "/types/location", nil, http.StatusOK)
	var listBody struct {
		LocationTypes []struct {
			ID                 string   `json:"id"`
			AllowedParentTypes []string `json:"allowed_parent_types"`
		} `json:"location_types"`
	}
	if err := json.Unmarshal(out, &listBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, lt := range listBody.LocationTypes {
		if lt.ID == "annex" {
			found = true
			if len(lt.AllowedParentTypes) != 2 || lt.AllowedParentTypes[0] != "wing" || lt.AllowedParentTypes[1] != "root" {
				t.Errorf("annex allowed_parent_types = %v, want [wing root]", lt.AllowedParentTypes)
			}
		}
	}
	if !found {
		t.Fatal("annex type not in list")
	}
	c.do(ownerTok, http.MethodPatch, "/types/location/annex",
		map[string]any{"allowed_parent_types": []string{}}, http.StatusOK)

	// In use: place a location of type wing, delete is refused (409).
	c.do(ownerTok, http.MethodPost, "/locations",
		map[string]any{"name": "w1", "location_type": "wing"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/types/location/wing", nil, http.StatusConflict)

	// Remove the location, then the type deletes (204).
	c.do(ownerTok, http.MethodDelete, "/locations/w1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/types/location/wing", nil, http.StatusNoContent)
}
