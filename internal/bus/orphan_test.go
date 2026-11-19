package bus

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestOrphanTallyWarnsOncePerOutpostPerWindow holds #1200's no-flood half: the
// first orphaned batch from an outpost in a window is the one warned about, the
// rest of the window is only counted, and the next warning carries how many
// went unwarned. Two outposts keep separate windows, so one noisy outpost cannot
// silence another.
func TestOrphanTallyWarnsOncePerOutpostPerWindow(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tally := newOrphanTally(time.Minute)

	steps := []struct {
		outpost        string
		at             time.Duration
		wantWarn       bool
		wantSuppressed int
	}{
		{"outpost-a", 0, true, 0},
		{"outpost-a", time.Second, false, 0},
		{"outpost-a", 30 * time.Second, false, 0},
		{"outpost-b", 31 * time.Second, true, 0},
		{"outpost-a", 59 * time.Second, false, 0},
		{"outpost-a", 61 * time.Second, true, 3},
		{"outpost-b", 62 * time.Second, false, 0},
		{"outpost-b", 92 * time.Second, true, 1},
	}
	for i, s := range steps {
		warn, suppressed := tally.observe(s.outpost, t0.Add(s.at))
		if warn != s.wantWarn || suppressed != s.wantSuppressed {
			t.Fatalf("step %d (%s at %s): warn=%v suppressed=%d, want warn=%v suppressed=%d",
				i, s.outpost, s.at, warn, suppressed, s.wantWarn, s.wantSuppressed)
		}
	}
}

// TestOrphanTallyDrainsEveryBatchOnce holds #1200's counting half: every
// orphaned batch is counted, warned about or not, a drain hands each one to
// the store exactly once with the time of the newest, and an outpost with nothing
// new drains nothing.
func TestOrphanTallyDrainsEveryBatchOnce(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tally := newOrphanTally(time.Minute)
	for i := range 5 {
		tally.observe("outpost-a", t0.Add(time.Duration(i)*time.Second))
	}
	tally.observe("outpost-b", t0)

	got := tally.drain()
	want := map[string]orphanCount{
		"outpost-a": {Batches: 5, Last: t0.Add(4 * time.Second)},
		"outpost-b": {Batches: 1, Last: t0},
	}
	if !maps.Equal(got, want) {
		t.Fatalf("first drain = %v, want %v", got, want)
	}
	if again := tally.drain(); len(again) != 0 {
		t.Fatalf("a second drain with nothing new = %v, want nothing", again)
	}
	tally.observe("outpost-a", t0.Add(10*time.Second))
	if got := tally.drain(); !maps.Equal(got, map[string]orphanCount{"outpost-a": {Batches: 1, Last: t0.Add(10 * time.Second)}}) {
		t.Fatalf("drain after one more = %v, want outpost-a's one", got)
	}
}

// TestOrphanTallyForgetsAQuietOutpost keeps the tally bounded: an outpost whose
// window has closed and whose count has drained holds no entry, so the map
// grows with the outposts orphaning now, not every outpost that ever did.
func TestOrphanTallyForgetsAQuietOutpost(t *testing.T) {
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tally := newOrphanTally(time.Minute)
	tally.observe("outpost-a", t0)
	tally.drain()
	tally.forgetQuiet(t0.Add(30 * time.Second))
	if tally.size() != 1 {
		t.Fatalf("an outpost inside its window was forgotten: size %d", tally.size())
	}
	tally.forgetQuiet(t0.Add(2 * time.Minute))
	if tally.size() != 0 {
		t.Fatalf("a quiet, drained outpost is still held: size %d", tally.size())
	}
	if warn, _ := tally.observe("outpost-a", t0.Add(3*time.Minute)); !warn {
		t.Fatal("a forgotten outpost's next orphan was not warned about")
	}
}

// goneOutpostStore answers every orphan count as an outpost that no longer exists,
// the way RecordOrphanedBatches does after the outpost is deleted.
type goneOutpostStore struct {
	fakeStore
	calls *int
}

func (g goneOutpostStore) RecordOrphanedBatches(context.Context, string, int64, time.Time) error {
	*g.calls++
	return storage.ErrOutpostNotFound
}

// TestADeletedOutpostsOrphanCountIsDropped holds the tally bounded across an outpost
// deletion: a count the store refuses because the outpost is gone is permanent,
// not transient, so it is dropped rather than kept for a flush that can never
// take it, and the outpost is then forgotten instead of retried every period.
func TestADeletedOutpostsOrphanCountIsDropped(t *testing.T) {
	calls := 0
	s := &Server{store: goneOutpostStore{calls: &calls}, orphans: newOrphanTally(time.Millisecond)}
	s.orphans.observe("outpost-gone", time.Now().Add(-time.Second))
	s.flushOrphans(context.Background())
	if calls != 1 {
		t.Fatalf("store called %d times, want 1", calls)
	}
	if s.orphans.size() != 0 {
		t.Fatalf("a deleted outpost's count was kept: size %d, want 0", s.orphans.size())
	}
	s.flushOrphans(context.Background())
	if calls != 1 {
		t.Fatalf("a deleted outpost's count was retried: %d store calls, want 1", calls)
	}
}
