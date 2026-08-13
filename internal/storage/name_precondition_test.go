package storage_test

import (
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The create form's name precondition (#702, and its review).
//
// The form is shown the REAL name a generated row will carry, read from the
// bucket's siblings without allocating anything. That answer is provisional by
// nature: another create in the same bucket can take the number between the
// preview and the submit, and the type that mints the name can be edited in the
// same window. So the form posts back the NAME it was shown, the create
// allocates under its own advisory lock exactly as it always did, and the two
// are compared before the row is written.
//
// The name and not the ordinal, which is what the review moved. A name is the
// stem, the suppression rule and the number in one value, and the claim the form
// is making is about the name, so a claim on the number alone passes while the
// other two move: display-1 drafted, monitor-1 written, precondition met. Since
// #703 an operator forks a type and rewrites what it mints as an ordinary act,
// so that is not a contrived window.
//
// What these hold, in the order that would hurt most if it broke:
//
//  1. an unchanged bucket and an unchanged type let the create through, so the
//     ordinary path is not a lottery;
//  2. a name that MOVED is refused, for either reason, rather than landing a
//     name the operator was never shown;
//  3. the refusal names what the create would have produced, because the form's
//     recovery is to show that name and let the operator resubmit; and
//  4. an expectation posted against a name the OPERATOR typed is refused
//     outright, since nothing is allocated on that path and a precondition
//     nobody checks is worse than none.

// TestACreateHonoursTheNameTheFormWasShown is the ordinary path: the form
// previews, nothing else happens, the create lands the name it expected.
func TestACreateHonoursTheNameTheFormWasShown(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	if drafted.Ordinal != 1 || drafted.Name == "" {
		t.Fatalf("drafted %+v, want the first ordinal in an empty bucket", drafted)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, ExpectedName: &drafted.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create with the expected name: %v", err)
	}
	if c.Name != drafted.Name {
		t.Errorf("created %q, the form was shown %q", c.Name, drafted.Name)
	}
	if c.Ordinal == nil || *c.Ordinal != drafted.Ordinal {
		t.Errorf("stored ordinal %v, expected %d", c.Ordinal, drafted.Ordinal)
	}
	if !c.NameGenerated {
		t.Error("the platform still holds the pen: a precondition is not a name")
	}
}

// TestACreateRefusesANameAnotherCreateTook is the race, played out in the order
// it happens in an estate: the form reads display-1, somebody else creates and
// takes it, the form submits. The second create must be refused rather than
// silently renamed to display-2, because the whole point of the locked field is
// that the operator was shown the name they get.
func TestACreateRefusesANameAnotherCreateTook(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	// The other create, which takes the name the form is holding.
	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("the racing create: %v", err)
	}
	if first.Name != drafted.Name {
		t.Fatalf("the racing create landed %q, not the %q the form was holding: this is not the race", first.Name, drafted.Name)
	}

	_, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, ExpectedName: &drafted.Name,
	}, all, all, all, all)
	var moved *storage.DraftedNameMovedError
	if !errors.As(err, &moved) {
		t.Fatalf("create after the race = %v, want a DraftedNameMovedError", err)
	}
	if moved.Expected != drafted.Name {
		t.Errorf("refusal reports expected %q, the form held %q", moved.Expected, drafted.Name)
	}
	// The name is what the form shows next: an operator reads a name, not an
	// ordinal, and the ordinal rides along only to explain the answer.
	if moved.Name == "" || moved.Name == drafted.Name {
		t.Errorf("refusal names %q, want the name the next create would mint, not the one that was taken", moved.Name)
	}
	if moved.Ordinal != drafted.Ordinal+1 {
		t.Errorf("refusal reports ordinal %d, want %d", moved.Ordinal, drafted.Ordinal+1)
	}
	if !errors.Is(err, storage.ErrDraftedNameMoved) {
		t.Error("the refusal does not carry its sentinel, so the API cannot map it without a type switch")
	}
	// And nothing was written: a refused create leaves the bucket exactly as
	// the racing create left it.
	comps, err := gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(comps) != 1 {
		t.Errorf("%d components after the refusal, want the one the race created", len(comps))
	}
}

// TestACreateRefusesANameTheTypeMovedUnderneath is the case an ordinal claim
// could not see, and the reason the precondition binds the name.
//
// Nothing races here and nothing about the bucket changes: the form drafts
// display-1, the component_type's stem is rewritten to monitor, and the create
// allocates ordinal 1 exactly as the form expected. An ordinal claim is
// therefore MET, and the row lands as monitor-1, which is precisely the silent
// rename the precondition exists to prevent.
func TestACreateRefusesANameTheTypeMovedUnderneath(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	if drafted.Name != "display-1" || drafted.Ordinal != 1 {
		t.Fatalf("drafted %+v, want display-1 at ordinal 1", drafted)
	}

	// The stem moves while the form is open, which #703 makes an ordinary
	// operator act rather than a race.
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{
		Stem: strptr("monitor"),
	}); err != nil {
		t.Fatalf("move the stem: %v", err)
	}

	_, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, ExpectedName: &drafted.Name,
	}, all, all, all, all)
	var moved *storage.DraftedNameMovedError
	if !errors.As(err, &moved) {
		t.Fatalf("create after the stem moved = %v, want a DraftedNameMovedError", err)
	}
	if moved.Expected != "display-1" || moved.Name != "monitor-1" {
		t.Errorf("refusal = %+v, want display-1 expected and monitor-1 produced", moved)
	}
	// The ordinal is UNCHANGED across the refusal, which is the whole point: a
	// claim on the number alone would have been satisfied here.
	if moved.Ordinal != drafted.Ordinal {
		t.Errorf("refusal reports ordinal %d, want the unchanged %d: the number is not what moved", moved.Ordinal, drafted.Ordinal)
	}
	comps, err := gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(comps) != 0 {
		t.Errorf("%d components after the refusal, want none", len(comps))
	}

	// Re-drafted, the form is shown the new name and the create takes it, still
	// with the platform holding the pen.
	redrafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("re-render draft: %v", err)
	}
	if redrafted.Name != "monitor-1" {
		t.Fatalf("re-draft %+v, want monitor-1", redrafted)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, ExpectedName: &redrafted.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create with the re-drafted name: %v", err)
	}
	if c.Name != "monitor-1" || !c.NameGenerated {
		t.Errorf("created %+v, want monitor-1 with the platform holding the pen", c)
	}
}

// TestTheNamePreconditionIsRefusedOnAnOperatorTypedName: a name the operator
// typed allocates no ordinal at all (the column is null by design, #681), so an
// expectation posted beside one can never be checked. Refused rather than
// ignored: a precondition nobody evaluates reads as a guarantee and is not one.
func TestTheNamePreconditionIsRefusedOnAnOperatorTypedName(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	_, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "front-panel", ProductName: strptr(qm55), LocationName: &room.Name, ExpectedName: strptr("front-panel"),
	}, all, all, all, all)
	if !errors.Is(err, storage.ErrNameExpectedOnTypedName) {
		t.Errorf("typed name with an expectation = %v, want ErrNameExpectedOnTypedName", err)
	}
	// Refused even when the two AGREE, which is what keeps the field a
	// precondition rather than a second spelling of the name: the row would carry
	// no ordinal, so there is no platform claim to check against.
	// The same create with no expectation is fine, so the refusal is about the
	// pairing and not about the name.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "front-panel", ProductName: strptr(qm55), LocationName: &room.Name,
	}, all, all, all, all); err != nil {
		t.Errorf("typed name with no expectation = %v, want no error", err)
	}
}

// TestTheNamePreconditionHoldsOnEveryTierThatGenerates: the precondition is one
// idea, so it is proven on all three kinds rather than on the component alone. A
// system suppresses its first ordinal and a location has two buckets instead of
// three, and both differences are now INSIDE the compared value rather than
// outside it, which is the other reason the name is the better claim: the
// suppressed "boardroom" and the "boardroom-2" beside it are one comparison.
func TestTheNamePreconditionHoldsOnEveryTierThatGenerates(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	// System: the shipped board type mints "boardroom", suppressing ordinal 1.
	sysDraft, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
		SystemTypeRef: "board", LocationName: room.Name,
	}, all, all)
	if err != nil {
		t.Fatalf("render system draft: %v", err)
	}
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		SystemTypeID: strptr("board"), LocationName: &room.Name, ExpectedName: &sysDraft.Name,
	}, all, all)
	if err != nil {
		t.Fatalf("create system with the expected name: %v", err)
	}
	if s.Name != sysDraft.Name {
		t.Errorf("system created %q, the form was shown %q", s.Name, sysDraft.Name)
	}
	// The second one in the bucket: the form is now shown the -2 name, and the
	// create takes it.
	next, err := gw.RenderSystemDraftLabel(ctx, storage.SystemLabelDraft{
		SystemTypeRef: "board", LocationName: room.Name,
	}, all, all)
	if err != nil {
		t.Fatalf("render the second system draft: %v", err)
	}
	if next.Ordinal != 2 || next.Name != s.Name+"-2" {
		t.Errorf("second system draft %+v, want ordinal 2 and %q", next, s.Name+"-2")
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		SystemTypeID: strptr("board"), LocationName: &room.Name, ExpectedName: &sysDraft.Name,
	}, all, all); !errors.Is(err, storage.ErrDraftedNameMoved) {
		t.Errorf("system create expecting the taken name = %v, want ErrDraftedNameMoved", err)
	}

	// Location: a positional type of the operator's own, since no shipped type
	// carries a name rule (ADR-0103).
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "deck", DisplayName: "Deck", NameRule: &storage.NameRule{},
	}); err != nil {
		t.Fatalf("create the positional location type: %v", err)
	}
	// The building makeRoomWithLabel already created above: the two tiers share
	// one estate here on purpose, so the location bucket under test is a real
	// one with a sibling room in it rather than an empty fixture.
	hq, err := gw.GetLocation(ctx, "hq", all)
	if err != nil {
		t.Fatalf("read the building: %v", err)
	}
	locDraft, err := gw.RenderLocationDraftLabel(ctx, storage.LocationLabelDraft{
		LocationTypeRef: "deck", ParentName: hq.Name,
	}, all)
	if err != nil {
		t.Fatalf("render location draft: %v", err)
	}
	if locDraft.Name != "1" || locDraft.Ordinal != 1 {
		t.Errorf("location draft %+v, want the positional name 1", locDraft)
	}
	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		LocationType: "deck", ParentName: &hq.Name, ExpectedName: &locDraft.Name,
	}, all)
	if err != nil {
		t.Fatalf("create location with the expected name: %v", err)
	}
	if l.Name != locDraft.Name {
		t.Errorf("location created %q, the form was shown %q", l.Name, locDraft.Name)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		LocationType: "deck", ParentName: &hq.Name, ExpectedName: &locDraft.Name,
	}, all); !errors.Is(err, storage.ErrDraftedNameMoved) {
		t.Errorf("location create expecting the taken name = %v, want ErrDraftedNameMoved", err)
	}
}
