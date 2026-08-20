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
	Name    string `json:"name"`
	Ordinal int    `json:"ordinal"`
	Label   string `json:"label"`
	Rule    string `json:"rule"`
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
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "room-204b", "location_type": "room", "parent": "hq", "label": "204B"}, http.StatusCreated)

	// A system, whose label the shipped rule renders from its type alone, so
	// this half is an EXACT comparison even though the platform mints the name.
	sysDraft := map[string]any{"system_type_id": "board", "location": "room-204b"}
	drafted := renderLabelAt(t, c, owner, "/systems:renderLabel", sysDraft, http.StatusOK)
	created := c.do(owner, http.MethodPost, "/systems", sysDraft, http.StatusCreated)
	var sys struct {
		Name          string `json:"name"`
		Label         string `json:"label"`
		NameGenerated bool   `json:"name_generated"`
		LabelGen      bool   `json:"label_generated"`
	}
	if err := json.Unmarshal(created, &sys); err != nil {
		t.Fatalf("parse system: %v", err)
	}
	if drafted.Label == "" || drafted.Label != sys.Label {
		t.Errorf("the form would have shown %q; the create stored %q", drafted.Label, sys.Label)
	}
	// The locked pair really is locked: neither field was posted, so both pens
	// came back with the platform.
	if !sys.NameGenerated || !sys.LabelGen {
		t.Errorf("name_generated=%v label_generated=%v, want both true for a body that posted neither", sys.NameGenerated, sys.LabelGen)
	}

	// A component with an operator-typed name, which is the case with no
	// unknown left in it at all: an operator-named row carries no ordinal, so
	// the drafted string and the stored one match character for character.
	compDraft := map[string]any{"product": "samsung-qm55", "location": "room-204b", "name": "front-panel"}
	drafted = renderLabelAt(t, c, owner, "/components:renderLabel", compDraft, http.StatusOK)
	created = c.do(owner, http.MethodPost, "/components", compDraft, http.StatusCreated)
	var comp struct {
		Label    string `json:"label"`
		LabelGen bool   `json:"label_generated"`
	}
	if err := json.Unmarshal(created, &comp); err != nil {
		t.Fatalf("parse component: %v", err)
	}
	if drafted.Label == "" || drafted.Label != comp.Label {
		t.Errorf("the form would have shown %q; the create stored %q", drafted.Label, comp.Label)
	}
	if !comp.LabelGen {
		t.Error("a body posting no label leaves the label pen with the platform")
	}
	if drafted.Rule == "" {
		t.Error("the answer names no rule, so the form cannot say where the label came from")
	}
	// An operator-typed name comes back as the name, with no ordinal beside it:
	// that row carries none, and the form must therefore post no precondition
	// (#702). Absent and zero are the same state here on purpose.
	if drafted.Name != "front-panel" || drafted.Ordinal != 0 {
		t.Errorf("drafted identity for a typed name = %+v, want the name back and no ordinal", drafted)
	}
}

// TestTheDraftedNameIsTheNameTheCreateStampsAPI is #702's acceptance at the wire:
// the form is shown a real name, posts it back as its precondition, and the row
// lands with exactly the name it showed. Everything below this tier can see one
// half or the other; only this can see the round trip a form makes.
func TestTheDraftedNameIsTheNameTheCreateStampsAPI(t *testing.T) {
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
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "room-204b", "location_type": "room", "parent": "hq"}, http.StatusCreated)

	body := map[string]any{"product": "samsung-qm55", "location": "room-204b"}
	drafted := renderLabelAt(t, c, owner, "/components:renderLabel", body, http.StatusOK)
	if drafted.Ordinal != 1 {
		t.Fatalf("first draft in an empty room = %+v, want ordinal 1", drafted)
	}
	// No digit-free shape any more: the form is shown the name, and the name
	// carries the number the create is about to use.
	if !strings.HasSuffix(drafted.Name, "-1") {
		t.Fatalf("drafted name %q, want the real ordinal in it rather than a token", drafted.Name)
	}

	created := c.do(owner, http.MethodPost, "/components",
		withDraftedName(body, drafted.Name), http.StatusCreated)
	var comp struct {
		Name          string `json:"name"`
		NameGenerated bool   `json:"name_generated"`
	}
	if err := json.Unmarshal(created, &comp); err != nil {
		t.Fatalf("parse component: %v", err)
	}
	if comp.Name != drafted.Name {
		t.Errorf("the form showed %q; the row landed %q", drafted.Name, comp.Name)
	}
	// The precondition is not a name: posting it leaves the pen exactly where a
	// body posting neither field leaves it.
	if !comp.NameGenerated {
		t.Error("name_generated=false: posting expected_name must not claim the pen")
	}

	// The same form, submitted twice, is the race: the second submission is
	// holding an ordinal the first one took.
	conflict := c.do(owner, http.MethodPost, "/components",
		withDraftedName(body, drafted.Name), http.StatusConflict)
	var problem struct {
		Detail string `json:"detail"`
		Errors []struct {
			Location string `json:"location"`
			Value    any    `json:"value"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(conflict, &problem); err != nil {
		t.Fatalf("parse the conflict: %v", err)
	}
	// A form has to tell this 409 from a name-collision 409, and it does it by
	// the location rather than by matching the sentence.
	if len(problem.Errors) != 1 || problem.Errors[0].Location != "body.expected_name" {
		t.Fatalf("conflict errors = %+v, want one located on body.expected_name", problem.Errors)
	}
	// The form re-reads, and is shown the name that is now free.
	next := renderLabelAt(t, c, owner, "/components:renderLabel", body, http.StatusOK)
	if next.Ordinal != drafted.Ordinal+1 || next.Name == drafted.Name {
		t.Errorf("re-read after the conflict = %+v, want the next ordinal and a different name", next)
	}
	// The machine-readable value is that same name, so a surface can show what
	// the create would have produced without waiting for the re-read.
	if v, ok := problem.Errors[0].Value.(string); !ok || v != next.Name {
		t.Errorf("conflict value = %v, want the name this create would have landed (%q)", problem.Errors[0].Value, next.Name)
	}
	// And the sentence names what the operator would have got, which is the
	// same name that re-read just returned: the form's recovery is to show it
	// rather than to say "try again".
	if !strings.Contains(problem.Detail, next.Name) {
		t.Errorf("conflict detail %q does not name %q, the name the create would have landed", problem.Detail, next.Name)
	}
	c.do(owner, http.MethodPost, "/components", withDraftedName(body, next.Name), http.StatusCreated)

	// An expectation beside a name the operator typed is refused, since that
	// path allocates nothing for it to be about, even when the two agree.
	c.do(owner, http.MethodPost, "/components",
		map[string]any{"product": "samsung-qm55", "location": "room-204b", "name": "front-panel", "expected_name": "front-panel"},
		http.StatusUnprocessableEntity)
	// And the empty string is not a spelling of "no expectation": the schema
	// refuses it, so a client cannot post an unevaluable precondition by
	// accident.
	c.do(owner, http.MethodPost, "/components", withDraftedName(body, ""), http.StatusUnprocessableEntity)
}

// withDraftedName copies a draft body and adds the precondition, so the test
// posts the SAME object it drafted with plus one field, which is exactly what
// the form does. The field carries the drafted NAME (#702 review): it asserts
// what the row will be called and never sets it, so the created row is still
// name_generated.
func withDraftedName(body map[string]any, name string) map[string]any {
	out := make(map[string]any, len(body)+1)
	for k, v := range body {
		out[k] = v
	}
	out["expected_name"] = name
	return out
}

// TestTheDraftedOrdinalFollowsThePlacementBucketAPI: the ordinal is per bucket,
// and the bucket includes the PARENT, which the draft body did not carry before
// #702. A draft that ignored it would answer 1 under a parent already holding
// one, and the create beside it would then be refused by the very precondition
// meant to protect it.
func TestTheDraftedOrdinalFollowsThePlacementBucketAPI(t *testing.T) {
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
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "room-204b", "location_type": "room", "parent": "hq"}, http.StatusCreated)
	rack := c.do(owner, http.MethodPost, "/components",
		map[string]any{"product": "generic-device", "location": "room-204b", "name": "rack-a"}, http.StatusCreated)
	var parent struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rack, &parent); err != nil {
		t.Fatalf("parse parent: %v", err)
	}
	// One child already under the rack, and none at the room level.
	c.do(owner, http.MethodPost, "/components",
		map[string]any{"product": "samsung-qm55", "parent": parent.Name}, http.StatusCreated)

	underParent := renderLabelAt(t, c, owner, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "parent": parent.Name}, http.StatusOK)
	atRoom := renderLabelAt(t, c, owner, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "location": "room-204b"}, http.StatusOK)
	if underParent.Ordinal != 2 {
		t.Errorf("draft under the parent = %+v, want ordinal 2: the parent's bucket already holds one", underParent)
	}
	if atRoom.Ordinal != 1 {
		t.Errorf("draft at the room = %+v, want ordinal 1: a parent's bucket is not the room's", atRoom)
	}
	// And the create under the parent takes the number the draft showed, which
	// is what would fail if the draft had read the wrong bucket.
	created := c.do(owner, http.MethodPost, "/components",
		map[string]any{"product": "samsung-qm55", "parent": parent.Name, "expected_name": underParent.Name}, http.StatusCreated)
	var child struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(created, &child); err != nil {
		t.Fatalf("parse child: %v", err)
	}
	if child.Name != underParent.Name {
		t.Errorf("the form showed %q under the parent; the row landed %q", underParent.Name, child.Name)
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
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "wing-a", "location_type": "building", "parent": "hq", "label": "Wing A"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "wing-b", "location_type": "building", "parent": "hq", "label": "Secret Wing"}, http.StatusCreated)

	wingA := entityID(t, c, owner, "/locations", "wing-a")
	// A rack for the narrow principal to draft UNDER. Every body below names it,
	// so the axis this test isolates is the location read scope alone: the
	// parentless bucket is gated on an all-scoped create grant (#702 review), and
	// a body that omitted the parent would be refused for that instead, which
	// would pass this test while proving nothing about the location.
	rack := createdID(t, c.do(owner, http.MethodPost, "/components", map[string]any{
		"name": "rack", "product": "generic-device", "location": "wing-a",
	}, http.StatusCreated))
	narrow := principalWithGrants(t, ctx, dsn, "wing-a-operator", []grant{
		{role: "operator", scopeKind: "location", scopeID: wingA},
		{role: "operator", scopeKind: "component", scopeID: rack},
	})

	// Inside its own wing the narrow principal gets a real answer, carrying the
	// label of the location it may read.
	got := renderLabelAt(t, c, narrow, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "name": "panel", "parent": rack, "location": "wing-a"}, http.StatusOK)
	if got.Label == "" {
		t.Fatal("the in-scope draft rendered nothing, so the refusal below proves nothing")
	}
	if want := "Wing A"; got.Label[:len(want)] != want {
		t.Errorf("in-scope draft %q does not open with the location's label %q", got.Label, want)
	}

	// The sibling wing is refused. The owner is shown the same request
	// succeeding, so this is a scope refusal and not a broken route.
	c.do(narrow, http.MethodPost, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "name": "panel", "parent": rack, "location": "wing-b"}, http.StatusUnprocessableEntity)
	leak := renderLabelAt(t, c, owner, "/components:renderLabel",
		map[string]any{"product": "samsung-qm55", "name": "panel", "parent": rack, "location": "wing-b"}, http.StatusOK)
	if leak.Label == "" || leak.Label[:len("Secret Wing")] != "Secret Wing" {
		t.Errorf("owner draft %q does not carry the sibling wing's label, so the refusal above is not a scope refusal", leak.Label)
	}
}

// TestTheRenderedLabelNeedsTheCreatePermission pins the gate itself. A viewer
// holds read everywhere and create nowhere, so it can already see every label
// in the fleet; what it must not have is the create form's own route, because
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

	// An unclassified system, and a room: the two states a shipped fleet can
	// reach through the pickers. EVERY shipped location type is in the second
	// state now (ADR-0103), a floor included, so the location arm of this route
	// answers 422 for the whole shipped vocabulary.
	c.do(owner, http.MethodPost, "/systems:renderLabel", map[string]any{}, http.StatusUnprocessableEntity)
	for _, shipped := range []string{"campus", "building", "floor", "room"} {
		c.do(owner, http.MethodPost, "/locations:renderLabel", map[string]any{"location_type": shipped}, http.StatusUnprocessableEntity)
	}

	// And the same two with a name supplied render fine: the refusal is about
	// generating a NAME, never about rendering a label.
	c.do(owner, http.MethodPost, "/systems:renderLabel", map[string]any{"name": "one-off"}, http.StatusOK)
	c.do(owner, http.MethodPost, "/locations:renderLabel", map[string]any{"location_type": "room", "name": "boardroom"}, http.StatusOK)

	// A type an OPERATOR made positional still drafts with the name omitted, so
	// the route keeps the arm the shipped vocabulary no longer reaches: its
	// drafted name is the ordinal token alone.
	c.do(owner, http.MethodPost, "/location-types", map[string]any{
		"name": "deck", "label": "Deck", "name_rule": map[string]any{"stem": ""},
	}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations:renderLabel", map[string]any{"location_type": "deck"}, http.StatusOK)
}

// TestTheRenderedLocationLabelIsTheShippedRulesInAShippedFleet documents the
// state every location create form opens in, over the wire and end to end. It
// used to be the empty one; #657 ships a location rule, so the form now shows
// the label the create is about to store, and the two are asserted to be the
// same string rather than assumed to be.
func TestTheRenderedLocationLabelIsTheShippedRulesInAShippedFleet(t *testing.T) {
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
		Label string `json:"label"`
	}
	if err := json.Unmarshal(created, &loc); err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if loc.Label != got.Label {
		t.Errorf("the form would have shown %q; the create stored %q", got.Label, loc.Label)
	}
}

// TestTheDraftRefusesTheRootBucketItCannotCreateIn is the review's repro at the
// wire (#702 review): the draft answered 200 for the ROOT bucket while the
// create beside it answered 403.
//
// It is a disclosure and not only an inconsistency. The answer carries the
// lowest free ordinal, read from the bucket's sibling NAMES, so it reports which
// names in the fleet root are taken. The probe is chosen rather than fixed: a
// forked location type's name rule (ordinary since #703, and reachable with
// location_type:create alone) supplies whatever stem the caller wants asked
// about.
func TestTheDraftRefusesTheRootBucketItCannotCreateIn(t *testing.T) {
	f := newLocationGrantFixture(t)

	// A generating type, declared the way an operator reaches one today: no
	// shipped location type carries a name rule (ADR-0103).
	f.c.do(f.owner, http.MethodPost, "/location-types", map[string]any{
		"name": "region", "label": "Region", "allowed_parent_types": []string{},
		"name_rule": map[string]any{"stem": "secret-region"},
	}, http.StatusCreated)
	// A root location occupying the first name that rule mints, so the probe has
	// a real fact to report rather than answering 1 into an empty bucket. Whether
	// "secret-region-1" is taken is exactly what the drafted ordinal discloses.
	f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{
		"name": "secret-region-1", "location_type": "region",
	}, http.StatusCreated)

	// The root bucket: refused, and refused with the SAME status the create
	// gives, which is the property that makes a form's preview honest.
	root := map[string]any{"location_type": "region"}
	f.c.do(f.techEast, http.MethodPost, "/locations:renderLabel", root, http.StatusForbidden)
	f.c.do(f.techEast, http.MethodPost, "/locations", root, http.StatusForbidden)

	// Inside its own subtree the same principal drafts and creates normally, so
	// what is gated is the bucket rather than the route.
	inside := map[string]any{"location_type": "room", "parent": "annex", "name": "studio-a"}
	f.c.do(f.techEast, http.MethodPost, "/locations:renderLabel", inside, http.StatusOK)
	f.c.do(f.techEast, http.MethodPost, "/locations", inside, http.StatusCreated)

	// The owner asks the refused question and is answered, with the ordinal that
	// says the root already holds one of these: the refusal above is a scope
	// boundary, and this is the fact it was handing out.
	got := renderLabelAt(t, f.c, f.owner, "/locations:renderLabel", root, http.StatusOK)
	if got.Ordinal != 2 || got.Name != "secret-region-2" {
		t.Errorf("owner root draft = %+v, want ordinal 2 / secret-region-2: the fixture is not carrying the fact the refusal protects", got)
	}
}

// TestThePreconditionBindsTheNameTheFormShowed is the review's repro of what the
// ordinal-only precondition could not see (#702 review).
//
// The precondition exists so a create lands the name the form displayed, or is
// refused. The ordinal is not that claim: the NAME is the stem, the suppression
// rule and the number together, and a claim on the number alone passes unchanged
// while the other two move. Forking a type and rewriting what it mints is
// ordinary since #703, so this is not a contrived race, and the console was
// shielded only by an accident (the stem is not in the draft body, so it is not
// in the query key that would have re-asked); a CLI or API caller had no shield
// at all.
func TestThePreconditionBindsTheNameTheFormShowed(t *testing.T) {
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
	body := map[string]any{"product": "samsung-qm55", "location": "hq"}
	drafted := renderLabelAt(t, c, owner, "/components:renderLabel", body, http.StatusOK)
	if drafted.Name != "display-1" || drafted.Ordinal != 1 {
		t.Fatalf("draft = %+v, want display-1 at ordinal 1", drafted)
	}

	// The stem moves under the open form. Nothing about the placement bucket
	// changes, so the ordinal the form is holding is still the one this create
	// allocates: the claim is met and the name is not.
	c.do(owner, http.MethodPatch, "/component-types/display", map[string]any{"stem": "monitor"}, http.StatusOK)

	status, raw := c.send(owner, http.MethodPost, "/components", withDraftedName(body, drafted.Name))
	if status != http.StatusConflict {
		t.Fatalf("create after the stem moved = %d, want 409: the form showed %q\nbody: %s", status, drafted.Name, raw)
	}
	if !strings.Contains(string(raw), "monitor-1") || !strings.Contains(string(raw), drafted.Name) {
		t.Errorf("409 body = %s, want it to name both what the form showed and what the create would produce", raw)
	}
	// Nothing was created, so re-drafting is the whole recovery.
	c.do(owner, http.MethodGet, "/components/monitor-1", nil, http.StatusNotFound)

	// Re-drafted, the form is shown the new name and the create takes it.
	redrafted := renderLabelAt(t, c, owner, "/components:renderLabel", body, http.StatusOK)
	if redrafted.Name != "monitor-1" {
		t.Fatalf("re-draft = %+v, want monitor-1", redrafted)
	}
	created := c.do(owner, http.MethodPost, "/components", withDraftedName(body, redrafted.Name), http.StatusCreated)
	var row struct {
		Name          string `json:"name"`
		NameGenerated bool   `json:"name_generated"`
	}
	if err := json.Unmarshal(created, &row); err != nil {
		t.Fatalf("parse created: %v", err)
	}
	if row.Name != "monitor-1" {
		t.Errorf("created row = %q, want monitor-1", row.Name)
	}
	// The precondition is not the name field: posting it must not claim the pen.
	if !row.NameGenerated {
		t.Errorf("created row is not name_generated: the precondition took the pen instead of checking it")
	}
}
