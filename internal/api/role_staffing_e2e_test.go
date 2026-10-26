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
	"github.com/jackc/pgx/v5"
)

// Role and staffing integrity (#504), driven over the real HTTP handler against
// a real Postgres: each test is the regression for one of the epic's defects,
// reproduced the way an operator or an integrator would hit it.

type staffingFixture struct {
	t     *testing.T
	ctx   context.Context
	dsn   string
	gw    storage.Gateway
	c     *apiClient
	owner string
	// Products minted for the fixture rather than borrowed from the seeded
	// catalog (#804): a video bar and a display, the two classifications the
	// typed-slot guard is asked to tell apart.
	bar, display string
}

func newStaffingFixture(t *testing.T) *staffingFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	return &staffingFixture{
		t: t, ctx: ctx, dsn: dsn, gw: gw,
		c:       &apiClient{t: t, ctx: ctx, base: srv.URL},
		owner:   bootstrapOwnerTok(t, ctx, gw),
		bar:     storagetest.MintProduct(t, ctx, gw, "video-bar").Name,
		display: storagetest.MintProduct(t, ctx, gw, "display").Name,
	}
}

func (f *staffingFixture) count(sql string, args ...any) int {
	f.t.Helper()
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		f.t.Fatalf("connect: %v", err)
	}
	defer conn.Close(f.ctx)
	var n int
	if err := conn.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("count: %v", err)
	}
	return n
}

func (f *staffingFixture) roles(tok, system string) systemRolesWire {
	f.t.Helper()
	var w systemRolesWire
	if err := json.Unmarshal(f.c.do(tok, http.MethodGet, "/systems/"+system+"/roles", nil, http.StatusOK), &w); err != nil {
		f.t.Fatalf("decode system roles: %v", err)
	}
	return w
}

func (f *staffingFixture) memberNames(system string) []string {
	f.t.Helper()
	var body struct {
		Members []struct {
			Component string `json:"component"`
		} `json:"members"`
	}
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodGet, "/systems/"+system+"/members", nil, http.StatusOK), &body); err != nil {
		f.t.Fatalf("decode members: %v", err)
	}
	out := make([]string, 0, len(body.Members))
	for _, m := range body.Members {
		out = append(out, m.Component)
	}
	return out
}

func (f *staffingFixture) component(name, product string) {
	f.t.Helper()
	f.c.do(f.owner, http.MethodPost, "/components", map[string]any{"name": name, "product": product}, http.StatusCreated)
}

// TestStaffingWritesNeedTheComponentInScope is #510: a membership or a role
// assignment writes onto its component (its primary membership, its label, the
// system band of its cascade, its contribution to health), so every route that
// names one needs component:update over the component as well as system:update
// over the system. A principal scoped to one system binds nothing it cannot
// write. Holding no component:update at all is a 403 naming the permission; a
// component outside the caller's component read scope is the non-disclosing 404,
// one it may read but not write a 403; and a member or occupant it reaches
// through the system's own read is a 403 whichever.
func TestStaffingWritesNeedTheComponentInScope(t *testing.T) {
	f := newStaffingFixture(t)
	c, owner := f.c, f.owner
	// The issue's custom role, in place before the first request builds the
	// role index: system:create,update,delete and nothing on any component.
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(f.ctx, `insert into role (name, official, permissions, inherits) values ('room-staffer', false, '{"system:create,update,delete"}', '{}')`); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	conn.Close(f.ctx)

	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "probe-room"}, http.StatusCreated)
	c.do(owner, http.MethodPatch, "/systems/probe-room/roles/spare", map[string]any{"label": "Spare"}, http.StatusOK)
	c.do(owner, http.MethodPatch, "/systems/probe-room/roles/second", map[string]any{"label": "Second"}, http.StatusOK)
	roomID := entityID(t, c, owner, "/systems", "probe-room")
	// The component the principal has no authority over, and a rack of its own.
	f.component("victim-dsp", f.display)
	f.component("own-rack", f.display)
	rackID := entityID(t, c, owner, "/components", "own-rack")
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "own-dsp", "product": f.display, "parent": rackID}, http.StatusCreated)
	// Members the principal finds already bound: one plain, one staffed.
	f.component("victim-member", f.display)
	f.component("victim-staffed", f.display)
	c.do(owner, http.MethodPut, "/systems/probe-room/members/victim-member", nil, http.StatusNoContent)
	c.do(owner, http.MethodPut, "/systems/probe-room/roles/second/assignments/victim-staffed", nil, http.StatusNoContent)

	// The issue's principal: one system, and no component authority at all.
	blind := principalWithGrants(t, f.ctx, f.dsn, "room-staffer-blind", []grant{
		{role: "room-staffer", scopeKind: "system", scopeID: roomID},
	})
	// The same, with component:update over its own rack and nothing else.
	racked := principalWithGrants(t, f.ctx, f.dsn, "room-staffer-racked", []grant{
		{role: "room-staffer", scopeKind: "system", scopeID: roomID},
		{role: "deploy", scopeKind: "component", scopeID: rackID},
	})
	// The same again beside the read floor: it reads every component and still
	// writes only its rack.
	floored := principalWithGrants(t, f.ctx, f.dsn, "room-staffer-floored", []grant{
		{role: "room-staffer", scopeKind: "system", scopeID: roomID},
		{role: "deploy", scopeKind: "component", scopeID: rackID},
		{role: "viewer", scopeKind: "all"},
	})

	refusals := []struct {
		op, name, method, path string
		racked, floored        int
	}{
		{"PUT /systems/{name}/members/{component}", "add-member", http.MethodPut, "/systems/probe-room/members/victim-dsp", http.StatusNotFound, http.StatusForbidden},
		{"PUT /systems/{name}/roles/{role}/assignments/{component}", "assign-role", http.MethodPut, "/systems/probe-room/roles/spare/assignments/victim-dsp", http.StatusNotFound, http.StatusForbidden},
		{"POST /systems/{name}/members/{component}:setPrimary", "set-primary", http.MethodPost, "/systems/probe-room/members/victim-member:setPrimary", http.StatusForbidden, http.StatusForbidden},
		{"DELETE /systems/{name}/members/{component}", "remove-member", http.MethodDelete, "/systems/probe-room/members/victim-member", http.StatusForbidden, http.StatusForbidden},
		{"DELETE /systems/{name}/roles/{role}/assignments/{component}", "unassign-role", http.MethodDelete, "/systems/probe-room/roles/second/assignments/victim-staffed", http.StatusForbidden, http.StatusForbidden},
	}
	// Every cross-tier write the spec carries is driven here: a new one fails
	// this until it gets a row (TestEveryCrossTierPathWriteGuardsBothEnds finds
	// them).
	driven := map[string]bool{}
	for _, r := range refusals {
		driven[r.op] = true
	}
	for op := range crossTierPathWrites(t) {
		if !driven[op] {
			t.Errorf("%s names a component from another tier but is not driven by this test; add a refusal row for it", op)
		}
	}
	for _, r := range refusals {
		st, raw := c.send(blind, r.method, r.path, nil)
		if st != http.StatusForbidden || !strings.Contains(string(raw), "component:update") {
			t.Errorf("%s by a principal with no component authority = %d %s, want 403 naming component:update", r.name, st, raw)
		}
		if st, raw := c.send(racked, r.method, r.path, nil); st != r.racked {
			t.Errorf("%s by a principal that cannot read the component = %d, want %d\nbody: %s", r.name, st, r.racked, raw)
		}
		st, raw = c.send(floored, r.method, r.path, nil)
		if st != r.floored {
			t.Errorf("%s by a principal that reads but cannot write the component = %d, want %d\nbody: %s", r.name, st, r.floored, raw)
		}
		if st == http.StatusForbidden && !strings.Contains(string(raw), "component:update scope") {
			t.Errorf("%s refusal = %s, want it to name the component:update scope", r.name, raw)
		}
	}
	// Nothing landed on the victim: no membership, no staffing.
	var mem struct {
		Memberships []struct {
			System string `json:"system"`
		} `json:"memberships"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodGet, "/components/victim-dsp/memberships", nil, http.StatusOK), &mem); err != nil {
		t.Fatalf("decode memberships: %v", err)
	}
	if len(mem.Memberships) != 0 {
		t.Fatalf("victim-dsp memberships = %+v, want none", mem.Memberships)
	}
	if members := f.memberNames("probe-room"); len(members) != 2 {
		t.Fatalf("probe-room members = %v, want the two the owner bound, untouched", members)
	}
	if mic := f.roles(owner, "probe-room").find(t, "second"); len(mic.AssignedTo) != 1 {
		t.Fatalf("second = %+v, want victim-staffed still in it", mic)
	}

	// Inside both scopes, every one of the five routes is served, so the
	// refusals above are the component end and not the routes being broken.
	c.do(racked, http.MethodPut, "/systems/probe-room/members/own-dsp", nil, http.StatusNoContent)
	c.do(racked, http.MethodPost, "/systems/probe-room/members/own-dsp:setPrimary", nil, http.StatusNoContent)
	c.do(racked, http.MethodPut, "/systems/probe-room/roles/spare/assignments/own-dsp", nil, http.StatusNoContent)
	c.do(racked, http.MethodDelete, "/systems/probe-room/roles/spare/assignments/own-dsp", nil, http.StatusNoContent)
	c.do(racked, http.MethodDelete, "/systems/probe-room/members/own-dsp", nil, http.StatusNoContent)
}

// crossTierPathWrites finds every write operation whose path names an entity of
// a scoped tier other than the one the route is gated on (today, a component
// named under a system), keyed "METHOD /path" with the tier it names. Such a
// write lands on both ends, so each end must be checked in its own tier's
// scope (#510).
func crossTierPathWrites(t *testing.T) map[string]string {
	t.Helper()
	tierOf := map[string]string{"component": "component", "system": "system", "location": "location"}
	out := map[string]string{}
	for _, op := range operations(t, storage.UnimplementedGateway{}) {
		if op.method == http.MethodGet || op.perm == "" {
			continue
		}
		gate := strings.SplitN(op.perm, ":", 2)[0]
		for _, param := range op.pathParams {
			if tier, ok := tierOf[param]; ok && tier != gate {
				out[op.method+" "+op.path] = tier
			}
		}
	}
	return out
}

// TestEveryCrossTierPathWriteGuardsBothEnds is the structural half of #510's
// proof: every write that names an entity of another scoped tier in its path
// publishes that tier's update permission as its conditional permission, so the
// spec says the route costs a write on both ends, and
// TestStaffingWritesNeedTheComponentInScope drives each one with a principal
// scoped to the gated end alone and proves it is refused. A new cross-tier
// route fails here until it is stamped, and fails there until it is driven.
func TestEveryCrossTierPathWriteGuardsBothEnds(t *testing.T) {
	writes := crossTierPathWrites(t)
	if len(writes) == 0 {
		t.Fatal("found no cross-tier path writes; the membership and assignment routes should be among them")
	}
	conds := map[string]string{}
	for _, op := range operations(t, storage.UnimplementedGateway{}) {
		conds[op.method+" "+op.path] = op.condPerm
	}
	for op, tier := range writes {
		if want := tier + ":update"; conds[op] != want {
			t.Errorf("%s names a %s in its path but publishes conditional permission %q, want %q: the write lands on both ends", op, tier, conds[op], want)
		}
	}
}
