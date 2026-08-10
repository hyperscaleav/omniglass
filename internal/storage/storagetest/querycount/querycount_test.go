package querycount_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	"github.com/jackc/pgx/v5"
)

// The tracer contract, checked at compile time rather than by hope. A Counter
// that stopped satisfying pgx.QueryTracer would not fail to build at the pool
// (storage.WithQueryTracer takes the interface, so the break would surface
// there), but this states the requirement where the type is defined.
var _ pgx.QueryTracer = (*querycount.Counter)(nil)

// stubRow and stubQuerier stand in for a database the wrapper's own test has no
// use for: what is under test here is the COUNTING, and the inner querier exists
// only to prove the call is forwarded intact. The real-Postgres proof that a
// counter observes actual gateway queries lives in storagetest (NewCountingDB's
// own test) and in the count assertions on the gateway's list paths.
type stubRow struct{}

func (stubRow) Scan(...any) error { return nil }

var errStubQuery = errors.New("stub query")

type stubQuerier struct {
	sql  []string
	args [][]any
	row  pgx.Row
}

func (s *stubQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	s.sql = append(s.sql, sql)
	s.args = append(s.args, args)
	return s.row
}

func (s *stubQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	s.sql = append(s.sql, sql)
	s.args = append(s.args, args)
	return nil, errStubQuery
}

// TestWrapCountsEveryCallAndForwardsIt holds the wrapper to both halves of its
// contract at once: the counter sees each call, and the inner querier receives
// the call unchanged and its results come back untouched. A wrapper that counted
// but swallowed an error would make every test using it green for the wrong
// reason.
func TestWrapCountsEveryCallAndForwardsIt(t *testing.T) {
	inner := &stubQuerier{row: stubRow{}}
	c := querycount.New()
	q := c.Wrap(inner)
	ctx := context.Background()

	if got := q.QueryRow(ctx, "select 1 from t where id = $1", "abc"); got != inner.row {
		t.Errorf("QueryRow returned %v, want the inner querier's own row", got)
	}
	rows, err := q.Query(ctx, "select * from t where id = any($1)", []string{"a", "b"})
	if rows != nil || !errors.Is(err, errStubQuery) {
		t.Errorf("Query returned (%v, %v), want the inner querier's own (nil, %v)", rows, err, errStubQuery)
	}

	if c.N() != 2 {
		t.Errorf("counted %d statements, want 2", c.N())
	}
	if want := []string{"select 1 from t where id = $1", "select * from t where id = any($1)"}; !equal(c.Statements(), want) {
		t.Errorf("statements = %q, want %q", c.Statements(), want)
	}
	if !equal(inner.sql, c.Statements()) {
		t.Errorf("inner saw %q, counter recorded %q: the wrapper must not rewrite the statement", inner.sql, c.Statements())
	}
	if len(inner.args) != 2 || len(inner.args[0]) != 1 || inner.args[0][0] != "abc" {
		t.Errorf("inner args = %v, want the caller's own arguments forwarded", inner.args)
	}
}

// TestTraceQueryStartCounts is the OTHER seam: the pgx hook a pool calls, which
// is the only one that can observe a gateway method that queries its own pool.
func TestTraceQueryStartCounts(t *testing.T) {
	c := querycount.New()
	ctx := context.Background()
	got := c.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "select 1"})
	c.TraceQueryEnd(got, nil, pgx.TraceQueryEndData{})

	if got != ctx {
		t.Errorf("TraceQueryStart returned a different context; pgx passes it to the rest of the call, so it must be threaded, not replaced")
	}
	if c.N() != 1 {
		t.Errorf("counted %d statements, want 1", c.N())
	}
	if s := c.Statements(); len(s) != 1 || s[0] != "select 1" {
		t.Errorf("statements = %q, want [select 1]", s)
	}
	// The end hook must not count a second time: one statement is one round
	// trip, not two.
	if c.N() != 1 {
		t.Errorf("TraceQueryEnd counted as well: %d, want 1", c.N())
	}
}

// TestResetClearsCountAndStatements pins the property every measurement depends
// on: a counter is reset before the call being measured, so anything the harness
// itself issued (the constructor's ping, the fixture's writes) is not attributed
// to it.
func TestResetClearsCountAndStatements(t *testing.T) {
	c := querycount.New()
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select 1"})
	c.Reset()
	if c.N() != 0 || len(c.Statements()) != 0 {
		t.Fatalf("after Reset: N = %d, statements = %q, want 0 and empty", c.N(), c.Statements())
	}
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select 2"})
	if s := c.Statements(); len(s) != 1 || s[0] != "select 2" {
		t.Errorf("statements after Reset then one call = %q, want [select 2]", s)
	}
}

// TestStatementsIsACopy: a caller holds the slice across further counting (a
// failure message built before the next measurement), so handing out the
// counter's own backing array would let it change underfoot.
func TestStatementsIsACopy(t *testing.T) {
	c := querycount.New()
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select 1"})
	held := c.Statements()
	held[0] = "mutated"
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select 2"})
	if s := c.Statements(); len(s) != 2 || s[0] != "select 1" {
		t.Errorf("statements = %q, want the counter's own record unaffected by a caller's copy", s)
	}
}

// TestSummaryCollapsesAndTruncates: the gateway's SQL is multi-line, so a raw
// statement list in a failure message is unreadable. Summary is what an
// assertion prints.
func TestSummaryCollapsesAndTruncates(t *testing.T) {
	c := querycount.New()
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select a,\n\t\tb\n\tfrom t"})
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select " + strings.Repeat("x", 300)})

	got := c.Summary()
	if len(got) != 2 {
		t.Fatalf("summary = %q, want 2 entries", got)
	}
	if got[0] != "select a, b from t" {
		t.Errorf("summary[0] = %q, want the whitespace collapsed to single spaces", got[0])
	}
	if len(got[1]) > 130 || !strings.HasSuffix(got[1], "...") {
		t.Errorf("summary[1] is %d chars (%q); a long statement must be cut short and marked", len(got[1]), got[1])
	}
	if s := c.Statements(); s[0] != "select a,\n\t\tb\n\tfrom t" {
		t.Errorf("Summary changed the recorded statement to %q; the record is the raw SQL", s[0])
	}
}

// TestConcurrentCountingLosesNothing: a pool hands connections to several
// goroutines, and pgx calls the tracer on each of them, so a counter that lost
// increments under concurrency would under-report exactly the N+1 it exists to
// catch.
func TestConcurrentCountingLosesNothing(t *testing.T) {
	c := querycount.New()
	const goroutines, each = 8, 50
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select 1"})
			}
		}()
	}
	wg.Wait()
	if want := goroutines * each; c.N() != want {
		t.Errorf("counted %d statements from %d goroutines, want %d", c.N(), goroutines, want)
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
