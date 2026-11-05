package compressbench

import (
	"context"
	"testing"
	"time"
)

// TestBenchBlockFormatsRoundTrip runs every homegrown format, hour and day
// blocks, against a real Postgres 18 and holds each to the exact round trip
// the shootout depends on. The extension candidates need the test-only images
// `make compress-bench` builds, so they are left to it.
func TestBenchBlockFormatsRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a Postgres container")
	}
	ctx := context.Background()
	servers := &Servers{}
	t.Cleanup(func() { servers.Stop(context.WithoutCancel(ctx)) })
	specs := []Spec{
		{Cadence: time.Minute, Series: 5, Span: 48 * time.Hour, Seed: 7},
		{Cadence: 24 * time.Hour, Series: 10, Span: 40 * 24 * time.Hour, Seed: 7},
	}
	r, err := Bench(ctx, servers, Options{Specs: specs, Runs: 1, Reps: 1, Only: []string{"raw", "F0", "F1", "F2", "F3"}})
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	want := map[string]bool{}
	for _, c := range r.Cells {
		if c.Error != "" {
			t.Errorf("%s at %s: %s", c.Candidate, c.Cadence, c.Error)
		}
		if c.BytesPerSample.Median <= 0 {
			t.Errorf("%s at %s: no bytes per sample", c.Candidate, c.Cadence)
		}
		want[c.Cadence+" "+c.Candidate] = true
	}
	for _, k := range []string{"60s raw", "60s F0", "60s F3", "24h F0/day", "24h F3/day"} {
		if !want[k] {
			t.Errorf("no cell %q in %v", k, want)
		}
	}
}
