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

// The draft label render (#699): what a create form can be told about the label
// a row will carry, BEFORE the row exists.
//
// ADR-0104 refused a draft preview that MINTS, on two grounds: the answer is
// provisional (another create can take the ordinal between the preview and the
// commit) and a rolled-back mint takes the same bucket advisory lock real
// creates need, so previewing per picker change would serialise the estate's
// creates. Neither objection reaches a render that ALLOCATES NOTHING, which is
// what this is: the same tier resolution, the same closed data map and the same
// one engine, with the token standing where the ordinal would go.
//
// So the properties this file holds are, in order of what would hurt most if
// they broke:
//
//  1. the rendered draft is the label the gateway then STORES (the whole claim),
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
	}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName:  qm55,
		Name:         "front-panel",
		LocationName: room.Name,
		SystemName:   "board-1",
	}, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "front-panel", ProductName: strptr(qm55), LocationName: &room.Name, SystemName: strptr("board-1"),
	}, all)
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

// TestTheDraftLabelIsTheLabelTheCreateStoresForAGeneratedName is the same
// acceptance where the platform owns the name too, which is the case the token
// exists for. The comparison substitutes the ordinal the create allocated back
// into the drafted string, because that number is exactly and only what the
// draft could not know.
func TestTheDraftLabelIsTheLabelTheCreateStoresForAGeneratedName(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name,
	}, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if c.Ordinal == nil {
		t.Fatal("a generated name stores the ordinal it was minted from")
	}
	// The token is substituted only where it stands alone as a word, which is
	// what the shipped rule renders it as ("Microphone n"). A blanket
	// replacement would corrupt any type name carrying the letter, and this
	// fixture's does.
	tok := regexp.MustCompile(`\b` + regexp.QuoteMeta(storage.OrdinalToken) + `\b`)
	want := tok.ReplaceAllString(drafted.Label, strconv.Itoa(*c.Ordinal))
	if want != c.DisplayName {
		t.Errorf("drafted %q with the ordinal filled in = %q, stored %q", drafted.Label, want, c.DisplayName)
	}
	if !strings.Contains(drafted.Label, storage.OrdinalToken) {
		t.Errorf("drafted %q carries no ordinal token, so the substitution above proves nothing", drafted.Label)
	}
}

// TestTheDraftSystemLabelIsExactWhereTheRuleReadsNoOrdinal is the generated-name
// case with no unknown left in it: the shipped system rule is "{{.TypeName}}",
// so a system whose name the platform mints still drafts a label the create
// stores character for character.
func TestTheDraftSystemLabelIsExactWhereTheRuleReadsNoOrdinal(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
		SystemTypeRef: "board", LocationName: room.Name,
	}, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		SystemTypeID: strptr("board"), LocationName: &room.Name,
	}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	if drafted.Label != s.DisplayName || drafted.Label == "" {
		t.Errorf("drafted %q, stored %q", drafted.Label, s.DisplayName)
	}
	if !s.NameGenerated {
		t.Error("the create was expected to mint the name, so this is the generated case")
	}
}

// TestTheDraftLabelIsEmptyWhereNoRuleResolves is the location tier, and it is
// not an edge case: the shipped GLOBAL location rule is deliberately empty
// (internal/seed/label_rules.yaml) and no seeded location_type carries one
// either, so EVERY location a shipped estate creates is in this state. The
// answer is the empty string rather than a refusal, because the read ladder's
// third rung is the entity's own name and that fallback is the design.
func TestTheDraftLabelIsEmptyWhereNoRuleResolves(t *testing.T) {
	gw, ctx := seededGateway(t)
	drafted, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
		LocationTypeRef: "room", Name: "boardroom",
	})
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	if drafted.Label != "" || drafted.Rule != "" {
		t.Errorf("drafted %+v, want an empty label and an empty rule: no location rule ships", drafted)
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
	}, all, all)
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
	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{}, all); !errors.Is(err, storage.ErrSystemTypeRequiredForName) {
		t.Errorf("unclassified system draft = %v, want ErrSystemTypeRequiredForName", err)
	}
	// A location_type with no name rule, which is every shipped type but floor.
	if _, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{LocationTypeRef: "room"}); !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
		t.Errorf("nameless room draft = %v, want ErrLocationTypeNoNameRule", err)
	}
	// And the same three, supplied with a name, render fine: the refusal is
	// about GENERATING a name, never about rendering a label.
	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{Name: "one-off"}, all); err != nil {
		t.Errorf("named unclassified system draft = %v, want no error", err)
	}
	if _, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{LocationTypeRef: "room", Name: "boardroom"}); err != nil {
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
	_, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{ProductName: "stemless-thing"}, all, all)
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
	}, narrow, narrow)
	if err != nil {
		t.Fatalf("render inside scope: %v", err)
	}
	if !strings.Contains(got.Label, "Mine") {
		t.Errorf("in-scope draft %q does not carry the location's label", got.Label)
	}
	if _, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, Name: "panel", LocationName: theirs.Name,
	}, narrow, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
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
	}, narrow); err != nil {
		t.Fatalf("render inside scope: %v", err)
	}
	if _, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
		SystemTypeRef: "board", Name: "board-x", LocationName: theirs.Name,
	}, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
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
	}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	narrow := scope.Set{IDs: []string{mine.ID}}

	if _, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, Name: "panel", SystemName: "board-x",
	}, narrow, narrow); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("out-of-scope system draft = %v, want the non-disclosing ErrSystemNotFound", err)
	}
}

// TestTheDraftLabelAllocatesNothing is ADR-0104's distinction, proven with
// #650's counting instrument rather than asserted. Three claims, each read off
// the SQL the render actually issued: no advisory lock, no write transaction,
// and no write.
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
		if _, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
			ProductName: qm55, LocationName: "room-1",
		}, all, all); err != nil {
			t.Fatalf("render draft: %v", err)
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
	}, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if c.Ordinal == nil || *c.Ordinal != 1 {
		t.Errorf("first create after five renders got ordinal %v, want 1: the render consumed one", c.Ordinal)
	}
}

// TestTheDraftShapeAndTheMintAgree pins the one piece of new pure logic against
// the allocator's own formatter. shape() writes the token where name() writes a
// number, and if the two ever disagree the form shows a shape the platform does
// not mint, which is the cross-tier defect this epic has now hit twice.
func TestTheDraftShapeAndTheMintAgree(t *testing.T) {
	for _, tc := range []struct {
		stem      string
		bareFirst bool
		want      string
	}{
		{stem: "display", bareFirst: false, want: "display-" + storage.OrdinalToken},
		{stem: "boardroom", bareFirst: true, want: "boardroom"},
		{stem: "", bareFirst: false, want: storage.OrdinalToken},
	} {
		got := storage.ExportMintShape(tc.stem, tc.bareFirst)
		if got != tc.want {
			t.Errorf("shape(%q, %v) = %q, want %q", tc.stem, tc.bareFirst, got, tc.want)
		}
		// The shape is the mint with the token in the ordinal's place. For a
		// suppressing mint that place is empty at n=1, so the shape IS the
		// first name; for the other two it is name(n) with n written out.
		if tc.bareFirst {
			if got != storage.ExportMintName(tc.stem, tc.bareFirst, 1) {
				t.Errorf("suppressing shape %q is not the name the first one gets, %q", got, storage.ExportMintName(tc.stem, tc.bareFirst, 1))
			}
			continue
		}
		for _, n := range []int{1, 2, 17} {
			want := storage.ExportMintName(tc.stem, tc.bareFirst, n)
			if strings.Replace(got, storage.OrdinalToken, strconv.Itoa(n), 1) != want {
				t.Errorf("shape %q with %d substituted != mint name %q", got, n, want)
			}
		}
	}
}
