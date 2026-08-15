package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	"github.com/jackc/pgx/v5"
)

// The health reads read the `property` series filtered by property type and a
// set of system ids, and until #725 the only owner index on that table led with
// `component_id`, so both of them scanned the whole series. The index they need
// is `property_system_owner_idx`, and this holds them to REACHING it.
//
// Why an access-path assertion rather than a catalog one: an index existing is
// not an index being used, and the ways a read stops reaching one (a coerced
// predicate, a dropped leading column, a partial predicate that stops being
// provable) leave `pg_indexes` reporting it present throughout. Why a usability
// assertion rather than a plan-shape one: ADR-0094's deferral of plan-shape
// assertions stands, because the plan a small fixture PREFERS is not the plan
// production prefers. See the accesspath package doc for the line between the
// two, which this test is the first consumer of.
//
// The statements are the ones the gateway really issued, captured off the pool
// tracer rather than copied into this file, since a copy is exactly what stops
// failing when the original changes.
func TestHealthReadsReachTheSystemOwnerIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, dsn, storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	verdictFleet(t, gw, 3)
	all := scope.Set{All: true}

	// The bulk read (#653) and the location rollup, in one pass each. The
	// rollup runs inside the recompute chain an alarm triggers, so raising one
	// is how a caller reaches it.
	counter.Reset()
	if _, err := gw.SystemVerdicts(ctx, all); err != nil {
		t.Fatalf("SystemVerdicts: %v", err)
	}
	if _, err := gw.RaiseAlarm(ctx, "", "bar-0", storage.AlarmSpec{
		Severity: "critical", Message: "index guard", DedupKey: "index-guard",
	}); err != nil {
		t.Fatalf("RaiseAlarm: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	for _, read := range []struct{ name, marker string }{
		{"SystemVerdicts, the bulk read a system list paints its column from", "distinct on (v.system_id)"},
		{"locationVerdict, the rollup every recompute pays per location", "distinct on (sd.system_id)"},
	} {
		t.Run(read.name, func(t *testing.T) {
			call := oneCall(t, counter, read.marker)
			plan, err := accesspath.Explain(ctx, conn, call.SQL, call.Args...)
			if err != nil {
				t.Fatalf("explain the captured statement: %v", err)
			}
			if ok, why := plan.MustReach("property", "property_system_owner_idx"); !ok {
				t.Errorf("the health read cannot reach the index it exists for: %s", why)
			}
		})
	}
}

// oneCall finds the captured statement containing marker, and fails if there is
// none: a read that never ran would leave the assertion below it proving
// nothing, which is the counting instrument's bypass hazard in another costume.
// Several occurrences are normal and legal, since a recompute rolls up every
// location in the chain with the same statement and different arguments, but
// they have to BE the same statement, or which one was explained would come
// down to ordering.
func oneCall(t *testing.T, counter *querycount.Counter, marker string) querycount.Call {
	t.Helper()
	var found []querycount.Call
	for _, call := range counter.Calls() {
		if strings.Contains(call.SQL, marker) {
			found = append(found, call)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no statement containing %q was issued, so nothing was explained; the statements issued were:\n\t%s",
			marker, strings.Join(counter.Summary(), "\n\t"))
	}
	for _, call := range found[1:] {
		if call.SQL != found[0].SQL {
			t.Fatalf("two different statements contain %q, so explaining one proves nothing about the other:\n\t%s\n\t%s",
				marker, found[0].SQL, call.SQL)
		}
	}
	return found[0]
}
