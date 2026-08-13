package storage_test

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The draft identity render (#699, #702): what a create form can be told about
// the name and the label a row will carry, BEFORE the row exists.
//
// ADR-0104 refused a draft preview that MINTS, on two grounds: the answer is
// provisional (another create can take the ordinal between the preview and the
// commit) and a rolled-back mint takes the same bucket advisory lock real
// creates need, so previewing per picker change would serialise the estate's
// creates. Neither objection reaches a render that ALLOCATES NOTHING, which is
// what this is: the same tier resolution, the same closed data map, the same one
// engine, and the lowest free ordinal READ from the bucket's siblings rather
// than minted. The provisional half is answered by the create's precondition
// (name_precondition_test.go), not by hiding the number.
//
// So the properties this file holds are, in order of what would hurt most if
// they broke:
//
//  1. the drafted name and label are the ones the gateway then STAMPS (the
//     whole claim, and exact on both fields since #702),
//  2. it takes no lock, opens no write transaction and allocates nothing,
//  3. it refuses a placement the caller cannot read, and
//  4. it refuses exactly what a nameless create would refuse, with the same
//     sentinel, so the form never shows a lock over a value the create declines
//     to produce.

// TestTheDraftLabelIsTheLabelTheCreateStores is the acceptance, and it is
// asserted at the tier that can see both answers: render the draft, create the
// row from the SAME inputs, and compare the rendered string with the column.
//
// The operator-named case is the exact-equality one and is therefore the
// primary assertion: an operator-typed name carries no ordinal (the column is
// null by design, #681), so the render has no unknown in it at all and the two
// strings must match character for character.
func TestTheDraftLabelIsTheLabelTheCreateStores(t *testing.T) {
	gw, ctx := seededGateway(t)
	// The epic's worked example, so both placement facts are exercised: the
	// draft resolves the location's label and the system's TYPE label from the
	// refs the form holds, and the create resolves them from the row and the
	// membership it just wrote. A fixture binding only a location would leave
	// the system half of the draft's placement resolve unproven.
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.SystemTypeLabel}} {{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-1", SystemTypeID: strptr("board"), LocationName: &room.Name,
	}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName:  qm55,
		Name:         "front-panel",
		LocationName: room.Name,
		SystemName:   "board-1",
	}, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "front-panel", ProductName: strptr(qm55), LocationName: &room.Name, SystemName: strptr("board-1"),
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if drafted.Label != c.DisplayName {
		t.Errorf("drafted label %q, stored %q: the form promised a label the create did not write", drafted.Label, c.DisplayName)
	}
	if drafted.Label == "" {
		t.Fatal("drafted label is empty, so the comparison above proves nothing")
	}
}

// TestTheDraftIsTheIdentityTheCreateStampsForAGeneratedName is the same
// acceptance where the platform owns the name too, and it is now an EXACT
// comparison on both fields (#702). It used to substitute the create's ordinal
// back into the drafted string, because the draft wrote a token where the number
// went; the number is read from the bucket before the label is rendered, so
// there is nothing left in either value for the create to fill in.
func TestTheDraftIsTheIdentityTheCreateStampsForAGeneratedName(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if c.Ordinal == nil {
		t.Fatal("a generated name stores the ordinal it was minted from")
	}
	if drafted.Name != c.Name {
		t.Errorf("drafted name %q, created %q: the form promised a name the create did not use", drafted.Name, c.Name)
	}
	if drafted.Ordinal != *c.Ordinal {
		t.Errorf("drafted ordinal %d, allocated %d", drafted.Ordinal, *c.Ordinal)
	}
	if drafted.Label != c.DisplayName {
		t.Errorf("drafted label %q, stored %q", drafted.Label, c.DisplayName)
	}
	// The guard: the fixture's rule reads .Ordinal, so a draft that had left the
	// number out entirely would still pass the equality above if the create had
	// too. The number has to be IN both.
	if !strings.Contains(drafted.Label, strconv.Itoa(*c.Ordinal)) {
		t.Errorf("drafted label %q carries no ordinal, so the comparisons above prove less than they look", drafted.Label)
	}
}

// stemOf resolves the stem a product's component_type chain yields, through the
// gateway's own walk rather than a fixture constant, so a test comparing a
// drafted name against the mint is comparing against the rule.
func stemOf(t *testing.T, gw *storage.PG, ctx context.Context, product string) string {
	t.Helper()
	pr, err := gw.GetProduct(ctx, product)
	if err != nil {
		t.Fatalf("get product %s: %v", product, err)
	}
	stem, err := gw.ExportStemForProduct(ctx, pr.ID)
	if err != nil {
		t.Fatalf("resolve stem for %s: %v", product, err)
	}
	return stem
}

// TestTheDraftedOrdinalIsTheLowestFreeOneInThatBucket is the number itself, and
// the two things about it that are easy to get wrong: it MOVES as the bucket
// fills, and it is per bucket rather than per estate. A draft that read the
// wrong bucket would still show a plausible number, which is why this asserts
// two buckets at once rather than one twice.
func TestTheDraftedOrdinalIsTheLowestFreeOneInThatBucket(t *testing.T) {
	gw, ctx := seededGateway(t)
	here := makeRoomWithLabel(t, gw, ctx, "room-here", "Here")
	there := makeRoomWithLabel(t, gw, ctx, "room-there", "There")

	draft := func(room string) storage.DraftLabel {
		t.Helper()
		d, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
			ProductName: qm55, LocationName: room,
		}, all, all, all)
		if err != nil {
			t.Fatalf("render draft at %s: %v", room, err)
		}
		return d
	}
	first := draft(here.Name)
	if first.Ordinal != 1 {
		t.Fatalf("first draft ordinal %d, want 1 in an empty bucket", first.Ordinal)
	}
	for i := range 2 {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
			ProductName: strptr(qm55), LocationName: &here.Name,
		}, all, all, all, all); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if got := draft(here.Name); got.Ordinal != 3 || got.Name != storage.ExportMintName(stemOf(t, gw, ctx, qm55), false, 3) {
		t.Errorf("draft after two creates = %+v, want ordinal 3 and the name the mint gives it", got)
	}
	// The other bucket is untouched by them, which is the whole reason the
	// draft has to resolve the placement before it reads the number.
	if got := draft(there.Name); got.Ordinal != 1 {
		t.Errorf("draft in the other room = %+v, want ordinal 1: the bucket is the placement, not the estate", got)
	}
}

// TestTheDraftedNameIsWhatTheAllocatorWouldMint pins the previewed name against
// the ALLOCATOR's own answer for the same bucket rather than against a restated
// format string. previewName and generateName share siblingNames and
// pickOrdinal, so the only way they can disagree is if one of them is handed a
// different mint or a different bucket, which is exactly what this catches.
func TestTheDraftedNameIsWhatTheAllocatorWouldMint(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name,
	}, all, all, all, all); err != nil {
		t.Fatalf("seed the bucket: %v", err)
	}
	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	name, ordinal, err := gw.ExportGenerateName(ctx, stemOf(t, gw, ctx, qm55), nil, &room.ID, nil)
	if err != nil {
		t.Fatalf("run the allocator: %v", err)
	}
	if drafted.Name != name || drafted.Ordinal != ordinal {
		t.Errorf("drafted %q/%d, the allocator would mint %q/%d", drafted.Name, drafted.Ordinal, name, ordinal)
	}
	if ordinal != 2 {
		t.Errorf("the allocator returned ordinal %d in a bucket holding one sibling, want 2: this fixture is not exercising the case", ordinal)
	}
}

// TestTheDraftSystemLabelCarriesTheOrdinalItWillLandWith is the generated-name
// case on the system tier, and since #693 there is a number in it: the shipped
// rule is "{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}", so a form drafting
// the SECOND boardroom in a room has to show "Boardroom 2" and not the
// "Boardroom" the first one gets.
//
// Both halves are asserted against the string as well as against each other,
// because a draft and a create that were both wrong in the same way would agree
// with each other perfectly. That is what this test asserted before, and it is
// why the rule's own change could not have failed it.
func TestTheDraftSystemLabelCarriesTheOrdinalItWillLandWith(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	for _, want := range []string{"Boardroom", "Boardroom 2"} {
		drafted, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
			SystemTypeRef: "board", LocationName: room.Name,
		}, all, all)
		if err != nil {
			t.Fatalf("render draft: %v", err)
		}
		s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
			SystemTypeID: strptr("board"), LocationName: &room.Name,
		}, all, all)
		if err != nil {
			t.Fatalf("create system: %v", err)
		}
		if drafted.Label != s.DisplayName || drafted.Label == "" {
			t.Errorf("drafted %q, stored %q", drafted.Label, s.DisplayName)
		}
		if drafted.Label != want {
			t.Errorf("drafted %q, want %q: the form must show the number the row lands with", drafted.Label, want)
		}
		if !s.NameGenerated {
			t.Error("the create was expected to mint the name, so this is the generated case")
		}
	}
}

// TestTheDraftedLocationLabelIsTheOneTheCreateStores is the location tier, and
// it is not an edge case: the shipped GLOBAL location rule
// (internal/seed/label_rules.yaml) is what EVERY location a shipped estate
// creates renders through, so this is the form's ordinary answer rather than a
// corner of it. The form shows it locked, and the create beside it must store
// the same string or the locked field was a lie.
func TestTheDraftedLocationLabelIsTheOneTheCreateStores(t *testing.T) {
	gw, ctx := seededGateway(t)
	drafted, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
		LocationTypeRef: "room", Name: "north-boardroom",
	}, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	if drafted.Label != "North Boardroom" || drafted.Rule != "{{title (words .Name)}}" {
		t.Errorf("drafted %+v, want the shipped location rule and the label it renders", drafted)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "building"}, all); err != nil {
		t.Fatalf("create building: %v", err)
	}
	hq := "hq"
	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "north-boardroom", LocationType: "room", ParentName: &hq,
	}, all)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if l.DisplayName != drafted.Label {
		t.Errorf("the form would have shown %q; the create stored %q", drafted.Label, l.DisplayName)
	}
}

// The empty answer is still reachable and still means what it meant: no rule
// resolves at any tier, so nothing is stored and the surface reads the name.
// It stopped being a shipped estate's default state when the location rule
// landed (#657), which is precisely why it is worth its own test now: an
// operator who clears the rule at every tier gets an empty label rather than a
// refusal, and the form has to have words for that.
func TestTheDraftLabelIsEmptyWhereNoRuleResolves(t *testing.T) {
	gw, ctx := seededGateway(t)
	if err := gw.UpsertLabelRuleDefault(ctx, "location", ""); err != nil {
		t.Fatalf("blank the shipped location rule: %v", err)
	}
	drafted, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
		LocationTypeRef: "room", Name: "boardroom",
	}, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	if drafted.Label != "" || drafted.Rule != "" {
		t.Errorf("drafted %+v, want an empty label and an empty rule: no rule resolves at any tier", drafted)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "building"}, all); err != nil {
		t.Fatalf("create building: %v", err)
	}
	hq := "hq"
	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "boardroom", LocationType: "room", ParentName: &hq,
	}, all)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if l.DisplayName != "" {
		t.Errorf("stored label %q, want none: the surface falls back to the name", l.DisplayName)
	}
}

// TestTheDraftLabelReportsTheRuleItRendered: the resolved rule travels with the
// answer, so the form can say WHERE the label came from rather than presenting
// it as a fact from nowhere. It is also the only way an operator can tell "no
// rule applies" from "the rule rendered nothing for this row".
func TestTheDraftLabelReportsTheRuleItRendered(t *testing.T) {
	gw, ctx := seededGateway(t)
	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, Name: "front-panel",
	}, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	if drafted.Rule != "{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}" {
		t.Errorf("reported rule %q, want the shipped component rule", drafted.Rule)
	}
}

// TestTheDraftLabelRefusesWhatANamelessCreateRefuses: the render asks the
// platform to name the row, so it meets the same refusals a nameless create
// does and returns the SAME sentinels. A form that showed a lock over a value
// the create then declines to produce is worse than one that showed nothing.
func TestTheDraftLabelRefusesWhatANamelessCreateRefuses(t *testing.T) {
	gw, ctx := seededGateway(t)

	// A system with no system_type at all: the stem lives on the registry row.
	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{}, all, all); !errors.Is(err, storage.ErrSystemTypeRequiredForName) {
		t.Errorf("unclassified system draft = %v, want ErrSystemTypeRequiredForName", err)
	}
	// A location_type with no name rule, which is every shipped type (ADR-0103).
	if _, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{LocationTypeRef: "room"}, all); !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
		t.Errorf("nameless room draft = %v, want ErrLocationTypeNoNameRule", err)
	}
	// And the same three, supplied with a name, render fine: the refusal is
	// about GENERATING a name, never about rendering a label.
	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{Name: "one-off"}, all, all); err != nil {
		t.Errorf("named unclassified system draft = %v, want no error", err)
	}
	if _, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{LocationTypeRef: "room", Name: "boardroom"}, all); err != nil {
		t.Errorf("named room draft = %v, want no error", err)
	}
}

// TestTheDraftLabelRefusesAComponentTypeChainWithNoStem is the component tier's
// refusal, on data only the trusted upsert path can produce (CreateComponentType
// requires a root type to carry a stem), which is exactly why it is worth
// holding: it is the case a form cannot reach through the picker and would
// therefore never have been noticed by hand.
func TestTheDraftLabelRefusesAComponentTypeChainWithNoStem(t *testing.T) {
	gw, ctx := seededGateway(t)
	if err := gw.UpsertComponentType(ctx, storage.ComponentType{
		Name: "stemless", DisplayName: "Stemless", Official: true,
	}); err != nil {
		t.Fatalf("upsert stemless type: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "stemless-thing", DisplayName: "Stemless Thing", ComponentType: "stemless",
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	_, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{ProductName: "stemless-thing"}, all, all, all)
	if !errors.Is(err, storage.ErrComponentTypeNoStem) {
		t.Errorf("stemless draft = %v, want ErrComponentTypeNoStem", err)
	}
}

// TestTheDraftLabelRendersWithinTheCallersReadScope is the disclosure gate. The
// rendered string is assembled from a LOCATION's label and a SYSTEM's type
// label, so a caller who cannot read the placement must not be handed one
// through the preview, whatever their grant on the entity being drafted.
//
// Driven by a narrow-scoped principal deliberately: three scope defects in this
// epic survived review because every fixture drove a principal holding all.
func TestTheDraftLabelRendersWithinTheCallersReadScope(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	theirs := makeRoomWithLabel(t, gw, ctx, "room-theirs", "Theirs")

	narrow := scope.Set{IDs: []string{mine.ID}}
	got, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, Name: "panel", LocationName: mine.Name,
	}, all, narrow, narrow)
	if err != nil {
		t.Fatalf("render inside scope: %v", err)
	}
	if !strings.Contains(got.Label, "Mine") {
		t.Errorf("in-scope draft %q does not carry the location's label", got.Label)
	}
	if _, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, Name: "panel", LocationName: theirs.Name,
	}, all, narrow, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("out-of-scope draft = %v, want the non-disclosing ErrLocationNotFound", err)
	}
}

// TestTheDraftSystemLabelRendersWithinTheCallersReadScope is the same gate on
// the system tier, whose map reads its location's label.
func TestTheDraftSystemLabelRendersWithinTheCallersReadScope(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the system rule: %v", err)
	}
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	theirs := makeRoomWithLabel(t, gw, ctx, "room-theirs", "Theirs")
	narrow := scope.Set{IDs: []string{mine.ID}}

	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
		SystemTypeRef: "board", Name: "board-x", LocationName: mine.Name,
	}, all, narrow); err != nil {
		t.Fatalf("render inside scope: %v", err)
	}
	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
		SystemTypeRef: "board", Name: "board-x", LocationName: theirs.Name,
	}, all, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("out-of-scope draft = %v, want the non-disclosing ErrLocationNotFound", err)
	}
}

// TestTheDraftComponentLabelReadsTheSystemWithinScope closes the second half of
// the component leak: SystemTypeLabel comes off a SYSTEM row, and a system the
// caller cannot read must not answer either.
func TestTheDraftComponentLabelReadsTheSystemWithinScope(t *testing.T) {
	gw, ctx := seededGateway(t)
	theirs := makeRoomWithLabel(t, gw, ctx, "room-theirs", "Theirs")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-x", SystemTypeID: strptr("board"), LocationName: &theirs.Name,
	}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	narrow := scope.Set{IDs: []string{mine.ID}}

	if _, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, Name: "panel", SystemName: "board-x",
	}, all, narrow, narrow); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("out-of-scope system draft = %v, want the non-disclosing ErrSystemNotFound", err)
	}
}

// TestTheDraftLabelAllocatesNothing is ADR-0104's distinction, proven with
// #650's counting instrument rather than asserted. Three claims, each read off
// the SQL the render actually issued: no advisory lock, no write transaction,
// and no write.
//
// It carries MORE weight since #702, not less. The render now READS the lowest
// free ordinal, which is the operation ADR-0104 conflated with minting one, so
// "the number is known and nothing was allocated to know it" is the whole claim
// of the slice rather than a side property: the statement scan below is what
// says the read did not become a mint, and the create at the end is what says no
// number was consumed.
//
// The counter is the POOL's (storagetest.NewCountingDB), not a wrapped querier:
// the render reaches for p.pool itself, so a counter handed to it as an
// argument would observe nothing at all and report a flat, fictional zero,
// which is the one failure mode of this instrument that reads as coverage.
func TestTheDraftLabelAllocatesNothing(t *testing.T) {
	ctx := context.Background()
	gw, counter := storagetest.NewCountingDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "building"}, all); err != nil {
		t.Fatalf("create building: %v", err)
	}
	hq := "hq"
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "room-1", LocationType: "room", ParentName: &hq,
	}, all); err != nil {
		t.Fatalf("create room: %v", err)
	}
	counter.Reset()

	for range 5 {
		d, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
			ProductName: qm55, LocationName: "room-1",
		}, all, all, all)
		if err != nil {
			t.Fatalf("render draft: %v", err)
		}
		// Every one of the five answers 1, which is the read half of the same
		// claim: a render that had allocated would hand the next one a
		// different number.
		if d.Ordinal != 1 {
			t.Errorf("a repeated draft answered ordinal %d, want 1 every time: a read does not consume", d.Ordinal)
		}
	}
	stmts := counter.Summary()
	if len(stmts) == 0 {
		t.Fatal("zero statements counted: the counter is not on the seam the render uses, so nothing below proves anything")
	}
	forbidden := regexp.MustCompile(`(?is)pg_advisory|^\s*begin|^\s*commit|\binsert\s+into\b|\bupdate\s+\w+\s+set\b|\bdelete\s+from\b`)
	for _, s := range stmts {
		if forbidden.MatchString(s) {
			t.Errorf("the draft render issued %q: it must take no lock, open no write transaction and write nothing", s)
		}
	}

	// And the allocation itself is untouched: five renders later, the first
	// create in that bucket is still ordinal 1. A mint that rolled back would
	// pass the statement scan above and fail this.
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: strptr("room-1"),
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if c.Ordinal == nil || *c.Ordinal != 1 {
		t.Errorf("first create after five renders got ordinal %v, want 1: the render consumed one", c.Ordinal)
	}
}

// TestTheDraftRefusesTheParentlessBucketItCannotCreateIn closes the disclosure
// the create's own all-scope gate already closed on the other side of the same
// question (#702 review).
//
// All three creates refuse a PARENTLESS row unless the create scope is all
// (CreateLocation, CreateComponent and CreateSystem each answer their forbidden
// sentinel), and the draft resolved an empty parent to nil with no gate at all.
// So a caller whose grant covers one subtree could ask what the ROOT bucket
// would name a row, and previewName reads that bucket's sibling NAMES: the
// answer says which names are taken there. It is a chosen probe rather than one
// fixed leak, because the caller supplies the stem (a forked location_type's
// name rule since #703, which needs only location_type:create).
//
// The three tiers are driven together because it is one seam (draftParentID),
// and a per-tier fix is exactly how two of them would drift.
func TestTheDraftRefusesTheParentlessBucketItCannotCreateIn(t *testing.T) {
	gw, ctx := seededGateway(t)
	hq, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create campus: %v", err)
	}
	// A location type that generates, so the location tier actually reaches the
	// sibling read rather than refusing earlier for want of a rule.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "deck", DisplayName: "Deck", AllowedParentTypes: []string{"campus"},
		NameRule: &storage.NameRule{Stem: "deck"},
	}); err != nil {
		t.Fatalf("create location type: %v", err)
	}
	subtree := scope.Set{IDs: []string{hq.ID}}

	// Location. The parentless bucket is the estate ROOT, and this caller's
	// create scope is one campus.
	if _, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
		LocationTypeRef: "deck",
	}, subtree); !errors.Is(err, storage.ErrLocationForbidden) {
		t.Errorf("root-bucket location draft = %v, want ErrLocationForbidden, the same refusal the create gives", err)
	}
	// And the create agrees, which is the whole point: the draft answers what
	// the create would do, so the two cannot disagree about whether it happens.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		LocationType: "deck",
	}, subtree); !errors.Is(err, storage.ErrLocationForbidden) {
		t.Errorf("root-bucket location create = %v, want ErrLocationForbidden", err)
	}

	// Component and system: their parentless bucket is the unplaced (or
	// location-only) one, and their creates gate it identically.
	if _, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55,
	}, subtree, all, all); !errors.Is(err, storage.ErrComponentForbidden) {
		t.Errorf("parentless component draft = %v, want ErrComponentForbidden", err)
	}
	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
		SystemTypeRef: "board",
	}, subtree, all); !errors.Is(err, storage.ErrSystemForbidden) {
		t.Errorf("parentless system draft = %v, want ErrSystemForbidden", err)
	}

	// An all-scoped caller still gets the answer, so the three refusals above are
	// the scope and not a broken draft.
	if _, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{LocationTypeRef: "deck"}, all); err != nil {
		t.Errorf("root-bucket draft for an all-scoped caller = %v, want the answer", err)
	}

	// A parent INSIDE the subtree is unaffected: what is gated is the bucket the
	// caller cannot write, not drafting at all.
	if _, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
		LocationTypeRef: "deck", ParentName: hq.Name,
	}, subtree); err != nil {
		t.Errorf("in-subtree draft = %v, want the answer", err)
	}
}

// TestTheNamePreconditionIsPureAndSaysSo pins the create's half of the
// precondition (#702) where it can be exercised exhaustively: the states no
// integration fixture reaches cheaply, and the one that carries the values a
// surface recovers with.
//
// The last case is the one the review moved. The two ordinals AGREE and the
// names do not, which is a stem edit under an open form: an ordinal claim passes
// it and lands a name nobody was shown, and a name claim refuses it.
func TestTheNamePreconditionIsPureAndSaysSo(t *testing.T) {
	one, two := 1, 2
	display1, display2 := "display-1", "display-2"
	if err := storage.ExportConfirmDraftedName(nil, &one, "display-1"); err != nil {
		t.Errorf("no expectation = %v, want no error: a caller that did not preview is not making a claim", err)
	}
	if err := storage.ExportConfirmDraftedName(nil, nil, "front-panel"); err != nil {
		t.Errorf("no expectation and no allocation = %v, want no error", err)
	}
	if err := storage.ExportConfirmDraftedName(&display1, &one, "display-1"); err != nil {
		t.Errorf("an expectation the create met = %v, want no error", err)
	}
	if err := storage.ExportConfirmDraftedName(&display1, nil, "front-panel"); !errors.Is(err, storage.ErrNameExpectedOnTypedName) {
		t.Errorf("an expectation with nothing allocated = %v, want ErrNameExpectedOnTypedName", err)
	}
	err := storage.ExportConfirmDraftedName(&display1, &two, "display-2")
	var moved *storage.DraftedNameMovedError
	if !errors.As(err, &moved) {
		t.Fatalf("a moved name = %v, want a DraftedNameMovedError", err)
	}
	if moved.Expected != display1 || moved.Name != display2 || moved.Ordinal != 2 {
		t.Errorf("refusal = %+v, want the name the form held, the name produced, and the ordinal behind it", moved)
	}
	if !strings.Contains(moved.Error(), "display-2") {
		t.Errorf("refusal message %q does not name the name the operator would get", moved.Error())
	}
	// The ordinals agree and the names do not: a stem that moved under the form.
	if err := storage.ExportConfirmDraftedName(&display1, &one, "monitor-1"); !errors.Is(err, storage.ErrDraftedNameMoved) {
		t.Errorf("same ordinal, different stem = %v, want ErrDraftedNameMoved: the number is not the claim", err)
	}
}
