package storage

import (
	"context"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/tail"
)

// The change lane (#2731): a write that moves an entity a live page shows
// names it, and the name publishes on the tail as a change notice once the
// transaction commits, never before (a reader that refetched on a notice of
// an uncommitted write would read the old state and stop listening). The
// write chokepoints note their entities on the transaction's connection;
// changeTracer, a pgx query tracer on every pool connection, sees that
// connection's commit and publishes what it noted, and drops it on rollback.
// It is the one place a commit is visible to the store whatever path began
// the transaction, so a chokepoint never needs its caller's cooperation.

// changeTracer holds each connection's noted changes until its transaction
// ends. A connection runs one transaction at a time, so the connection is the
// transaction's identity while it is open.
type changeTracer struct {
	hub     *tail.Hub
	mu      sync.Mutex
	pending map[*pgx.Conn][]tail.Row
}

func newChangeTracer(hub *tail.Hub) *changeTracer {
	return &changeTracer{hub: hub, pending: map[*pgx.Conn][]tail.Row{}}
}

// txEnd is what a statement does to its connection's transaction.
type txEnd int

const (
	txNone txEnd = iota
	txBegin
	txCommit
	txRollback
)

// txEndOf classifies a statement: a transaction's begin, its commit, its
// rollback, or anything else. A savepoint's release or rollback is none: a
// change noted inside a rolled-back savepoint still publishes, a spurious
// refetch rather than a missed one.
func txEndOf(sql string) txEnd {
	s := strings.ToLower(strings.TrimSpace(sql))
	switch {
	case s == "commit" || s == "end" || strings.HasPrefix(s, "commit "):
		return txCommit
	case s == "rollback" || s == "abort":
		return txRollback
	case strings.HasPrefix(s, "begin") || strings.HasPrefix(s, "start transaction"):
		return txBegin
	}
	return txNone
}

type txEndKey struct{}

// TraceQueryStart tags the statements that end a transaction; every other
// statement passes through untouched, so the tracer costs a string check.
func (t *changeTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if end := txEndOf(data.SQL); end != txNone {
		return context.WithValue(ctx, txEndKey{}, end)
	}
	return ctx
}

// TraceQueryEnd publishes a committed transaction's changes and drops a
// rolled-back one's. A commit Postgres answered with ROLLBACK (the
// transaction had already failed) committed nothing. A begin clears what a
// connection left behind (a transaction its caller abandoned).
func (t *changeTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	end, _ := ctx.Value(txEndKey{}).(txEnd)
	if end == txNone {
		return
	}
	t.mu.Lock()
	rows, held := t.pending[conn]
	if held {
		delete(t.pending, conn)
	}
	t.mu.Unlock()
	if end == txCommit && held && data.Err == nil && data.CommandTag.String() != "ROLLBACK" {
		t.hub.Publish(rows)
	}
}

// note holds a change for conn's open transaction, once per entity.
func (t *changeTracer) note(conn *pgx.Conn, r tail.Row) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, held := range t.pending[conn] {
		if held.OwnerKind == r.OwnerKind && held.OwnerID == r.OwnerID && held.Key == r.Key {
			return
		}
	}
	t.pending[conn] = append(t.pending[conn], r)
}

// noteChange names an entity a write moved. Inside a transaction it publishes
// when the transaction commits; on the pool (autocommit) the write is already
// durable, so it publishes now.
func (p *PG) noteChange(q txQuerier, kind, id string) {
	r := tail.Row{Lane: tail.LaneChange, OwnerKind: kind, OwnerID: id, Key: tail.ChangeUpdate}
	if tx, ok := q.(interface{ Conn() *pgx.Conn }); ok && p.changes != nil {
		p.changes.note(tx.Conn(), r)
		return
	}
	p.tail.Publish([]tail.Row{r})
}
