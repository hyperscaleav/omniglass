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

// placementFixture is the estate every test below shares: one campus, two wings
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
	owner    string
	narrow   string
	rackID   string
	wingBSys string
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
		"name": "wing-a", "location_type": "building", "parent": "hq", "display_name": "Wing A",
	}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{
		"name": "wing-b", "location_type": "building", "parent": "hq", "display_name": "Secret Wing",
	}, http.StatusCreated)

	// A root component and a root system both need an all-scoped grant, so the
	// owner builds the two rows the narrow principal then writes under. The rack
	// sits in wing-a, which is what puts it inside that principal's own scope.
	rack := c.do(owner, http.MethodPost, "/components", map[string]any{
		"name": "rack", "product": "samsung-qm55", "location": "wing-a",
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

	narrow := principalWithGrants(t, ctx, dsn, "wing-a-deploy", []grant{
		{role: "deploy", scopeKind: "component", scopeID: rackBody.ID},
		{role: "deploy", scopeKind: "system", scopeID: entityID(t, c, owner, "/systems", "av-a")},
		{role: "deploy", scopeKind: "location", scopeID: entityID(t, c, owner, "/locations", "wing-a")},
	})
	return &placementFixture{c: c, owner: owner, narrow: narrow, rackID: rackBody.ID, wingBSys: "av-b"}
}

// label reads display_name off a create or move response.
func label(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("parse entity: %v", err)
	}
	return body.DisplayName
}

// TestAComponentCreateRefusesAPlacementTheCallerCannotRead is the defect this
// issue names, on both of a component's placement references.
func TestAComponentCreateRefusesAPlacementTheCallerCannotRead(t *testing.T) {
	f := newPlacementFixture(t)

	// Inside its own wing the narrow principal creates normally, and the answer
	// carries the label of the location it may read.
	got := f.c.do(f.narrow, http.MethodPost, "/components", map[string]any{
		"name": "panel-a", "product": "samsung-qm55", "parent": f.rackID, "location": "wing-a",
	}, http.StatusCreated)
	if !strings.HasPrefix(label(t, got), "Wing A") {
		t.Fatalf("in-scope create stored %q, want it to open with the location's label", label(t, got))
	}

	// The sibling wing is refused, on the same body the draft route already
	// refuses, and with the same status: the preview and the create it previews
	// have to agree or an operator is refused a draft and then creates anyway.
	leakBody := map[string]any{
		"name": "panel-b", "product": "samsung-qm55", "parent": f.rackID, "location": "wing-b",
	}
	f.c.do(f.narrow, http.MethodPost, "/components:renderLabel", map[string]any{
		"name": "panel-b", "product": "samsung-qm55", "location": "wing-b",
	}, http.StatusUnprocessableEntity)
	f.c.do(f.narrow, http.MethodPost, "/components", leakBody, http.StatusUnprocessableEntity)

	// The system reference is the component map's other placement fact, and it
	// leaks the same way: av-b sits in the wing this principal cannot read.
	f.c.do(f.narrow, http.MethodPost, "/components", map[string]any{
		"name": "panel-c", "product": "samsung-qm55", "parent": f.rackID, "system": f.wingBSys,
	}, http.StatusUnprocessableEntity)

	// The owner runs the identical bodies and is served, so both refusals above
	// are a scope boundary and not a broken route.
	owned := f.c.do(f.owner, http.MethodPost, "/components", leakBody, http.StatusCreated)
	if !strings.HasPrefix(label(t, owned), "Secret Wing") {
		t.Errorf("owner create stored %q, want the sibling wing's label: the refusals above are not scope refusals", label(t, owned))
	}
	owned = f.c.do(f.owner, http.MethodPost, "/components", map[string]any{
		"name": "panel-c", "product": "samsung-qm55", "parent": f.rackID, "system": f.wingBSys,
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
		"name": "sub-b", "system_type_id": "board", "location": "wing-b",
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
		"name": "panel-a", "product": "samsung-qm55", "parent": f.rackID, "location": "wing-a",
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
