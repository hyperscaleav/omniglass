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

// :renderLabel driven through the live HTTP stack (#699): the route a create
// form calls so it can show the label the platform is about to write, in a
// locked field, before the row exists.
//
// Two things are asserted here that no lower tier can see. The rendered label
// is compared with the label the CREATE then stores, over the same wire, so a
// drift between the draft's placement resolve and the stamp's would fail here
// rather than shipping as a form that promises one label and writes another.
// And the disclosure gate is driven by a NARROW-SCOPED principal, not by an
// owner: three scope defects in this epic survived review because every fixture
// held an all scope, most recently the preview-scope bug this branch fixed.

type draftLabelBody struct {
	Label string `json:"label"`
	Rule  string `json:"rule"`
}

func renderLabelAt(t *testing.T, c *apiClient, tok, path string, body map[string]any, status int) draftLabelBody {
	t.Helper()
	raw := c.do(tok, http.MethodPost, path, body, status)
	var out draftLabelBody
	if status == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
	}
	return out
}

// TestTheRenderedLabelIsTheLabelTheCreateStoresAPI is the acceptance, end to
// end through the API both times: draft, create with the identical body, and
// compare the string the form would have shown with the column the row came
// back carrying.
func TestTheRenderedLabelIsTheLabelTheCreateStoresAPI(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "building"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "room-204b", "location_type": "room", "parent": "hq", "display_name": "204B"}, http.StatusCreated)

	// A system, whose label the shipped rule renders from its type alone, so
	// this half is an EXACT comparison even though the platform mints the name.
	sysDraft := map[string]any{"system_type_id": "board", "location": "room-204b"}
	drafted := renderLabelAt(t, c, owner, "/systems:renderLabel", sysDraft, http.StatusOK)
	created := c.do(owner, http.MethodPost, "/systems", sysDraft, http.StatusCreated)
	var sys struct {
		Name          string `json:"name"`
		DisplayName   string `json:"display_name"`
		NameGenerated bool   `json:"name_generated"`
		LabelGen      bool   `json:"display_name_generated"`
	}
	if err := json.Unmarshal(created, &sys); err != nil {
		t.Fatalf("parse system: %v", err)
	}
	if drafted.Label == "" || drafted.Label != sys.DisplayName {
		t.Errorf("the form would have shown %q; the create stored %q", drafted.Label, sys.DisplayName)
	}
	// The locked pair really is locked: neither field was posted, so both pens
	// came back with the platform.
	if !sys.NameGenerated || !sys.LabelGen {
		t.Errorf("name_generated=%v display_name_generated=%v, want both true for a body that posted neither", sys.NameGenerated, sys.LabelGen)
	}

	// A component with an operator-typed name, which is the case with no
	// unknown left in it at all: an operator-named row carries no ordinal, so
	// the drafted string and the stored one match character for character.
	compDraft := map[string]any{"product": "samsung-qm55", "location": "room-204b", "name": "front-panel"}
	drafted = renderLabelAt(t, c, owner, "/components:renderLabel", compDraft, http.StatusOK)
	created = c.do(owner, http.MethodPost, "/components", compDraft, http.StatusCreated)
	var comp struct {
		DisplayName string `json:"display_name"`
		LabelGen    bool   `json:"display_name_generated"`
	}
	if err := json.Unmarshal(created, &comp); err != nil {
		t.Fatalf("parse component: %v", err)
	}
	if drafted.Label == "" || drafted.Label != comp.DisplayName {
		t.Errorf("the form would have shown %q; the create stored %q", drafted.Label, comp.DisplayName)
	}
	if !comp.LabelGen {
		t.Error("a body posting no display_name leaves the label pen with the platform")
	}
	if drafted.Rule == "" {
		t.Error("the answer names no rule, so the form cannot say where the label came from")
	}
}

// TestTheRenderedLabelStopsAtTheCallersReadScope is the disclosure gate, driven
// by a principal scoped to ONE wing.
//
// The rendered string is assembled from a location's label, so a caller who can
// create components but cannot read a location must not be able to read that
// location's label back out of the create form. The owner is asserted alongside
// only to prove the fixture would have disclosed it: an assertion that a narrow
// principal sees nothing passes just as well when the route is broken.
func TestTheRenderedLabelStopsAtTheCallersReadScope(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	// A rule that reads the placement, so the leak this gate exists to close is
	// actually present in the answer.
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "wing-a", "location_type": "building", "parent": "hq", "display_name": "Wing A"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "wing-b", "location_type": "building", "parent": "hq", "display_name": "Secret Wing"}, http.StatusCreated)

	wingA := entityID(t, c, owner, "/locations", "wing-a")
	narrow := principalWithGrants(t, ctx, dsn, "wing-a-operator",
		[]grant{{role: "operator", scopeKind: "location", scopeID: wingA}})

	// Inside its own wing the narrow principal gets a real answer, carrying the
	// label of the location it may read.
	got := renderLabelAt(t, c, narrow, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "name": "panel", "location": "wing-a"}, http.StatusOK)
	if got.Label == "" {
		t.Fatal("the in-scope draft rendered nothing, so the refusal below proves nothing")
	}
	if want := "Wing A"; got.Label[:len(want)] != want {
		t.Errorf("in-scope draft %q does not open with the location's label %q", got.Label, want)
	}

	// The sibling wing is refused. The owner is shown the same request
	// succeeding, so this is a scope refusal and not a broken route.
	c.do(narrow, http.MethodPost, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "name": "panel", "location": "wing-b"}, http.StatusUnprocessableEntity)
	leak := renderLabelAt(t, c, owner, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "name": "panel", "location": "wing-b"}, http.StatusOK)
	if leak.Label == "" || leak.Label[:len("Secret Wing")] != "Secret Wing" {
		t.Errorf("owner draft %q does not carry the sibling wing's label, so the refusal above is not a scope refusal", leak.Label)
	}
}

// TestTheRenderedLabelNeedsTheCreatePermission pins the gate itself. A viewer
// holds read everywhere and create nowhere, so it can already see every label
// in the estate; what it must not have is the create form's own route, because
// the gate has to be one fact the spec publishes rather than a judgement call
// per route.
func TestTheRenderedLabelNeedsTheCreatePermission(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	viewer := principalWithGrants(t, ctx, dsn, "viewer-all", []grant{{role: "viewer", scopeKind: "all"}})
	c.do(viewer, http.MethodPost, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "name": "panel"}, http.StatusForbidden)
	c.do(viewer, http.MethodPost, "/systems:renderLabel",
		map[string]any{"system_type_id": "board", "name": "board-x"}, http.StatusForbidden)
	c.do(viewer, http.MethodPost, "/locations:renderLabel",
		map[string]any{"location_type": "room", "name": "boardroom"}, http.StatusForbidden)
}

// TestTheRenderedLabelRefusesWhatANamelessCreateRefuses: the form asks with the
// name omitted whenever the field is locked, so the route has to refuse exactly
// where the create would, or the console shows a lock over a value that cannot
// be produced.
func TestTheRenderedLabelRefusesWhatANamelessCreateRefuses(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	// An unclassified system, and a room: the two states a shipped estate can
	// reach through the pickers.
	c.do(owner, http.MethodPost, "/systems:renderLabel", map[string]any{}, http.StatusUnprocessableEntity)
	c.do(owner, http.MethodPost, "/locations:renderLabel", map[string]any{"location_type": "room"}, http.StatusUnprocessableEntity)

	// And the same two with a name supplied render fine: the refusal is about
	// generating a NAME, never about rendering a label.
	c.do(owner, http.MethodPost, "/systems:renderLabel", map[string]any{"name": "one-off"}, http.StatusOK)
	c.do(owner, http.MethodPost, "/locations:renderLabel", map[string]any{"location_type": "room", "name": "boardroom"}, http.StatusOK)

	// The floor is the one shipped location_type that generates, and it
	// generates a POSITIONAL name, so its drafted name is the token alone.
	c.do(owner, http.MethodPost, "/locations:renderLabel", map[string]any{"location_type": "floor"}, http.StatusOK)
}

// TestTheRenderedLocationLabelIsTheShippedRulesInAShippedEstate documents the
// state every location create form opens in, over the wire and end to end. It
// used to be the empty one; #657 ships a location rule, so the form now shows
// the label the create is about to store, and the two are asserted to be the
// same string rather than assumed to be.
func TestTheRenderedLocationLabelIsTheShippedRulesInAShippedEstate(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	got := renderLabelAt(t, c, owner, "/locations:renderLabel",
		map[string]any{"location_type": "building", "name": "north-wing"}, http.StatusOK)
	if got.Label != "North Wing" || got.Rule != "{{title (words .Name)}}" {
		t.Errorf("shipped location draft = %+v, want the shipped rule and %q", got, "North Wing")
	}
	created := c.do(owner, http.MethodPost, "/locations",
		map[string]any{"location_type": "building", "name": "north-wing"}, http.StatusCreated)
	var loc struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(created, &loc); err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if loc.DisplayName != got.Label {
		t.Errorf("the form would have shown %q; the create stored %q", got.Label, loc.DisplayName)
	}
}
