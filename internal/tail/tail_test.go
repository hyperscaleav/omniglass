package tail_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/tail"
)

func metric(key string, v float64) tail.Row {
	return tail.Row{Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "c1", Key: key, Number: v, TS: time.Unix(0, 0)}
}

// A commit's rows publish only when the commit succeeds, each commit under
// the next sequence number, and Read returns every commit after a cursor in
// order.
func TestCommitPublishesOnlyWhatCommitted(t *testing.T) {
	h := tail.New(8)
	c0, err := h.Mark(func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Commit(func() error { return nil }, []tail.Row{metric("a", 1)}); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("rolled back")
	if err := h.Commit(func() error { return failed }, []tail.Row{metric("b", 2)}); !errors.Is(err, failed) {
		t.Fatalf("a failed commit's error: %v", err)
	}
	if err := h.Commit(func() error { return nil }, []tail.Row{metric("c", 3)}); err != nil {
		t.Fatal(err)
	}
	commits, next, err := h.Read(c0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Rows[0].Key != "a" || commits[1].Rows[0].Key != "c" {
		t.Fatalf("commits after the mark: %+v", commits)
	}
	if next != commits[1].Cursor {
		t.Fatalf("next cursor %q, want the last commit's %q", next, commits[1].Cursor)
	}
	again, _, err := h.Read(next, 0)
	if err != nil || len(again) != 0 {
		t.Fatalf("read from the newest cursor: %v %+v", err, again)
	}
}

// A commit with no rows advances nothing: the tail is what landed.
func TestCommitWithoutRowsPublishesNothing(t *testing.T) {
	h := tail.New(8)
	c0, _ := h.Mark(func() error { return nil })
	if err := h.Commit(func() error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	commits, next, err := h.Read(c0, 0)
	if err != nil || len(commits) != 0 || next != c0 {
		t.Fatalf("an empty commit published: %v %+v %q", err, commits, next)
	}
}

// Read caps how many commits it returns; the cursor it hands back resumes
// right after the last one returned.
func TestReadPagesByMax(t *testing.T) {
	h := tail.New(8)
	c0, _ := h.Mark(func() error { return nil })
	for i := 0; i < 5; i++ {
		_ = h.Commit(func() error { return nil }, []tail.Row{metric("k", float64(i))})
	}
	first, next, err := h.Read(c0, 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("first page: %v %+v", err, first)
	}
	rest, _, err := h.Read(next, 0)
	if err != nil || len(rest) != 3 || rest[0].Rows[0].Number != 2 {
		t.Fatalf("resumed page: %v %+v", err, rest)
	}
}

// A cursor older than the ring holds, or from another boot of the hub, or
// that does not parse, is expired: the reader re-reads history.
func TestReadExpiresWhatTheRingNoLongerHolds(t *testing.T) {
	h := tail.New(2)
	c0, _ := h.Mark(func() error { return nil })
	for i := 0; i < 3; i++ {
		_ = h.Commit(func() error { return nil }, []tail.Row{metric("k", float64(i))})
	}
	if _, _, err := h.Read(c0, 0); !errors.Is(err, tail.ErrExpired) {
		t.Fatalf("a cursor the ring dropped: %v", err)
	}
	other := tail.New(2)
	foreign, _ := other.Mark(func() error { return nil })
	if _, _, err := h.Read(foreign, 0); !errors.Is(err, tail.ErrExpired) {
		t.Fatalf("another boot's cursor: %v", err)
	}
	if _, _, err := h.Read("nonsense", 0); !errors.Is(err, tail.ErrExpired) {
		t.Fatalf("an unparsed cursor: %v", err)
	}
	cur := h.Cursor()
	ahead := cur[:len(cur)-1] + "9"
	if _, _, err := h.Read(ahead, 0); !errors.Is(err, tail.ErrExpired) {
		t.Fatalf("a cursor ahead of the hub: %v", err)
	}
}

// Mark excludes every commit while its open runs: a commit either finished
// before the mark (at or below its cursor) or starts after it (above it).
// open is where the reader takes its database snapshot, so no commit can
// land in the snapshot and above the cursor, or outside it and below.
func TestMarkFencesCommits(t *testing.T) {
	h := tail.New(64)
	inOpen := make(chan struct{})
	release := make(chan struct{})
	var cursor string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cursor, _ = h.Mark(func() error {
			close(inOpen)
			<-release
			return nil
		})
	}()
	<-inOpen
	committed := make(chan struct{})
	go func() {
		_ = h.Commit(func() error { return nil }, []tail.Row{metric("during", 1)})
		close(committed)
	}()
	select {
	case <-committed:
		t.Fatal("a commit ran while a mark was taking its snapshot")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	<-committed
	commits, _, err := h.Read(cursor, 0)
	if err != nil || len(commits) != 1 || commits[0].Rows[0].Key != "during" {
		t.Fatalf("the commit the mark held off reads after its cursor: %v %+v", err, commits)
	}
}

// Wait wakes on the next commit, so a watcher never polls.
func TestWaitWakesOnCommit(t *testing.T) {
	h := tail.New(8)
	woke := h.Wait()
	select {
	case <-woke:
		t.Fatal("woke with nothing committed")
	default:
	}
	_ = h.Commit(func() error { return nil }, []tail.Row{metric("k", 1)})
	select {
	case <-woke:
	case <-time.After(time.Second):
		t.Fatal("a commit did not wake the waiter")
	}
}

// A nil hub is a store with no tail: it commits through, and its marks carry
// no cursor.
func TestNilHubCommitsThrough(t *testing.T) {
	var h *tail.Hub
	ran := false
	if err := h.Commit(func() error { ran = true; return nil }, []tail.Row{metric("k", 1)}); err != nil || !ran {
		t.Fatalf("nil hub commit: %v ran=%v", err, ran)
	}
	c, err := h.Mark(func() error { return nil })
	if err != nil || c != "" {
		t.Fatalf("nil hub mark: %q %v", c, err)
	}
}

// Seq orders this hub's cursors, so a reader following several bindings from
// different cursors knows which commits each one has already seen; a cursor
// it cannot read is expired.
func TestSeqOrdersCursors(t *testing.T) {
	h := tail.New(8)
	c0, _ := h.Mark(func() error { return nil })
	_ = h.Commit(func() error { return nil }, []tail.Row{metric("k", 1)})
	c1 := h.Cursor()
	s0, err0 := h.Seq(c0)
	s1, err1 := h.Seq(c1)
	if err0 != nil || err1 != nil || s1 != s0+1 {
		t.Fatalf("seq %d %v, %d %v", s0, err0, s1, err1)
	}
	if _, err := tail.New(8).Seq(c1); !errors.Is(err, tail.ErrExpired) {
		t.Fatalf("another boot's cursor: %v", err)
	}
	if _, err := h.Seq(c1[:len(c1)-1] + "9"); !errors.Is(err, tail.ErrExpired) {
		t.Fatalf("a cursor ahead of the hub: %v", err)
	}
}
