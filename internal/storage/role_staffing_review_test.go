package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The adversarial review of the role staffing slice (#504) found each of these
// by driving the real gateway against a real Postgres; each is the regression
// for one finding.

// isDeadlock reports whether err is Postgres aborting a transaction to break a
// lock cycle.
func isDeadlock(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "40P01"
}

// waitForWaiters polls pg_locks from inside tx until at least n lock requests
// are waiting, or done reports the write under test finished without waiting
// (which is how the defect shows), whichever comes first.
func waitForWaiters(t *testing.T, ctx context.Context, tx pgx.Tx, n int, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if done != nil && done() {
			return
		}
		var c int
		if err := tx.QueryRow(ctx, `select count(*) from pg_locks where not granted`).Scan(&c); err != nil {
			t.Fatal(err)
		}
		if c >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never saw %d lock waiter(s)", n)
}

// TestStaffingNeverDeadlocksALabelCascade: a label cascade takes the label lock
// and then the system rows FOR UPDATE; a staffing write takes the label lock
// too (staffing re-stamps the component's label). The staffing write must not
// hold the system's row while it waits for the label lock, or the two wait on
// each other. The cascade is held open on a raw connection.
func TestStaffingNeverDeadlocksALabelCascade(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	if err := f.gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	label, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = label.Rollback(ctx) }()
	if _, err := label.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('label/recompute', 0))`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- f.gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all, f.all, f.all)
	}()
	waitForWaiters(t, ctx, label, 1, nil)
	_, lerr := label.Exec(ctx, `select id from system where location_id = (select id from location where name = 'hq-r1') order by id for update`)
	if lerr == nil {
		lerr = label.Commit(ctx)
	}
	aerr := <-done
	if isDeadlock(lerr) || isDeadlock(aerr) {
		t.Fatalf("a staffing write and a label cascade deadlocked: cascade %v, assign %v", lerr, aerr)
	}
	if lerr != nil || aerr != nil {
		t.Fatalf("cascade %v, assign %v", lerr, aerr)
	}
}

// TestSeedRepairLeavesAnOperatorDeclaredRoleAlone: a role an operator declared
// on a shipped standard, never revised and accepting any type, carries the same
// timestamps an interrupted seed leaves. A boot whose shipped YAML declares the
// same name must not fill its empty set: only a role the seed itself wrote is
// the seed's to repair.
func TestSeedRepairLeavesAnOperatorDeclaredRoleAlone(t *testing.T) {
	r := newSeedRig(t)
	if _, err := r.gw.SetSystemRole(r.ctx, "", "standard", "seed-std", storage.SystemRoleSpec{Name: "op-role", Label: "Operator role", Quorum: 1}); err != nil {
		t.Fatalf("the operator declares: %v", err)
	}
	var id string
	if err := r.conn.QueryRow(r.ctx, `select id from system_role where name = 'op-role'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := r.seedRole(storage.SystemRoleSpec{Name: "op-role", AcceptedTypes: []string{"display"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if types, products := r.slot(id); len(types) != 0 || len(products) != 0 {
		t.Fatalf("the operator's role accepts %v %v after a boot, want any type as they declared it", types, products)
	}
}

// TestRoleWithdrawalWaitsForAStandardChange: a standard change locks the
// stranded assignments in position order, and a role withdrawal's cascade
// deletes the same rows in heap order; after a swap the two disagree. The
// withdrawal must serialize on the role-name lock every other declaration
// write takes, so it waits for the change instead of deadlocking with it.
func TestRoleWithdrawalWaitsForAStandardChange(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	capacity := 3
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", "health-huddle", storage.SystemRoleSpec{
		Name: "table-mic", Label: "Table microphone", Quorum: 1, Capacity: &capacity, AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	}); err != nil {
		t.Fatal(err)
	}
	f.mustBar(t, ctx, "bar-2")
	if err := f.gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-2", f.all, f.all, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.SwapPositions(ctx, "", "hq-huddle", "table-mic", 1, 2, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	f.declareRole(t, ctx, "huddle-next", storage.SystemRoleSpec{Name: "other", Label: "Other", Quorum: 1})
	var lowest string
	if err := f.conn.QueryRow(ctx, `select id from system_role_assignment where position = 1`).Scan(&lowest); err != nil {
		t.Fatal(err)
	}
	// A third transaction holds the row the change locks first (a component
	// delete's key-share check can, in production).
	z, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Rollback(ctx) }()
	if _, err := z.Exec(ctx, `select 1 from system_role_assignment where id = $1 for update`, lowest); err != nil {
		t.Fatal(err)
	}
	next := "huddle-next"
	change := make(chan error, 1)
	go func() {
		_, err := f.gw.UpdateSystem(ctx, "", "hq-huddle", storage.SystemPatch{StandardID: &next}, f.all, f.all)
		change <- err
	}()
	waitForWaiters(t, ctx, z, 1, nil)
	withdraw := make(chan error, 1)
	go func() { withdraw <- f.gw.DeleteSystemRole(ctx, "", "standard", "health-huddle", "table-mic") }()
	waitForWaiters(t, ctx, z, 2, nil)
	_ = z.Rollback(ctx)
	e1, e2 := <-change, <-withdraw
	if isDeadlock(e1) || isDeadlock(e2) {
		t.Fatalf("a standard change and a role withdrawal deadlocked: change %v, withdrawal %v", e1, e2)
	}
	if e1 != nil || e2 != nil {
		t.Fatalf("change %v, withdrawal %v", e1, e2)
	}
}

// TestDeletingAStandardCannotSkipTheCarry: deleting a standard counts the
// systems on it and then deletes it, and the foreign key nulls any system on it
// by then, a standard change that skips the carry, its audit and its health
// recompute. A change onto the standard landing between the two must either
// make the delete a refusal or fail itself; both landing is the defect. The
// delete is held between its count and its delete by a raw lock on the row.
func TestDeletingAStandardCannotSkipTheCarry(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	f.declareRole(t, ctx, "huddle-next", storage.SystemRoleSpec{
		Name: "table-mic", Label: "Table microphone", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	})
	z, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Rollback(ctx) }()
	if _, err := z.Exec(ctx, `select 1 from standard where name = 'huddle-next' for share`); err != nil {
		t.Fatal(err)
	}
	deleted := make(chan error, 1)
	go func() { deleted <- f.gw.DeleteStandard(ctx, "", "huddle-next") }()
	waitForWaiters(t, ctx, z, 1, nil)
	next := "huddle-next"
	changed := make(chan error, 1)
	go func() {
		_, err := f.gw.UpdateSystem(ctx, "", "hq-huddle", storage.SystemPatch{StandardID: &next}, f.all, f.all)
		changed <- err
	}()
	var changeErr error
	changeDone := false
	waitForWaiters(t, ctx, z, 2, func() bool {
		select {
		case changeErr = <-changed:
			changeDone = true
		default:
		}
		return changeDone
	})
	_ = z.Rollback(ctx)
	deleteErr := <-deleted
	if !changeDone {
		changeErr = <-changed
	}
	if deleteErr == nil && changeErr == nil {
		var std *string
		if err := f.conn.QueryRow(ctx, `select standard_id::text from system where name = 'hq-huddle'`).Scan(&std); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("the standard was deleted and the change onto it both landed (hq-huddle standard now %v): the delete nulled a standard the carry had just staffed", std)
	}
}

// TestUnbindingDisclosesNoUnreadableComponent: a removal (unbind, re-default,
// unassign) naming a component that is not a member here answers the same
// not-found whether the name is a component outside the caller's read scope or
// nothing at all, so none of the three routes is an oracle for what exists.
func TestUnbindingDisclosesNoUnreadableComponent(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	f.mustBar(t, ctx, "caller-rack")
	f.mustBar(t, ctx, "hidden-codec")
	var rackID string
	if err := f.conn.QueryRow(ctx, `select id from component where name = 'caller-rack'`).Scan(&rackID); err != nil {
		t.Fatal(err)
	}
	narrow := scope.Set{IDs: []string{rackID}}
	for _, write := range []struct {
		name string
		do   func(ref string) error
	}{
		{"unbind", func(ref string) error {
			return f.gw.RemoveMember(ctx, "", "hq-huddle", ref, f.all, f.all, narrow, narrow)
		}},
		{"re-default", func(ref string) error {
			return f.gw.SetPrimaryMember(ctx, "", "hq-huddle", ref, f.all, f.all, narrow, narrow)
		}},
		{"unassign", func(ref string) error {
			return f.gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", ref, f.all, f.all, narrow, narrow)
		}},
	} {
		hidden, missing := write.do("hidden-codec"), write.do("no-such-component")
		if !errors.Is(missing, storage.ErrComponentNotFound) {
			t.Fatalf("%s of a name nothing has = %v, want ErrComponentNotFound", write.name, missing)
		}
		if !errors.Is(hidden, storage.ErrComponentNotFound) {
			t.Errorf("%s of a component outside the read scope = %v, want the same ErrComponentNotFound a missing name gets", write.name, hidden)
		}
	}
	// Inside the read scope the more specific answer stays: it exists, it is
	// just not bound here.
	if err := f.gw.RemoveMember(ctx, "", "hq-huddle", "caller-rack", f.all, f.all, narrow, narrow); !errors.Is(err, storage.ErrMemberNotFound) {
		t.Fatalf("unbind of a readable non-member = %v, want ErrMemberNotFound", err)
	}
}

// TestAnUnchangedStandardTakesNoFleetWideLock: the console resends a system's
// standard on every save, so a save that leaves it where it is must not queue
// behind the role-name lock every role declaration and standard change in the
// install serializes on. The lock is held on a raw connection.
func TestAnUnchangedStandardTakesNoFleetWideLock(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	held, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback(ctx) }()
	if _, err := held.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('system_role/name', 0))`); err != nil {
		t.Fatal(err)
	}
	quick, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	std, label := "health-huddle", "Huddle"
	if _, err := f.gw.UpdateSystem(quick, "", "hq-huddle", storage.SystemPatch{Label: &label, StandardID: &std}, f.all, f.all); err != nil {
		t.Fatalf("a label save resending the current standard = %v, want it served without the role-name lock", err)
	}
}

// TestDeletingAStandardCannotNullANewSystem: a system created onto a standard
// takes no role-name lock, so the delete must hold the standard's own row
// across its in-use count and its delete. A create landing between the two
// would otherwise be nulled by the foreign key, and a simulated system's
// assignments with it, unaudited. The delete is held between the two by a raw
// share lock on the row.
func TestDeletingAStandardCannotNullANewSystem(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	f.declareRole(t, ctx, "huddle-next", storage.SystemRoleSpec{Name: "table-mic", Label: "Table microphone", Quorum: 1, Impact: "degraded"})
	z, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Rollback(ctx) }()
	if _, err := z.Exec(ctx, `select 1 from standard where name = 'huddle-next' for share`); err != nil {
		t.Fatal(err)
	}
	deleted := make(chan error, 1)
	go func() { deleted <- f.gw.DeleteStandard(ctx, "", "huddle-next") }()
	waitForWaiters(t, ctx, z, 1, nil)
	std, room := "huddle-next", "hq-r1"
	_, createErr := f.gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "fresh-room", StandardID: &std, LocationName: &room}, f.all, all)
	_ = z.Rollback(ctx)
	deleteErr := <-deleted
	if createErr == nil && deleteErr == nil {
		var sid *string
		if err := f.conn.QueryRow(ctx, `select standard_id::text from system where name = 'fresh-room'`).Scan(&sid); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("the standard was deleted and a system created onto it both landed (fresh-room standard now %v)", sid)
	}
}

// TestASaveThatRacesAStandardChangeCarriesNothing: a save that resends the
// standard it read, while a change of that standard commits, is not a
// standard change: it records no move of its own, and the change it raced has
// already carried and recomputed.
func TestASaveThatRacesAStandardChangeCarriesNothing(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	f.declareRole(t, ctx, "huddle-next", storage.SystemRoleSpec{
		Name: "table-mic", Label: "Table microphone", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	})
	// The change holds the system's row, its standard already moved.
	change, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = change.Rollback(ctx) }()
	if _, err := change.Exec(ctx, `update system set standard_id = (select id from standard where name = 'huddle-next') where name = 'hq-huddle'`); err != nil {
		t.Fatal(err)
	}
	saved := make(chan error, 1)
	go func() {
		std, label := "health-huddle", "Huddle"
		_, err := f.gw.UpdateSystem(ctx, "", "hq-huddle", storage.SystemPatch{Label: &label, StandardID: &std}, f.all, f.all)
		saved <- err
	}()
	waitForWaiters(t, ctx, change, 1, nil)
	if err := change.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-saved; err != nil {
		t.Fatalf("the save: %v", err)
	}
	// The raw change carried nothing, so bar-1 is still on health-huddle's
	// table-mic: a save that ran the carry itself would have moved it.
	var standard string
	if err := f.conn.QueryRow(ctx, `select st.name from system_role_assignment ra
		join system_role r on r.id = ra.role_id join standard st on st.id = r.standard_id
		where ra.component_id = (select id from component where name = 'bar-1')`).Scan(&standard); err != nil {
		t.Fatalf("read bar-1's assignment: %v", err)
	}
	if standard != "health-huddle" {
		t.Fatalf("bar-1 is on %q's table-mic after the save: a save that moved no standard ran the carry", standard)
	}
}
