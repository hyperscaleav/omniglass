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

// TestEffectiveCapabilities proves the resolved set a component provides: its
// product's capabilities, plus what the component adds, minus what it suppresses.
// The productless case is the one that matters most, because it is what lets a
// component without a product still be staffed under the strict assignment guard.
func TestEffectiveCapabilities(t *testing.T) {
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

	// cisco-room-bar declares microphone, speaker, camera, codec.
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	got, err := gw.EffectiveCapabilities(ctx, conn, "bar-1")
	if err != nil {
		t.Fatalf("effective capabilities: %v", err)
	}
	if !hasAll(got, "microphone", "speaker", "camera", "codec") {
		t.Fatalf("product capabilities = %v, want the room bar's four", got)
	}

	// The component adds one its product does not claim, and suppresses one it does.
	if _, err := conn.Exec(ctx, `insert into component_capability (component_id, capability_id, present)
		select id, 'touch-panel', true from component where name = 'bar-1'
		union all
		select id, 'camera', false from component where name = 'bar-1'`); err != nil {
		t.Fatalf("declare component capabilities: %v", err)
	}
	got, _ = gw.EffectiveCapabilities(ctx, conn, "bar-1")
	if !hasAll(got, "touch-panel") {
		t.Fatalf("added capability missing: %v", got)
	}
	if hasAll(got, "camera") {
		t.Fatalf("suppressed capability still present: %v", got)
	}

	// A productless component provides exactly what it declares, and nothing else.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "loose-mic"}, all); err != nil {
		t.Fatalf("create productless: %v", err)
	}
	if got, _ = gw.EffectiveCapabilities(ctx, conn, "loose-mic"); len(got) != 0 {
		t.Fatalf("productless with no declarations = %v, want empty", got)
	}
	if _, err := conn.Exec(ctx, `insert into component_capability (component_id, capability_id)
		select id, 'microphone' from component where name = 'loose-mic'`); err != nil {
		t.Fatalf("declare on productless: %v", err)
	}
	if got, _ = gw.EffectiveCapabilities(ctx, conn, "loose-mic"); !hasAll(got, "microphone") || len(got) != 1 {
		t.Fatalf("productless capabilities = %v, want exactly [microphone]", got)
	}
}

// TestEffectiveRolesAndAssignment proves roles resolve from both arcs (inherited
// from the standard, ad-hoc on the system), that staffing is visible, and that the
// assignment guard refuses a component that does not provide what the role needs,
// naming the gap.
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
	// wants a table mic (microphone + speaker, quorum 2); the system itself also
	// declares an ad-hoc display role.
	if err := gw.UpsertStandard(ctx, storage.Standard{ID: "test-huddle", DisplayName: "Test Huddle"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	std := "test-huddle"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hq-huddle", StandardID: &std}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	var micRole string
	if err := conn.QueryRow(ctx, `
		insert into system_role (owner_kind, standard_id, name, display_name, quorum)
		values ('standard','test-huddle','table-mic','Table microphone',2)
		returning id`).Scan(&micRole); err != nil {
		t.Fatalf("declare standard role: %v", err)
	}
	// Scoped to the role id this test just created: matching on name alone would
	// reach across owners and constrain what any other standard may declare.
	if _, err := conn.Exec(ctx, `
		insert into role_capability (role_id, capability_id)
		select $1, c from unnest(array['microphone','speaker']) c`, micRole); err != nil {
		t.Fatalf("require capabilities: %v", err)
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

	// A room bar provides microphone and speaker, so it can fill the mic role.
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

	// A display provides neither microphone nor speaker: refused, and the refusal
	// names exactly what is missing so the operator can act on it.
	qm := "samsung-qm55"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1", ProductName: &qm}, all); err != nil {
		t.Fatalf("create panel: %v", err)
	}
	err = gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "panel-1", all)
	var short *storage.CapabilityShortfall
	if !errors.As(err, &short) {
		t.Fatalf("assign a non-satisfying component: err = %v, want CapabilityShortfall", err)
	}
	if !hasAll(short.Missing, "microphone", "speaker") {
		t.Fatalf("shortfall missing = %v, want both microphone and speaker named", short.Missing)
	}

	// The decision this whole capability model exists for: a PRODUCTLESS component
	// that declares its own capabilities can be staffed, so strict refusal does not
	// lock out a component just because it has no product.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "loose-mic"}, all); err != nil {
		t.Fatalf("create productless: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into component_capability (component_id, capability_id)
		select id, 'microphone' from component where name = 'loose-mic'
		union all
		select id, 'speaker' from component where name = 'loose-mic'`); err != nil {
		t.Fatalf("declare capabilities: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "loose-mic", all); err != nil {
		t.Fatalf("assign a productless component that declares what the role needs: %v", err)
	}
	roles, _ = gw.EffectiveRoles(ctx, "hq-huddle", all)
	for _, r := range roles {
		if r.Name == "table-mic" && (r.Assigned() != 2 || r.Understaffed() != 0) {
			t.Fatalf("after staffing to quorum: %+v, want 2 assigned and fully staffed", r)
		}
	}

	// Unassign removes it; unassigning again is an explicit miss.
	if err := gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "loose-mic", all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "loose-mic", all); !errors.Is(err, storage.ErrAssignmentMissing) {
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

	// A component that does not exist is a not-found, NOT a capability shortfall.
	// An absent component resolves to an empty capability set, so without an
	// existence check the operator gets "missing microphone, speaker" for what is
	// really a typo.
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
