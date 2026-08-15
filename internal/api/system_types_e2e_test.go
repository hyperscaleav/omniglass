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

// systemTypeWire is the decoded system_type wire shape for the e2e assertions.
type systemTypeWire struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Label                 string `json:"label"`
	Stem                  string `json:"stem"`
	Icon                  string `json:"icon"`
	ResolvedIcon          string `json:"resolved_icon"`
	InheritedStem         string `json:"inherited_stem"`
	InheritedStemSource   string `json:"inherited_stem_source"`
	InheritedIcon         string `json:"inherited_icon"`
	InheritedIconSource   string `json:"inherited_icon_source"`
	InheritedAbbrev       string `json:"inherited_abbrev"`
	InheritedAbbrevSource string `json:"inherited_abbrev_source"`
	Abbrev                string `json:"abbrev"`
	Official              bool   `json:"official"`
	ParentID              string `json:"parent_id"`
	Parent                string `json:"parent"`
}

// TestSystemTypesAPI drives the system_type registry over HTTP on the same
// contract component_type's e2e pins: list shows the shipped tree with parent
// links in both forms, a viewer reads but creates nothing, an admin creates a
// custom child under a shipped node, the official rows are read-only, an
// unknown parent is a 422, and a delete is refused for a row that still parents
// another. The fork mechanism (#655) is a separate slice and does not land
// here, so official stays flatly read-only.
func TestSystemTypesAPI(t *testing.T) {
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

	list := func(tok string) []systemTypeWire {
		out := c.do(tok, http.MethodGet, "/system-types", nil, http.StatusOK)
		var body struct {
			SystemTypes []systemTypeWire `json:"system_types"`
		}
		if err := json.Unmarshal(out, &body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return body.SystemTypes
	}
	find := func(rows []systemTypeWire, name string) systemTypeWire {
		for _, r := range rows {
			if r.Name == name {
				return r
			}
		}
		t.Fatalf("system_type %q not in list", name)
		return systemTypeWire{}
	}

	// A viewer reads the shipped tree via the *:read floor.
	viewerTok := principalWithGrants(t, ctx, dsn, "st-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	rows := list(viewerTok)
	if len(rows) == 0 {
		t.Fatal("system_types empty, want the shipped tree")
	}

	av := find(rows, "av")
	if av.ParentID != "" || av.Parent != "" {
		t.Fatalf("av parent = %+v, want a root type with no parent", av)
	}
	if !av.Official || av.Stem == "" || av.Icon == "" {
		t.Fatalf("av = %+v, want official=true with its own stem and icon", av)
	}
	room := find(rows, "room")
	if room.ParentID != av.ID || room.Parent != "av" {
		t.Fatalf("room parent link = id:%q name:%q, want av's id %q and name", room.ParentID, room.Parent, av.ID)
	}
	// board states its own stem and abbrev (what #657's rules read) but no
	// icon: the wire carries the RAW row, inheritance is the resolver's job.
	board := find(rows, "board")
	if board.Stem != "boardroom" || board.Abbrev != "br" {
		t.Fatalf("board stem/abbrev = %q/%q, want boardroom/br", board.Stem, board.Abbrev)
	}
	if board.Icon != "" {
		t.Fatalf("board icon = %q, want empty on the wire (it inherits room's; this read does not resolve)", board.Icon)
	}
	// The RESOLVED icon is the one the console draws, and it is served beside
	// the raw one rather than instead of it (#695): the raw field is what an
	// edit blade posts back, the resolved field is what a glyph is drawn from.
	// board must show room's, never the root av's, which is the tier a walk
	// gets wrong by stopping too late.
	if board.ResolvedIcon != room.Icon {
		t.Fatalf("board resolved_icon = %q, want room's %q (the NEAREST ancestor that sets one, not av's %q)", board.ResolvedIcon, room.Icon, av.Icon)
	}
	if room.Icon == av.Icon {
		t.Fatalf("the fixture cannot tell the tiers apart: room and av both show %q", room.Icon)
	}
	if av.ResolvedIcon != av.Icon {
		t.Fatalf("av resolved_icon = %q, want its own icon %q", av.ResolvedIcon, av.Icon)
	}

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/system-types",
		map[string]any{"name": "nope", "label": "Nope"}, http.StatusForbidden)

	// Admin (owner) creates a custom child under room.
	var created systemTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "custom-room", "label": "Custom Room", "parent_id": "room", "abbrev": "cst",
	}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Name != "custom-room" || created.Official {
		t.Fatalf("created = %+v, want name=custom-room official=false", created)
	}
	if created.ParentID != room.ID || created.Parent != "room" {
		t.Fatalf("created parent link = id:%q name:%q, want room's id and name", created.ParentID, created.Parent)
	}
	if found := find(list(ownerTok), "custom-room"); found.ParentID != room.ID {
		t.Fatalf("relisted custom-room parent_id = %q, want %q", found.ParentID, room.ID)
	}

	// An unknown parent is a 422; a stemless ROOT is a 422 too (nothing to
	// inherit from).
	c.do(ownerTok, http.MethodPost, "/system-types",
		map[string]any{"name": "orphan", "label": "Orphan", "parent_id": "no-such-type"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/system-types",
		map[string]any{"name": "stemless-root", "label": "Stemless Root"}, http.StatusUnprocessableEntity)

	// A stem is a name prefix, so a bad slug is refused at the edge on both verbs.
	c.do(ownerTok, http.MethodPost, "/system-types",
		map[string]any{"name": "bad-stem", "label": "Bad Stem", "stem": "Bad Stem"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/system-types/custom-room",
		map[string]any{"stem": "also bad"}, http.StatusUnprocessableEntity)

	// The custom row is mutable; the shipped one is not, on either verb.
	c.do(ownerTok, http.MethodPatch, "/system-types/custom-room",
		map[string]any{"label": "Custom Room Pro"}, http.StatusOK)
	c.do(ownerTok, http.MethodPatch, "/system-types/room",
		map[string]any{"label": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/system-types/room", nil, http.StatusUnprocessableEntity)

	// A custom row that still parents another is refused (409); its leaf is not.
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "custom-room-leaf", "label": "Custom Room Leaf", "parent_id": "custom-room",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/system-types/custom-room", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/system-types/custom-room-leaf", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/system-types/custom-room", nil, http.StatusNoContent)
}

// TestSystemCarriesItsTypeAPI drives the classification from the system's side:
// set on create, read back in both forms on a GET and a LIST, repointed by a
// patch, cleared by the empty string, and refused (422) for a type that names
// nothing. A type still classifying a system cannot be deleted (409), which is
// the #507 no-cascade lesson observed from the outside.
func TestSystemCarriesItsTypeAPI(t *testing.T) {
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

	type sysWire struct {
		Name         string `json:"name"`
		SystemType   string `json:"system_type"`
		SystemTypeID string `json:"system_type_id"`
	}
	decode := func(b []byte) sysWire {
		var s sysWire
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatalf("decode system: %v", err)
		}
		return s
	}

	created := decode(c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "sct-api-room", "label": "Room 101", "system_type_id": "board",
	}, http.StatusCreated))
	if created.SystemType != "board" || created.SystemTypeID == "" {
		t.Fatalf("created system = %+v, want system_type board with its uuid", created)
	}
	typeID := created.SystemTypeID

	got := decode(c.do(ownerTok, http.MethodGet, "/systems/sct-api-room", nil, http.StatusOK))
	if got.SystemType != "board" || got.SystemTypeID != typeID {
		t.Fatalf("GET system = %+v, want system_type board (%s)", got, typeID)
	}

	// A patch that names another field leaves the classification alone.
	kept := decode(c.do(ownerTok, http.MethodPatch, "/systems/sct-api-room",
		map[string]any{"label": "Room 101A"}, http.StatusOK))
	if kept.SystemType != "board" {
		t.Fatalf("system_type after an unrelated patch = %q, want it unchanged (board)", kept.SystemType)
	}

	moved := decode(c.do(ownerTok, http.MethodPatch, "/systems/sct-api-room",
		map[string]any{"system_type_id": "huddle"}, http.StatusOK))
	if moved.SystemType != "huddle" || moved.SystemTypeID == typeID {
		t.Fatalf("system_type after reclassify = %+v, want huddle with a different uuid", moved)
	}

	// A type that still classifies a system cannot be deleted.
	c.do(ownerTok, http.MethodDelete, "/system-types/huddle", nil, http.StatusUnprocessableEntity) // official first

	cleared := decode(c.do(ownerTok, http.MethodPatch, "/systems/sct-api-room",
		map[string]any{"system_type_id": ""}, http.StatusOK))
	if cleared.SystemType != "" || cleared.SystemTypeID != "" {
		t.Fatalf("system_type after the empty-string clear = %+v, want both empty", cleared)
	}

	// An unknown type is a 422 on both write paths.
	c.do(ownerTok, http.MethodPatch, "/systems/sct-api-room",
		map[string]any{"system_type_id": "no-such-system-type"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/systems",
		map[string]any{"name": "sct-api-bogus", "system_type_id": "no-such-system-type"}, http.StatusUnprocessableEntity)

	// A custom type in use is the same refusal without the official short-circuit.
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "sct-api-custom", "label": "Custom", "parent_id": "room",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/systems/sct-api-room",
		map[string]any{"system_type_id": "sct-api-custom"}, http.StatusOK)
	c.do(ownerTok, http.MethodDelete, "/system-types/sct-api-custom", nil, http.StatusConflict)
}
