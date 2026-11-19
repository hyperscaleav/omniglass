package storage

import (
	"testing"
	"time"
)

// TestJudgedInArrival: judgedInArrival returns only the samples the ingest
// would have judged, skipping those that arrived after a newer sample had
// already set the state. A folded sample with id >= 0 (arrival_id stored,
// ADR-1530) participates in the stale check the same as a raw sample; one
// with id < 0 (old block, no arrival_id) is always kept.
func TestJudgedInArrival(t *testing.T) {
	t.Parallel()
	T1 := time.Now().Add(-3 * time.Hour)
	T2 := T1.Add(time.Minute)
	T3 := T2.Add(time.Minute)

	t.Run("late raw sample is history", func(t *testing.T) {
		t.Parallel()
		late := previewSample{id: 200, ts: T1}    // arrived after inOrder (id 200 > 100) but at an earlier time
		inOrder := previewSample{id: 100, ts: T2} // arrived first; newer
		got := judgedInArrival([]previewSample{inOrder, late})
		if len(got) != 1 || got[0].id != 100 {
			t.Fatalf("got %v, want only the in-order sample (id 100)", ids(got))
		}
	})

	t.Run("late folded sample with known arrival id is history", func(t *testing.T) {
		t.Parallel()
		// Same scenario but samples are folded (from a block that stored arrival_id).
		late := previewSample{id: 200, ts: T1, folded: true}
		inOrder := previewSample{id: 100, ts: T2, folded: true}
		got := judgedInArrival([]previewSample{inOrder, late})
		if len(got) != 1 || got[0].id != 100 {
			t.Fatalf("got %v, want only the in-order folded sample (id 100)", ids(got))
		}
	})

	t.Run("old block (no arrival_id, negative ids) always kept", func(t *testing.T) {
		t.Parallel()
		// Negative ids signal an older block; arrival order is unknown.
		s1 := previewSample{id: -1, ts: T1, folded: true}
		s2 := previewSample{id: -2, ts: T2, folded: true}
		got := judgedInArrival([]previewSample{s2, s1})
		if len(got) != 2 {
			t.Fatalf("got %v, want both old-block samples kept", ids(got))
		}
	})

	t.Run("late folded sample is history alongside raw samples", func(t *testing.T) {
		t.Parallel()
		// A late folded sample (arrived after the raw inOrder, so its id > inOrder.id)
		// but at an earlier time: should be dropped even though it is folded.
		lateFolded := previewSample{id: 300, ts: T1, folded: true}
		inOrder := previewSample{id: 150, ts: T3}
		got := judgedInArrival([]previewSample{inOrder, lateFolded})
		if len(got) != 1 || got[0].id != 150 {
			t.Fatalf("got %v, want only the in-order raw sample (id 150)", ids(got))
		}
	})

	t.Run("in-order samples all kept", func(t *testing.T) {
		t.Parallel()
		s1 := previewSample{id: 10, ts: T1}
		s2 := previewSample{id: 20, ts: T2}
		s3 := previewSample{id: 30, ts: T3}
		got := judgedInArrival([]previewSample{s1, s2, s3})
		if len(got) != 3 {
			t.Fatalf("got %v, want all three in-order samples", ids(got))
		}
	})
}

func ids(ss []previewSample) []int64 {
	out := make([]int64, len(ss))
	for i, s := range ss {
		out[i] = s.id
	}
	return out
}
