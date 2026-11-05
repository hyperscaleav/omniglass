package compressbench

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

func TestSummarize(t *testing.T) {
	ms := []Measure{
		{Samples: 2000, Bytes: 20000, Compress: 40 * time.Millisecond, ReadRaw: 3 * time.Millisecond, ReadBuckets: time.Millisecond},
		{Samples: 2000, Bytes: 24000, Compress: 20 * time.Millisecond, ReadRaw: time.Millisecond, ReadBuckets: time.Millisecond},
		{Samples: 2000, Bytes: 22000, Compress: 30 * time.Millisecond, ReadRaw: 2 * time.Millisecond, ReadBuckets: time.Millisecond},
	}
	c := Summarize("F0", 2*time.Second, ms, true)
	if c.Cadence != "2s" || c.Samples != 2000 {
		t.Fatalf("cell %+v", c)
	}
	if c.BytesPerSample.Median != 11 || c.BytesPerSample.Min != 10 || c.BytesPerSample.Max != 12 {
		t.Fatalf("bytes per sample %+v, want 11 (10 to 12)", c.BytesPerSample)
	}
	if c.CompressPer1k.Median != 15 {
		t.Fatalf("compress per 1k %+v, want 15 ms (30 ms over 2000 samples)", c.CompressPer1k)
	}
	if c.ReadRaw.Median != 2 {
		t.Fatalf("raw read %+v, want 2 ms", c.ReadRaw)
	}
	raw := Summarize("raw", 2*time.Second, ms, false)
	if raw.CompressPer1k.N != 0 {
		t.Fatalf("raw is not compressed, got %+v", raw.CompressPer1k)
	}
}

func TestCadenceLabel(t *testing.T) {
	for d, want := range map[time.Duration]string{2 * time.Second: "2s", 10 * time.Second: "10s", 30 * time.Second: "30s", time.Minute: "60s", 5 * time.Minute: "5m", time.Hour: "1h", 24 * time.Hour: "24h"} {
		if got := CadenceLabel(d); got != want {
			t.Errorf("%s: %q, want %q", d, got, want)
		}
	}
}

func TestTable(t *testing.T) {
	ms := []Measure{{Samples: 1000, Bytes: 10400, Compress: time.Millisecond, ReadRaw: time.Millisecond, ReadBuckets: time.Millisecond}}
	bm := []Measure{{Samples: 1000, Bytes: 10400, Compress: time.Millisecond, ReadRaw: time.Millisecond, ReadBuckets: time.Millisecond,
		Blocks: true, SummaryBytes: 2600, CompressedRows: 0.5, Merged: true, Merge: 2 * time.Millisecond, MergeWAL: 8192}}
	r := Report{Cells: []Cell{
		Summarize("raw", 2*time.Second, ms, false),
		Summarize("F0", 2*time.Second, bm, true),
		Summarize("raw", 24*time.Hour, ms, false),
		Summarize("F0/day", 24*time.Hour, ms, true),
		Summarize("F0", 24*time.Hour, ms, true),
	}}
	r.Cells[1].Error = "round trip: sample 3 | differs\nmore"
	out := r.Table()
	for _, want := range []string{
		"### 2s cadence", "### 24h cadence",
		"| F0 | 1000 | 10.40 | 25.0% | 50.0% | 1.00 | 1.00 | 1.00 | 2.00 | 8192 | **no**: round trip: sample 3 \\| differs |",
		"| raw | 1000 | 10.40 | n/a | n/a | n/a |",
		"| candidate | 2s | 24h |",
		"| F0/day | n/a | 10.40 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	summary := out[strings.Index(out, "### Bytes per sample"):]
	if strings.Index(summary, "| F0 |") > strings.Index(summary, "| F0/day |") {
		t.Errorf("F0 should precede its day variant:\n%s", out)
	}
}

// TestSpanRule picks, per cadence and family, the smallest span whose rows
// compress, falls back to the smallest bytes when none does, and keeps a
// series raw when no block beats the narrow row.
func TestSpanRule(t *testing.T) {
	cell := func(name, cad string, bps, compressed float64) Cell {
		c := Cell{Candidate: name, Cadence: cad, BytesPerSample: labfootprint.StatOf([]float64{bps})}
		if name != "raw" {
			c.CompressedRows = labfootprint.StatOf([]float64{compressed})
		}
		return c
	}
	r := Report{
		Specs: []Spec{{Cadence: 10 * time.Second}, {Cadence: time.Hour}, {Cadence: 24 * time.Hour}},
		Cells: []Cell{
			cell("raw", "10s", 40, 0), cell("F2", "10s", 6, 1), cell("F2/day", "10s", 5, 1),
			cell("raw", "1h", 40, 0), cell("F2", "1h", 70, 0), cell("F2/day", "1h", 20, 0), cell("F2/week", "1h", 9, 1), cell("F2/month", "1h", 8, 1),
			cell("raw", "24h", 40, 0), cell("F2", "24h", 90, 0), cell("F2/day", "24h", 90, 0),
		},
	}
	got := SpanRule(r, "F2")
	want := []SpanChoice{
		{Cadence: "10s", Span: "hour", BytesPerSample: 6, RawBytesPerSample: 40},
		{Cadence: "1h", Span: "week", BytesPerSample: 9, RawBytesPerSample: 40},
		{Cadence: "24h", Span: "raw", BytesPerSample: 40, RawBytesPerSample: 40},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rule %+v, want %+v", got, want)
	}
	if c := RawCutoff(got); c != "24h" {
		t.Fatalf("cutoff %q, want 24h", c)
	}
	if c := RawCutoff(want[:2]); c != "" {
		t.Fatalf("cutoff %q with no raw choice", c)
	}
	out := r.RuleTable()
	for _, w := range []string{"| 1h | week | 9.00 | 40.00 |", "| 24h | raw | 40.00 | 40.00 |", "at 24h or slower"} {
		if !strings.Contains(out, w) {
			t.Errorf("rule table lacks %q:\n%s", w, out)
		}
	}
}
