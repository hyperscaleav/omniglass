package compressbench

import (
	"strings"
	"testing"
	"time"
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
	for d, want := range map[time.Duration]string{2 * time.Second: "2s", 30 * time.Second: "30s", time.Minute: "60s", 24 * time.Hour: "24h"} {
		if got := CadenceLabel(d); got != want {
			t.Errorf("%s: %q, want %q", d, got, want)
		}
	}
}

func TestTable(t *testing.T) {
	ms := []Measure{{Samples: 1000, Bytes: 10400, Compress: time.Millisecond, ReadRaw: time.Millisecond, ReadBuckets: time.Millisecond}}
	r := Report{Cells: []Cell{
		Summarize("raw", 2*time.Second, ms, false),
		Summarize("F0", 2*time.Second, ms, true),
		Summarize("raw", 24*time.Hour, ms, false),
		Summarize("F0/day", 24*time.Hour, ms, true),
		Summarize("F0", 24*time.Hour, ms, true),
	}}
	r.Cells[1].Error = "round trip: sample 3 | differs\nmore"
	out := r.Table()
	for _, want := range []string{
		"### 2s cadence", "### 24h cadence",
		"| F0 | 1000 | 10.40 | 1.00 | 1.00 | 1.00 | **no**: round trip: sample 3 \\| differs |",
		"| raw | 1000 | 10.40 | n/a |",
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
