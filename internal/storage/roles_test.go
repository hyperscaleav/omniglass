package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestEffectiveRolesAndAssignment proves roles resolve from both arcs (inherited
// from the standard, ad-hoc on the system), that staffing is visible, and that the
// typed-slot guard refuses a component whose product's component_type is not
// within an accepted type's subtree, naming both what the component is and what
// the role wants.
func TestEffectiveRolesAndAssignment(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// The test owns its own standard rather than piggybacking on a seeded one, so
	// what the boot seed happens to declare cannot change what this asserts. It
	// wants a table mic (a video-bar, quorum 2); the system itself also
	// declares an ad-hoc display role.
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "test-huddle", DisplayName: "Test Huddle"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	std := "test-huddle"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hq-huddle", StandardID: &std}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	var micRole string
	if err := conn.QueryRow(ctx, `
		insert into system_role (owner_kind, standard_id, name, display_name, quorum)
		values ('standard',(select id from standard where name = 'test-huddle'),'table-mic','Table microphone',2)
		returning id`).Scan(&micRole); err != nil {
		t.Fatalf("declare standard role: %v", err)
	}
	// Scoped to the role id this test just created: matching on name alone would
	// reach across owners and constrain what any other standard may declare.
	if _, err := conn.Exec(ctx, `
		insert into system_role_type (role_id, component_type_id)
		select $1, id from component_type where name = 'video-bar'`, micRole); err != nil {
		t.Fatalf("require accepted type: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into system_role (owner_kind, system_id, name, display_name)
		select 'system', id, 'wall-display', 'Wall display' from system where name = 'hq-huddle'`); err != nil {
		t.Fatalf("declare ad-hoc role: %v", err)
	}

	roles, err := gw.EffectiveRoles(ctx, "hq-huddle", all)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("roles = %d, want the inherited one plus the ad-hoc one: %+v", len(roles), roles)
	}
	byRole := map[string]storage.EffectiveRole{}
	for _, r := range roles {
		byRole[r.Name] = r
	}
	if mic := byRole["table-mic"]; !mic.FromStandard || mic.Quorum != 2 || mic.Understaffed() != 2 {
		t.Fatalf("table-mic = %+v, want inherited, quorum 2, understaffed 2", mic)
	}
	if disp := byRole["wall-display"]; disp.FromStandard {
		t.Fatalf("wall-display should be ad-hoc, got FromStandard=true")
	}

	// A room bar is classified video-bar, so it fills the table-mic slot.
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, all); err != nil {
		t.Fatalf("create bar: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", all); err != nil {
		t.Fatalf("assign a satisfying component: %v", err)
	}
	roles, _ = gw.EffectiveRoles(ctx, "hq-huddle", all)
	for _, r := range roles {
		if r.Name == "table-mic" {
			if r.Assigned() != 1 || r.Understaffed() != 1 {
				t.Fatalf("after one assignment: %+v, want 1 assigned and still 1 short of quorum", r)
			}
		}
	}
	// Idempotent: assigning the same component again does not duplicate.
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", all); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	roles, _ = gw.EffectiveRoles(ctx, "hq-huddle", all)
	for _, r := range roles {
		if r.Name == "table-mic" && r.Assigned() != 1 {
			t.Fatalf("re-assign duplicated: %d assigned", r.Assigned())
		}
	}

	// A display is not within the video-bar subtree: refused, and the refusal
	// names both what the component is and what the role wants.
	qm := "samsung-qm55"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1", ProductName: &qm}, all); err != nil {
		t.Fatalf("create panel: %v", err)
	}
	err = gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "panel-1", all)
	var short *storage.TypeShortfall
	if !errors.As(err, &short) {
		t.Fatalf("assign a non-satisfying component: err = %v, want TypeShortfall", err)
	}
	if short.Component != "panel-1" || short.ComponentType != "display" || short.Role != "Table microphone" || !hasAll(short.WantTypes, "video-bar") {
		t.Fatalf("shortfall = %+v, want panel-1/display refused against role Table microphone wanting video-bar", short)
	}

	// A second video-bar reaches quorum.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-2", ProductName: &bar}, all); err != nil {
		t.Fatalf("create second bar: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-2", all); err != nil {
		t.Fatalf("assign a second satisfying component: %v", err)
	}
	roles, _ = gw.EffectiveRoles(ctx, "hq-huddle", all)
	for _, r := range roles {
		if r.Name == "table-mic" && (r.Assigned() != 2 || r.Understaffed() != 0) {
			t.Fatalf("after staffing to quorum: %+v, want 2 assigned and fully staffed", r)
		}
	}

	// Unassign removes it; unassigning again is an explicit miss.
	if err := gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "bar-2", all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "bar-2", all); !errors.Is(err, storage.ErrAssignmentMissing) {
		t.Fatalf("unassign twice: err = %v, want ErrAssignmentMissing", err)
	}

	// A component staffing a role cannot be deleted out from under the system. The
	// refusal is a conflict (409), not an opaque server error: the restrict FK fires
	// from outside the structural child check, so without mapping it this is a 500.
	err = gw.DeleteComponent(ctx, "", "bar-1", all, all)
	if !errors.Is(err, storage.ErrReferenced) {
		t.Fatalf("delete a component staffing a role: err = %v, want ErrReferenced", err)
	}
	// It must not blame child components. This component has none, so an operator
	// told that goes looking for something that is not there. The delete path cannot
	// tell which reference stopped it, so it says that it is referenced and no more.
	if strings.Contains(err.Error(), "child") {
		t.Fatalf("refusal blames children it cannot know about: %q", err)
	}
	// Unassigned, it deletes cleanly.
	if err := gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", all); err != nil {
		t.Fatalf("unassign before delete: %v", err)
	}
	if err := gw.DeleteComponent(ctx, "", "bar-1", all, all); err != nil {
		t.Fatalf("delete an unassigned component: %v", err)
	}

	// A component that does not exist is a not-found, NOT a type shortfall. An
	// absent component has no product to classify, so without an existence
	// check the operator gets a confusing type refusal for what is really a
	// typo.
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "no-such-component", all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("assign an unknown component: err = %v, want ErrComponentNotFound", err)
	}

	// An unknown role on a real system is a clear not-found, not a silent no-op.
	if err := gw.AssignRole(ctx, "", "hq-huddle", "no-such-role", "bar-1", all); !errors.Is(err, storage.ErrRoleNotFound) {
		t.Fatalf("assign to unknown role: err = %v, want ErrRoleNotFound", err)
	}

	// A one-off system sees only what it declares itself.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "one-off"}, all); err != nil {
		t.Fatalf("create one-off: %v", err)
	}
	if got, _ := gw.EffectiveRoles(ctx, "one-off", all); len(got) != 0 {
		t.Fatalf("one-off system roles = %+v, want none (it conforms to no standard)", got)
	}
}

// typedSlotSystem creates a fresh standalone system for a typed-slot guard
// test, isolated from whatever the boot seed happens to declare.
func typedSlotSystem(t *testing.T, ctx context.Context, gw storage.Gateway, all scope.Set, name string) {
	t.Helper()
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: name}, all); err != nil {
		t.Fatalf("create system %s: %v", name, err)
	}
}

// TestAssignRefusesWrongType proves the #626 guard's headline refusal: a
// component classified outside every type a role accepts is refused with a
// 422-shaped error naming both parties in operator vocabulary, exactly the
// touch-panel-into-a-display-slot example from the task brief.
func TestAssignRefusesWrongType(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	typedSlotSystem(t, ctx, gw, all, "wrong-type-sys")

	if _, err := gw.SetSystemRole(ctx, "", "system", "wrong-type-sys", storage.SystemRoleSpec{
		Name: "display-left", DisplayName: "Display (Left)", AcceptedTypes: []string{"display"},
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	crestron := "crestron-tss-1070" // component_type: touch-panel
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1", ProductName: &crestron}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	err = gw.AssignRole(ctx, "", "wrong-type-sys", "display-left", "panel-1", all)
	var short *storage.TypeShortfall
	if !errors.As(err, &short) {
		t.Fatalf("assign a wrong-type component: err = %v, want TypeShortfall", err)
	}
	if short.Component != "panel-1" || short.ComponentType != "touch-panel" ||
		short.Role != "Display (Left)" || !hasAll(short.WantTypes, "display") {
		t.Fatalf("shortfall = %+v, want panel-1/touch-panel refused against role Display (Left) wanting display", short)
	}
}

// TestAssignAcceptsSubtype proves the guard walks the component_type subtree:
// a role that accepts "display" also accepts "interactive-display", one of
// its declared children, without the role naming the subtype itself.
func TestAssignAcceptsSubtype(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	typedSlotSystem(t, ctx, gw, all, "subtype-sys")

	if _, err := gw.SetSystemRole(ctx, "", "system", "subtype-sys", storage.SystemRoleSpec{
		Name: "display-left", DisplayName: "Display (Left)", AcceptedTypes: []string{"display"},
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "test-interactive-display", DisplayName: "Test Interactive Display",
		ComponentType: "interactive-display", Kind: "device",
	}); err != nil {
		t.Fatalf("create subtype product: %v", err)
	}
	sub := "test-interactive-display"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-2", ProductName: &sub}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	if err := gw.AssignRole(ctx, "", "subtype-sys", "display-left", "panel-2", all); err != nil {
		t.Fatalf("assign a subtype component: %v, want success (interactive-display is within display's subtree)", err)
	}
}

// TestAssignProductPin proves the second half of the guard: when a role pins
// specific products, a component of an otherwise-accepted type but a
// different product is refused naming the pinned products, and the pinned
// product itself is accepted.
func TestAssignProductPin(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	typedSlotSystem(t, ctx, gw, all, "pin-sys")

	if _, err := gw.SetSystemRole(ctx, "", "system", "pin-sys", storage.SystemRoleSpec{
		Name: "main-display", DisplayName: "Main Display",
		AcceptedTypes: []string{"display"}, PinnedProducts: []string{"samsung-qm55"},
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "other-display", DisplayName: "Other Display", ComponentType: "display", Kind: "device",
	}); err != nil {
		t.Fatalf("create other display product: %v", err)
	}
	other := "other-display"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-3", ProductName: &other}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	err = gw.AssignRole(ctx, "", "pin-sys", "main-display", "panel-3", all)
	var pinShort *storage.ProductPinShortfall
	if !errors.As(err, &pinShort) {
		t.Fatalf("assign right type, wrong product: err = %v, want ProductPinShortfall", err)
	}
	if pinShort.Component != "panel-3" || pinShort.ComponentProduct != "other-display" ||
		pinShort.Role != "Main Display" || !hasAll(pinShort.WantProducts, "samsung-qm55") {
		t.Fatalf("shortfall = %+v, want panel-3/other-display refused against role Main Display wanting samsung-qm55", pinShort)
	}

	qm := "samsung-qm55"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "qm-2", ProductName: &qm}, all); err != nil {
		t.Fatalf("create pinned-product component: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "pin-sys", "main-display", "qm-2", all); err != nil {
		t.Fatalf("assign the pinned product: %v, want success", err)
	}
}

func hasAll(got []string, want ...string) bool {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
