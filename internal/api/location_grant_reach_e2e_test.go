package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// What a LOCATION-scoped deploy grant actually reaches on the system tier.
//
// The `deploy` role's own description says it "builds out and edits everything
// inside a subtree (add rooms, systems, and components)" when granted at a
// location, and devseed ships exactly that grant as `tech-east`
// (internal/devseed/fixtures.yaml). The scope resolver does not agree with the
// sentence: applicableKinds("system") is {"system"} alone
// (internal/scope/scope.go), the cross-tier expansion that would make a location
// grant reach the systems inside it is unbuilt (#10), so a location-kind grant
// contributes NOTHING to any scope resolved for "system".
//
// This test is the evidence for that, driven rather than reasoned, because the
// question decides how far a fix has to go. If a location grant could do system
// work before #707 then #707 broke a shipped role and the repair is
// cross-tier expansion; if it could not, #707 extended a pre-existing gap by one
// path and the repair is an honest refusal and a console that does not offer
// what the server will decline. Every system route below is untouched by this
// branch (git diff origin/main...HEAD names neither internal/api/systems.go nor
// internal/api/members.go), so what it answers today is what it answered before
// #707.
//
// It is a regression test as much as a record: if the cross-tier expansion ever
// lands, these refusals become successes, and this test is where that shows up
// as a deliberate decision rather than a surprise.

type locationGrantFixture struct {
	c *apiClient
	// dsn is kept so a test can mint a further principal against the same estate.
	dsn string
	// owner runs the control half: the identical body, served, so every refusal
	// below is a scope boundary rather than a broken route.
	owner string
	// techEast is devseed's shape exactly: one deploy grant at a location root,
	// with the under-only operator, and nothing else.
	techEast string
	// floored is that same grant beside the all-scoped viewer floor a real
	// principal carries, which is what gives it system:READ without giving it
	// any system-tier write scope.
	floored string
	// builder adds a COMPONENT-tier grant to floored, so it can actually create a
	// component and the create's system bind is the only thing left to refuse.
	// Three grants for three tiers is the shape a real principal takes while the
	// cross-tier expansion is unbuilt (#10); see placement_scope_e2e_test.go.
	builder string
	rackID  string
}

func newLocationGrantFixture(t *testing.T) *locationGrantFixture {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
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
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "east", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{
		"name": "annex", "location_type": "building", "parent": "east",
	}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{
		"name": "av", "system_type_id": "board", "location": "annex",
	}, http.StatusCreated)
	rack := createdID(t, c.do(owner, http.MethodPost, "/components", map[string]any{
		"name": "rack", "product": "generic-device", "location": "annex",
	}, http.StatusCreated))

	eastID := entityID(t, c, owner, "/locations", "east")
	deployAtEast := grant{role: "deploy", scopeKind: "location", scopeID: eastID, scopeOp: "subtree_excl_root"}
	floor := grant{role: "viewer", scopeKind: "all"}
	return &locationGrantFixture{
		c: c, dsn: dsn, owner: owner, rackID: rack,
		techEast: principalWithGrants(t, ctx, dsn, "tech-east", []grant{deployAtEast}),
		floored:  principalWithGrants(t, ctx, dsn, "tech-east-floored", []grant{deployAtEast, floor}),
		builder: principalWithGrants(t, ctx, dsn, "tech-east-builder", []grant{
			deployAtEast, floor, {role: "deploy", scopeKind: "component", scopeID: rack},
		}),
	}
}

// TestALocationGrantSeesNoSystemAtAll is the devseed principal as shipped. Its
// one grant carries system:read (the viewer floor `deploy` inherits) and
// system:create,update,rename,move, and none of them resolves to anything,
// because the grant's scope_kind is "location".
func TestALocationGrantSeesNoSystemAtAll(t *testing.T) {
	f := newLocationGrantFixture(t)

	if names := listNames(t, f.c, f.techEast, "/systems"); len(names) != 0 {
		t.Errorf("tech-east lists systems %v, want none: a location-kind grant fills no system-tier scope", names)
	}
	f.c.do(f.techEast, http.MethodGet, "/systems/av", nil, http.StatusNotFound)

	// The location tier of the same grant works, so the empty system scope is
	// this grant's reach and not a broken fixture.
	if names := listNames(t, f.c, f.techEast, "/locations"); len(names) == 0 {
		t.Fatalf("tech-east lists no locations: the grant itself is not resolving")
	}
	f.c.do(f.techEast, http.MethodPost, "/locations", map[string]any{
		"name": "studio", "location_type": "room", "parent": "annex",
	}, http.StatusCreated)
}

// TestALocationGrantWritesNoSystem drives every system-tier write route with the
// read floor in place, so the refusals are about the write scope alone. This is
// the answer to "could deploy@location do system work before #707": no route
// here is touched by this branch, and all of them refuse.
func TestALocationGrantWritesNoSystem(t *testing.T) {
	f := newLocationGrantFixture(t)

	// The floor is real: the system this principal cannot write, it can read.
	f.c.do(f.floored, http.MethodGet, "/systems/av", nil, http.StatusOK)

	// Create, at its own location and under its own system: both refused, the
	// first because a parentless system needs an all-scoped create grant, the
	// second because the parent resolves in a system:create scope that is empty.
	f.c.do(f.floored, http.MethodPost, "/systems", map[string]any{
		"name": "av-2", "system_type_id": "board", "location": "annex",
	}, http.StatusForbidden)
	f.c.do(f.floored, http.MethodPost, "/systems", map[string]any{
		"name": "sub", "system_type_id": "board", "parent": "av",
	}, http.StatusForbidden)

	// Edit, rename, move: readable, so the read half resolves and the action half
	// does not, which is the 403 side of the read-then-action split.
	f.c.do(f.floored, http.MethodPatch, "/systems/av", map[string]any{"label": "Renamed"}, http.StatusForbidden)
	f.c.do(f.floored, http.MethodPost, "/systems/av:rename", map[string]any{"name": "av-renamed"}, http.StatusForbidden)
	f.c.do(f.floored, http.MethodPost, "/systems/av:move", map[string]any{"location": "annex"}, http.StatusForbidden)

	// The membership route, which is the one the component create writes through.
	// It answers 403 alongside its siblings now (#736): it used to resolve its
	// system with the update scope alone and so reported a readable system
	// absent, which is the one refusal on this list that disagreed with the
	// three above it for no reason a caller could see.
	f.c.do(f.floored, http.MethodPut, "/systems/av/members/"+f.rackID, nil, http.StatusForbidden)

	// The owner runs the identical bodies and is served on each, so every refusal
	// above is a scope boundary rather than a broken route.
	f.c.do(f.owner, http.MethodPost, "/systems", map[string]any{
		"name": "av-2", "system_type_id": "board", "location": "annex",
	}, http.StatusCreated)
	f.c.do(f.owner, http.MethodPatch, "/systems/av", map[string]any{"label": "Renamed"}, http.StatusOK)
	f.c.do(f.owner, http.MethodPost, "/systems/av:move", map[string]any{"location": "annex"}, http.StatusOK)
	f.c.do(f.owner, http.MethodPut, "/systems/av/members/"+f.rackID, nil, http.StatusNoContent)
}

// TestTheComponentCreateWasTheOnlySystemWriteALocationGrantCouldReach pins the
// exception, which is the whole of what #707 changed for this principal. Before
// #707 the create's system bind resolved in the system:READ scope, which the
// all-scoped viewer floor fills, so this one path wrote a membership that every
// route above refused. That is the hole ADR-0107 closed, not a working shape it
// broke.
//
// What the refusal SAYS is the part this wave still owes the operator: the
// caller just read this system, so answering "system not found" is a lie about
// existence.
func TestTheComponentCreateWasTheOnlySystemWriteALocationGrantCouldReach(t *testing.T) {
	f := newLocationGrantFixture(t)

	f.c.do(f.builder, http.MethodGet, "/systems/av", nil, http.StatusOK)

	// The same create without the system is served, so what follows is about the
	// bind and not about the caller's right to create a component at all.
	f.c.do(f.builder, http.MethodPost, "/components", map[string]any{
		"name": "plate", "product": "generic-device", "parent": f.rackID,
	}, http.StatusCreated)

	status, body := f.c.send(f.builder, http.MethodPost, "/components", map[string]any{
		"name": "panel", "product": "generic-device", "parent": f.rackID, "system": "av",
	})
	if status != http.StatusForbidden {
		t.Fatalf("create naming a readable system out of the update scope = %d, want 403\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "system:update") {
		t.Errorf("refusal = %s, want it to name the authority that is missing rather than the row's existence", body)
	}
	if strings.Contains(string(body), "not found") {
		t.Errorf("refusal = %s, want it not to deny the existence of a system the caller just read", body)
	}
	// Nothing was written.
	f.c.do(f.builder, http.MethodGet, "/components/panel", nil, http.StatusNotFound)
}

// TestALocationGrantWritesNoComponentEither is the third tier of the same
// claim, and the one no test drove (#714). The description said a location
// grant adds "rooms, systems, and components"; the system half is refuted
// above, and this is the component half, driven the same way and refused the
// same way, so the sentence in internal/seed/roles.yaml is now backed by all
// three of the things it talks about rather than one of them.
func TestALocationGrantWritesNoComponentEither(t *testing.T) {
	f := newLocationGrantFixture(t)

	// The devseed shape, with its single grant: it cannot even SEE a component
	// in the wing it is scoped to, which is the part an admin granting this role
	// is least likely to expect.
	if names := listNames(t, f.c, f.techEast, "/components"); len(names) != 0 {
		t.Errorf("tech-east lists components %v, want none: a location-kind grant fills no component-tier scope", names)
	}
	f.c.do(f.techEast, http.MethodGet, "/components/rack", nil, http.StatusNotFound)

	// With the read floor in place, so the refusals below are about the write
	// scope alone rather than about visibility.
	f.c.do(f.floored, http.MethodGet, "/components/rack", nil, http.StatusOK)

	// Create, at its own location and under a component it can read: both
	// refused, the first because a parentless component needs an all-scoped
	// create grant, the second because the parent resolves in a component:create
	// scope that is empty.
	f.c.do(f.floored, http.MethodPost, "/components", map[string]any{
		"name": "panel-a", "product": "generic-device", "location": "annex",
	}, http.StatusForbidden)
	f.c.do(f.floored, http.MethodPost, "/components", map[string]any{
		"name": "panel-b", "product": "generic-device", "parent": f.rackID,
	}, http.StatusForbidden)

	// Edit, rename, move: readable, so the read half resolves and the action
	// half does not.
	f.c.do(f.floored, http.MethodPatch, "/components/rack", map[string]any{"label": "Renamed"}, http.StatusForbidden)
	f.c.do(f.floored, http.MethodPost, "/components/rack:rename", map[string]any{"name": "rack-renamed"}, http.StatusForbidden)
	f.c.do(f.floored, http.MethodPost, "/components/rack:move", map[string]any{"location": "annex"}, http.StatusForbidden)

	// The one thing the role's description promises that is TRUE: the location
	// tier, minus the scope root, which is what "under only" means.
	f.c.do(f.floored, http.MethodPost, "/locations", map[string]any{
		"name": "closet", "location_type": "room", "parent": "annex",
	}, http.StatusCreated)
	f.c.do(f.floored, http.MethodPatch, "/locations/east", map[string]any{"label": "Renamed root"}, http.StatusForbidden)

	// The owner runs the identical bodies and is served on each, so every
	// refusal above is a scope boundary rather than a broken route.
	f.c.do(f.owner, http.MethodPost, "/components", map[string]any{
		"name": "panel-a", "product": "generic-device", "location": "annex",
	}, http.StatusCreated)
	f.c.do(f.owner, http.MethodPatch, "/components/rack", map[string]any{"label": "Renamed"}, http.StatusOK)
	f.c.do(f.owner, http.MethodPost, "/components/rack:move", map[string]any{"location": "annex"}, http.StatusOK)
}
