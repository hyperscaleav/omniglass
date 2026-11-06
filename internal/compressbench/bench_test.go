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

// TestBenchPlainBlocks holds the summary-free block to its span rule against a
// real Postgres 18: a week of changing cadence with a week minimum that sends
// the sparse series raw, then 1 h and 24 h week blocks with metric_trend
// beside them, each held to the exact round trip and every series-day's span.
func TestBenchPlainBlocks(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a Postgres container")
	}
	ctx := context.Background()
	servers := &Servers{}
	t.Cleanup(func() { servers.Stop(context.WithoutCancel(ctx)) })
	mixed, err := Bench(ctx, servers, Options{Specs: []Spec{MixedSpec(7)}, Runs: 1, Reps: 1, BucketWidth: time.Hour,
		Candidates: func(Spec) []Candidate { return []Candidate{Plain{WeekMin: 10}.Candidate(MixedSpec(7))} }})
	if err != nil {
		t.Fatalf("mixed: %v", err)
	}
	spans := map[string]int{}
	for _, c := range mixed.Cells {
		for _, d := range c.DaySpans {
			spans[d.Span]++
		}
	}
	if spans["raw"] == 0 || spans["week"] == 0 || spans["day"] == 0 {
		t.Errorf("mixed spans %v, want raw, week and day days", spans)
	}
	slow := []Spec{
		{Cadence: time.Hour, Series: 3, Span: 21 * 24 * time.Hour, Seed: 7, Start: monday},
		{Cadence: 24 * time.Hour, Series: 10, Span: 40 * 24 * time.Hour, Seed: 7},
	}
	r, err := Bench(ctx, servers, Options{Specs: slow, Runs: 1, Reps: 1, BucketWidth: time.Hour, Candidates: PlainCandidates})
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	r.Cells = append(r.Cells, mixed.Cells...)
	cells := map[string]Cell{}
	for _, c := range r.Cells {
		if c.Error != "" {
			t.Errorf("%s at %s: %s", c.Candidate, c.Cadence, c.Error)
		}
		cells[c.Cadence+" "+c.Candidate] = c
	}
	for _, k := range []string{"1h raw", "1h F2/week", "1h F2p", "1h F2p trend", "24h F2p", "24h F2p trend", "mixed F2p"} {
		if _, ok := cells[k]; !ok {
			t.Errorf("no cell %q", k)
		}
	}
	if c := cells["24h F2p"]; c.SpanDays["week"] == 0 || c.SummaryShare.N != 0 || c.CompressedRows.N != 1 {
		t.Errorf("24h F2p spans %v, summary share %+v, rows compressed %+v", c.SpanDays, c.SummaryShare, c.CompressedRows)
	}
}
