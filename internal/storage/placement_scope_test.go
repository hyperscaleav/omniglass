package storage_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A write resolves the placement it binds within the caller's READ scope on the
// referenced tier (#700), at the gateway seam that owns the rule.
//
// The disclosure these close is not the binding, it is the ANSWER: since
// ADR-0100 a component's label data map reads its location's label and its
// primary system's TYPE label, and a system's reads its location's, so a create
// or a move stamps those facts into a row it hands straight back. Resolved
// existence-only, that turns either verb into a read of a row the caller holds
// no grant to read, which is exactly what the draft render beside it refuses.
//
// Every test here drives a caller whose READ scope covers one room and not
// another while its create/move scope stays wide, which isolates the axis: the
// refusal cannot be explained by a missing write grant. The wide caller runs the
// same call alongside and is served the leaked label, so a broken gateway that
// refused everyone would fail these rather than pass them.

// TestAComponentCreateBindsOnlyAPlacementItCanRead covers both of a component's
// placement references, since both are label inputs and both were unscoped.
func TestAComponentCreateBindsOnlyAPlacementItCanRead(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.SystemTypeLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	theirs := makeRoomWithLabel(t, gw, ctx, "room-theirs", "Theirs")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-theirs", SystemTypeID: strptr("board"), LocationName: &theirs.Name,
	}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	narrow := scope.Set{IDs: []string{mine.ID}}

	// The room it may read binds, and the label carries that room's label: the
	// fixture is live.
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-mine", ProductName: strptr(qm55), LocationName: &mine.Name,
	}, all, narrow, narrow)
	if err != nil {
		t.Fatalf("create into the readable room: %v", err)
	}
	if !strings.HasPrefix(c.DisplayName, "Mine") {
		t.Fatalf("in-scope create stored %q, want the room's label in it", c.DisplayName)
	}

	// The room it may not read is refused, and refused as the non-disclosing
	// not-found an absent room gives, never as a forbidden that would separate
	// "no such room" from "a room you cannot see".
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-theirs", ProductName: strptr(qm55), LocationName: &theirs.Name,
	}, all, narrow, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("create into the unreadable room = %v, want the non-disclosing ErrLocationNotFound", err)
	}
	// The system reference is the map's other placement fact, guarded by the
	// caller's system:read scope rather than its location one.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-sys", ProductName: strptr(qm55), SystemName: strptr("board-theirs"),
	}, all, narrow, narrow); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("create into the unreadable system = %v, want the non-disclosing ErrSystemNotFound", err)
	}

	// The wide caller runs both bodies and is served, with the labels the narrow
	// caller was refused: these are scope refusals, not a broken create.
	leaked, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-theirs", ProductName: strptr(qm55), LocationName: &theirs.Name,
	}, all, all, all)
	if err != nil {
		t.Fatalf("wide create into the same room: %v", err)
	}
	if !strings.HasPrefix(leaked.DisplayName, "Theirs") {
		t.Errorf("wide create stored %q, want the room's label: the refusal above proves nothing otherwise", leaked.DisplayName)
	}
	leaked, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-sys", ProductName: strptr(qm55), SystemName: strptr("board-theirs"),
	}, all, all, all)
	if err != nil {
		t.Fatalf("wide create into the same system: %v", err)
	}
	if !strings.Contains(leaked.DisplayName, "Boardroom") {
		t.Errorf("wide create stored %q, want the system type's label in it", leaked.DisplayName)
	}
}

// TestASystemCreateBindsOnlyALocationItCanRead is the system tier of the same
// guard: a system's map carries one placement fact, its location's label.
func TestASystemCreateBindsOnlyALocationItCanRead(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the system rule: %v", err)
	}
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	theirs := makeRoomWithLabel(t, gw, ctx, "room-theirs", "Theirs")
	narrow := scope.Set{IDs: []string{mine.ID}}

	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-mine", SystemTypeID: strptr("board"), LocationName: &mine.Name,
	}, all, narrow)
	if err != nil {
		t.Fatalf("create into the readable room: %v", err)
	}
	if !strings.HasPrefix(s.DisplayName, "Mine") {
		t.Fatalf("in-scope create stored %q, want the room's label in it", s.DisplayName)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-theirs", SystemTypeID: strptr("board"), LocationName: &theirs.Name,
	}, all, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("create into the unreadable room = %v, want the non-disclosing ErrLocationNotFound", err)
	}
	leaked, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-theirs", SystemTypeID: strptr("board"), LocationName: &theirs.Name,
	}, all, all)
	if err != nil {
		t.Fatalf("wide create into the same room: %v", err)
	}
	if !strings.HasPrefix(leaked.DisplayName, "Theirs") {
		t.Errorf("wide create stored %q, want the room's label: the refusal above proves nothing otherwise", leaked.DisplayName)
	}
}

// TestAMoveBindsOnlyADestinationItCanRead is the same guard on the other verb,
// on both tiers. A relocate restamps the label from the DESTINATION, so an
// unguarded move discloses a room's label exactly as an unguarded create does.
func TestAMoveBindsOnlyADestinationItCanRead(t *testing.T) {
	gw, ctx := seededGateway(t)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the system rule: %v", err)
	}
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	theirs := makeRoomWithLabel(t, gw, ctx, "room-theirs", "Theirs")
	narrow := scope.Set{IDs: []string{mine.ID}}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel", ProductName: strptr(qm55), LocationName: &mine.Name,
	}, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board", SystemTypeID: strptr("board"), LocationName: &mine.Name,
	}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	if _, err := gw.MoveComponent(ctx, "", "panel", storage.ComponentMove{
		LocationName: &theirs.Name,
	}, all, all, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("move to the unreadable room = %v, want the non-disclosing ErrLocationNotFound", err)
	}
	if _, err := gw.MoveSystem(ctx, "", "board", storage.SystemMove{
		LocationName: &theirs.Name,
	}, all, all, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("move to the unreadable room = %v, want the non-disclosing ErrLocationNotFound", err)
	}

	// The readable room still accepts both, and the wide caller still reaches
	// the other one, carrying its label.
	if _, err := gw.MoveComponent(ctx, "", "panel", storage.ComponentMove{
		LocationName: &mine.Name,
	}, all, all, narrow); err != nil {
		t.Errorf("move within the readable room = %v, want ok", err)
	}
	moved, err := gw.MoveComponent(ctx, "", "panel", storage.ComponentMove{LocationName: &theirs.Name}, all, all, all)
	if err != nil {
		t.Fatalf("wide move: %v", err)
	}
	if !strings.HasPrefix(moved.DisplayName, "Theirs") {
		t.Errorf("wide move stored %q, want the destination's label: the refusal above proves nothing otherwise", moved.DisplayName)
	}
	movedSys, err := gw.MoveSystem(ctx, "", "board", storage.SystemMove{LocationName: &theirs.Name}, all, all, all)
	if err != nil {
		t.Fatalf("wide move: %v", err)
	}
	if !strings.HasPrefix(movedSys.DisplayName, "Theirs") {
		t.Errorf("wide move stored %q, want the destination's label: the refusal above proves nothing otherwise", movedSys.DisplayName)
	}
}

// TestTheDraftAndTheCreateRefuseTheSamePlacement is the acceptance the issue was
// filed for: the console's preview and the create it previews, asked the same
// question by the same caller, must give the same answer. Before this they did
// not, and the create was the lenient one.
func TestTheDraftAndTheCreateRefuseTheSamePlacement(t *testing.T) {
	gw, ctx := seededGateway(t)
	mine := makeRoomWithLabel(t, gw, ctx, "room-mine", "Mine")
	theirs := makeRoomWithLabel(t, gw, ctx, "room-theirs", "Theirs")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-theirs", SystemTypeID: strptr("board"), LocationName: &theirs.Name,
	}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	narrow := scope.Set{IDs: []string{mine.ID}}

	for _, tc := range []struct {
		what     string
		location string
		system   string
		want     error
	}{
		{what: "an unreadable location", location: theirs.Name, want: storage.ErrLocationNotFound},
		{what: "an unreadable system", system: "board-theirs", want: storage.ErrSystemNotFound},
	} {
		_, draftErr := gw.RenderComponentDraftLabel(ctx, storage.ComponentLabelDraft{
			ProductName: qm55, Name: "panel", LocationName: tc.location, SystemName: tc.system,
		}, narrow, narrow)
		spec := storage.ComponentSpec{Name: "panel", ProductName: strptr(qm55)}
		if tc.location != "" {
			spec.LocationName = &tc.location
		}
		if tc.system != "" {
			spec.SystemName = strptr(tc.system)
		}
		_, createErr := gw.CreateComponent(ctx, "", spec, all, narrow, narrow)
		if !errors.Is(draftErr, tc.want) || !errors.Is(createErr, tc.want) {
			t.Errorf("%s: draft = %v, create = %v, want both %v", tc.what, draftErr, createErr, tc.want)
		}
	}
}
