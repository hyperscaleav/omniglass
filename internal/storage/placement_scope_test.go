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
		Name: "panel-mine", ProductName: strptr(edge55), LocationName: &mine.Name,
	}, all, narrow, narrow, narrow)
	if err != nil {
		t.Fatalf("create into the readable room: %v", err)
	}
	if !strings.HasPrefix(c.Label, "Mine") {
		t.Fatalf("in-scope create stored %q, want the room's label in it", c.Label)
	}

	// The room it may not read is refused, and refused as the non-disclosing
	// not-found an absent room gives, never as a forbidden that would separate
	// "no such room" from "a room you cannot see".
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-theirs", ProductName: strptr(edge55), LocationName: &theirs.Name,
	}, all, narrow, narrow, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("create into the unreadable room = %v, want the non-disclosing ErrLocationNotFound", err)
	}
	// The system reference is the map's other placement fact, guarded by the
	// caller's system:read scope rather than its location one.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-sys", ProductName: strptr(edge55), SystemName: strptr("board-theirs"),
	}, all, narrow, narrow, narrow); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("create into the unreadable system = %v, want the non-disclosing ErrSystemNotFound", err)
	}

	// The wide caller runs both bodies and is served, with the labels the narrow
	// caller was refused: these are scope refusals, not a broken create.
	leaked, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-theirs", ProductName: strptr(edge55), LocationName: &theirs.Name,
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("wide create into the same room: %v", err)
	}
	if !strings.HasPrefix(leaked.Label, "Theirs") {
		t.Errorf("wide create stored %q, want the room's label: the refusal above proves nothing otherwise", leaked.Label)
	}
	leaked, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel-sys", ProductName: strptr(edge55), SystemName: strptr("board-theirs"),
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("wide create into the same system: %v", err)
	}
	if !strings.Contains(leaked.Label, "Boardroom") {
		t.Errorf("wide create stored %q, want the system type's label in it", leaked.Label)
	}
}

// TestTheSystemBindSaysWhichAuthorityIsMissing pins the OTHER half of the system
// bind's refusal (#707 review). Both halves refuse, so nothing about whether the
// create is served is at stake here; what is at stake is which of two opposite
// claims about the fleet the caller is handed.
//
// The pair this drives (system:read covering the row, system:update not) is not
// exotic, it is the ordinary shape today: applicableKinds("system") is
// {"system"} alone (internal/scope/scope.go) and the cross-tier expansion is
// unbuilt (#10), so a location-scoped deploy grant beside the all-scoped viewer
// floor a real principal carries resolves to exactly it. Answering
// ErrSystemNotFound there tells a caller that a row it can GET does not exist.
func TestTheSystemBindSaysWhichAuthorityIsMissing(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoomWithLabel(t, gw, ctx, "room-bind", "Bind")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-bind", SystemTypeID: strptr("board"), LocationName: &room.Name,
	}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	none := scope.Set{}
	spec := func(name string) storage.ComponentSpec {
		return storage.ComponentSpec{Name: name, ProductName: strptr(edge55), SystemName: strptr("board-bind")}
	}

	// Readable and not updatable: the refusal is about authority, and it must not
	// ALSO satisfy the not-found sentinel, or a mapper keyed on that one would go
	// on reporting the row as absent.
	_, err := gw.CreateComponent(ctx, "", spec("panel-authority"), all, all, all, none)
	if !errors.Is(err, storage.ErrSystemBindForbidden) {
		t.Errorf("bind of a readable system outside the update scope = %v, want ErrSystemBindForbidden", err)
	}
	if errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("bind of a readable system also matched ErrSystemNotFound: the refusal still denies the row's existence")
	}

	// Not readable either: unchanged, the non-disclosing not-found, because that
	// caller must not learn the row is there.
	if _, err := gw.CreateComponent(ctx, "", spec("panel-hidden"), all, all, none, none); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("bind of an unreadable system = %v, want the non-disclosing ErrSystemNotFound", err)
	}

	// Both sets covering it: served, and the membership is real, so the two
	// refusals above are scope boundaries rather than a broken bind.
	c, err := gw.CreateComponent(ctx, "", spec("panel-ok"), all, all, all, all)
	if err != nil {
		t.Fatalf("bind with both scopes: %v", err)
	}
	if c.PrimarySystem == nil || *c.PrimarySystem != "board-bind" {
		t.Errorf("created component's primary system = %v, want board-bind", c.PrimarySystem)
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
	if !strings.HasPrefix(s.Label, "Mine") {
		t.Fatalf("in-scope create stored %q, want the room's label in it", s.Label)
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
	if !strings.HasPrefix(leaked.Label, "Theirs") {
		t.Errorf("wide create stored %q, want the room's label: the refusal above proves nothing otherwise", leaked.Label)
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
		Name: "panel", ProductName: strptr(edge55), LocationName: &mine.Name,
	}, all, all, all, all); err != nil {
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
	if !strings.HasPrefix(moved.Label, "Theirs") {
		t.Errorf("wide move stored %q, want the destination's label: the refusal above proves nothing otherwise", moved.Label)
	}
	movedSys, err := gw.MoveSystem(ctx, "", "board", storage.SystemMove{LocationName: &theirs.Name}, all, all, all)
	if err != nil {
		t.Fatalf("wide move: %v", err)
	}
	if !strings.HasPrefix(movedSys.Label, "Theirs") {
		t.Errorf("wide move stored %q, want the destination's label: the refusal above proves nothing otherwise", movedSys.Label)
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
			ProductName: edge55, Name: "panel", LocationName: tc.location, SystemName: tc.system,
		}, all, narrow, narrow, narrow)
		spec := storage.ComponentSpec{Name: "panel", ProductName: strptr(edge55)}
		if tc.location != "" {
			spec.LocationName = &tc.location
		}
		if tc.system != "" {
			spec.SystemName = strptr(tc.system)
		}
		_, createErr := gw.CreateComponent(ctx, "", spec, all, narrow, narrow, narrow)
		if !errors.Is(draftErr, tc.want) || !errors.Is(createErr, tc.want) {
			t.Errorf("%s: draft = %v, create = %v, want both %v", tc.what, draftErr, createErr, tc.want)
		}
	}
}

// TestTheDraftResolvesTheSystemInTheScopeTheBindRequires is #713 at the
// gateway: the preview resolves the same reference the create binds, in the
// same set, so the two cannot disagree about a refusal.
//
// The pairing this drives is the ordinary one, not an exotic fixture:
// applicableKinds("system") is {"system"} alone and the cross-tier expansion is
// unbuilt (#10), so a location-scoped deploy grant beside the all-scoped viewer
// floor resolves to exactly "reads every system, updates none". Resolving the
// draft's system in the READ set alone served that caller a preview, assembled
// from the system's own type label, for a create the platform then refused.
func TestTheDraftResolvesTheSystemInTheScopeTheBindRequires(t *testing.T) {
	gw, ctx := seededGateway(t)
	// A rule that reads the system's type label, so the answer actually carries
	// the fact the refusal withholds.
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.SystemTypeLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("set the component rule: %v", err)
	}
	room := makeRoomWithLabel(t, gw, ctx, "room-draft-bind", "Draft Bind")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "board-draft-bind", SystemTypeID: strptr("board"), LocationName: &room.Name,
	}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	none := scope.Set{}
	draft := storage.ComponentLabelDraft{ProductName: edge55, Name: "panel", SystemName: "board-draft-bind"}
	spec := func(name string) storage.ComponentSpec {
		return storage.ComponentSpec{Name: name, ProductName: strptr(edge55), SystemName: strptr("board-draft-bind")}
	}

	// Readable and not updatable: the draft refuses by AUTHORITY, the same
	// sentinel the create raises, and must not also satisfy the not-found one.
	_, draftErr := gw.RenderComponentDraftLabel(ctx, draft, all, all, all, none)
	_, createErr := gw.CreateComponent(ctx, "", spec("panel-authority"), all, all, all, none)
	if !errors.Is(draftErr, storage.ErrSystemBindForbidden) || !errors.Is(createErr, storage.ErrSystemBindForbidden) {
		t.Errorf("readable system outside the update scope: draft = %v, create = %v, want both ErrSystemBindForbidden", draftErr, createErr)
	}
	if errors.Is(draftErr, storage.ErrSystemNotFound) {
		t.Errorf("the draft's refusal also matched ErrSystemNotFound: it still denies the row's existence")
	}

	// Not readable either: unchanged on both, the non-disclosing not-found,
	// because that caller must not learn the row is there.
	_, draftErr = gw.RenderComponentDraftLabel(ctx, draft, all, all, none, none)
	_, createErr = gw.CreateComponent(ctx, "", spec("panel-hidden"), all, all, none, none)
	if !errors.Is(draftErr, storage.ErrSystemNotFound) || !errors.Is(createErr, storage.ErrSystemNotFound) {
		t.Errorf("unreadable system: draft = %v, create = %v, want both the non-disclosing ErrSystemNotFound", draftErr, createErr)
	}

	// Both sets covering it: the draft renders, and what it renders is the label
	// the create then stores, so the refusals above are scope boundaries rather
	// than a draft that never worked.
	d, err := gw.RenderComponentDraftLabel(ctx, draft, all, all, all, all)
	if err != nil {
		t.Fatalf("draft with both scopes: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", spec("panel"), all, all, all, all)
	if err != nil {
		t.Fatalf("create with both scopes: %v", err)
	}
	if d.Label == "" || d.Label != c.Label {
		t.Errorf("drafted label %q, stored %q: the form promised a label the create did not write", d.Label, c.Label)
	}
	if !strings.Contains(d.Label, "Boardroom") {
		t.Errorf("drafted label %q does not carry the system type's label, so the refusals above withhold nothing", d.Label)
	}
}
