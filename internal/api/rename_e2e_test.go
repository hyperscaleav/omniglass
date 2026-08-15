package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A rename is its own act, not a field write.
//
// Renaming an entity breaks every reference an operator stored outside the system:
// a bookmark, a runbook step, a dashboard filter, an integration's config. That is
// worth being able to withhold on its own, which a field on a PATCH body cannot
// express: anyone who may edit a label could rename. So each of the four
// name-bearing entities carries a :rename custom method gated by
// <resource>:rename, and the PATCH body no longer takes a name at all.

// renameable is one entity that carries the :rename custom method: how to create
// one, and the path segment the rename addresses it by (a name for the estate
// entities, a uuid for a principal group).
type renameable struct {
	resource string // the permission resource: "component", "principal_group", ...
	base     string // "/components"
	create   func(c *apiClient, tok, name string) string
}

func renameables() []renameable {
	return []renameable{
		{
			resource: "component", base: "/components",
			create: func(c *apiClient, tok, name string) string {
				c.do(tok, http.MethodPost, "/components", map[string]any{"name": name, "product": "generic-device"}, http.StatusCreated)
				return name
			},
		},
		{
			resource: "system", base: "/systems",
			create: func(c *apiClient, tok, name string) string {
				c.do(tok, http.MethodPost, "/systems", map[string]any{"name": name}, http.StatusCreated)
				return name
			},
		},
		{
			resource: "location", base: "/locations",
			create: func(c *apiClient, tok, name string) string {
				c.do(tok, http.MethodPost, "/locations", map[string]any{"name": name, "location_type": "campus"}, http.StatusCreated)
				return name
			},
		},
		{
			// A group is addressed by its uuid, not its name, so the path segment is
			// the created id. The rename still moves the name.
			resource: "principal_group", base: "/principal-groups",
			create: func(c *apiClient, tok, name string) string {
				return createdID(c.t, c.do(tok, http.MethodPost, "/principal-groups", map[string]any{"name": name}, http.StatusCreated))
			},
		},
	}
}

// roleSlug turns a permission resource into a role name (the role table takes the
// entity name rule, which admits hyphens and not underscores).
func roleSlug(resource string) string { return strings.ReplaceAll(resource, "_", "-") }

// TestRenameIsItsOwnPermission is the load-bearing case of the whole split: a
// caller that holds <resource>:update, the permission that used to carry the
// rename, is refused; a caller that holds <resource>:rename is allowed. If update
// still implied rename, the first half of every pair below would pass.
func TestRenameIsItsOwnPermission(t *testing.T) {
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

	// Every custom role must exist before the first authenticated request, which is
	// what loads the role index (lazy, once per handler).
	for _, r := range renameables() {
		insertRole(t, ctx, dsn, roleSlug(r.resource)+"-updater", []string{r.resource + ":read,update"})
		insertRole(t, ctx, dsn, roleSlug(r.resource)+"-renamer", []string{r.resource + ":read,rename"})
	}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	for _, r := range renameables() {
		t.Run(r.resource, func(t *testing.T) {
			slug := roleSlug(r.resource)
			updaterTok := principalWithGrants(t, ctx, dsn, slug+"-updater-svc",
				[]grant{{role: slug + "-updater", scopeKind: "all"}})
			renamerTok := principalWithGrants(t, ctx, dsn, slug+"-renamer-svc",
				[]grant{{role: slug + "-renamer", scopeKind: "all"}})

			ref := r.create(c, ownerTok, slug+"-before")
			path := r.base + "/" + ref + ":rename"

			// An update grant no longer reaches a rename. This is the entire point of
			// the split, so it is the assertion that must not be softened.
			if code, body := c.send(updaterTok, http.MethodPost, path, map[string]any{"name": slug + "-sneaky"}); code != http.StatusForbidden {
				t.Fatalf("%s:update renamed (%d, body %s), want 403: update must not imply rename", r.resource, code, body)
			}

			// And the permission that does grant it works.
			out := c.do(renamerTok, http.MethodPost, path, map[string]any{"name": slug + "-after"}, http.StatusOK)
			var got struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("decode rename: %v", err)
			}
			if got.Name != slug+"-after" {
				t.Fatalf("name after rename = %q, want %q", got.Name, slug+"-after")
			}
		})
	}
}

// TestGroupRenameOverHTTP is the group's leg of the rename surface: it moves the
// name, the group still answers to its uuid (which is how a group is addressed at
// all), a taken name is a 409, and an illegal or uuid-shaped name is a 422.
func TestGroupRenameOverHTTP(t *testing.T) {
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

	id := createdID(t, c.do(ownerTok, http.MethodPost, "/principal-groups", map[string]any{"name": "field-techs"}, http.StatusCreated))
	other := createdID(t, c.do(ownerTok, http.MethodPost, "/principal-groups", map[string]any{"name": "night-shift"}, http.StatusCreated))

	out := c.do(ownerTok, http.MethodPost, "/principal-groups/"+id+":rename", map[string]any{"name": "field-engineers"}, http.StatusOK)
	var renamed struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "field-engineers" {
		t.Fatalf("name = %q, want field-engineers", renamed.Name)
	}
	if renamed.ID != id {
		t.Fatalf("id = %q, want %q: a rename moves the name, never the identity", renamed.ID, id)
	}
	// The uuid is the address, so it still resolves and reports the new name.
	var got struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/principal-groups/"+id, nil, http.StatusOK), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Name != "field-engineers" {
		t.Fatalf("get after rename = %q, want field-engineers", got.Name)
	}

	// The name is no longer patchable: two ways to rename is what this method removed.
	c.do(ownerTok, http.MethodPatch, "/principal-groups/"+id, map[string]any{"name": "sneaky"}, http.StatusUnprocessableEntity)

	// Onto a taken name -> 409.
	c.do(ownerTok, http.MethodPost, "/principal-groups/"+other+":rename", map[string]any{"name": "field-engineers"}, http.StatusConflict)

	// Illegal slug -> 422 at the edge; uuid-shaped -> 422 from the gateway.
	c.do(ownerTok, http.MethodPost, "/principal-groups/"+other+":rename", map[string]any{"name": "Bad Name"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/principal-groups/"+other+":rename",
		map[string]any{"name": "019f8754-461f-7b82-b5f2-fc4bbe1c3765"}, http.StatusUnprocessableEntity)

	// An unknown group is a 404.
	c.do(ownerTok, http.MethodPost, "/principal-groups/00000000-0000-0000-0000-000000000000:rename",
		map[string]any{"name": "ghosts"}, http.StatusNotFound)
}
