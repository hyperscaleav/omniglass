package bus

import (
	"maps"
	"testing"
	"time"
)

// TestOrphanTallyWarnsOncePerNodePerWindow holds #1200's no-flood half: the
// first orphaned batch from a node in a window is the one warned about, the
// rest of the window is only counted, and the next warning carries how many
// went unwarned. Two nodes keep separate windows, so one noisy node cannot
// silence another.
func TestOrphanTallyWarnsOncePerNodePerWindow(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tally := newOrphanTally(time.Minute)

	steps := []struct {
		node           string
		at             time.Duration
		wantWarn       bool
		wantSuppressed int
	}{
		{"node-a", 0, true, 0},
		{"node-a", time.Second, false, 0},
		{"node-a", 30 * time.Second, false, 0},
		{"node-b", 31 * time.Second, true, 0},
		{"node-a", 59 * time.Second, false, 0},
		{"node-a", 61 * time.Second, true, 3},
		{"node-b", 62 * time.Second, false, 0},
		{"node-b", 92 * time.Second, true, 1},
	}
	for i, s := range steps {
		warn, suppressed := tally.observe(s.node, t0.Add(s.at))
		if warn != s.wantWarn || suppressed != s.wantSuppressed {
			t.Fatalf("step %d (%s at %s): warn=%v suppressed=%d, want warn=%v suppressed=%d",
				i, s.node, s.at, warn, suppressed, s.wantWarn, s.wantSuppressed)
		}
	}
}

// TestOrphanTallyDrainsEveryBatchOnce holds #1200's counting half: every
// orphaned batch is counted, warned about or not, a drain hands each one to
// the store exactly once with the time of the newest, and a node with nothing
// new drains nothing.
func TestOrphanTallyDrainsEveryBatchOnce(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tally := newOrphanTally(time.Minute)
	for i := range 5 {
		tally.observe("node-a", t0.Add(time.Duration(i)*time.Second))
	}
	tally.observe("node-b", t0)

	got := tally.drain()
	want := map[string]orphanCount{
		"node-a": {Batches: 5, Last: t0.Add(4 * time.Second)},
		"node-b": {Batches: 1, Last: t0},
	}
	if !maps.Equal(got, want) {
		t.Fatalf("first drain = %v, want %v", got, want)
	}
	if again := tally.drain(); len(again) != 0 {
		t.Fatalf("a second drain with nothing new = %v, want nothing", again)
	}
	tally.observe("node-a", t0.Add(10*time.Second))
	if got := tally.drain(); !maps.Equal(got, map[string]orphanCount{"node-a": {Batches: 1, Last: t0.Add(10 * time.Second)}}) {
		t.Fatalf("drain after one more = %v, want node-a's one", got)
	}
}

// TestOrphanTallyForgetsAQuietNode keeps the tally bounded: a node whose
// window has closed and whose count has drained holds no entry, so the map
// grows with the nodes orphaning now, not every node that ever did.
func TestOrphanTallyForgetsAQuietNode(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tally := newOrphanTally(time.Minute)
	tally.observe("node-a", t0)
	tally.drain()
	tally.forgetQuiet(t0.Add(30 * time.Second))
	if tally.size() != 1 {
		t.Fatalf("a node inside its window was forgotten: size %d", tally.size())
	}
	tally.forgetQuiet(t0.Add(2 * time.Minute))
	if tally.size() != 0 {
		t.Fatalf("a quiet, drained node is still held: size %d", tally.size())
	}
	if warn, _ := tally.observe("node-a", t0.Add(3*time.Minute)); !warn {
		t.Fatal("a forgotten node's next orphan was not warned about")
	}
}
