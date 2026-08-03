package views_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/views"
)

// TestResultHash proves the hash is stable for identical rows and moves for
// any cell delta, the whole contract the change detector rests on.
func TestResultHash(t *testing.T) {
	ts := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	a1 := views.ResultHash([][]any{{"disp-1", "up", ts}})
	a2 := views.ResultHash([][]any{{"disp-1", "up", ts}})
	if a1 != a2 {
		t.Errorf("identical rows hash differently: %s vs %s", a1, a2)
	}
	if b := views.ResultHash([][]any{{"disp-1", "down", ts}}); b == a1 {
		t.Errorf("a changed cell did not move the hash")
	}
	if empty := views.ResultHash([][]any{}); empty == "" {
		t.Errorf("empty rows must hash cleanly, got %q", empty)
	}
	// Row ORDER is content: a re-ordered result is a change a watcher must
	// see, which is why every view's query owes a total order.
	rows := [][]any{{"a", "up", ts}, {"b", "down", ts}}
	swapped := [][]any{{"b", "down", ts}, {"a", "up", ts}}
	if views.ResultHash(rows) == views.ResultHash(swapped) {
		t.Errorf("row order does not affect the hash")
	}
	// Cell boundaries are real: splitting a value across two cells must not
	// collide with the joined form.
	if views.ResultHash([][]any{{"ab", "c"}}) == views.ResultHash([][]any{{"a", "bc"}}) {
		t.Errorf("cell boundaries collide")
	}
	if views.ResultHash([][]any{{"a"}, {"b"}}) == views.ResultHash([][]any{{"a", "b"}}) {
		t.Errorf("row boundaries collide")
	}
	// Values a JSON encoder would refuse but Postgres can legally hold: a NaN
	// or an infinity in a double column, a year outside 0..9999 in a
	// timestamptz. The hash must fold them, not fail: an error here would kill
	// the stream on every re-run.
	for _, cell := range []any{math.NaN(), math.Inf(1), math.Inf(-1), time.Date(-4000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if h := views.ResultHash([][]any{{cell}}); h == "" {
			t.Errorf("hash of %v is empty", cell)
		}
	}
	// An equal instant in a different location is not a change.
	loc := time.FixedZone("plus2", 2*3600)
	if views.ResultHash([][]any{{ts}}) != views.ResultHash([][]any{{ts.In(loc)}}) {
		t.Errorf("the same instant hashes differently across locations")
	}
	// nil is its own value, not the empty string.
	if views.ResultHash([][]any{{nil}}) == views.ResultHash([][]any{{""}}) {
		t.Errorf("a null cell hashes as the empty string")
	}
}

// TestWatchNotifiesBaselineThenDeltasOnly drives the detector loop with a fake
// tick channel: the first run notifies immediately (the connect baseline, so a
// reconnecting client always refetches once), an unchanged re-run stays
// silent, and a changed hash notifies exactly once per delta.
func TestWatchNotifiesBaselineThenDeltasOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tick := make(chan time.Time)
	hashes := []string{"h1", "h1", "h2", "h2"}
	i := 0
	run := func(context.Context) (string, error) {
		h := hashes[i]
		if i < len(hashes)-1 {
			i++
		}
		return h, nil
	}
	var got []string
	done := make(chan error, 1)
	go func() {
		done <- views.Watch(ctx, tick, run, func(h string) error {
			got = append(got, h)
			return nil
		})
	}()
	// Three ticks walk the script: h1 (silent), h2 (notify), h2 (silent).
	for range 3 {
		tick <- time.Time{}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("watch: %v", err)
	}
	want := []string{"h1", "h2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("notifications = %v, want %v (baseline then one delta)", got, want)
	}
}

// TestWatchStopsOnError proves a failing run or a failing notify ends the loop
// with the error surfaced, never a silent stall.
func TestWatchStopsOnError(t *testing.T) {
	boom := errors.New("boom")
	err := views.Watch(context.Background(), nil, func(context.Context) (string, error) {
		return "", boom
	}, func(string) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("run error = %v, want boom", err)
	}

	err = views.Watch(context.Background(), nil, func(context.Context) (string, error) {
		return "h", nil
	}, func(string) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("notify error = %v, want boom", err)
	}
}

// TestWatchEndsOnContext proves cancellation ends the loop cleanly (nil), the
// disconnect path of every SSE connection.
func TestWatchEndsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- views.Watch(ctx, make(chan time.Time), func(context.Context) (string, error) {
			return "h", nil
		}, func(string) error { return nil })
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancelled watch = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("watch did not end on context cancellation")
	}
}
