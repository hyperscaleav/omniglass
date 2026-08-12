package storage_test

import (
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The create form's ordinal precondition (#702).
//
// The form is now shown the REAL number a generated name will carry, read from
// the bucket's siblings without allocating anything. That answer is provisional
// by nature: another create in the same bucket can take the number between the
// preview and the submit. So the form posts back the ordinal it was shown, the
// create allocates under its own advisory lock exactly as it always did, and the
// two are compared before the row is written.
//
// What these hold, in the order that would hurt most if it broke:
//
//  1. an unchanged bucket lets the create through, so the ordinary path is not
//     a lottery;
//  2. a bucket that MOVED is refused with the number that moved, rather than
//     landing a name the operator was never shown;
//  3. the refusal names both numbers and the name that would have been minted,
//     because the form's recovery is to show that name and let the operator
//     resubmit; and
//  4. an expectation posted against a name the OPERATOR typed is refused
//     outright, since nothing is allocated on that path and a precondition
//     nobody checks is worse than none.

func intptr(n int) *int { return &n }

// TestACreateHonoursTheOrdinalTheFormWasShown is the ordinary path: the form
// previews, nothing else happens, the create takes the number it expected.
func TestACreateHonoursTheOrdinalTheFormWasShown(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	if drafted.Ordinal != 1 || drafted.Name == "" {
		t.Fatalf("drafted %+v, want the first ordinal in an empty bucket", drafted)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, ExpectedOrdinal: &drafted.Ordinal,
	}, all, all, all)
	if err != nil {
		t.Fatalf("create with the expected ordinal: %v", err)
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

// TestACreateRefusesAnOrdinalAnotherCreateTook is the race, played out in the
// order it happens in an estate: the form reads 1, somebody else creates and
// takes 1, the form submits. The second create must be refused rather than
// silently renamed to 2, because the whole point of the locked field is that the
// operator was shown the name they get.
func TestACreateRefusesAnOrdinalAnotherCreateTook(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	drafted, err := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
		ProductName: qm55, LocationName: room.Name,
	}, all, all, all)
	if err != nil {
		t.Fatalf("render draft: %v", err)
	}
	// The other create, which takes the number the form is holding.
	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name,
	}, all, all, all)
	if err != nil {
		t.Fatalf("the racing create: %v", err)
	}
	if first.Name != drafted.Name {
		t.Fatalf("the racing create landed %q, not the %q the form was holding: this is not the race", first.Name, drafted.Name)
	}

	_, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, ExpectedOrdinal: &drafted.Ordinal,
	}, all, all, all)
	var taken *storage.OrdinalTakenError
	if !errors.As(err, &taken) {
		t.Fatalf("create after the race = %v, want an OrdinalTakenError", err)
	}
	if taken.Expected != drafted.Ordinal {
		t.Errorf("refusal reports expected %d, the form held %d", taken.Expected, drafted.Ordinal)
	}
	if taken.Ordinal != drafted.Ordinal+1 {
		t.Errorf("refusal reports ordinal %d, want %d: the number that moved is what the form re-reads", taken.Ordinal, drafted.Ordinal+1)
	}
	// The name is in the refusal because it is what the form shows next: an
	// operator reads a name, not an ordinal.
	if taken.Name == "" || taken.Name == drafted.Name {
		t.Errorf("refusal names %q, want the name the next create would mint, not the one that was taken", taken.Name)
	}
	if !errors.Is(err, storage.ErrOrdinalTaken) {
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

// TestTheOrdinalPreconditionIsRefusedOnAnOperatorTypedName: a name the operator
// typed allocates no ordinal at all (the column is null by design, #681), so an
// expectation posted beside one can never be checked. Refused rather than
// ignored: a precondition nobody evaluates reads as a guarantee and is not one.
func TestTheOrdinalPreconditionIsRefusedOnAnOperatorTypedName(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")

	_, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "front-panel", ProductName: strptr(qm55), LocationName: &room.Name, ExpectedOrdinal: intptr(1),
	}, all, all, all)
	if !errors.Is(err, storage.ErrOrdinalExpectedOnTypedName) {
		t.Errorf("typed name with an expectation = %v, want ErrOrdinalExpectedOnTypedName", err)
	}
	// The same create with no expectation is fine, so the refusal is about the
	// pairing and not about the name.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "front-panel", ProductName: strptr(qm55), LocationName: &room.Name,
	}, all, all, all); err != nil {
		t.Errorf("typed name with no expectation = %v, want no error", err)
	}
}

// TestTheOrdinalPreconditionHoldsOnEveryTierThatGenerates: the precondition is
// one idea, so it is proven on all three kinds rather than on the component
// alone. A system suppresses its first ordinal and a location has two buckets
// instead of three, and neither difference reaches this comparison: what is
// compared is the number, and the mint is what turns it into a name.
func TestTheOrdinalPreconditionHoldsOnEveryTierThatGenerates(t *testing.T) {
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
		SystemTypeID: strptr("board"), LocationName: &room.Name, ExpectedOrdinal: &sysDraft.Ordinal,
	}, all, all)
	if err != nil {
		t.Fatalf("create system with the expected ordinal: %v", err)
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
		SystemTypeID: strptr("board"), LocationName: &room.Name, ExpectedOrdinal: intptr(1),
	}, all, all); !errors.Is(err, storage.ErrOrdinalTaken) {
		t.Errorf("system create expecting the taken ordinal = %v, want ErrOrdinalTaken", err)
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
		LocationType: "deck", ParentName: &hq.Name, ExpectedOrdinal: &locDraft.Ordinal,
	}, all)
	if err != nil {
		t.Fatalf("create location with the expected ordinal: %v", err)
	}
	if l.Name != locDraft.Name {
		t.Errorf("location created %q, the form was shown %q", l.Name, locDraft.Name)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		LocationType: "deck", ParentName: &hq.Name, ExpectedOrdinal: intptr(1),
	}, all); !errors.Is(err, storage.ErrOrdinalTaken) {
		t.Errorf("location create expecting the taken ordinal = %v, want ErrOrdinalTaken", err)
	}
}
