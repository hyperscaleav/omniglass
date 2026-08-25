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

// A write binds a placement it can READ (#700), driven over the wire.
//
// A create and a move both name a location (and a create names a system), and
// since #657 put placement into the label data map those references are read
// back out of the answer: the stored label carries the location's own label and
// the primary system's TYPE label. A reference resolved existence-only therefore
// turns a create into a disclosure channel for a row the caller holds no grant
// to read, which is exactly what #699's draft route already refuses. These tests
// hold the two to the same answer.
//
// Every one of them drives a NARROW-scoped principal and asserts the owner
// succeeding alongside on the identical body. Three scope defects in this arc
// survived review because every fixture held an all scope, so an assertion that
// a narrow principal is refused proves nothing on its own: a broken route
// refuses everyone.

// placementFixture is the fleet every test below shares: one campus, two wings
// under it, and a component and a system inside wing-a for a narrow principal to
// write against. wing-b is the one it must never be able to read, and its label
// is the string that must never appear in an answer it receives.
//
// The narrow principal carries THREE grants, one per tier, because the
// cross-tier scope expansion is not built (internal/scope/scope.go's
// applicableKinds, tracked in #10): a location-scoped grant confers no component
// scope, so a principal that can create a component and can also read one wing
// has to be granted at each tier explicitly. That is the shape a real grant set
// takes today, and it is what makes the two halves of every assertion below
// independent: the write scope says which rows it may write, the read scope says
// which placements those writes may name.
type placementFixture struct {
	c        *apiClient
	dsn      string
	owner    string
	narrow   string
	rackID   string
	sysAID   string
	wingBSys string
	product  storage.Product
}

func newPlacementFixture(t *testing.T) *placementFixture {
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
	// The product every component below is created from: minted for the
	// fixture rather than borrowed from the seeded catalog (#804).
	product := storagetest.MintProduct(t, ctx, gw, "")

	// Rules that read the placement, so the leak these gates exist to close is
	// actually present in the answer rather than assumed to be.
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.SystemTypeLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the system rule: %v", err)
	}

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{
		"name": "wing-a", "location_type": "building", "parent": "hq", "label": "Wing A",
	}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{
		"name": "wing-b", "location_type": "building", "parent": "hq", "label": "Secret Wing",
	}, http.StatusCreated)

	// A root component and a root system both need an all-scoped grant, so the
	// owner builds the two rows the narrow principal then writes under. The rack
	// sits in wing-a, which is what puts it inside that principal's own scope.
	rack := c.do(owner, http.MethodPost, "/components", map[string]any{
		"name": "rack", "product": product.Name, "location": "wing-a",
	}, http.StatusCreated)
	var rackBody struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rack, &rackBody); err != nil {
		t.Fatalf("parse rack: %v", err)
	}
	c.do(owner, http.MethodPost, "/systems", map[string]any{
		"name": "av-a", "system_type_id": "board", "location": "wing-a",
	}, http.StatusCreated)
	// The system in the wing the narrow principal cannot read. A component
	// created into it would carry its TYPE label, the second placement fact a
	// component's map reads.
	c.do(owner, http.MethodPost, "/systems", map[string]any{
		"name": "av-b", "system_type_id": "board", "location": "wing-b",
	}, http.StatusCreated)

	sysAID := entityID(t, c, owner, "/systems", "av-a")
	narrow := principalWithGrants(t, ctx, dsn, "wing-a-deploy", []grant{
		{role: "deploy", scopeKind: "component", scopeID: rackBody.ID},
		{role: "deploy", scopeKind: "system", scopeID: sysAID},
		{role: "deploy", scopeKind: "location", scopeID: entityID(t, c, owner, "/locations", "wing-a")},
	})
	return &placementFixture{
		c: c, dsn: dsn, owner: owner, narrow: narrow,
		rackID: rackBody.ID, sysAID: sysAID, wingBSys: "av-b", product: product,
	}
}

// label reads label off a create or move response.
func label(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("parse entity: %v", err)
	}
	return body.Label
}

// TestAComponentCreateRefusesAPlacementTheCallerCannotRead is the defect this
// issue names, on both of a component's placement references.
func TestAComponentCreateRefusesAPlacementTheCallerCannotRead(t *testing.T) {
	f := newPlacementFixture(t)

	// Inside its own wing the narrow principal creates normally, and the answer
	// carries the label of the location it may read.
	got := f.c.do(f.narrow, http.MethodPost, "/components", map[string]any{
		"name": "panel-a", "product": f.product.Name, "parent": f.rackID, "location": "wing-a",
	}, http.StatusCreated)
	if !strings.HasPrefix(label(t, got), "Wing A") {
		t.Fatalf("in-scope create stored %q, want it to open with the location's label", label(t, got))
	}

	// The sibling wing is refused, on the same body the draft route already
	// refuses, and with the same status: the preview and the create it previews
	// have to agree or an operator is refused a draft and then creates anyway.
	leakBody := map[string]any{
		"name": "panel-b", "product": f.product.Name, "parent": f.rackID, "location": "wing-b",
	}
	// The draft carries the parent the create posts, which it did not before the
	// #702 review: the parentless bucket is now gated on an all-scoped create
	// grant, so a body without it would be refused for the bucket rather than for
	// the location, and this assertion would stop being about wing-b at all.
	f.c.do(f.narrow, http.MethodPost, "/components:renderLabel", map[string]any{
		"name": "panel-b", "product": f.product.Name, "parent": f.rackID, "location": "wing-b",
	}, http.StatusUnprocessableEntity)
	f.c.do(f.narrow, http.MethodPost, "/components", leakBody, http.StatusUnprocessableEntity)

	// The system reference is the component map's other placement fact, and it
	// leaks the same way: av-b sits in the wing this principal cannot read.
	f.c.do(f.narrow, http.MethodPost, "/components", map[string]any{
		"name": "panel-c", "product": f.product.Name, "parent": f.rackID, "system": f.wingBSys,
	}, http.StatusUnprocessableEntity)

	// The owner runs the identical bodies and is served, so both refusals above
	// are a scope boundary and not a broken route.
	owned := f.c.do(f.owner, http.MethodPost, "/components", leakBody, http.StatusCreated)
	if !strings.HasPrefix(label(t, owned), "Secret Wing") {
		t.Errorf("owner create stored %q, want the sibling wing's label: the refusals above are not scope refusals", label(t, owned))
	}
	owned = f.c.do(f.owner, http.MethodPost, "/components", map[string]any{
		"name": "panel-c", "product": f.product.Name, "parent": f.rackID, "system": f.wingBSys,
	}, http.StatusCreated)
	if !strings.Contains(label(t, owned), "Boardroom") {
		t.Errorf("owner create stored %q, want the out-of-scope system's type label in it", label(t, owned))
	}
}

// TestASystemCreateRefusesALocationTheCallerCannotRead is the system tier of the
// same defect: a system's label reads its location's.
func TestASystemCreateRefusesALocationTheCallerCannotRead(t *testing.T) {
	f := newPlacementFixture(t)

	got := f.c.do(f.narrow, http.MethodPost, "/systems", map[string]any{
		"name": "sub-a", "system_type_id": "board", "parent": "av-a", "location": "wing-a",
	}, http.StatusCreated)
	if !strings.HasPrefix(label(t, got), "Wing A") {
		t.Fatalf("in-scope create stored %q, want it to open with the location's label", label(t, got))
	}

	leakBody := map[string]any{
		"name": "sub-b", "system_type_id": "board", "parent": "av-a", "location": "wing-b",
	}
	f.c.do(f.narrow, http.MethodPost, "/systems:renderLabel", map[string]any{
		"name": "sub-b", "system_type_id": "board", "parent": "av-a", "location": "wing-b",
	}, http.StatusUnprocessableEntity)
	f.c.do(f.narrow, http.MethodPost, "/systems", leakBody, http.StatusUnprocessableEntity)

	owned := f.c.do(f.owner, http.MethodPost, "/systems", leakBody, http.StatusCreated)
	if !strings.HasPrefix(label(t, owned), "Secret Wing") {
		t.Errorf("owner create stored %q, want the sibling wing's label: the refusal above is not a scope refusal", label(t, owned))
	}
}

// TestAMoveRefusesALocationTheCallerCannotRead is the same shape on the other
// verb. A move restamps the label from the DESTINATION, so a relocate into an
// unreadable wing hands its label back exactly as a create into it would.
func TestAMoveRefusesALocationTheCallerCannotRead(t *testing.T) {
	f := newPlacementFixture(t)

	f.c.do(f.narrow, http.MethodPost, "/components", map[string]any{
		"name": "panel-a", "product": f.product.Name, "parent": f.rackID, "location": "wing-a",
	}, http.StatusCreated)
	f.c.do(f.narrow, http.MethodPost, "/systems", map[string]any{
		"name": "sub-a", "system_type_id": "board", "parent": "av-a", "location": "wing-a",
	}, http.StatusCreated)

	f.c.do(f.narrow, http.MethodPost, "/components/panel-a:move",
		map[string]any{"location": "wing-b"}, http.StatusUnprocessableEntity)
	f.c.do(f.narrow, http.MethodPost, "/systems/sub-a:move",
		map[string]any{"location": "wing-b"}, http.StatusUnprocessableEntity)

	moved := f.c.do(f.owner, http.MethodPost, "/components/panel-a:move",
		map[string]any{"location": "wing-b"}, http.StatusOK)
	if !strings.HasPrefix(label(t, moved), "Secret Wing") {
		t.Errorf("owner move stored %q, want the destination wing's label: the refusal above is not a scope refusal", label(t, moved))
	}
	moved = f.c.do(f.owner, http.MethodPost, "/systems/sub-a:move",
		map[string]any{"location": "wing-b"}, http.StatusOK)
	if !strings.HasPrefix(label(t, moved), "Secret Wing") {
		t.Errorf("owner move stored %q, want the destination wing's label: the refusal above is not a scope refusal", label(t, moved))
	}
}

// TestAGrantOnOnlyTheWrittenTierBindsNoPlacement pins the corollary of the three
// tests above, which is the case most likely to be read later as a defect and
// "fixed" by accident. It is the only test in the repo that drives an EMPTY
// placement read scope: every other fixture in this arc holds a grant on each
// tier or an all scope, so without this one, nothing pins what an empty set
// does and the behavior could move in either direction unnoticed.
//
// # Why the scope is empty, since no grant here says "none"
//
// scope.Resolve keeps a grant only when its scope_kind is one of
// applicableKinds(resource), and applicableKinds("location") is {"location"}
// alone (internal/scope/scope.go). The cross-tier cascade is unbuilt (#10) and
// it does not run in this direction either: a grant scoped to a COMPONENT or a
// SYSTEM contributes nothing to a location-tier resolve, so a principal holding
// exactly one such grant resolves location:read to the empty set. Since #700
// binds a placement within that set, it can create only what it places nowhere,
// and is refused even the location its own scope root sits in.
//
// # Why that is the correct answer rather than a hole
//
// The refusal cannot be a false negative: resolvePlacementRef and scopedGet are
// the same call on the same scope (internal/storage/scopedcrud.go), so anything
// this caller could GET it can still bind, and an empty location:read means it
// can GET no location at all. And it is not the shape a real principal has: the
// last stanza puts the ordinary viewer read floor beside the IDENTICAL narrow
// write grant, which changes no write scope (viewer carries no create), and the
// same refused body is created.
func TestAGrantOnOnlyTheWrittenTierBindsNoPlacement(t *testing.T) {
	f := newPlacementFixture(t)

	// One grant, on the component tier, at the rack that sits in wing-a.
	// component:create resolves to that rack; location:read resolves to nothing.
	componentOnly := principalWithGrants(t, f.c.ctx, f.dsn, "rack-operator", []grant{
		{role: "operator", scopeKind: "component", scopeID: f.rackID},
	})

	// Placing nowhere names no location, so nothing is resolved in the empty set
	// and the create is served: the write grant on its own is sufficient.
	f.c.do(componentOnly, http.MethodPost, "/components", map[string]any{
		"name": "panel-unplaced", "product": f.product.Name, "parent": f.rackID,
	}, http.StatusCreated)

	// The same body naming wing-a, the rack's OWN location, is refused. The only
	// difference between the two calls is the placement field.
	f.c.do(componentOnly, http.MethodPost, "/components", map[string]any{
		"name": "panel-placed", "product": f.product.Name, "parent": f.rackID, "location": "wing-a",
	}, http.StatusUnprocessableEntity)

	// The other verb answers the same, and its control is a move that names only
	// a component-tier reference, which the same principal is served.
	f.c.do(componentOnly, http.MethodPost, "/components/panel-unplaced:move",
		map[string]any{"parent": f.rackID}, http.StatusOK)
	f.c.do(componentOnly, http.MethodPost, "/components/panel-unplaced:move",
		map[string]any{"location": "wing-a"}, http.StatusUnprocessableEntity)

	// The system tier is the same sentence with the other scope kind: a grant at
	// av-a resolves system:create there and location:read nowhere.
	systemOnly := principalWithGrants(t, f.c.ctx, f.dsn, "av-a-deploy", []grant{
		{role: "deploy", scopeKind: "system", scopeID: f.sysAID},
	})
	f.c.do(systemOnly, http.MethodPost, "/systems", map[string]any{
		"name": "sub-unplaced", "system_type_id": "board", "parent": "av-a",
	}, http.StatusCreated)
	f.c.do(systemOnly, http.MethodPost, "/systems", map[string]any{
		"name": "sub-placed", "system_type_id": "board", "parent": "av-a", "location": "wing-a",
	}, http.StatusUnprocessableEntity)

	// The read floor restores the bind. Same narrow write grant, plus the
	// all-scoped viewer a real principal carries beside it, and the body refused
	// above is created with wing-a's label stamped into it.
	withFloor := principalWithGrants(t, f.c.ctx, f.dsn, "rack-operator-with-floor", []grant{
		{role: "operator", scopeKind: "component", scopeID: f.rackID},
		{role: "viewer", scopeKind: "all"},
	})
	got := f.c.do(withFloor, http.MethodPost, "/components", map[string]any{
		"name": "panel-floored", "product": f.product.Name, "parent": f.rackID, "location": "wing-a",
	}, http.StatusCreated)
	if !strings.HasPrefix(label(t, got), "Wing A") {
		t.Errorf("create under the read floor stored %q, want the location's label: the refusals above are about the empty read scope, and this proves it", label(t, got))
	}
}

// TestAnAmbiguousPlacementNamesOnlyTheCandidatesTheCallerCanRead is #697 driven
// over the wire, and it is the proof that populating the candidate list did not
// re-open the disclosure the redaction was closing.
//
// Every building's first floor may be named "1" (a positional name is allocated
// per placement bucket, and the bucket is the placement), so a bind by the bare
// name matches one row per building and the answer has to say WHICH rows, or the
// operator is told their input is ambiguous and handed nothing to disambiguate
// with. Since #700 the bind resolves inside the caller's own location:read
// scope, so every candidate is a row that caller already may read.
//
// The narrow principal runs first and the owner runs the identical body second:
// the same reference is ambiguous between TWO rooms for the one and THREE for
// the other, off the same fleet, which is what makes the filtering a property of
// the caller's scope rather than of the fixture.
func TestAnAmbiguousPlacementNamesOnlyTheCandidatesTheCallerCanRead(t *testing.T) {
	f := newPlacementFixture(t)

	// Two floors inside wing-a, so the two rooms named "1" sit in different
	// placement buckets and the partial-unique index legalizes both.
	f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{
		"name": "east", "location_type": "floor", "parent": "wing-a",
	}, http.StatusCreated)
	f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{
		"name": "west", "location_type": "floor", "parent": "wing-a",
	}, http.StatusCreated)
	eastOne := createdID(t, f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{
		"name": "1", "location_type": "room", "parent": "east",
	}, http.StatusCreated))
	westOne := createdID(t, f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{
		"name": "1", "location_type": "room", "parent": "west",
	}, http.StatusCreated))
	// The third "1", in the wing this principal cannot read at all.
	hiddenOne := createdID(t, f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{
		"name": "1", "location_type": "room", "parent": "wing-b",
	}, http.StatusCreated))

	create := map[string]any{"name": "panel-1", "product": f.product.Name, "parent": f.rackID, "location": "1"}
	status, body := f.c.send(f.narrow, http.MethodPost, "/components", create)
	if status != http.StatusConflict {
		t.Fatalf("create against an in-scope-ambiguous location = %d, want 409\nbody: %s", status, body)
	}
	for _, id := range []string{eastOne, westOne} {
		if !strings.Contains(string(body), id) {
			t.Errorf("409 body = %s, want it to name the in-scope candidate %s the operator has to choose between", body, id)
		}
	}
	if strings.Contains(string(body), hiddenOne) {
		t.Fatalf("409 body = %s, leaks the out-of-scope room's uuid %s", body, hiddenOne)
	}

	// A uuid from the list is a spelling that resolves, so the answer is
	// actionable and not merely descriptive.
	f.c.do(f.narrow, http.MethodPost, "/components", map[string]any{
		"name": "panel-1", "product": f.product.Name, "parent": f.rackID, "location": eastOne,
	}, http.StatusCreated)

	// So is the dotted address, which this reference has accepted since #627
	// Task 12 (loadByRef parses one before it ever falls through to a name), so
	// the issue's second question, whether these binds need one, is already
	// answered by the code. It is absolute, rooted at a root location, which is
	// exactly the string the location's own read hands back as its path.
	f.c.do(f.narrow, http.MethodPost, "/components", map[string]any{
		"name": "panel-2", "product": f.product.Name, "parent": f.rackID, "location": "hq.wing-a.west.1",
	}, http.StatusCreated)

	// The owner, whose scope holds all three, is refused with all three named:
	// the two-row list above is this caller's scope, not the fleet's shape.
	status, body = f.c.send(f.owner, http.MethodPost, "/components", create)
	if status != http.StatusConflict {
		t.Fatalf("owner create against the ambiguous location = %d, want 409\nbody: %s", status, body)
	}
	for _, id := range []string{eastOne, westOne, hiddenOne} {
		if !strings.Contains(string(body), id) {
			t.Errorf("owner 409 body = %s, want it to name candidate %s", body, id)
		}
	}
}
