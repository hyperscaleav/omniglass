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
	typeField string // "location_type"; empty when the entity carries no type on create
	typeValue string // a valid type id
	// actions are the mutating routes that hang OFF this entity and resolve it
	// as their target: a property write on it, a membership or role write on it,
	// an alarm write on it. The entity's own PATCH is not listed here; the matrix
	// asserts that one directly, because every entity has exactly one and it needs
	// no description. These do need describing, because they are NOT uniform
	// across the registry: properties exist on all three tiers, membership and
	// roles only on system, alarms only on component (#736).
	actions []actionRoute
}

// actionRoute describes one such route to the matrix. The fields are what the
// three-way split needs and nothing more: where to send the request, what has to
// exist on the target before it is sent, what a permitted call answers, and which
// permission beyond the entity's own write the route is gated on.
//
// Deliberately not a bare (method, path) pair: half of these routes act on a row
// rather than creating one (an alarm to acknowledge, a membership to unbind, an
// assignment to remove), and a refusal asserted against a target with no such row
// would pass for the wrong reason. prepare is what makes the assertion mean "the
// caller can see this and may not touch it" rather than "there was nothing here".
//
// Deliberately not a free-form func(t) either: a hook that could send its own
// requests would let a route assert something other than the split, which is the
// one thing this matrix exists to prove for all of them at once.
type actionRoute struct {
	// name is the subtest name, and doubles as the fixture's own unique
	// discriminator (role names, component names) so two routes preparing the
	// same target cannot collide.
	name string
	// needs are permissions the route is gated on beyond the entity's own
	// create/update/delete. They join the matrix's write-only role, so they
	// carry that role's narrow grant rather than the viewer floor's reach:
	// alarm:acknowledge resolves its scope from the acknowledgement, and a
	// principal holding it at root only must not be able to acknowledge
	// fleet-wide just because it can read fleet-wide.
	needs []string
	// prepare runs as the OWNER against one target before the split is driven,
	// and returns whatever path segment its work produced (an alarm id, a
	// component name), or "" when the request needs nothing from it.
	prepare func(c *apiClient, ownerTok, target string) string
	// request builds the call against target, given what prepare returned.
	request func(target, made string) (method, path string, body map[string]any)
	// ok is the status a permitted call answers.
	ok int
}

// matrixProperty is the catalog property the property routes write. Any seeded
// string property does; nothing in the split depends on which.
const matrixProperty = "serial-number"

var scopedEntities = []scopedEntity{
	{
		resource: "location", base: "/locations", typeField: "location_type", typeValue: "campus",
		actions: propertyActions("/locations"),
	},
	{
		resource: "system", base: "/systems", typeField: "standard_id", typeValue: "meeting-room",
		actions: append(propertyActions("/systems"), systemActions()...),
	},
	// component has no type field, but the same mechanism supplies its required
	// product (the classification floor: every component is an instance of a
	// product), so the matrix's generic create body works unchanged.
	{
		resource: "component", base: "/components", typeField: "product", typeValue: "generic-device",
		actions: append(propertyActions("/components"), componentActions()...),
	},
}

// propertyActions is the set every tier shares: the declared-property write and
// its clear. One function rather than three literals, because the three routes
// differ only in the base path, and a copy per tier is a copy that can drift out
// of step with the tier it was copied from.
func propertyActions(base string) []actionRoute {
	path := func(target string) string { return base + "/" + target + "/properties/" + matrixProperty }
	return []actionRoute{
		{
			name: "set-property",
			request: func(target, _ string) (string, string, map[string]any) {
				return http.MethodPut, path(target), map[string]any{"value": "sn-matrix"}
			},
			ok: http.StatusOK,
		},
		{
			name: "clear-property",
			prepare: func(c *apiClient, ownerTok, target string) string {
				c.do(ownerTok, http.MethodPut, path(target), map[string]any{"value": "sn-matrix"}, http.StatusOK)
				return ""
			},
			request: func(target, _ string) (string, string, map[string]any) {
				return http.MethodDelete, path(target), nil
			},
			ok: http.StatusNoContent,
		},
	}
}

// matrixCommand is the command_type the issue route invokes: the seeded
// fire-and-forget `reboot`. It is the fixture precisely because it has no target
// arm (nothing is opened to settle) and no params_schema (nothing to satisfy), so
// the only thing that can decide the status is the scope split. A settleable type
// would need an observed series row on both targets first, which would put the
// settlement's own semantics inside an authorization assertion.
const matrixCommand = "reboot"

// componentActions are the alarm writes and the command issue, the routes that
// hang off a component.
func componentActions() []actionRoute {
	alarms := func(target string) string { return "/components/" + target + "/alarms" }
	// raiseAs returns a prepare that opens one alarm under its own dedup key, so
	// each route works on an alarm of its own and clearing one does not empty the
	// row another route is about to act on.
	raiseAs := func(key string) func(*apiClient, string, string) string {
		return func(c *apiClient, ownerTok, target string) string {
			raw := c.do(ownerTok, http.MethodPost, alarms(target),
				map[string]any{"severity": "info", "message": "matrix", "dedup_key": key}, http.StatusCreated)
			var body struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				c.t.Fatalf("decode raised alarm: %v", err)
			}
			return body.ID
		}
	}
	return []actionRoute{
		{
			name: "raise-alarm",
			request: func(target, _ string) (string, string, map[string]any) {
				return http.MethodPost, alarms(target),
					map[string]any{"severity": "info", "message": "matrix", "dedup_key": "matrix-raise"}
			},
			ok: http.StatusCreated,
		},
		{
			name:    "clear-alarm",
			prepare: raiseAs("matrix-clear"),
			request: func(target, made string) (string, string, map[string]any) {
				return http.MethodDelete, alarms(target) + "/" + made, nil
			},
			ok: http.StatusNoContent,
		},
		{
			// The route #728 hit and deliberately left alone: it is gated on
			// alarm:acknowledge and resolves its scope from that permission, so
			// it is the one route here whose action set is not the entity's own
			// update.
			name:    "acknowledge-alarm",
			needs:   []string{"alarm:acknowledge"},
			prepare: raiseAs("matrix-ack"),
			request: func(target, made string) (string, string, map[string]any) {
				return http.MethodPost, alarms(target) + "/" + made + ":acknowledge", nil
			},
			ok: http.StatusOK,
		},
		{
			// The actuation route (#749), and the only one on this list whose
			// refusal keeps a physical device from being told to do something.
			// It is gated on command:issue and, like the acknowledgement,
			// resolves its ACTION scope from that permission rather than from
			// component:update, so a wide component read cannot widen what may
			// be commanded.
			//
			// It needs no prepare: a command records an invocation rather than
			// acting on a row that has to exist first, so the target itself is
			// the whole fixture and both branches refuse against a component
			// that is really there.
			name:  "issue-command",
			needs: []string{"command:issue"},
			request: func(target, _ string) (string, string, map[string]any) {
				return http.MethodPost, "/components/" + target + "/commands:issue",
					map[string]any{"command_type": matrixCommand}
			},
			ok: http.StatusOK,
		},
	}
}

// systemActions are the membership and role writes, the routes that hang off a
// system. Each one that needs a component makes its own, named after the route
// and the target, so no two prepares contend for one row.
func systemActions() []actionRoute {
	members := func(target, comp string) string { return "/systems/" + target + "/members/" + comp }
	roles := func(target, role string) string { return "/systems/" + target + "/roles/" + role }
	// makeComponent creates an orphan component the membership and staffing
	// routes bind. It is addressed by name, and the name carries the route and
	// the target so the component tree stays collision-free across the whole run.
	makeComponent := func(c *apiClient, ownerTok, name string) string {
		c.do(ownerTok, http.MethodPost, "/components",
			map[string]any{"name": name, "product": "generic-device"}, http.StatusCreated)
		return name
	}
	declareRole := func(c *apiClient, ownerTok, target, role string) {
		c.do(ownerTok, http.MethodPatch, roles(target, role),
			map[string]any{"label": "Matrix role"}, http.StatusOK)
	}
	return []actionRoute{
		{
			name: "add-member",
			prepare: func(c *apiClient, ownerTok, target string) string {
				return makeComponent(c, ownerTok, target+"-add-member")
			},
			request: func(target, made string) (string, string, map[string]any) {
				return http.MethodPut, members(target, made), nil
			},
			ok: http.StatusNoContent,
		},
		{
			name: "remove-member",
			prepare: func(c *apiClient, ownerTok, target string) string {
				comp := makeComponent(c, ownerTok, target+"-remove-member")
				c.do(ownerTok, http.MethodPut, members(target, comp), nil, http.StatusNoContent)
				return comp
			},
			request: func(target, made string) (string, string, map[string]any) {
				return http.MethodDelete, members(target, made), nil
			},
			ok: http.StatusNoContent,
		},
		{
			name: "set-primary-member",
			prepare: func(c *apiClient, ownerTok, target string) string {
				comp := makeComponent(c, ownerTok, target+"-set-primary")
				c.do(ownerTok, http.MethodPut, members(target, comp), nil, http.StatusNoContent)
				return comp
			},
			request: func(target, made string) (string, string, map[string]any) {
				return http.MethodPost, members(target, made) + ":setPrimary", nil
			},
			ok: http.StatusNoContent,
		},
		{
			name: "set-system-role",
			request: func(target, _ string) (string, string, map[string]any) {
				return http.MethodPatch, roles(target, "matrix-declare"),
					map[string]any{"label": "Matrix role"}
			},
			ok: http.StatusOK,
		},
		{
			name: "delete-system-role",
			prepare: func(c *apiClient, ownerTok, target string) string {
				declareRole(c, ownerTok, target, "matrix-withdraw")
				return ""
			},
			request: func(target, _ string) (string, string, map[string]any) {
				return http.MethodDelete, roles(target, "matrix-withdraw"), nil
			},
			ok: http.StatusNoContent,
		},
		{
			name: "assign-system-role",
			prepare: func(c *apiClient, ownerTok, target string) string {
				declareRole(c, ownerTok, target, "matrix-assign")
				return makeComponent(c, ownerTok, target+"-assign")
			},
			request: func(target, made string) (string, string, map[string]any) {
				return http.MethodPut, roles(target, "matrix-assign") + "/assignments/" + made, nil
			},
			ok: http.StatusNoContent,
		},
		{
			name: "unassign-system-role",
			prepare: func(c *apiClient, ownerTok, target string) string {
				declareRole(c, ownerTok, target, "matrix-unassign")
				comp := makeComponent(c, ownerTok, target+"-unassign")
				c.do(ownerTok, http.MethodPut, roles(target, "matrix-unassign")+"/assignments/"+comp, nil, http.StatusNoContent)
				return comp
			},
			request: func(target, made string) (string, string, map[string]any) {
				return http.MethodDelete, roles(target, "matrix-unassign") + "/assignments/" + made, nil
			},
			ok: http.StatusNoContent,
		},
		{
			name: "swap-role-positions",
			prepare: func(c *apiClient, ownerTok, target string) string {
				declareRole(c, ownerTok, target, "matrix-swap")
				for _, suffix := range []string{"-swap-a", "-swap-b"} {
					comp := makeComponent(c, ownerTok, target+suffix)
					c.do(ownerTok, http.MethodPut, roles(target, "matrix-swap")+"/assignments/"+comp, nil, http.StatusNoContent)
				}
				return ""
			},
			request: func(target, _ string) (string, string, map[string]any) {
				return http.MethodPost, roles(target, "matrix-swap") + ":swapPositions",
					map[string]any{"position": 1, "with": 2}
			},
			ok: http.StatusNoContent,
		},
	}
}

func (e scopedEntity) createBody(name, parent string) map[string]any {
	// location alone is placement-constrained (allowed_parent_types): a child
	// needs a type compatible with a campus parent, since only the tree shape
	// matters to this matrix, not the real-world type semantics.
	typeValue := e.typeValue
	if e.resource == "location" && parent != "" {
		typeValue = "building"
	}
	b := map[string]any{"name": name}
	if e.typeField != "" {
		b[e.typeField] = typeValue
	}
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
	// inherit), plus whatever its action routes are separately gated on. Its
	// holder also gets <res>:read via the :read floor, scoped to its grant; it
	// reads no other resource or out-of-scope target.
	//
	// The extra permissions belong on THIS role and not on viewer: they have to
	// carry the narrow grant, or the readable-but-not-actionable principal below
	// would be actionable everywhere it can read and the case would evaporate.
	writerRole := e.resource + "-writer"
	writePerms := []string{e.resource + ":create,update,delete"}
	for _, r := range e.actions {
		writePerms = append(writePerms, r.needs...)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into role (name, official, permissions, inherits) values ($1, false, $2, '{}')`,
		writerRole, writePerms); err != nil {
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

	patch := map[string]any{"label": "x"}
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

	// And the same three-way split on every route that hangs off this entity
	// and resolves it as its target (#736). The entity's own PATCH proved the
	// split for the tree CRUD alone; these are the routes that reach the same
	// row through a different door.
	for _, r := range e.actions {
		t.Run(r.name, func(t *testing.T) {
			runActionRouteSplit(t, ctx, srv.URL, ownerTok, narrow, mixed, r)
		})
	}
}

// runActionRouteSplit drives one action route through the same three principals
// the entity's own PATCH is driven through.
//
// The refusals go to az-other and the success to az-child, so a mutating success
// never disturbs the row a refusal is asserted on. Both targets are prepared
// first, which is the part that makes the 403 mean what it says: the caller is
// being turned away from something that is really there and that it really can
// see, not from an empty address.
func runActionRouteSplit(t *testing.T, ctx context.Context, base, ownerTok, narrow, mixed string, r actionRoute) {
	// Its own client, bound to the subtest's own T: the parent's client would
	// report a failure against the parent and unwind the wrong goroutine.
	c := &apiClient{t: t, ctx: ctx, base: base}

	var otherMade, childMade string
	if r.prepare != nil {
		otherMade = r.prepare(c, ownerTok, "az-other")
		childMade = r.prepare(c, ownerTok, "az-child")
	}

	// Out of the read scope entirely: the non-disclosing 404, unchanged.
	method, path, body := r.request("az-other", otherMade)
	c.do(narrow, method, path, body, http.StatusNotFound)

	// Readable (viewer@all) but outside the action scope (writer@root): the
	// truthful 403. This is the branch #736 built.
	method, path, body = r.request("az-other", otherMade)
	c.do(mixed, method, path, body, http.StatusForbidden)

	// Inside both: the success, so the refusals above are the scope boundary and
	// not the route being broken.
	method, path, body = r.request("az-child", childMade)
	c.do(mixed, method, path, body, r.ok)
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
	if _, err := conn.Exec(ctx, `insert into service (principal_id, name) values ($1, $2)`, pid, label); err != nil {
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
			`insert into principal_grant (principal_id, role_id, scope_kind, scope_id, scope_op) values ($1, (select id from role where name = $2), $3, $4, $5)`,
			pid, g.role, g.scopeKind, scopeID, op); err != nil {
			t.Fatalf("insert grant %+v: %v", g, err)
		}
	}
	return tok
}
