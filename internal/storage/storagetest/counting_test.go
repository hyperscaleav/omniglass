package storagetest

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestNewCountingDBObservesTheListPath is the proof the whole instrument rests
// on, and it is deliberately an EXACT number rather than "more than zero".
//
// The hazard NewCountingDB exists to close is a counter that observes nothing:
// the gateway's list paths query p.pool directly, so a counter wrapped around an
// argument sees none of them and reports a flat, fictional number forever. A
// zero here (or a one that turns out to be the ping) is the tell. ListLocations
// against an empty estate is the tightest possible case: one scoped-list query,
// and no path attach at all, because attachPaths is skipped for an empty page.
// If that reads 1, the tracer is on the connection the gateway actually used.
func TestNewCountingDBObservesTheListPath(t *testing.T) {
	gw, counter := NewCountingDB(t)
	ctx := context.Background()

	// The constructor's own connectivity ping must not be charged to the first
	// measurement.
	if n := counter.N(); n != 0 {
		t.Fatalf("counter starts at %d (%q), want 0: NewCountingDB resets after the pool's ping", n, counter.Summary())
	}

	locs, err := gw.ListLocations(ctx, scope.Set{All: true})
	if err != nil {
		t.Fatalf("list locations: %v", err)
	}
	if len(locs) != 0 {
		t.Fatalf("a fresh database has %d locations, want none: this test's exact count assumes an empty page", len(locs))
	}
	if n := counter.N(); n != 1 {
		t.Fatalf("ListLocations on an empty estate cost %d statements, want exactly 1: %q", n, counter.Summary())
	}
	// Read against the RAW statement rather than the Summary, which truncates at
	// a fixed width: this matched the summary's tail until a column was added to
	// the location select list and pushed the table name past the cut, so the
	// assertion was measuring how long the select list is rather than which
	// table was read. Statements() carries the whole SQL and cannot drift that
	// way.
	if got := counter.Statements()[0]; !strings.Contains(got, "from location") {
		t.Errorf("the counted statement is %q, want the scoped list of location: the counter must be seeing the gateway's own query, not something else on the pool", got)
	}
}

// TestCountingDBCountsWritesToo pins the unit of measurement the primitive
// claims: pgx traces Exec as well as Query, so a count is round trips, not reads.
// UpsertRole is a single bare Exec with no transaction around it, which makes it
// the one gateway write with an unambiguous expected number.
func TestCountingDBCountsWritesToo(t *testing.T) {
	gw, counter := NewCountingDB(t)
	ctx := context.Background()

	if err := gw.UpsertRole(ctx, storage.Role{Name: "counting-probe", Permissions: []string{"component:read"}}); err != nil {
		t.Fatalf("upsert role: %v", err)
	}
	if n := counter.N(); n != 1 {
		t.Fatalf("UpsertRole cost %d statements, want exactly 1 (one Exec): %q", n, counter.Summary())
	}

	counter.Reset()
	if n := counter.N(); n != 0 {
		t.Fatalf("counter is %d after Reset, want 0", n)
	}
	if err := gw.UpsertRole(ctx, storage.Role{Name: "counting-probe-2"}); err != nil {
		t.Fatalf("upsert second role: %v", err)
	}
	if n := counter.N(); n != 1 {
		t.Errorf("after Reset, one more write counted %d, want 1: %q", n, counter.Summary())
	}
}
