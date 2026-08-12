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
// = display_name). The 403 for a principal without location_type:read is covered
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

	out := c.do(ownerTok, http.MethodGet, "/location-types", nil, http.StatusOK)
	var body struct {
		LocationTypes []struct {
			ID                 string   `json:"id"`
			Name               string   `json:"name"`
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
		gotIDs[i] = lt.Name
	}
	if len(gotIDs) != len(want) {
		t.Fatalf("location_types = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("location_types order = %v, want %v", gotIDs, want)
		}
	}
	// A shipped location type is OFFICIAL on the wire since #703: the platform
	// owns the row so a release can withdraw what it ships, and an operator's
	// edit of one forks rather than writing it. This assertion is the inverted
	// twin of the one it replaced ("want non-empty label + operator-owned").
	for _, lt := range body.LocationTypes {
		if lt.DisplayName == "" || !lt.Official {
			t.Errorf("type %q: display_name=%q official=%v, want non-empty label + platform-owned", lt.Name, lt.DisplayName, lt.Official)
		}
	}
	// The icon travels the wire so the console can render each type's leading tree
	// glyph without a second lookup.
	wantIcons := map[string]string{"campus": "landmark", "building": "building", "floor": "layers", "room": "door-open"}
	for _, lt := range body.LocationTypes {
		if lt.Icon != wantIcons[lt.Name] {
			t.Errorf("type %q: icon=%q, want %q", lt.Name, lt.Icon, wantIcons[lt.Name])
		}
	}

	// allowed_parent_types travels the wire, matching the seeded hierarchy.
	wantParents := map[string][]string{
		"campus": {"root"}, "building": {"root", "campus"},
		"floor": {"building", "campus"}, "room": {"floor", "building", "campus"},
	}
	for _, lt := range body.LocationTypes {
		want := wantParents[lt.Name]
		if len(lt.AllowedParentTypes) != len(want) {
			t.Errorf("type %q: allowed_parent_types = %v, want %v", lt.Name, lt.AllowedParentTypes, want)
			continue
		}
		for i := range want {
			if lt.AllowedParentTypes[i] != want[i] {
				t.Errorf("type %q: allowed_parent_types = %v, want %v", lt.Name, lt.AllowedParentTypes, want)
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
	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"name": "wing", "display_name": "Wing", "icon": "layers"}, http.StatusCreated)

	// Update it (200).
	c.do(ownerTok, http.MethodPatch, "/location-types/wing",
		map[string]any{"display_name": "West Wing"}, http.StatusOK)

	// A shipped type is editable: the estate shapes its own place vocabulary,
	// through a fork since #703. TestLocationTypeForkAndClearAPI holds what the
	// fork does on the wire.
	c.do(ownerTok, http.MethodPatch, "/location-types/campus",
		map[string]any{"display_name": "Campus"}, http.StatusOK)

	// "root" is reserved: creating a type with that id is refused (422).
	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"name": "root", "display_name": "Root"}, http.StatusUnprocessableEntity)

	// allowed_parent_types round-trips through create and update.
	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"name": "annex", "display_name": "Annex", "allowed_parent_types": []string{"wing", "root"}}, http.StatusCreated)
	out := c.do(ownerTok, http.MethodGet, "/location-types", nil, http.StatusOK)
	var listBody struct {
		LocationTypes []struct {
			ID                 string   `json:"id"`
			Name               string   `json:"name"`
			AllowedParentTypes []string `json:"allowed_parent_types"`
		} `json:"location_types"`
	}
	if err := json.Unmarshal(out, &listBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, lt := range listBody.LocationTypes {
		if lt.Name == "annex" {
			found = true
			if len(lt.AllowedParentTypes) != 2 || lt.AllowedParentTypes[0] != "wing" || lt.AllowedParentTypes[1] != "root" {
				t.Errorf("annex allowed_parent_types = %v, want [wing root]", lt.AllowedParentTypes)
			}
		}
	}
	if !found {
		t.Fatal("annex type not in list")
	}
	c.do(ownerTok, http.MethodPatch, "/location-types/annex",
		map[string]any{"allowed_parent_types": []string{}}, http.StatusOK)

	// In use: place a location of type wing, delete is refused (409).
	c.do(ownerTok, http.MethodPost, "/locations",
		map[string]any{"name": "w1", "location_type": "wing"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/location-types/wing", nil, http.StatusConflict)

	// Remove the location, then the type deletes (204).
	c.do(ownerTok, http.MethodDelete, "/locations/w1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/location-types/wing", nil, http.StatusNoContent)
}

// TestLocationTypeForkAndClearAPI drives the two halves of this slice as an
// operator meets them: editing a SHIPPED type on the wire (#703, which forks it
// under one unchanged id and offers `:restore`), and clearing its name rule
// (#692, the explicit null under update_mask that turns location auto-naming
// back off).
func TestLocationTypeForkAndClearAPI(t *testing.T) {
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

	type typeBody struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Official    bool   `json:"official"`
		Forked      bool   `json:"forked"`
		NameRule    *struct {
			Stem      string `json:"stem"`
			BareFirst bool   `json:"bare_first"`
		} `json:"name_rule"`
	}
	decode := func(raw []byte) typeBody {
		t.Helper()
		var b typeBody
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return b
	}

	// Editing a shipped type answers 200 with the SAME id, marked forked: the
	// namespace never reaches the URL or the response.
	shipped := decode(c.do(ownerTok, http.MethodPatch, "/location-types/campus",
		map[string]any{"display_name": "Site", "name_rule": map[string]any{"stem": "site"}}, http.StatusOK))
	if shipped.DisplayName != "Site" || !shipped.Official || !shipped.Forked {
		t.Fatalf("the forked shipped type = %+v, want Site with official and forked both true", shipped)
	}
	if shipped.NameRule == nil || shipped.NameRule.Stem != "site" {
		t.Fatalf("the rule on the forked type = %+v, want stem site", shipped.NameRule)
	}

	// An omitted key is still unchanged: only the mask clears.
	kept := decode(c.do(ownerTok, http.MethodPatch, "/location-types/campus",
		map[string]any{"display_name": "Site 2"}, http.StatusOK))
	if kept.NameRule == nil {
		t.Fatal("an unrelated patch cleared the rule; an omitted key means unchanged")
	}

	// The mask plus no rule is the clear, and it is the ONLY spelling of it.
	cleared := decode(c.do(ownerTok, http.MethodPatch, "/location-types/campus",
		map[string]any{"update_mask": []string{"name_rule"}}, http.StatusOK))
	if cleared.NameRule != nil {
		t.Fatalf("the rule after a masked clear = %+v, want absent (operator-named)", cleared.NameRule)
	}
	if cleared.DisplayName != "Site 2" {
		t.Fatalf("the masked clear also wrote display_name (%q); a mask writes exactly what it names", cleared.DisplayName)
	}

	// A mask naming a field this resource does not patch is a 422 naming it,
	// never a silent no-op (ADR-0091).
	c.do(ownerTok, http.MethodPatch, "/location-types/campus",
		map[string]any{"update_mask": []string{"official"}}, http.StatusUnprocessableEntity)

	// :restore discards the whole fork, rule included.
	restored := decode(c.do(ownerTok, http.MethodPost, "/location-types/campus:restore", nil, http.StatusOK))
	if restored.DisplayName != "Campus" || restored.Forked || restored.ID != shipped.ID {
		t.Fatalf("the restored type = %+v, want the shipped Campus under the same id, unforked", restored)
	}
	// Nothing left to discard is a 409 rather than a silent success.
	c.do(ownerTok, http.MethodPost, "/location-types/campus:restore", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/location-types/no-such-type:restore", nil, http.StatusNotFound)

	// The same clear on an operator's OWN type is a plain write of null, and
	// reads the same from here: two paths, one operator-visible behavior.
	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"name": "deck", "display_name": "Deck", "name_rule": map[string]any{"stem": ""}}, http.StatusCreated)
	own := decode(c.do(ownerTok, http.MethodPatch, "/location-types/deck",
		map[string]any{"update_mask": []string{"name_rule"}}, http.StatusOK))
	if own.NameRule != nil || own.Official || own.Forked {
		t.Fatalf("the operator's own type after a masked clear = %+v, want no rule and neither flag", own)
	}
}
