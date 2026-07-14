package api_test

import (
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
	"github.com/jackc/pgx/v5"
)

// grant is a (role x scope) pair for building test principals.
type grant struct {
	role      string
	scopeKind string
	scopeID   string // "" for the all scope
	scopeOp   string // "" (subtree), "subtree_excl_root", or "self"
}

// scopedEntity describes a scoped tree entity so the authorization conformance
// matrix runs against every one of them. Adding a new scoped entity (component,
// group, ...) is one line here, and it inherits the full security matrix; no
// bespoke per-entity authz test needed.
type scopedEntity struct {
	resource  string // "location", "system": drives the role permission + grant scope_kind
	base      string // "/locations"
	typeField string // "location_type"
	typeValue string // a valid type id
}

var scopedEntities = []scopedEntity{
	{resource: "location", base: "/locations", typeField: "location_type", typeValue: "campus"},
	{resource: "system", base: "/systems", typeField: "system_type", typeValue: "meeting-room"},
	{resource: "component", base: "/components", typeField: "component_type", typeValue: "display"},
}

func (e scopedEntity) createBody(name, parent string) map[string]any {
	// location alone is placement-constrained (allowed_parent_types): a child
	// needs a type compatible with a campus parent, since only the tree shape
	// matters to this matrix, not the real-world type semantics.
	typeValue := e.typeValue
	if e.resource == "location" && parent != "" {
		typeValue = "building"
	}
	b := map[string]any{"name": name, e.typeField: typeValue}
	if parent != "" {
		b["parent"] = parent
	}
	return b
}

// TestAuthzConformance is the foundation security test, run against EVERY scoped
// entity: it drives the live API with realistically-granted principals and
// asserts the full authorization matrix end to end (grant -> role index ->
// per-action visible_set -> gateway 3-way split):
//
//   - capability fast-reject (403) when the action is in no grant;
//   - the over-permit fix: read-everywhere + write-narrow yields a SCOPE 403 on a
//     readable-but-out-of-write-scope target (not a 404, not a silent success);
//   - non-disclosing 404 when the target is outside the read scope entirely;
//   - success only inside the action scope, and the read/act asymmetry.
//
// Because it iterates scopedEntities, a new scoped entity is covered the moment
// it is registered.
func TestAuthzConformance(t *testing.T) {
	for _, e := range scopedEntities {
		t.Run(e.resource, func(t *testing.T) {
			runAuthzMatrix(t, e)
		})
	}
}

func runAuthzMatrix(t *testing.T, e scopedEntity) {
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

	// A custom write-only role for this entity (create/update/delete, no
	// inherit). Its holder also gets <res>:read via the :read floor, scoped to
	// its grant; it reads no other resource or out-of-scope target.
	writerRole := e.resource + "-writer"
	writePerm := e.resource + ":create,update,delete"
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into role (id, official, permissions, inherits) values ($1, false, $2, '{}')`,
		writerRole, []string{writePerm}); err != nil {
		t.Fatalf("insert %s role: %v", writerRole, err)
	}
	conn.Close(ctx)

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner builds: root > child; plus other (a separate root). The first owner
	// request builds the role index (lazy), by which point the custom role
	// exists.
	c.do(ownerTok, http.MethodPost, e.base, e.createBody("az-root", ""), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, e.base, e.createBody("az-child", "az-root"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, e.base, e.createBody("az-other", ""), http.StatusCreated)
	rootID := entityID(t, c, ownerTok, e.base, "az-root")

	patch := map[string]any{"display_name": "x"}
	path := func(name string) string { return e.base + "/" + name }

	// Principal 1: viewer@all (read everywhere, write nothing).
	reader := principalWithGrants(t, ctx, dsn, "reader", []grant{{role: "viewer", scopeKind: "all"}})
	c.do(reader, http.MethodGet, path("az-other"), nil, http.StatusOK)            // reads anything
	c.do(reader, http.MethodPatch, path("az-child"), patch, http.StatusForbidden) // capability 403 (no write anywhere)

	// Principal 2: write-only, scoped to root only (no @all). Read scope = root
	// subtree (via floor); az-other is outside it.
	narrow := principalWithGrants(t, ctx, dsn, "narrow", []grant{{role: writerRole, scopeKind: e.resource, scopeID: rootID}})
	c.do(narrow, http.MethodPatch, path("az-child"), patch, http.StatusOK)       // write in scope
	c.do(narrow, http.MethodGet, path("az-other"), nil, http.StatusNotFound)     // out of read scope -> non-disclosing 404
	c.do(narrow, http.MethodPatch, path("az-other"), patch, http.StatusNotFound) // 404, not 403

	// Principal 3: viewer@all + writer@root (the over-permit case): reads
	// everywhere, writes only under root.
	mixed := principalWithGrants(t, ctx, dsn, "mixed", []grant{
		{role: "viewer", scopeKind: "all"},
		{role: writerRole, scopeKind: e.resource, scopeID: rootID},
	})
	c.do(mixed, http.MethodGet, path("az-other"), nil, http.StatusOK)     // readable (viewer@all)
	c.do(mixed, http.MethodPatch, path("az-child"), patch, http.StatusOK) // write in scope
	// The crown jewel: az-other is READABLE (viewer@all) but OUTSIDE the write
	// scope (writer@root) -> 403 scope, NOT 404, NOT silent success. The read
	// grant must not widen the write set.
	c.do(mixed, http.MethodPatch, path("az-other"), patch, http.StatusForbidden)
}

// entityID lists base as the owner and returns the id of the row named name.
// The list envelope has a single array under a resource-named key, so this is
// generic across entities.
func entityID(t *testing.T, c *apiClient, tok, base, name string) string {
	t.Helper()
	// The list envelope carries a $schema string alongside the single array, so
	// decode to raw values and pick the one that is an array of rows.
	var env map[string]json.RawMessage
	if err := json.Unmarshal(c.do(tok, http.MethodGet, base, nil, http.StatusOK), &env); err != nil {
		t.Fatalf("decode list %s: %v", base, err)
	}
	for _, raw := range env {
		var rows []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &rows) != nil {
			continue
		}
		for _, it := range rows {
			if it.Name == name {
				return it.ID
			}
		}
	}
	t.Fatalf("%s named %q not found under %s", base, name, base)
	return ""
}

// listNames returns the names in a scoped list, in the order the endpoint returns
// them (by name), for asserting exactly which rows a scope admits.
func listNames(t *testing.T, c *apiClient, tok, base string) []string {
	t.Helper()
	var env map[string]json.RawMessage
	if err := json.Unmarshal(c.do(tok, http.MethodGet, base, nil, http.StatusOK), &env); err != nil {
		t.Fatalf("decode list %s: %v", base, err)
	}
	for _, raw := range env {
		var rows []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &rows) != nil || rows == nil {
			continue
		}
		out := make([]string, 0, len(rows))
		for _, it := range rows {
			out = append(out, it.Name)
		}
		return out
	}
	return nil
}

func bootstrapOwnerTok(t *testing.T, ctx context.Context, gw storage.Gateway) string {
	t.Helper()
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return tok
}

// principalWithGrants creates a service principal with a bearer credential and
// the given grants, returning its token.
func principalWithGrants(t *testing.T, ctx context.Context, dsn, label string, grants []grant) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var pid string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into service (principal_id, label) values ($1, $2)`, pid, label); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		pid, hash, prefix); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	for _, g := range grants {
		var scopeID any
		if g.scopeID != "" {
			scopeID = g.scopeID
		}
		op := g.scopeOp
		if op == "" {
			op = "subtree"
		}
		if _, err := conn.Exec(ctx,
			`insert into principal_grant (principal_id, role_id, scope_kind, scope_id, scope_op) values ($1, $2, $3, $4, $5)`,
			pid, g.role, g.scopeKind, scopeID, op); err != nil {
			t.Fatalf("insert grant %+v: %v", g, err)
		}
	}
	return tok
}
