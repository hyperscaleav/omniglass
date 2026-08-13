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

// componentTypeWire is the decoded component_type wire shape for the e2e
// assertions.
type componentTypeWire struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Stem         string   `json:"stem"`
	Icon         string   `json:"icon"`
	ResolvedIcon string   `json:"resolved_icon"`
	Abbrev       string   `json:"abbrev"`
	LabelRule    string   `json:"label_rule"`
	DefaultTags  []string `json:"default_tags"`
	Official     bool     `json:"official"`
	Forked       bool     `json:"forked"`
	ParentID     string   `json:"parent_id"`
	Parent       string   `json:"parent"`
}

// TestComponentTypesAPI drives the component_type registry over HTTP: list
// shows the seeded tree with parent links (a child's parent id AND name), a
// viewer creates nothing (403), an admin (owner) creates a custom child under
// an existing root (mic), and delete refuses an official row and a row still
// parenting another. Skipped under -short.
func TestComponentTypesAPI(t *testing.T) {
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

	list := func(tok string) []componentTypeWire {
		out := c.do(tok, http.MethodGet, "/component-types", nil, http.StatusOK)
		var body struct {
			ComponentTypes []componentTypeWire `json:"component_types"`
		}
		if err := json.Unmarshal(out, &body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return body.ComponentTypes
	}
	find := func(rows []componentTypeWire, name string) componentTypeWire {
		for _, r := range rows {
			if r.Name == name {
				return r
			}
		}
		t.Fatalf("component_type %q not in list", name)
		return componentTypeWire{}
	}

	// A viewer reads the seeded tree via the *:read floor.
	viewerTok := principalWithGrants(t, ctx, dsn, "ct-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	rows := list(viewerTok)
	if len(rows) == 0 {
		t.Fatal("component_types empty, want the seeded tree")
	}

	// A root type carries no parent link; a child carries both forms (the
	// stable id and the display name), and it is official (seeded reference
	// data, unlike location_type's editable example content).
	mic := find(rows, "mic")
	if mic.ParentID != "" || mic.Parent != "" {
		t.Fatalf("mic parent = %+v, want a root type with no parent", mic)
	}
	if !mic.Official || mic.Stem != "mic" || mic.Icon != "mic" {
		t.Fatalf("mic = %+v, want official=true stem=mic icon=mic", mic)
	}
	wireless := find(rows, "wireless-mic")
	if wireless.ParentID != mic.ID || wireless.Parent != "mic" {
		t.Fatalf("wireless-mic parent link = id:%q name:%q, want mic's id and name %q", wireless.ParentID, wireless.Parent, mic.ID)
	}
	// wireless-mic declares no stem/icon of its own in the seed: the wire
	// carries the raw (uninherited) row, resolution is ResolveTypeFacts's job.
	if wireless.Stem != "" || wireless.Icon != "" {
		t.Fatalf("wireless-mic stem/icon = %q/%q, want both empty (inherits, not resolved on this read)", wireless.Stem, wireless.Icon)
	}
	// The RESOLVED icon is served beside the raw one rather than instead of it
	// (#695), which is what lets an edit blade post the raw field back while a
	// list cell draws the inherited glyph. Before this, the console climbed the
	// chain itself in TypeScript against the gateway's climb in Go.
	if wireless.ResolvedIcon != mic.Icon {
		t.Fatalf("wireless-mic resolved_icon = %q, want its ancestor mic's %q", wireless.ResolvedIcon, mic.Icon)
	}
	if mic.ResolvedIcon != mic.Icon {
		t.Fatalf("mic resolved_icon = %q, want its own icon %q", mic.ResolvedIcon, mic.Icon)
	}

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/component-types",
		map[string]any{"name": "nope", "display_name": "Nope"}, http.StatusForbidden)

	// Admin (owner) creates a custom child under mic.
	var created componentTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/component-types", map[string]any{
		"name": "custom-mic", "display_name": "Custom Mic", "parent_id": "mic",
	}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Name != "custom-mic" || created.Official {
		t.Fatalf("created = %+v, want name=custom-mic official=false", created)
	}
	if created.ParentID != mic.ID || created.Parent != "mic" {
		t.Fatalf("created parent link = id:%q name:%q, want mic's id and name", created.ParentID, created.Parent)
	}
	// And it shows up in a fresh list.
	found := find(list(ownerTok), "custom-mic")
	if found.ParentID != mic.ID {
		t.Fatalf("relisted custom-mic parent_id = %q, want %q", found.ParentID, mic.ID)
	}

	// An unknown parent is a 422.
	c.do(ownerTok, http.MethodPost, "/component-types",
		map[string]any{"name": "orphan", "display_name": "Orphan", "parent_id": "no-such-type"}, http.StatusUnprocessableEntity)

	// The custom row is mutable.
	c.do(ownerTok, http.MethodPatch, "/component-types/custom-mic",
		map[string]any{"display_name": "Custom Mic Pro"}, http.StatusOK)

	// The seeded official row (mic) is not writable but is no longer a dead
	// end: a patch forks it (#655, ADR-0095), covered end to end by
	// TestComponentTypeForkAndRestoreAPI below. Delete still refuses it (still
	// official, independent of its children and of any fork).
	c.do(ownerTok, http.MethodDelete, "/component-types/mic", nil, http.StatusUnprocessableEntity)

	// A row still parenting another cannot be deleted (409): custom-mic has no
	// children, so it deletes cleanly; a seeded parent like mic would 409 on
	// its children if it were not already refused for being official first.
	c.do(ownerTok, http.MethodDelete, "/component-types/custom-mic", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
}

// TestComponentTypeStemRejectsBadNames drives the #627 Task 14 fix over
// HTTP: a stem is a name prefix (the generator mints it straight into a
// component's name), so it is refused a bad slug at the edge exactly like
// name is, on both create and update, and a valid one still works.
func TestComponentTypeStemRejectsBadNames(t *testing.T) {
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

	// A stem with a space and an uppercase letter is refused on create.
	c.do(ownerTok, http.MethodPost, "/component-types",
		map[string]any{"name": "stem-bad-create", "display_name": "Stem Bad Create", "stem": "Bad Stem"},
		http.StatusUnprocessableEntity)

	// A valid stem still works.
	var created componentTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/component-types", map[string]any{
		"name": "stem-ok", "display_name": "Stem OK", "stem": "good-stem",
	}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create with a valid stem: %v", err)
	}
	if created.Stem != "good-stem" {
		t.Fatalf("created stem = %q, want good-stem", created.Stem)
	}

	// The same guard applies to an update of an existing row.
	c.do(ownerTok, http.MethodPatch, "/component-types/stem-ok",
		map[string]any{"stem": "also bad"}, http.StatusUnprocessableEntity)
}

// TestComponentTypeForkAndRestoreAPI drives #655 over HTTP: patching a shipped
// row forks it under the SAME id, every read (by name and by uuid, list and
// blade alike) returns the operator's version with no namespace anywhere in
// the address, the shipped row is still shipped, `:restore` puts the shipped
// values back, and a caller who cannot update cannot fork.
func TestComponentTypeForkAndRestoreAPI(t *testing.T) {
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

	find := func(tok, name string) componentTypeWire {
		out := c.do(tok, http.MethodGet, "/component-types", nil, http.StatusOK)
		var body struct {
			ComponentTypes []componentTypeWire `json:"component_types"`
		}
		if err := json.Unmarshal(out, &body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		var hits []componentTypeWire
		for _, r := range body.ComponentTypes {
			if r.Name == name {
				hits = append(hits, r)
			}
		}
		if len(hits) != 1 {
			t.Fatalf("%q appears %d times in the list, want exactly 1 (a fork is an overlay, not a second row)", name, len(hits))
		}
		return hits[0]
	}

	shipped := find(ownerTok, "mic")
	if !shipped.Official || shipped.Forked {
		t.Fatalf("mic = official:%v forked:%v, want shipped and unforked", shipped.Official, shipped.Forked)
	}

	// A viewer holds component_type:read but not :update, so they cannot fork
	// a row they could not have written either.
	viewerTok := principalWithGrants(t, ctx, dsn, "fork-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	c.do(viewerTok, http.MethodPatch, "/component-types/mic",
		map[string]any{"display_name": "Viewer Mic"}, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/component-types/mic:restore", nil, http.StatusForbidden)
	if again := find(ownerTok, "mic"); again.Forked || again.DisplayName != shipped.DisplayName {
		t.Fatalf("mic = %+v after the refused viewer patch, want untouched", again)
	}

	// The owner's patch forks: 200, the same id, the operator's values, and
	// forked=true beside a still-true official.
	var forked componentTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/component-types/mic",
		map[string]any{"display_name": "House Microphone", "abbrev": "hm"}, http.StatusOK), &forked); err != nil {
		t.Fatalf("decode fork: %v", err)
	}
	if forked.ID != shipped.ID {
		t.Fatalf("forked id = %q, want the shipped id %q (a fork never re-addresses the row)", forked.ID, shipped.ID)
	}
	if !forked.Official || !forked.Forked {
		t.Fatalf("forked = official:%v forked:%v, want both true (yours, overriding shipped)", forked.Official, forked.Forked)
	}
	if forked.DisplayName != "House Microphone" || forked.Abbrev != "hm" {
		t.Fatalf("forked = %+v, want the operator's display_name and abbrev", forked)
	}
	if listed := find(ownerTok, "mic"); listed.DisplayName != "House Microphone" || !listed.Forked {
		t.Fatalf("relisted mic = %+v, want the forked values", listed)
	}

	// Addressing by uuid resolves the same logical row: no namespace in the
	// URL, no second handle for the operator's copy.
	var byUUID componentTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/component-types/"+shipped.ID,
		map[string]any{"icon": "mic-house"}, http.StatusOK), &byUUID); err != nil {
		t.Fatalf("decode fork by uuid: %v", err)
	}
	if byUUID.ID != shipped.ID || byUUID.DisplayName != "House Microphone" || byUUID.Icon != "mic-house" {
		t.Fatalf("patch by uuid = %+v, want the same row carrying both edits", byUUID)
	}

	// Delete is still refused on a shipped row, forked or not.
	c.do(ownerTok, http.MethodDelete, "/component-types/mic", nil, http.StatusUnprocessableEntity)

	// Restore discards the fork and the shipped values come back.
	var restored componentTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/component-types/mic:restore", nil, http.StatusOK), &restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Forked || restored.DisplayName != shipped.DisplayName || restored.Abbrev != shipped.Abbrev || restored.Icon != shipped.Icon {
		t.Fatalf("restored = %+v, want the shipped row %+v unforked", restored, shipped)
	}
	if listed := find(ownerTok, "mic"); listed.Forked || listed.DisplayName != shipped.DisplayName {
		t.Fatalf("relisted mic after restore = %+v, want the shipped values", listed)
	}

	// Restoring again has nothing to discard.
	c.do(ownerTok, http.MethodPost, "/component-types/mic:restore", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/component-types/no-such-type:restore", nil, http.StatusNotFound)
}

// TestLabelRuleIsRefusedAtEditTime drives the acceptance behavior that decides
// where a broken rule is caught (#682): a label rule is operator-authored text
// that a later WRITE PATH executes, so an unparseable one has to be refused
// here, at the edit, with the template engine's own message. The alternative is
// a column that silently produces no label on every component created from that
// type, discovered whenever somebody notices.
//
// The three cases are three distinct ways to be unparseable (an unclosed
// action, an unbalanced block, and a function the closed FuncMap does not
// define), and the last assertion is the one that matters: after every refusal
// the stored rule is still the good one.
func TestLabelRuleIsRefusedAtEditTime(t *testing.T) {
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

	good := c.do(ownerTok, http.MethodPatch, "/component-types/display",
		map[string]any{"label_rule": "{{.TypeName}} {{.Ordinal}}"}, http.StatusOK)
	var forked componentTypeWire
	if err := json.Unmarshal(good, &forked); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if forked.LabelRule != "{{.TypeName}} {{.Ordinal}}" {
		t.Fatalf("label_rule = %q, want the rule just set", forked.LabelRule)
	}
	// A shipped row is never written: the edit forked it, under the same id.
	if !forked.Official || !forked.Forked {
		t.Fatalf("official = %v forked = %v, want both true", forked.Official, forked.Forked)
	}

	for _, bad := range []string{"{{.TypeName", "{{if .Ordinal}}{{.Ordinal}}", "{{.TypeName | exec}}"} {
		c.do(ownerTok, http.MethodPatch, "/component-types/display",
			map[string]any{"label_rule": bad}, http.StatusUnprocessableEntity)
	}

	after := c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
	var body struct {
		ComponentTypes []componentTypeWire `json:"component_types"`
	}
	if err := json.Unmarshal(after, &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, ct := range body.ComponentTypes {
		if ct.Name != "display" {
			continue
		}
		if ct.LabelRule != "{{.TypeName}} {{.Ordinal}}" {
			t.Fatalf("stored label_rule = %q, want the good rule to have survived every refusal", ct.LabelRule)
		}
		return
	}
	t.Fatal("display is missing from the registry listing")
}
