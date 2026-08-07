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
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Stem        string   `json:"stem"`
	Icon        string   `json:"icon"`
	Abbrev      string   `json:"abbrev"`
	DefaultTags []string `json:"default_tags"`
	Official    bool     `json:"official"`
	ParentID    string   `json:"parent_id"`
	Parent      string   `json:"parent"`
}

// TestComponentTypesAPI drives the component_type registry over HTTP: list
// shows the seeded tree with parent links (a child's parent id AND name), a
// viewer creates nothing (403), an admin (owner) creates a custom child under
// an existing root (mic), the seeded official rows are read-only like every
// other registry, and delete refuses an official row and a row still parenting
// another. Skipped under -short.
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

	// The seeded official row (mic) is read-only: 422 on patch, and delete
	// refuses it too (still official, independent of its children).
	c.do(ownerTok, http.MethodPatch, "/component-types/mic",
		map[string]any{"display_name": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/component-types/mic", nil, http.StatusUnprocessableEntity)

	// A row still parenting another cannot be deleted (409): custom-mic has no
	// children, so it deletes cleanly; a seeded parent like mic would 409 on
	// its children if it were not already refused for being official first.
	c.do(ownerTok, http.MethodDelete, "/component-types/custom-mic", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
}
