package views_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/views"
)

// TestResultHash proves the hash is stable for identical rows and moves for
// any cell delta, the whole contract the change detector rests on.
func TestResultHash(t *testing.T) {
	ts := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	a1, err := views.ResultHash([][]any{{"disp-1", "up", ts}})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	a2, err := views.ResultHash([][]any{{"disp-1", "up", ts}})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if a1 != a2 {
		t.Errorf("identical rows hash differently: %s vs %s", a1, a2)
	}
	b, err := views.ResultHash([][]any{{"disp-1", "down", ts}})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if b == a1 {
		t.Errorf("a changed cell did not move the hash")
	}
	empty, err := views.ResultHash([][]any{})
	if err != nil || empty == "" {
		t.Errorf("empty rows must hash cleanly, got %q, %v", empty, err)
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
