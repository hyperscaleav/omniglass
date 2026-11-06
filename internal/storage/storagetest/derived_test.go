package storagetest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

// stampBuild is a build step that leaves a recognisable row in the derived
// template and counts how many times it ran.
func stampBuild(runs *atomic.Int64, name string) func(context.Context, string) error {
	return func(ctx context.Context, dsn string) error {
		runs.Add(1)
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()
		_, err = conn.Exec(ctx, `insert into location_type (name, label) values ($1, $1)`, name)
		return err
	}
}

// TestDerivedTemplateIsBuiltOnceAndClonedPerTest is the property the primitive
// exists for: a package that seeds a fixture into every test pays for it once
// per binary, and each test still gets a database of its own. The fixture the
// build step wrote is in every clone, a write in one clone is in no other, and
// none reaches the template the next clone is copied from.
func TestDerivedTemplateIsBuiltOnceAndClonedPerTest(t *testing.T) {
	ctx := context.Background()
	var runs atomic.Int64
	build := stampBuild(&runs, "derived-fixture-1537")

	first := NewDSNFrom(t, "fixture_once", build)
	firstConn := connect(t, ctx, first)
	if _, err := firstConn.Exec(ctx, `insert into location_type (name, label) values ('written-after-clone', 'x')`); err != nil {
		t.Fatal(err)
	}
	second := NewDSNFrom(t, "fixture_once", build)
	third := NewDSNFrom(t, "fixture_once", build)

	if first == second || second == third {
		t.Fatalf("clones share a database: %q %q %q", first, second, third)
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("the build step ran %d times for three clones, want once", got)
	}
	for _, dsn := range []string{first, second, third} {
		if got := probeCount(t, ctx, connect(t, ctx, dsn), "derived-fixture-1537"); got != 1 {
			t.Errorf("%s: the built fixture is in %d rows, want 1", dbNameOf(t, dsn), got)
		}
	}
	if got := probeCount(t, ctx, connect(t, ctx, second), "written-after-clone"); got != 0 {
		t.Errorf("a write in one clone reached another: %d rows", got)
	}
	if got := probeCount(t, ctx, connect(t, ctx, third), "written-after-clone"); got != 0 {
		t.Errorf("a write in one clone reached the derived template: %d rows", got)
	}
}

// TestDerivedTemplatesAreKeyedApart pins that two keys are two fixtures: a
// package with a booted-only database and a fully seeded one must not get each
// other's.
func TestDerivedTemplatesAreKeyedApart(t *testing.T) {
	ctx := context.Background()
	var aRuns, bRuns atomic.Int64
	a := NewDSNFrom(t, "keyed_a", stampBuild(&aRuns, "only-in-a"))
	b := NewDSNFrom(t, "keyed_b", stampBuild(&bRuns, "only-in-b"))

	if probeCount(t, ctx, connect(t, ctx, a), "only-in-a") != 1 || probeCount(t, ctx, connect(t, ctx, a), "only-in-b") != 0 {
		t.Error("key a's database does not hold exactly a's fixture")
	}
	if probeCount(t, ctx, connect(t, ctx, b), "only-in-b") != 1 || probeCount(t, ctx, connect(t, ctx, b), "only-in-a") != 0 {
		t.Error("key b's database does not hold exactly b's fixture")
	}
}

// TestDerivedTemplateIsBuiltOnceUnderConcurrency pins the mutex: parallel tests
// that all ask for the same fixture at once must not build it twice, which
// would cost the time the primitive saves, nor race two builds into one
// database.
func TestDerivedTemplateIsBuiltOnceUnderConcurrency(t *testing.T) {
	var runs atomic.Int64
	build := stampBuild(&runs, "concurrent-fixture")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- ensureDerived("fixture_conc", build)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ensureDerived: %v", err)
		}
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("eight concurrent callers ran the build step %d times, want once", got)
	}
}

// TestDerivedTemplateBuildFailureIsNotCached is TestTemplateBuildFailureIsNotCached
// one layer up: a build step that fails once (a seed that hit a flapping
// connection) costs the test that hit it, not every later test that wants the
// fixture. The retry also has to get past the half-built database the failed
// attempt left behind.
func TestDerivedTemplateBuildFailureIsNotCached(t *testing.T) {
	var runs atomic.Int64
	good := stampBuild(&runs, "recovered-fixture")
	broken := func(ctx context.Context, dsn string) error {
		if err := good(ctx, dsn); err != nil { // leaves a row behind, as a seed that dies midway does
			return err
		}
		return errors.New("the seed lost its connection")
	}

	if err := ensureDerived("fixture_retry", broken); err == nil {
		t.Fatal("ensureDerived returned nil for a build step that failed")
	}
	if err := ensureDerived("fixture_retry", good); err != nil {
		t.Fatalf("the next call did not retry: %v", err)
	}
	ctx := context.Background()
	dsn := NewDSNFrom(t, "fixture_retry", good)
	if got := probeCount(t, ctx, connect(t, ctx, dsn), "recovered-fixture"); got != 1 {
		t.Errorf("the recovered fixture is in %d rows, want exactly 1 (the half-built attempt must not survive the retry)", got)
	}
	if got := runs.Load(); got != 2 {
		t.Errorf("build step ran %d times, want 2 (the failure's row, then the retry)", got)
	}
}

// TestDerivedTemplateRefusesAKeyThatIsNotAnIdentifier: the key becomes part of
// a database name, so it is held to a plain lowercase identifier rather than
// quoted into something surprising.
func TestDerivedTemplateRefusesAKeyThatIsNotAnIdentifier(t *testing.T) {
	noop := func(context.Context, string) error { return nil }
	for _, key := range []string{"", "Has-Dash", "has space", "1leading", "a;drop", "waytoolongwaytoolongwaytoolongwaytoolong"} {
		if err := ensureDerived(key, noop); err == nil {
			t.Errorf("key %q accepted", key)
		}
	}
}
