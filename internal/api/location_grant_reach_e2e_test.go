package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// What a LOCATION-scoped deploy grant reaches on the system and component
// tiers, driven against the shape devseed ships as `tech-east`
// (internal/devseed/fixtures.yaml): one deploy grant at a location root, with
// the under-only operator, and nothing else.
//
// Until #1498 that grant reached no system and no component at all, not even
// to read one, because every tree tier was contained by its own scope kind
// alone (#714). The cross-tier expansion's create-and-read half has landed: a
// location grant now reads the systems and components placed under its
// location, and creates root ones placed there, while every modify action
// (update, rename, move, delete) keeps its own tier until the rest of #10. This
// file is where each half of that line shows up as a deliberate decision.

type locationGrantFixture struct {
	c *apiClient
	// dsn is kept so a test can mint a further user against the same fleet.
	dsn string
	// owner runs the control half: the identical body, served, so every refusal
	// below is a scope boundary rather than a broken route.
	owner string
	// techEast is devseed's shape exactly: one deploy grant at a location root,
	// with the under-only operator, and nothing else.
	techEast string
	// floored is that same grant beside the all-scoped viewer floor, so it can
	// READ west, the location outside its grant: a refusal there is then about
	// the create scope, not about visibility.
	floored string
	rackID  string
}

// newLocationGrantFixture builds east (with annex under it) and west, each
// holding a system and a component placed by the owner. amp is placed by its
// primary system alone (no location of its own), which is the effective-place
// rule a component follows.
func newLocationGrantFixture(t *testing.T) *locationGrantFixture {
	t.Helper()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "east", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{
		"name": "annex", "location_type": "building", "parent": "east",
	}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "west", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{
		"name": "av", "system_type_id": "board", "location": "annex",
	}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{
		"name": "av-west", "system_type_id": "board", "location": "west",
	}, http.StatusCreated)
	rack := createdID(t, c.do(owner, http.MethodPost, "/components", map[string]any{
		"name": "rack", "product": "generic-device", "location": "annex",
	}, http.StatusCreated))
	c.do(owner, http.MethodPost, "/components", map[string]any{
		"name": "amp", "product": "generic-device", "system": "av",
	}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{
		"name": "rack-west", "product": "generic-device", "location": "west",
	}, http.StatusCreated)

	eastID := entityID(t, c, owner, "/locations", "east")
	deployAtEast := grant{role: "deploy", scopeKind: "location", scopeID: eastID, scopeOp: "subtree_excl_root"}
	return &locationGrantFixture{
		c: c, dsn: dsn, owner: owner, rackID: rack,
		techEast: userWithGrants(t, ctx, dsn, "tech-east", []grant{deployAtEast}),
		floored:  userWithGrants(t, ctx, dsn, "tech-east-floored", []grant{deployAtEast, {role: "viewer", scopeKind: "all"}}),
	}
}

// TestALocationGrantCreatesARootSystemAndComponentInItsLocation is the field
// tech's build-out (#1498): with the one grant devseed ships, a root system and
// a root component placed inside the granted location are served. A location
// outside the grant, which the caller cannot read, is the placement bind's
// non-disclosing 422; a root with no location at all has nothing that could
// admit it and stays the 403 it was.
func TestALocationGrantCreatesARootSystemAndComponentInItsLocation(t *testing.T) {
	t.Parallel()
	f := newLocationGrantFixture(t)

	f.c.do(f.techEast, http.MethodPost, "/systems", map[string]any{
		"name": "lab-av", "label": "Media Lab AV", "location": "annex",
	}, http.StatusCreated)
	f.c.do(f.techEast, http.MethodPost, "/components", map[string]any{
		"name": "lab-display", "label": "Lab Display", "product": "generic-device", "location": "annex",
	}, http.StatusCreated)

	// Outside east: not readable, so refused as absent, exactly as before.
	f.c.do(f.techEast, http.MethodPost, "/systems", map[string]any{
		"name": "far-av", "location": "west",
	}, http.StatusUnprocessableEntity)
	f.c.do(f.techEast, http.MethodPost, "/components", map[string]any{
		"name": "far-display", "product": "generic-device", "location": "west",
	}, http.StatusUnprocessableEntity)

	// Readable through the floor but outside the create scope: the location
	// exists for this caller, so the refusal names the missing authority.
	f.c.do(f.floored, http.MethodPost, "/systems", map[string]any{
		"name": "far-av", "location": "west",
	}, http.StatusForbidden)
	f.c.do(f.floored, http.MethodPost, "/components", map[string]any{
		"name": "far-display", "product": "generic-device", "location": "west",
	}, http.StatusForbidden)

	// No location: a root placed nowhere still needs an all-scoped grant.
	f.c.do(f.techEast, http.MethodPost, "/systems", map[string]any{"name": "loose-av"}, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodPost, "/components", map[string]any{
		"name": "loose-display", "product": "generic-device",
	}, http.StatusForbidden)

	// The owner is served on the refused bodies, so each refusal above is a
	// scope boundary rather than a broken route.
	f.c.do(f.owner, http.MethodPost, "/systems", map[string]any{"name": "far-av", "location": "west"}, http.StatusCreated)
	f.c.do(f.owner, http.MethodPost, "/systems", map[string]any{"name": "loose-av"}, http.StatusCreated)
}

// TestALocationGrantReadsTheSystemsAndComponentsPlacedInIt is the read half:
// the list and the get answer with exactly the rows placed under east, the
// ones the tech created beside the ones already there, and nothing in west. A
// component with no location of its own is placed by its primary system.
func TestALocationGrantReadsTheSystemsAndComponentsPlacedInIt(t *testing.T) {
	t.Parallel()
	f := newLocationGrantFixture(t)

	f.c.do(f.techEast, http.MethodPost, "/systems", map[string]any{"name": "lab-av", "location": "annex"}, http.StatusCreated)
	f.c.do(f.techEast, http.MethodPost, "/components", map[string]any{
		"name": "lab-display", "product": "generic-device", "location": "annex",
	}, http.StatusCreated)
	// A child under a component placed in east is inside the create scope too:
	// the placed row brings its subtree, as a same-tier grant on it would.
	f.c.do(f.techEast, http.MethodPost, "/components", map[string]any{
		"name": "plate", "product": "generic-device", "parent": f.rackID,
	}, http.StatusCreated)

	systems := listNames(t, f.c, f.techEast, "/systems")
	slices.Sort(systems)
	if want := []string{"av", "lab-av"}; !slices.Equal(systems, want) {
		t.Errorf("tech-east lists systems %v, want %v", systems, want)
	}
	components := listNames(t, f.c, f.techEast, "/components")
	slices.Sort(components)
	if want := []string{"amp", "lab-display", "plate", "rack"}; !slices.Equal(components, want) {
		t.Errorf("tech-east lists components %v, want %v", components, want)
	}

	for _, path := range []string{"/systems/av", "/systems/lab-av", "/components/rack", "/components/amp", "/components/lab-display"} {
		f.c.do(f.techEast, http.MethodGet, path, nil, http.StatusOK)
	}
	for _, path := range []string{"/systems/av-west", "/components/rack-west"} {
		f.c.do(f.techEast, http.MethodGet, path, nil, http.StatusNotFound)
	}
}

// TestALocationGrantModifiesNoSystemOrComponent is the half that has not moved
// (#10): every modify action keeps its own tier. The rows are readable now, so
// each refusal is the 403 side of the read-then-action split rather than a 404.
func TestALocationGrantModifiesNoSystemOrComponent(t *testing.T) {
	t.Parallel()
	f := newLocationGrantFixture(t)

	f.c.do(f.techEast, http.MethodPatch, "/systems/av", map[string]any{"label": "Renamed"}, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodPost, "/systems/av:rename", map[string]any{"name": "av-renamed"}, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodPost, "/systems/av:move", map[string]any{"location": "annex"}, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodDelete, "/systems/av", nil, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodPut, "/systems/av/members/"+f.rackID, nil, http.StatusForbidden)
	// A child system is a write under av, which the create scope admits now
	// that av is placed in east, so it is served; editing av itself is not.
	f.c.do(f.techEast, http.MethodPost, "/systems", map[string]any{"name": "sub", "parent": "av"}, http.StatusCreated)

	f.c.do(f.techEast, http.MethodPatch, "/components/rack", map[string]any{"label": "Renamed"}, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodPost, "/components/rack:rename", map[string]any{"name": "rack-renamed"}, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodPost, "/components/rack:move", map[string]any{"location": "annex"}, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodDelete, "/components/rack", nil, http.StatusForbidden)

	// A row the tech created itself is no different: it is placed in east, so
	// it reads, and modifying it still takes a grant on its own tier.
	f.c.do(f.techEast, http.MethodPost, "/systems", map[string]any{"name": "lab-av", "location": "annex"}, http.StatusCreated)
	f.c.do(f.techEast, http.MethodPatch, "/systems/lab-av", map[string]any{"label": "Renamed"}, http.StatusForbidden)

	// The location tier of the same grant, minus the scope root, which is what
	// "under only" means.
	f.c.do(f.techEast, http.MethodPost, "/locations", map[string]any{
		"name": "closet", "location_type": "room", "parent": "annex",
	}, http.StatusCreated)
	f.c.do(f.techEast, http.MethodPatch, "/locations/east", map[string]any{"label": "Renamed root"}, http.StatusForbidden)

	// The owner runs the identical bodies and is served on each.
	f.c.do(f.owner, http.MethodPatch, "/systems/av", map[string]any{"label": "Renamed"}, http.StatusOK)
	f.c.do(f.owner, http.MethodPost, "/systems/av:move", map[string]any{"location": "annex"}, http.StatusOK)
	f.c.do(f.owner, http.MethodPut, "/systems/av/members/"+f.rackID, nil, http.StatusNoContent)
	f.c.do(f.owner, http.MethodPatch, "/components/rack", map[string]any{"label": "Renamed"}, http.StatusOK)
	f.c.do(f.owner, http.MethodPost, "/components/rack:move", map[string]any{"location": "annex"}, http.StatusOK)
}

// TestAComponentCreateNamingASystemStillNeedsTheSystemWrite pins #707 under
// the new reach: the tech reads av and may create a component, but naming av
// inserts a membership, a system write a location grant does not carry. The
// refusal names that authority rather than denying a system the caller reads.
func TestAComponentCreateNamingASystemStillNeedsTheSystemWrite(t *testing.T) {
	t.Parallel()
	f := newLocationGrantFixture(t)

	f.c.do(f.techEast, http.MethodGet, "/systems/av", nil, http.StatusOK)
	status, body := f.c.send(f.techEast, http.MethodPost, "/components", map[string]any{
		"name": "panel", "product": "generic-device", "location": "annex", "system": "av",
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
	f.c.do(f.techEast, http.MethodGet, "/components/panel", nil, http.StatusNotFound)
}

// TestTheCrossTierReachIsOnlyASubtreeLocationGrants holds the two negatives:
// a self location grant is a leaf-lock that grows nothing under its node on
// any tier, and a system-kind grant confers no location access, so neither
// gains a root create the subtree location grant now has.
func TestTheCrossTierReachIsOnlyASubtreeLocationGrants(t *testing.T) {
	t.Parallel()
	f := newLocationGrantFixture(t)
	ctx := context.Background()

	annexID := entityID(t, f.c, f.owner, "/locations", "annex")
	self := userWithGrants(t, ctx, f.dsn, "tech-annex-self", []grant{
		{role: "deploy", scopeKind: "location", scopeID: annexID, scopeOp: "self"},
	})
	// The self grant reads annex itself, so the bind resolves and the refusal
	// is the create scope's.
	f.c.do(self, http.MethodGet, "/locations/annex", nil, http.StatusOK)
	f.c.do(self, http.MethodPost, "/systems", map[string]any{"name": "lab-av", "location": "annex"}, http.StatusForbidden)
	f.c.do(self, http.MethodPost, "/components", map[string]any{
		"name": "lab-display", "product": "generic-device", "location": "annex",
	}, http.StatusForbidden)
	if names := listNames(t, f.c, self, "/systems"); len(names) != 0 {
		t.Errorf("a self location grant lists systems %v, want none", names)
	}

	avID := entityID(t, f.c, f.owner, "/systems", "av")
	sys := userWithGrants(t, ctx, f.dsn, "tech-av", []grant{{role: "deploy", scopeKind: "system", scopeID: avID}})
	if names := listNames(t, f.c, sys, "/locations"); len(names) != 0 {
		t.Errorf("a system grant lists locations %v, want none", names)
	}
	f.c.do(sys, http.MethodGet, "/locations/annex", nil, http.StatusNotFound)
	// Its create scope has no location root to place a root system by, so the
	// root is refused before the location is resolved, as it always was.
	f.c.do(sys, http.MethodPost, "/systems", map[string]any{"name": "lab-av", "location": "annex"}, http.StatusForbidden)
}
