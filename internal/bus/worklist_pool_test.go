package bus

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/nats-io/nats.go"
)

// The pool is the asked-for size, or eight, held under the database pool
// less the connection kept for ingest, and never under one.
func TestTheWorklistPoolIsHeldUnderTheDatabasePool(t *testing.T) {
	for _, c := range []struct{ want, db, got int }{
		{0, 0, DefaultWorklistWorkers},
		{0, 20, DefaultWorklistWorkers},
		{0, 4, 3},
		{12, 20, 12},
		{12, 5, 4},
		{3, 0, 3},
		{-1, 0, DefaultWorklistWorkers},
		{8, 1, 1},
		{8, 2, 1},
	} {
		if got := worklistWorkers(c.want, c.db); got != c.got {
			t.Errorf("worklistWorkers(%d, %d) = %d, want %d", c.want, c.db, got, c.got)
		}
	}
}

// A pull's deadline is its arrival plus the budget its header states, or the
// default budget when it states none.
func TestAWorklistPullsDeadlineIsItsArrivalPlusItsBudget(t *testing.T) {
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	hdr := nats.Header{}
	hdr.Set(collection.WorklistTimeoutHeader, "2s")
	if got := worklistDeadline(hdr, at); !got.Equal(at.Add(2 * time.Second)) {
		t.Errorf("stated 2s: deadline %v", got)
	}
	if got := worklistDeadline(nil, at); !got.Equal(at.Add(defaultWorklistBudget)) {
		t.Errorf("stated none: deadline %v", got)
	}
}

// worklistReads counts its worklist reads.
type worklistReads struct {
	worklistOf
	reads *int
}

func (s worklistReads) NodeWorklist(ctx context.Context, node string) (storage.Worklist, error) {
	*s.reads++
	return s.worklistOf.NodeWorklist(ctx, node)
}

// A pull whose node stopped waiting while it queued is dropped unread: no
// database read, no reply, one warning and one count.
func TestAWorklistPullPastItsDeadlineIsDroppedUnread(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	reads := 0
	s := &Server{store: worklistReads{worklistOf{n: 2}, &reads}}
	now := time.Now()
	s.serveWorklistPull(context.Background(), worklistPull{node: "edge-1", msg: &nats.Msg{Subject: "og.v1.worklist.edge-1"}, deadline: now}, now)

	if reads != 0 {
		t.Errorf("an expired pull read the worklist %d times", reads)
	}
	if got := s.WorklistCounts(); got.Expired != 1 || got.Served != 0 {
		t.Errorf("counts %+v, want one expired", got)
	}
	if line := logged.String(); !strings.Contains(line, "level=WARN") || !strings.Contains(line, "node=edge-1") || !strings.Contains(line, "expired") {
		t.Errorf("no expiry warning: %s", line)
	}
}

// slowStore's read outlasts its caller's deadline.
type slowStore struct{ worklistOf }

func (s slowStore) NodeWorklist(ctx context.Context, node string) (storage.Worklist, error) {
	<-ctx.Done()
	return storage.Worklist{}, ctx.Err()
}

// A read the node gave up on mid-way is abandoned: no reply (the node has
// gone), and counted, not taken for a storage failure.
func TestAWorklistReadItsNodeGaveUpOnIsAbandoned(t *testing.T) {
	capture(t, slog.LevelDebug)
	s := &Server{store: slowStore{}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	var r sent
	s.answerWorklist(ctx, "edge-1", "og.v1.worklist.edge-1", 1<<20, r.respond, time.Now())

	if r.calls != 0 {
		t.Errorf("sent %d replies to a node that gave up", r.calls)
	}
	if got := s.WorklistCounts(); got.Abandoned != 1 || got.ReadFailed != 0 {
		t.Errorf("counts %+v, want one abandoned", got)
	}
}

// A worklist past 64 KB is sent gzipped under its header, recorded at its
// wire size, and reads back whole.
func TestALargeWorklistReplyIsSentGzipped(t *testing.T) {
	capture(t, slog.LevelDebug)
	s := &Server{store: worklistOf{n: 2000}}
	var r sent
	s.answerWorklist(context.Background(), "hq", "og.v1.worklist.hq", 1<<20, r.respond, time.Now())

	if r.hdr.Get(collection.ContentEncodingHeader) != "gzip" {
		t.Fatalf("header %v, want gzip", r.hdr)
	}
	if got := s.WorklistReplyBytes()["hq"]; got != len(r.body) {
		t.Errorf("recorded %d bytes, sent %d", got, len(r.body))
	}
	wl, err := collection.DecodeWorklistReply(r.hdr, r.body)
	if err != nil || len(wl.Tasks) != 2000 {
		t.Fatalf("read back %d tasks, %v", len(wl.Tasks), err)
	}
	if s.WorklistCounts().Served != 1 {
		t.Errorf("counts %+v, want one served", s.WorklistCounts())
	}
}
