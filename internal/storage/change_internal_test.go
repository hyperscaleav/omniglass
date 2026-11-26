package storage

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/tail"
)

func TestTxEndOf(t *testing.T) {
	t.Parallel()
	cases := map[string]txEnd{
		"commit": txCommit, " COMMIT ": txCommit, "end": txCommit, "commit and chain": txCommit,
		"rollback": txRollback, "ROLLBACK": txRollback, "abort": txRollback,
		"begin": txBegin, "begin isolation level repeatable read read only": txBegin, "start transaction": txBegin,
		"rollback to savepoint sp_1": txNone, "release savepoint sp_1": txNone, "savepoint sp_1": txNone,
		"select 1": txNone, "insert into property values (1)": txNone, "committed_at": txNone,
	}
	for sql, want := range cases {
		if got := txEndOf(sql); got != want {
			t.Errorf("txEndOf(%q) = %d, want %d", sql, got, want)
		}
	}
}

// end runs one statement through the tracer on conn as pgx does: start, then
// end with the answer Postgres gave.
func end(tr *changeTracer, conn *pgx.Conn, sql, tag string, err error) {
	ctx := tr.TraceQueryStart(context.Background(), conn, pgx.TraceQueryStartData{SQL: sql})
	tr.TraceQueryEnd(ctx, conn, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag(tag), Err: err})
}

// A noted change publishes only when its connection's transaction commits:
// a rollback, a commit Postgres answered ROLLBACK, a failed commit, and a
// begin over an abandoned transaction all drop it, and each connection's
// changes are its own.
func TestChangeTracerPublishesOnlyOnCommit(t *testing.T) {
	t.Parallel()
	hub := tail.New(64, 0)
	tr := newChangeTracer(hub)
	a, b := new(pgx.Conn), new(pgx.Conn)
	change := func(id string) tail.Row {
		return tail.Row{Lane: tail.LaneChange, OwnerKind: "system", OwnerID: id, Key: tail.ChangeUpdate}
	}
	start := hub.Cursor()
	read := func() []string {
		t.Helper()
		commits, next, err := hub.Read(start, 0)
		if err != nil {
			t.Fatal(err)
		}
		start = next
		var ids []string
		for _, c := range commits {
			for _, r := range c.Rows {
				ids = append(ids, r.OwnerID)
			}
		}
		return ids
	}

	tr.note(a, change("s1"))
	tr.note(a, change("s1"))
	tr.note(b, change("s2"))
	end(tr, a, "select 1", "SELECT 1", nil)
	if got := read(); len(got) != 0 {
		t.Fatalf("published before commit: %v", got)
	}
	end(tr, a, "commit", "COMMIT", nil)
	if got := read(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("a's commit published %v, want s1 once", got)
	}
	end(tr, b, "rollback", "ROLLBACK", nil)
	tr.note(a, change("s3"))
	end(tr, a, "commit", "ROLLBACK", nil)
	tr.note(a, change("s4"))
	end(tr, a, "commit", "", context.Canceled)
	tr.note(a, change("s5"))
	end(tr, a, "begin", "BEGIN", nil)
	end(tr, a, "commit", "COMMIT", nil)
	if got := read(); len(got) != 0 {
		t.Fatalf("an uncommitted change published: %v", got)
	}
}
