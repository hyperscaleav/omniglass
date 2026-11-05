package compressbench

import (
	"context"
	"testing"
	"time"
)

// TestBenchBlockFormatsRoundTrip runs every homegrown format, hour to month
// blocks, against a real Postgres 18 and holds each to the exact round trip
// and hourly summaries the shootout depends on, before and after a late
// sample is merged into a closed block. The extension candidates need the test-only images
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
		{Cadence: time.Hour, Series: 3, Span: 70 * 24 * time.Hour, Seed: 7},
		{Cadence: 24 * time.Hour, Series: 10, Span: 40 * 24 * time.Hour, Seed: 7},
	}
	r, err := Bench(ctx, servers, Options{Specs: specs, Runs: 1, Reps: 1, Only: []string{"raw", "F0", "F2"}})
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
		if c.MergeMs.N != 1 || c.MergeWAL.Median <= 0 {
			t.Errorf("%s at %s: late merge %+v, WAL %+v", c.Candidate, c.Cadence, c.MergeMs, c.MergeWAL)
		}
		if blocks := c.Candidate != "raw"; blocks != (c.SummaryShare.N == 1) || blocks != (c.CompressedRows.N == 1) {
			t.Errorf("%s at %s: summary share %+v, rows compressed %+v", c.Candidate, c.Cadence, c.SummaryShare, c.CompressedRows)
		}
		want[c.Cadence+" "+c.Candidate] = true
	}
	for _, k := range []string{"60s raw", "60s F0", "60s F2/day", "1h F0/week", "1h F2/month", "24h F2/day"} {
		if !want[k] {
			t.Errorf("no cell %q in %v", k, want)
		}
	}
	if got := SpanRule(r, "F2"); len(got) != len(specs) {
		t.Errorf("span rule %+v, want a choice a cadence", got)
	}
}
