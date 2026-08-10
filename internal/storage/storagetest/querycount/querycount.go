// Package querycount counts the SQL statements a piece of Storage Gateway code
// issues against Postgres. It is the repo's performance instrument, and the
// reason it counts round trips rather than timing them: the Gateway's dominant
// cost is round trips to Postgres, the regression that hurts at estate scale is
// the N+1, and a count is deterministic. It needs no stored baseline and no
// threshold policy, and it fails with an exact number that names the defect,
// where a wall-clock measurement on a laptop or a shared runner has variance
// that swamps anything short of a catastrophe.
//
// # The bypass hazard
//
// A count means nothing if the code under test does not go through the counted
// seam. This is the instrument's most important property and the one that fails
// SILENTLY: a wrapper the code never reaches for reports a small, flat, entirely
// fictional number, and the assertion built on it reads as coverage while
// measuring nothing at all.
//
// So there are two seams here, and which one is honest depends on what is being
// measured:
//
//   - [Counter.Wrap] wraps a [Querier] and observes only a function that takes
//     its querier AS A PARAMETER (the gateway's attachPaths hooks, PathsOf, the
//     resolve helpers). A method that reaches for its own pool never sees the
//     wrapper, so wrapping one and asserting a low number measures nothing.
//   - [Counter] is itself a pgx.QueryTracer. Attached to a pool's connection
//     config (storage.WithQueryTracer, or storagetest.NewCountingDB in a test),
//     it observes EVERY statement that pool issues, whichever code issued it.
//     This is the seam a gateway LIST needs, because the scoped read paths query
//     the pool directly rather than an injected querier.
//
// A count of zero (or of one, where the call obviously reads several tables) is
// the tell that the seam is wrong. Assert against the number rather than
// trusting the wiring, and prove a new assertion has teeth by breaking the
// implementation and watching the number move.
//
// # What a count is
//
// One count is one statement executed: pgx traces Query, QueryRow, and Exec, so
// a transaction's BEGIN and COMMIT each count, and a QueryRow counts once (it is
// one Query underneath). The extended-protocol Parse that pgx may send before a
// statement is not a separate event, so this is a count of STATEMENTS, not of
// literal network packets. That is the unit an N+1 is measured in.
package querycount

import (
	"context"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
)

// Querier is the two-method read interface the gateway's internal helpers take
// (structurally the same as storage's own unexported querier, which is why a
// [Counter.Wrap] result can be passed to one). *pgxpool.Pool, *pgx.Conn and
// pgx.Tx all satisfy it.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Counter counts statements and remembers their SQL. The zero value is ready to
// use, and every method is safe for concurrent use, so one counter can be shared
// by a pool that hands connections to several goroutines.
type Counter struct {
	mu    sync.Mutex
	stmts []string
}

// New returns an empty Counter.
func New() *Counter { return &Counter{} }

// N is how many statements have been counted since the last [Counter.Reset].
func (c *Counter) N() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.stmts)
}

// Statements returns the SQL of every counted statement, in the order it was
// issued. The slice is a copy, so a caller can hold it across further counting.
func (c *Counter) Statements() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.stmts...)
}

// Summary is [Counter.Statements] with each statement's whitespace collapsed and
// long ones cut short: what a failing assertion prints so the failure names the
// statements that were issued rather than only how many there were. An N+1 is
// obvious in this list (the same statement, over and over) and almost invisible
// in a bare number.
func (c *Counter) Summary() []string {
	stmts := c.Statements()
	out := make([]string, len(stmts))
	for i, s := range stmts {
		out[i] = compact(s)
	}
	return out
}

// Reset drops the count and the recorded statements. Constructing a gateway runs
// statements of its own (a connectivity ping), so a measurement starts from a
// reset counter, not from a fresh one.
func (c *Counter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stmts = nil
}

func (c *Counter) record(sql string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stmts = append(c.stmts, sql)
}

// TraceQueryStart counts one statement and returns ctx unchanged. It is pgx's
// hook at the start of every Query, QueryRow and Exec on a connection whose
// config carries this tracer, which is what makes a Counter observe a gateway
// read end to end (see the package doc's bypass hazard).
func (c *Counter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.record(data.SQL)
	return ctx
}

// TraceQueryEnd completes the pgx.QueryTracer contract. Counting happens at
// start, so a statement that errors still counts: it cost a round trip.
func (c *Counter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Wrap returns a Querier that counts every call and forwards it to inner
// unchanged. Nothing about the database is faked: the statements still run
// against real Postgres, and the results are the inner querier's own.
//
// This seam only observes code that USES the querier it is given. Handing it to
// a function that queries a pool of its own counts zero, which is the failure
// mode the package doc opens with.
func (c *Counter) Wrap(inner Querier) Querier {
	return &countingQuerier{counter: c, inner: inner}
}

type countingQuerier struct {
	counter *Counter
	inner   Querier
}

func (q *countingQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	q.counter.record(sql)
	return q.inner.QueryRow(ctx, sql, args...)
}

func (q *countingQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.counter.record(sql)
	return q.inner.Query(ctx, sql, args...)
}

// maxSummaryLen is where a summarized statement is cut. Long enough to tell two
// statements apart by their select list and first table, short enough that a
// twenty-line failure message stays readable.
const maxSummaryLen = 120

func compact(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	if len(s) > maxSummaryLen {
		return s[:maxSummaryLen] + "..."
	}
	return s
}
