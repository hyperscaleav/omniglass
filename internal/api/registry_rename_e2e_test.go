package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestRegistryRenameAPI drives #555 over HTTP: each of the five registries
// renames through POST /<registry>/{id}:rename (the house custom method,
// ADR-0076), gated by <registry>:rename, which admin holds and operator does
// not. The answer is the row under its new name with the same uuid; the old
// name stops resolving. An official row is read-only (422), a taken name is a
// 409, an illegal or uuid-shaped name a 422, an unknown row a 404, and a
// location type may not take the reserved "root".
func TestRegistryRenameAPI(t *testing.T) {
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
	// A role holding vendor:update but not vendor:rename: the rename is its own
	// permission (ADR-0555), so an editor is not thereby a renamer. Inserted
	// before the handler reads the role set.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ('vendor-editor', false, $1, '{viewer}')`,
		[]string{"vendor:update"}); err != nil {
		t.Fatalf("insert vendor-editor role: %v", err)
	}
	conn.Close(ctx)
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	adminTok := principalWithGrants(t, ctx, dsn, "registry-admin", []grant{{role: "admin", scopeKind: "all"}})
	operatorTok := principalWithGrants(t, ctx, dsn, "registry-operator", []grant{{role: "operator", scopeKind: "all"}})

	type row struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	create := func(path string, body map[string]any) row {
		t.Helper()
		var r row
		if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, path, body, http.StatusCreated), &r); err != nil {
			t.Fatalf("decode create %s: %v", path, err)
		}
		return r
	}
	cases := []struct {
		collection, old, renamed, official string
		created                            row
	}{
		{collection: "vendors", old: "acme", renamed: "acme-av", official: "kestrel",
			created: create("/vendors", map[string]any{"name": "acme", "label": "Acme", "kind": "manufacturer"})},
		{collection: "drivers", old: "acme-ctl", renamed: "acme-controller", official: "snmp-generic",
			created: create("/drivers", map[string]any{"name": "acme-ctl", "label": "Acme Controller"})},
		{collection: "products", old: "acme-bar", renamed: "acme-soundbar", official: "generic-device",
			created: create("/products", map[string]any{"name": "acme-bar", "label": "Acme Bar", "kind": "device", "component_type": "video-bar"})},
		{collection: "standards", old: "huddle", renamed: "huddle-space",
			created: create("/standards", map[string]any{"name": "huddle", "label": "Huddle"})},
		{collection: "location-types", old: "wing", renamed: "east-wing", official: "room",
			created: create("/location-types", map[string]any{"name": "wing", "label": "Wing", "allowed_parent_types": []string{"root"}})},
	}
	// resolves reports whether a name answers in its registry. Four registries
	// read one row by name; location types have no single-row GET, so their
	// list is read instead.
	resolves := func(t *testing.T, c *apiClient, collection, name string, want bool) {
		t.Helper()
		if collection != "location-types" {
			status := http.StatusNotFound
			if want {
				status = http.StatusOK
			}
			c.do(adminTok, http.MethodGet, "/"+collection+"/"+name, nil, status)
			return
		}
		var list struct {
			LocationTypes []row `json:"location_types"`
		}
		if err := json.Unmarshal(c.do(adminTok, http.MethodGet, "/location-types", nil, http.StatusOK), &list); err != nil {
			t.Fatalf("decode location types: %v", err)
		}
		got := slices.ContainsFunc(list.LocationTypes, func(r row) bool { return r.Name == name })
		if got != want {
			t.Fatalf("location type %q listed = %v, want %v", name, got, want)
		}
	}
	for _, tc := range cases {
		t.Run(tc.collection, func(t *testing.T) {
			c := &apiClient{t: t, ctx: ctx, base: srv.URL}
			path := "/" + tc.collection + "/" + tc.old + ":rename"
			// An operator holds no <registry>:rename (403, before anything resolves).
			c.do(operatorTok, http.MethodPost, path, map[string]any{"name": tc.renamed}, http.StatusForbidden)
			// Illegal and uuid-shaped names are refused, and leave the row as it was.
			c.do(adminTok, http.MethodPost, path, map[string]any{"name": "Not_Kebab"}, http.StatusUnprocessableEntity)
			c.do(adminTok, http.MethodPost, path, map[string]any{"name": "0198f2c1-4a5b-7c3d-9e0f-1a2b3c4d5e6f"}, http.StatusUnprocessableEntity)

			var got row
			if err := json.Unmarshal(c.do(adminTok, http.MethodPost, path, map[string]any{"name": tc.renamed}, http.StatusOK), &got); err != nil {
				t.Fatalf("decode rename: %v", err)
			}
			if got.ID != tc.created.ID || got.Name != tc.renamed {
				t.Fatalf("rename answered %+v, want id %s under %s", got, tc.created.ID, tc.renamed)
			}
			resolves(t, c, tc.collection, tc.renamed, true)
			resolves(t, c, tc.collection, tc.old, false)
			// By uuid works too, and a rename onto a name another row holds is a conflict.
			c.do(adminTok, http.MethodPost, "/"+tc.collection+"/"+tc.created.ID+":rename", map[string]any{"name": tc.old}, http.StatusOK)
			if tc.official != "" {
				c.do(adminTok, http.MethodPost, "/"+tc.collection+"/"+tc.old+":rename", map[string]any{"name": tc.official}, http.StatusConflict)
				c.do(adminTok, http.MethodPost, "/"+tc.collection+"/"+tc.official+":rename", map[string]any{"name": "renamed-official"}, http.StatusUnprocessableEntity)
			}
			c.do(adminTok, http.MethodPost, "/"+tc.collection+"/no-such-row:rename", map[string]any{"name": "anything"}, http.StatusNotFound)
		})
	}
	c.do(adminTok, http.MethodPost, "/location-types/wing:rename", map[string]any{"name": "root"}, http.StatusUnprocessableEntity)

	// A standard forked from a template renames like any of the operator's own:
	// nothing plants it again by name (ADR-0827).
	c.do(adminTok, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "meeting-room"}, http.StatusCreated)
	c.do(adminTok, http.MethodPost, "/standards/meeting-room:rename", map[string]any{"name": "conference-room"}, http.StatusOK)
	c.do(adminTok, http.MethodGet, "/standards/conference-room", nil, http.StatusOK)
	c.do(adminTok, http.MethodGet, "/standards/meeting-room", nil, http.StatusNotFound)

	// vendor:update without vendor:rename edits the vendor and cannot rename it.
	editorTok := principalWithGrants(t, ctx, dsn, "vendor-editor", []grant{{role: "vendor-editor", scopeKind: "all"}})
	c.do(editorTok, http.MethodPatch, "/vendors/acme", map[string]any{"label": "Acme Audio"}, http.StatusOK)
	c.do(editorTok, http.MethodPost, "/vendors/acme:rename", map[string]any{"name": "acme-audio"}, http.StatusForbidden)
	c.do(adminTok, http.MethodGet, "/vendors/acme", nil, http.StatusOK)
}
