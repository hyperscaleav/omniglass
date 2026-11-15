package storage_test

import (
	"context"
	"fmt"
	"testing"
)

// A first-time bind inserts into system_member, whose foreign key on system_id
// takes the system's row FOR KEY SHARE, and then restamps the component's label,
// which waits for the label lock. A label cascade holds the label lock and waits
// for the same system row FOR UPDATE, so a bind that held the row while it waited
// for the lock closed a cycle with every cascade (#1214). The lock order is the
// label lock, then the system row, on every bind path.

// bindRound holds the label lock on a raw transaction (a cascade's first move),
// starts bind through the gateway, and once bind is waiting on the lock plays
// the rest of the cascade (the location's systems FOR UPDATE) and commits. Either
// side Postgres aborts to break a lock cycle fails the round.
func bindRound(t *testing.T, ctx context.Context, f *healthFixture, what string, bind func() error) {
	t.Helper()
	cascade, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cascade.Rollback(ctx) }()
	if _, err := cascade.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('label/recompute', 0))`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- bind() }()
	waitForWaiters(t, ctx, cascade, 1, nil)
	_, cerr := cascade.Exec(ctx, `select id from system where location_id = (select id from location where name = 'hq-r1') order by id for update`)
	if cerr == nil {
		cerr = cascade.Commit(ctx)
	}
	berr := <-done
	if isDeadlock(cerr) || isDeadlock(berr) {
		t.Fatalf("%s and a label cascade deadlocked: cascade %v, bind %v", what, cerr, berr)
	}
	if cerr != nil || berr != nil {
		t.Fatalf("%s: cascade %v, bind %v", what, cerr, berr)
	}
}

// TestFirstTimeBindNeverDeadlocksALabelCascade: the explicit bind (PUT
// /systems/{name}/members/{component}) of a component that is in no system yet.
func TestFirstTimeBindNeverDeadlocksALabelCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHealthFixture(t)
	for i := 0; i < bindRounds(); i++ {
		name := fmt.Sprintf("bind-%d", i)
		f.mustBar(t, ctx, name)
		bindRound(t, ctx, f, "a first-time bind", func() error {
			return f.gw.AddMember(ctx, "", "hq-huddle", name, f.all, f.all, f.all, f.all)
		})
		if n := f.count(t, ctx, `select count(*) from system_member where component_id = (select id from component where name = $1)`, name); n != 1 {
			t.Fatalf("round %d: %s has %d memberships, want 1", i, name, n)
		}
	}
}

// TestRoleAssignmentBindNeverDeadlocksALabelCascade: assigning a role to a
// component that is not yet a member binds it implicitly, through the same insert.
func TestRoleAssignmentBindNeverDeadlocksALabelCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newHealthFixture(t)
	for i := 0; i < bindRounds(); i++ {
		name := fmt.Sprintf("staff-%d", i)
		f.mustBar(t, ctx, name)
		bindRound(t, ctx, f, "a role assignment binding a non-member", func() error {
			return f.gw.AssignRole(ctx, "", "hq-huddle", "table-mic", name, f.all, f.all, f.all, f.all)
		})
		if n := f.count(t, ctx, `select count(*) from system_member where component_id = (select id from component where name = $1)`, name); n != 1 {
			t.Fatalf("round %d: %s has %d memberships, want 1", i, name, n)
		}
	}
}

// bindRounds is how many concurrent rounds each bind path runs: enough that the
// defect, which deadlocks most rounds, cannot pass by luck.
func bindRounds() int { return 50 }
