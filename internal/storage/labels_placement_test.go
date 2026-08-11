package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Placement in the label data map, and the cascade that keeps it honest (#685).
//
// Slice 2 excluded placement deliberately, and its write-path completeness
// argument rested on the exclusion: the five acts it enumerated were provably
// the whole set BECAUSE every fact the map held was the row's own. That
// property is gone. A component's label can now read the label of its LOCATION
// and the label of its primary system's TYPE, both of them columns on other
// rows, so the set of acts that stale a label is no longer the set of acts on
// the labelled row.
//
// Everything in this file is the consequence: the map keys, the acts that now
// change them, and the rows those acts must restamp.

// makeRoomWithLabel creates a room under the shared building with a label an
// operator typed, so the location's read ladder answers with the LABEL rather
// than falling through to the name. Returns the location.
func makeRoomWithLabel(t *testing.T, gw *storage.PG, ctx context.Context, name, label string) *storage.Location {
	t.Helper()
	if _, err := gw.GetLocation(ctx, "hq", all); err != nil {
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "building"}, all); err != nil {
			t.Fatalf("create building: %v", err)
		}
	}
	hq := "hq"
	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: name, DisplayName: label, LocationType: "room", ParentName: &hq,
	}, all)
	if err != nil {
		t.Fatalf("create location %s: %v", name, err)
	}
	return l
}

// The architect's worked example, end to end and with nothing mocked: a rule
// that reads the system's type, the location, and the component's own type
// produces "Boardroom 204B Display".
func TestTheWorkedExampleReadsPlacement(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.SystemTypeLabel}} {{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	room := makeRoomWithLabel(t, gw, ctx, "room-204b", "204B")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-1", SystemTypeID: strptr("board"), LocationName: &room.Name,
	}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, SystemName: strptr("board-1"),
	}, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if c.DisplayName != "Boardroom 204B Display" {
		t.Fatalf("label = %q, want %q", c.DisplayName, "Boardroom 204B Display")
	}
}

// The pinned key sets, extended. Slice 2's own pin lives in labels_test.go and
// covers the component map; this one covers the two new keys explicitly, so a
// reader of this slice sees what it widened rather than having to diff a list.
func TestThePlacementKeysAreExactlyTwoOnComponentAndOneOnSystem(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-a", "Room A")
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "sys-a", SystemTypeID: strptr("board"), LocationName: &room.Name,
	}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room.Name, SystemName: &s.Name,
	}, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}

	cd, err := gw.ExportComponentLabelData(ctx, c.ID)
	if err != nil {
		t.Fatalf("component label data: %v", err)
	}
	if got := cd["LocationLabel"]; got != "Room A" {
		t.Errorf("component LocationLabel = %q, want %q", got, "Room A")
	}
	if got := cd["SystemTypeLabel"]; got != "Boardroom" {
		t.Errorf("component SystemTypeLabel = %q, want %q", got, "Boardroom")
	}

	sd, err := gw.ExportSystemLabelData(ctx, s.ID)
	if err != nil {
		t.Fatalf("system label data: %v", err)
	}
	if got := sd["LocationLabel"]; got != "Room A" {
		t.Errorf("system LocationLabel = %q, want %q", got, "Room A")
	}
	if _, widened := sd["SystemTypeLabel"]; widened {
		t.Errorf("the system map grew SystemTypeLabel: a system's own type is already TypeName, and two spellings of one fact is a rule author's trap")
	}

	// A location's own map is UNCHANGED by this slice. It gains no ancestry
	// key, which is exactly why a location MOVE restamps nothing: see the
	// cascade tests below.
	loc, err := gw.GetLocation(ctx, room.Name, all)
	if err != nil {
		t.Fatalf("get location: %v", err)
	}
	ld, err := gw.ExportLocationLabelData(ctx, loc.ID)
	if err != nil {
		t.Fatalf("location label data: %v", err)
	}
	if got := sortedKeys(ld); !equalStrings(got, []string{"Name", "TypeName"}) {
		t.Fatalf("the location data map carries %v, want exactly [Name TypeName]: giving a location an ancestry key turns every location move into a subtree restamp, which is a decision to take deliberately", got)
	}
}

// The location read ladder, in the one place a rule consumes it: a location
// with a typed label reads the label, one without reads its name. Without this
// the worked example cannot produce "204B" for a room named "room-204b", and
// every shipped estate (whose locations carry no generated label at all, since
// the shipped location rule is empty) would read placement as blank.
func TestLocationLabelFallsThroughToTheLocationName(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "[{{.LocationLabel}}]"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	unlabelled := makeRoom(t, gw, ctx, "room-plain")
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &unlabelled}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "[room-plain]" {
		t.Fatalf("label = %q, want %q: an unlabelled location must read as its name, not as nothing", c.DisplayName, "[room-plain]")
	}

	// And an unplaced component reads placement as absent rather than as an
	// error or a "<no value>".
	orphan, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55)}, all)
	if err != nil {
		t.Fatalf("create unplaced: %v", err)
	}
	if orphan.DisplayName != "[]" {
		t.Fatalf("unplaced label = %q, want %q", orphan.DisplayName, "[]")
	}
}

// --- the cascade --------------------------------------------------------

// Renaming a location restamps the rows that read it, and only those.
func TestRenamingALocationRestampsTheRowsThatReadIt(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("system rule: %v", err)
	}
	here := makeRoom(t, gw, ctx, "room-here")
	elsewhere := makeRoom(t, gw, ctx, "room-elsewhere")

	inHere, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &here}, all)
	if err != nil {
		t.Fatalf("create in here: %v", err)
	}
	inElsewhere, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &elsewhere}, all)
	if err != nil {
		t.Fatalf("create elsewhere: %v", err)
	}
	sysHere, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-here", SystemTypeID: strptr("board"), LocationName: &here}, all)
	if err != nil {
		t.Fatalf("create system here: %v", err)
	}
	if inHere.DisplayName != "room-here Display" {
		t.Fatalf("before = %q, want %q", inHere.DisplayName, "room-here Display")
	}
	beforeElsewhere := inElsewhere.UpdatedAt

	if _, err := gw.RenameLocation(ctx, "", here, "room-204b", all, all); err != nil {
		t.Fatalf("rename location: %v", err)
	}

	after, err := gw.GetComponent(ctx, inHere.ID, all)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.DisplayName != "room-204b Display" {
		t.Fatalf("after the rename = %q, want %q: the label reads a location it no longer names", after.DisplayName, "room-204b Display")
	}
	afterSys, err := gw.GetSystem(ctx, sysHere.ID, all)
	if err != nil {
		t.Fatalf("re-read system: %v", err)
	}
	if afterSys.DisplayName != "room-204b Boardroom" {
		t.Fatalf("system after the rename = %q, want %q", afterSys.DisplayName, "room-204b Boardroom")
	}

	// And only those: a row in another room is not touched at all, neither its
	// label nor its updated_at.
	other, err := gw.GetComponent(ctx, inElsewhere.ID, all)
	if err != nil {
		t.Fatalf("re-read elsewhere: %v", err)
	}
	if other.DisplayName != "room-elsewhere Display" {
		t.Fatalf("a component in another room reads %q, want %q", other.DisplayName, "room-elsewhere Display")
	}
	if !other.UpdatedAt.Equal(beforeElsewhere) {
		t.Errorf("a component in another room was written (updated_at moved): the cascade is restamping rows that read nothing that changed")
	}
}

// A location's LABEL moving is the same cascade as its name moving, and it is
// the one that matters most, since it is the fact the worked example reads.
func TestRelabellingALocationRestampsTheRowsThatReadIt(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	room := makeRoomWithLabel(t, gw, ctx, "room-a", "204B")
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &room.Name}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "204B Display" {
		t.Fatalf("before = %q, want %q", c.DisplayName, "204B Display")
	}
	if _, err := gw.UpdateLocation(ctx, "", room.Name, storage.LocationPatch{DisplayName: strptr("204C")}, all, all); err != nil {
		t.Fatalf("relabel: %v", err)
	}
	after, err := gw.GetComponent(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.DisplayName != "204C Display" {
		t.Fatalf("after the relabel = %q, want %q", after.DisplayName, "204C Display")
	}

	// Clearing the location's label hands its pen back to the platform, and the
	// ladder falls through to the name: the components below must follow that
	// too, not only a label swapped for another label.
	if _, err := gw.UpdateLocation(ctx, "", room.Name, storage.LocationPatch{DisplayName: strptr("")}, all, all); err != nil {
		t.Fatalf("clear the label: %v", err)
	}
	cleared, err := gw.GetComponent(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("re-read after clear: %v", err)
	}
	if cleared.DisplayName != "room-a Display" {
		t.Fatalf("after clearing the location label = %q, want %q", cleared.DisplayName, "room-a Display")
	}
}

// A system's reclassify changes every member component's SystemTypeLabel. The
// members are not the row being written, so nothing in slice 2 would have
// restamped them.
func TestReclassifyingASystemRestampsItsMembers(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.SystemTypeLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	room := makeRoom(t, gw, ctx, "room-a")
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-a", SystemTypeID: strptr("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	member, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room, SystemName: &s.Name,
	}, all)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if member.DisplayName != "Boardroom Display" {
		t.Fatalf("before = %q, want %q", member.DisplayName, "Boardroom Display")
	}
	if _, err := gw.UpdateSystem(ctx, "", s.ID, storage.SystemPatch{SystemTypeID: strptr("huddle")}, all, all); err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	after, err := gw.GetComponent(ctx, member.ID, all)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.DisplayName != "Huddle Room Display" {
		t.Fatalf("after the reclassify = %q, want %q", after.DisplayName, "Huddle Room Display")
	}
}

// Membership is the write path nothing in the epic's own list named: a
// component's primary system is a row in system_member, and every act that
// binds, unbinds or re-defaults one changes what SystemTypeLabel reads.
func TestMembershipChangesRestampTheComponent(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "[{{.SystemTypeLabel}}]"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	room := makeRoom(t, gw, ctx, "room-a")
	board, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-board", SystemTypeID: strptr("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create board system: %v", err)
	}
	huddle, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-huddle", SystemTypeID: strptr("huddle"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create huddle system: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.DisplayName != "[]" {
		t.Fatalf("an unbound component reads %q, want %q", c.DisplayName, "[]")
	}

	// bind: the first membership becomes the primary with nobody asking
	if err := gw.AddMember(ctx, "", board.Name, c.ID, all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "[Boardroom]" {
		t.Fatalf("after the bind = %q, want %q", got, "[Boardroom]")
	}

	// a second membership does not steal the default, so nothing moves
	if err := gw.AddMember(ctx, "", huddle.Name, c.ID, all); err != nil {
		t.Fatalf("add second member: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "[Boardroom]" {
		t.Fatalf("after the second bind = %q, want %q: a later membership is not the default", got, "[Boardroom]")
	}

	// re-defaulting moves it
	if err := gw.SetPrimaryMember(ctx, "", huddle.Name, c.ID, all); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "[Huddle Room]" {
		t.Fatalf("after the re-default = %q, want %q", got, "[Huddle Room]")
	}

	// unbinding the default promotes the sole survivor, which moves it again
	if err := gw.RemoveMember(ctx, "", huddle.Name, c.ID, all); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "[Boardroom]" {
		t.Fatalf("after the unbind = %q, want %q", got, "[Boardroom]")
	}

	// and unbinding the last one leaves no system at all
	if err := gw.RemoveMember(ctx, "", board.Name, c.ID, all); err != nil {
		t.Fatalf("remove last member: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "[]" {
		t.Fatalf("after the last unbind = %q, want %q", got, "[]")
	}
}

// A system's own move changes its own LocationLabel. MoveSystem is the one
// write path on these three tiers that stamped NO label before this slice,
// because a system's map carried no fact a move could touch. It does now.
func TestMovingASystemRestampsItsOwnLabel(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("system rule: %v", err)
	}
	from := makeRoom(t, gw, ctx, "room-from")
	to := makeRoom(t, gw, ctx, "room-to")
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-a", SystemTypeID: strptr("board"), LocationName: &from}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	if s.DisplayName != "room-from Boardroom" {
		t.Fatalf("before = %q, want %q", s.DisplayName, "room-from Boardroom")
	}
	moved, err := gw.MoveSystem(ctx, "", s.ID, storage.SystemMove{LocationName: &to}, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.DisplayName != "room-to Boardroom" {
		t.Fatalf("after the move = %q, want %q", moved.DisplayName, "room-to Boardroom")
	}
}

// A component's own move changes its own LocationLabel, on top of the ordinal
// re-mint slice 2 already stamped for.
func TestMovingAComponentRestampsItsLocationLabel(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	from := makeRoom(t, gw, ctx, "room-from")
	to := makeRoom(t, gw, ctx, "room-to")
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &from}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	moved, err := gw.MoveComponent(ctx, "", c.ID, storage.ComponentMove{LocationName: &to}, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.DisplayName != "room-to Display" {
		t.Fatalf("after the move = %q, want %q", moved.DisplayName, "room-to Display")
	}
}

// A row whose label the operator owns is never restamped by any of the
// cascades, which is the pen's whole contract applied to a write the operator
// did not make on that row.
func TestAnOperatorOwnedLabelSurvivesEveryCascade(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.SystemTypeLabel}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}}"); err != nil {
		t.Fatalf("system rule: %v", err)
	}
	room := makeRoom(t, gw, ctx, "room-a")
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "sys-a", DisplayName: "The Operator's System", SystemTypeID: strptr("board"), LocationName: &room,
	}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "hand-named", DisplayName: "The Operator's Own", ProductName: strptr(qm55),
		LocationName: &room, SystemName: &s.Name,
	}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := gw.RenameLocation(ctx, "", room, "room-renamed", all, all); err != nil {
		t.Fatalf("rename location: %v", err)
	}
	if _, err := gw.UpdateSystem(ctx, "", s.ID, storage.SystemPatch{SystemTypeID: strptr("huddle")}, all, all); err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if _, err := gw.RecomputeLabels(ctx, "", "component", all, all); err != nil {
		t.Fatalf("bulk recompute: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "The Operator's Own" {
		t.Fatalf("the operator's component label became %q", got)
	}
	after, err := gw.GetSystem(ctx, s.ID, all)
	if err != nil {
		t.Fatalf("re-read system: %v", err)
	}
	if after.DisplayName != "The Operator's System" {
		t.Fatalf("the operator's system label became %q", after.DisplayName)
	}
}

// Moving a LOCATION restamps nothing, and that is a finding rather than a gap.
// A location's own data map carries Name and TypeName, neither of which a
// reparent touches, and a component reads the label of the location it SITS
// AT, not of that location's ancestors. So the acceptance behavior "moving a
// location restamps the moved subtree" is vacuous under this map, and this
// test pins the reason: if a later slice gives a location an ancestry key
// (#687's positional types are the obvious candidate), this test fails and the
// cascade has to grow a location-subtree arm.
func TestMovingALocationChangesNoLabelUnderIt(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "location", "{{.TypeName}} {{.Name}}"); err != nil {
		t.Fatalf("location rule: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "campus-a", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create campus: %v", err)
	}
	other := "campus-a"
	room := makeRoom(t, gw, ctx, "room-a")
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := labelOf(t, gw, ctx, c.ID)
	if _, err := gw.MoveLocation(ctx, "", room, storage.LocationMove{ParentName: &other}, all, all); err != nil {
		t.Fatalf("move location: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != before {
		t.Fatalf("a component's label moved from %q to %q on a location REPARENT: the location map must have grown an ancestry fact, and the cascade owes it a subtree arm", before, got)
	}
	// And the recompute agrees: nothing anywhere is stale afterwards.
	assertNoLabelDrift(t, gw, ctx)
}

// --- the verb -----------------------------------------------------------

// A preview lists exactly the rows a rule change would alter, and changes
// nothing; applying then changes exactly that set; a re-apply is idempotent.
func TestAPreviewListsExactlyWhatAnApplyThenChanges(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "room-a")
	var ids []string
	for range 4 {
		c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &room}, all)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, c.ID)
	}
	// One row the operator owns, which must appear in neither the preview nor
	// the applied set.
	owned, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room, DisplayName: "The Operator's Own",
	}, all)
	if err != nil {
		t.Fatalf("create owned: %v", err)
	}

	// The rule changes with no cascade behind it: this is the act the verb
	// exists for, and nothing is restamped by it.
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}}/{{.TypeName}}/{{.Ordinal}}"); err != nil {
		t.Fatalf("set rule: %v", err)
	}
	preview, err := gw.PreviewLabelRecompute(ctx, "component", all)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview) != 4 {
		t.Fatalf("preview lists %d rows, want 4: %+v", len(preview), preview)
	}
	for _, ch := range preview {
		if ch.ID == owned.ID {
			t.Fatalf("the preview lists a row whose label the operator owns")
		}
		if ch.From == ch.To {
			t.Fatalf("the preview lists %s with From == To (%q): a preview that lists unchanged rows is not a blast radius", ch.Name, ch.From)
		}
	}

	// A preview changes nothing.
	still, err := gw.GetComponent(ctx, ids[0], all)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if still.DisplayName != "Display 1" {
		t.Fatalf("after the preview the label is %q, want the unchanged %q", still.DisplayName, "Display 1")
	}

	applied, err := gw.RecomputeLabels(ctx, "", "component", all, all)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !sameChangeSet(preview, applied) {
		t.Fatalf("the apply changed a different set from the preview:\n preview %+v\n applied %+v", preview, applied)
	}
	for _, ch := range applied {
		got := labelOf(t, gw, ctx, ch.ID)
		if got != ch.To {
			t.Fatalf("%s reads %q, the apply reported %q", ch.Name, got, ch.To)
		}
	}
	if got := labelOf(t, gw, ctx, owned.ID); got != "The Operator's Own" {
		t.Fatalf("the operator's label became %q", got)
	}

	// Idempotent: the second apply finds nothing, and so does a fresh preview.
	again, err := gw.RecomputeLabels(ctx, "", "component", all, all)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("a re-apply changed %d rows, want none: %+v", len(again), again)
	}
	repreview, err := gw.PreviewLabelRecompute(ctx, "component", all)
	if err != nil {
		t.Fatalf("re-preview: %v", err)
	}
	if len(repreview) != 0 {
		t.Fatalf("a preview after an apply lists %d rows, want none", len(repreview))
	}
}

// The verb covers all three kinds, not components alone.
func TestTheVerbRecomputesSystemsAndLocationsToo(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "room-a")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-a", SystemTypeID: strptr("board"), LocationName: &room}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.TypeName}} at {{.LocationLabel}}"); err != nil {
		t.Fatalf("system rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "location", "{{.TypeName}} {{.Name}}"); err != nil {
		t.Fatalf("location rule: %v", err)
	}
	sysChanges, err := gw.RecomputeLabels(ctx, "", "system", all, all)
	if err != nil {
		t.Fatalf("recompute systems: %v", err)
	}
	if len(sysChanges) != 1 || sysChanges[0].To != "Boardroom at room-a" {
		t.Fatalf("system recompute = %+v, want one row labelled %q", sysChanges, "Boardroom at room-a")
	}
	// A location recompute does not stop at locations: moving a location's
	// label stales every row placed at it, and the returned set says so rather
	// than leaving the operator to find out.
	locPreview, err := gw.PreviewLabelRecompute(ctx, "location", all)
	if err != nil {
		t.Fatalf("preview locations: %v", err)
	}
	locChanges, err := gw.RecomputeLabels(ctx, "", "location", all, all)
	if err != nil {
		t.Fatalf("recompute locations: %v", err)
	}
	if !sameChangeSet(locPreview, locChanges) {
		t.Fatalf("the location preview and apply disagree:\n preview %+v\n applied %+v", locPreview, locChanges)
	}
	byKind := map[string]int{}
	for _, ch := range locChanges {
		byKind[ch.Kind]++
	}
	if byKind["location"] != 2 {
		t.Errorf("location recompute changed %d locations, want the building and the room: %+v", byKind["location"], locChanges)
	}
	if byKind["system"] != 1 {
		t.Errorf("location recompute changed %d systems, want the one whose LocationLabel just moved: %+v", byKind["system"], locChanges)
	}
	assertNoLabelDrift(t, gw, ctx)
}

// An unknown entity kind is refused by name rather than silently recomputing
// nothing, which is what a typo in a generated client would otherwise be.
func TestTheVerbRefusesAnUnknownKind(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.PreviewLabelRecompute(ctx, "node", all); err == nil {
		t.Fatalf("preview over an unknown kind succeeded, want a refusal")
	}
	if _, err := gw.RecomputeLabels(ctx, "", "node", all, all); err == nil {
		t.Fatalf("apply over an unknown kind succeeded, want a refusal")
	}
}

// --- scope --------------------------------------------------------------

// Scope is respected on both halves of the verb: a caller sees and changes
// only the rows inside their own read scope.
func TestTheVerbRespectsScope(t *testing.T) {
	gw, ctx := seededGateway(t)
	mine := makeRoom(t, gw, ctx, "room-mine")
	theirs := makeRoom(t, gw, ctx, "room-theirs")
	inMine, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &mine}, all)
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	inTheirs, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(qm55), LocationName: &theirs}, all)
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set rule: %v", err)
	}
	narrow := scope.Set{SelfIDs: []string{inMine.ID}}

	preview, err := gw.PreviewLabelRecompute(ctx, "component", narrow)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(preview) != 1 || preview[0].ID != inMine.ID {
		t.Fatalf("a scoped preview listed %+v, want only the one row in scope", preview)
	}
	applied, err := gw.RecomputeLabels(ctx, "", "component", narrow, narrow)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(applied) != 1 || applied[0].ID != inMine.ID {
		t.Fatalf("a scoped apply changed %+v, want only the one row in scope", applied)
	}
	if got := labelOf(t, gw, ctx, inTheirs.ID); got != "Display 1" {
		t.Fatalf("a row outside the caller's scope was restamped to %q", got)
	}

	// An empty scope reaches nothing at all rather than everything.
	if changes, err := gw.RecomputeLabels(ctx, "", "component", scope.Set{}, scope.Set{}); err != nil {
		t.Fatalf("empty-scope apply: %v", err)
	} else if len(changes) != 0 {
		t.Fatalf("an empty scope changed %d rows", len(changes))
	}

	// A caller who can READ a row but not UPDATE it changes nothing.
	readAll, updateNone := all, scope.Set{SelfIDs: []string{inMine.ID}}
	if changes, err := gw.RecomputeLabels(ctx, "", "component", readAll, updateNone); err != nil {
		t.Fatalf("read-wide update-narrow apply: %v", err)
	} else {
		for _, ch := range changes {
			if ch.ID == inTheirs.ID {
				t.Fatalf("a row outside the caller's UPDATE scope was changed")
			}
		}
	}
}

// --- helpers ------------------------------------------------------------

func labelOf(t *testing.T, gw *storage.PG, ctx context.Context, id string) string {
	t.Helper()
	c, err := gw.GetComponent(ctx, id, all)
	if err != nil {
		t.Fatalf("get component %s: %v", id, err)
	}
	return c.DisplayName
}

func sameChangeSet(a, b []storage.LabelChange) bool {
	if len(a) != len(b) {
		return false
	}
	index := make(map[string]storage.LabelChange, len(a))
	for _, ch := range a {
		index[ch.ID] = ch
	}
	for _, ch := range b {
		other, ok := index[ch.ID]
		if !ok || other.From != ch.From || other.To != ch.To {
			return false
		}
	}
	return true
}

// assertNoLabelDrift asserts the recompute-and-compare invariant through the
// gateway's own preview: a preview that lists nothing is the statement that
// every stored label already equals what its rule produces.
func assertNoLabelDrift(t *testing.T, gw *storage.PG, ctx context.Context) {
	t.Helper()
	for _, kind := range []string{"component", "system", "location"} {
		drift, err := gw.PreviewLabelRecompute(ctx, kind, all)
		if err != nil {
			t.Fatalf("preview %s: %v", kind, err)
		}
		if len(drift) != 0 {
			t.Errorf("%s labels are stale after the acts above: %+v", kind, drift)
		}
	}
}

// --- the invariant ------------------------------------------------------

// The recompute-and-compare invariant, over an estate that has been PUT
// THROUGH every act this slice knows of, on all three tiers, with rules that
// read placement on two of them.
//
// It is deliberately not an enumeration of write paths the way slice 2's was.
// An enumeration is only as good as the argument that it is complete, and this
// slice is here because that argument turned out to be false. This test makes
// no such argument: it drives the estate and then asks the gateway itself
// whether any stored label disagrees with what its rules produce. A write path
// nobody thought of fails it, which is the property an enumeration cannot have.
func TestNoActLeavesALabelStaleAnywhere(t *testing.T) {
	gw, ctx := seededGateway(t)
	// Rules that read everything a rule can read about placement, so an act
	// that fails to restamp is a visible mismatch rather than a coincidence.
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.SystemTypeLabel}}/{{.LocationLabel}}/{{.TypeName}}/{{.Name}}/{{if .Ordinal}}{{.Ordinal}}{{else}}-{{end}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}}/{{.TypeName}}/{{.Name}}"); err != nil {
		t.Fatalf("system rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "location", "{{.TypeName}} {{.Name | upper}}"); err != nil {
		t.Fatalf("location rule: %v", err)
	}

	roomA := makeRoomWithLabel(t, gw, ctx, "room-a", "204A")
	roomB := makeRoom(t, gw, ctx, "room-b")
	roomC := makeRoom(t, gw, ctx, "room-c")

	board, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-board", SystemTypeID: strptr("board"), LocationName: &roomA.Name}, all)
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	huddle, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-huddle", SystemTypeID: strptr("huddle"), LocationName: &roomB}, all)
	if err != nil {
		t.Fatalf("create huddle: %v", err)
	}
	mover, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-mover", SystemTypeID: strptr("class"), LocationName: &roomA.Name}, all)
	if err != nil {
		t.Fatalf("create mover: %v", err)
	}

	var comps []string
	for _, spec := range []storage.ComponentSpec{
		{ProductName: strptr(qm55), LocationName: &roomA.Name, SystemName: &board.Name},  // 0 stays put
		{ProductName: strptr(qm55), LocationName: &roomA.Name, SystemName: &board.Name},  // 1 moves rooms
		{ProductName: strptr(qm55), LocationName: &roomB, SystemName: &huddle.Name},      // 2 re-defaulted
		{ProductName: strptr(qm55), LocationName: &roomB},                                // 3 bound later
		{ProductName: strptr("shure-mxa920"), LocationName: &roomC},                      // 4 renamed then reset
		{ProductName: strptr(qm55), LocationName: &roomC, DisplayName: "Operator's Own"}, // 5 the pen stays theirs
		{ProductName: strptr(qm55), LocationName: &roomC, SystemName: &mover.Name},       // 6 its system moves
	} {
		c, err := gw.CreateComponent(ctx, "", spec, all)
		if err != nil {
			t.Fatalf("create %+v: %v", spec, err)
		}
		comps = append(comps, c.ID)
	}

	// Every act, in an order that makes each one actually move something.
	acts := []struct {
		what string
		run  func() error
	}{
		{"rename the location the labels read", func() error {
			_, err := gw.RenameLocation(ctx, "", roomA.Name, "room-204a", all, all)
			return err
		}},
		{"relabel that location", func() error {
			_, err := gw.UpdateLocation(ctx, "", "room-204a", storage.LocationPatch{DisplayName: strptr("204A West")}, all, all)
			return err
		}},
		{"clear that label so the ladder falls through to the name", func() error {
			_, err := gw.UpdateLocation(ctx, "", "room-204a", storage.LocationPatch{DisplayName: strptr("")}, all, all)
			return err
		}},
		{"reclassify a location", func() error {
			_, err := gw.UpdateLocation(ctx, "", roomC, storage.LocationPatch{LocationType: strptr("floor")}, all, all)
			return err
		}},
		{"move a location", func() error {
			_, err := gw.MoveLocation(ctx, "", roomB, storage.LocationMove{ParentName: strptr("hq")}, all, all)
			return err
		}},
		{"move a component to another room", func() error {
			_, err := gw.MoveComponent(ctx, "", comps[1], storage.ComponentMove{LocationName: &roomB}, all, all)
			return err
		}},
		{"rename a component", func() error {
			_, err := gw.RenameComponent(ctx, "", comps[4], "hand-mic", all, all)
			return err
		}},
		{"reset a component name", func() error {
			_, err := gw.ResetComponentName(ctx, "", comps[4], all, all)
			return err
		}},
		{"reclassify a component", func() error {
			_, err := gw.UpdateComponent(ctx, "", comps[0], storage.ComponentPatch{ProductName: strptr("shure-mxa920")}, all, all)
			return err
		}},
		{"bind a component to a system", func() error {
			return gw.AddMember(ctx, "", huddle.Name, comps[3], all)
		}},
		{"bind a second system and re-default to it", func() error {
			if err := gw.AddMember(ctx, "", board.Name, comps[2], all); err != nil {
				return err
			}
			return gw.SetPrimaryMember(ctx, "", board.Name, comps[2], all)
		}},
		{"unbind the default", func() error {
			return gw.RemoveMember(ctx, "", board.Name, comps[2], all)
		}},
		{"reclassify a system", func() error {
			_, err := gw.UpdateSystem(ctx, "", huddle.ID, storage.SystemPatch{SystemTypeID: strptr("training")}, all, all)
			return err
		}},
		{"move a system to another room", func() error {
			_, err := gw.MoveSystem(ctx, "", mover.ID, storage.SystemMove{LocationName: &roomB}, all, all)
			return err
		}},
		{"rename a system", func() error {
			_, err := gw.RenameSystem(ctx, "", board.ID, "sys-board-renamed", all, all)
			return err
		}},
		// A delete belongs in this loop for the same reason every other act
		// does, and its absence is the hole this test's own claim (a write path
		// nobody thought of fails it) could not cover: the claim only reaches
		// acts the fixture performs. Deleting a system cascades its memberships
		// away in the database, which moves what its members read for
		// .SystemTypeLabel with no membership call anywhere in the gateway.
		{"delete a system its members are bound to", func() error {
			return gw.DeleteSystem(ctx, "", huddle.ID, all, all)
		}},
	}
	for _, act := range acts {
		if err := act.run(); err != nil {
			t.Fatalf("%s: %v", act.what, err)
		}
		for _, kind := range []string{"component", "system", "location"} {
			drift, err := gw.PreviewLabelRecompute(ctx, kind, all)
			if err != nil {
				t.Fatalf("preview %s after %q: %v", kind, act.what, err)
			}
			if len(drift) != 0 {
				t.Fatalf("after %q, %d %s label(s) disagree with their rules: %+v", act.what, len(drift), kind, drift)
			}
		}
	}

	// The operator's own label is untouched by all of it, and the invariant
	// above must not be passing because nothing is platform-owned.
	if got := labelOf(t, gw, ctx, comps[5]); got != "Operator's Own" {
		t.Errorf("the operator's label became %q", got)
	}
	owned := 0
	cs, err := gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, c := range cs {
		if c.DisplayNameGenerated {
			owned++
		}
	}
	if owned < 6 {
		t.Fatalf("only %d component labels are platform-owned, so the invariant above checked too little", owned)
	}
}

// Staffing a role IS membership (AssignRole binds the component into the system
// on the operator's behalf), so it is a label write path with no membership
// call in it. It gets its own test rather than a line in the invariant loop,
// because a role assignment needs a standard, and the fixture that gives it one
// would drown the loop it sits in.
func TestStaffingARoleRestampsTheComponentItBinds(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "[{{.SystemTypeLabel}}]"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	room := makeRoom(t, gw, ctx, "room-a")
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "sys-a", SystemTypeID: strptr("board"), StandardID: strptr("huddle-room"), LocationName: &room,
	}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr("cisco-room-bar"), LocationName: &room,
	}, all)
	if err != nil {
		t.Fatalf("create video bar: %v", err)
	}
	if bar.DisplayName != "[]" {
		t.Fatalf("an unbound component reads %q, want %q", bar.DisplayName, "[]")
	}
	if err := gw.AssignRole(ctx, "", s.Name, "conf-bar", bar.ID, all); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	if got := labelOf(t, gw, ctx, bar.ID); got != "[Boardroom]" {
		t.Fatalf("after staffing a role = %q, want %q: the implicit membership is a label write path too", got, "[Boardroom]")
	}
	assertNoLabelDrift(t, gw, ctx)
}

// Deleting a system is the membership write path with no membership call in it.
// system_member_system_id_fkey is ON DELETE CASCADE, so the delete takes every
// membership with it, and a component whose PRIMARY membership was one of them
// reads a different .SystemTypeLabel from that instant. Slice 5 derived its
// write paths from the data map and caught every EXPLICIT mover of a primary
// membership (AddMember, RemoveMember, SetPrimaryMember, AssignRole); this is
// the implicit one the database performs on the gateway's behalf.
func TestDeletingASystemRestampsItsMembers(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "[{{.SystemTypeLabel}}]"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	room := makeRoom(t, gw, ctx, "room-a")
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "sys-a", SystemTypeID: strptr("board"), LocationName: &room,
	}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room, SystemName: &s.Name,
	}, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if c.DisplayName != "[Boardroom]" {
		t.Fatalf("a bound component reads %q, want %q", c.DisplayName, "[Boardroom]")
	}
	if err := gw.DeleteSystem(ctx, "", s.ID, all, all); err != nil {
		t.Fatalf("delete system: %v", err)
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "[]" {
		t.Fatalf("after deleting the system its member reads %q, want %q: the cascade took the membership and nothing restamped the label", got, "[]")
	}
	assertNoLabelDrift(t, gw, ctx)
}

// The second half of the same act: a component with two memberships loses the
// PRIMARY one to the delete, and the sole survivor has to become the default,
// exactly as it does when the operator unbinds that membership by hand
// (RemoveMember calls promoteSolePrimary; the delete reached the same table by
// a different door and did not). The label is what makes the divergence visible
// rather than theoretical: with no default at all a rule reading
// .SystemTypeLabel renders empty for a component that is still in a system.
func TestDeletingASystemPromotesTheSoleSurvivingMembership(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "[{{.SystemTypeLabel}}]"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	room := makeRoom(t, gw, ctx, "room-a")
	board, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "sys-board", SystemTypeID: strptr("board"), LocationName: &room,
	}, all)
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	huddle, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "sys-huddle", SystemTypeID: strptr("huddle"), LocationName: &room,
	}, all)
	if err != nil {
		t.Fatalf("create huddle: %v", err)
	}
	// The first membership becomes the default with nobody asking, so the
	// board is primary and the huddle is the ordinary second.
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(qm55), LocationName: &room, SystemName: &board.Name,
	}, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AddMember(ctx, "", huddle.Name, c.ID, all); err != nil {
		t.Fatalf("add the second membership: %v", err)
	}
	if err := gw.DeleteSystem(ctx, "", board.ID, all, all); err != nil {
		t.Fatalf("delete the primary's system: %v", err)
	}
	ms, err := gw.ComponentMemberships(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("read memberships: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("memberships after the delete = %d, want 1", len(ms))
	}
	if !ms[0].IsPrimary {
		t.Fatalf("the sole surviving membership is not the default: a component with one system carries an unanswered question")
	}
	if got := labelOf(t, gw, ctx, c.ID); got != "[Huddle Room]" {
		t.Fatalf("after the delete the component reads %q, want %q", got, "[Huddle Room]")
	}
	assertNoLabelDrift(t, gw, ctx)
}
