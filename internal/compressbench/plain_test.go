package compressbench

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// monday is the ISO week start the plain tests and the week sweep build on.
var monday = time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

// daySamples is n samples of series spread evenly over the day starting at day.
func daySamples(series int64, day time.Time, n int) []Sample {
	out := make([]Sample, n)
	for i := range out {
		out[i] = Sample{series, day.Add(time.Duration(i) * 24 * time.Hour / time.Duration(n)), float64(i)}
	}
	return out
}

func TestPlainSpansThresholds(t *testing.T) {
	var s []Sample
	s = append(s, daySamples(1, monday, 24*PlainHourMin)...)
	s = append(s, daySamples(1, monday.AddDate(0, 0, 1), 24*PlainHourMin-1)...)
	s = append(s, daySamples(1, monday.AddDate(0, 0, 2), PlainDayMin)...)
	s = append(s, daySamples(1, monday.AddDate(0, 0, 3), PlainDayMin-1)...)
	got := PlainSpans(s, 1)
	want := []DaySpan{
		{1, monday, "hour", 24 * PlainHourMin},
		{1, monday.AddDate(0, 0, 1), "day", 24*PlainHourMin - 1},
		{1, monday.AddDate(0, 0, 2), "day", PlainDayMin},
		{1, monday.AddDate(0, 0, 3), "week", PlainDayMin - 1},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("spans\n got %+v\nwant %+v", got, want)
	}
}

// A day too sparse for its own block waits for its ISO week, which goes raw
// when the week's waiting days hold fewer than the week minimum.
func TestPlainSpansWeekMinimum(t *testing.T) {
	sun := monday.AddDate(0, 0, -1)
	var s []Sample
	s = append(s, daySamples(1, sun, 1)...)                     // the week before, alone
	s = append(s, daySamples(1, monday, 1)...)                  // waits with Tuesday
	s = append(s, daySamples(1, monday.AddDate(0, 0, 1), 1)...) // two in the week
	s = append(s, daySamples(1, monday.AddDate(0, 0, 2), PlainDayMin)...)
	s = append(s, daySamples(2, monday, 1)...)
	got := PlainSpans(s, 2)
	want := []DaySpan{
		{1, sun, "raw", 1},
		{1, monday, "week", 1},
		{1, monday.AddDate(0, 0, 1), "week", 1},
		{1, monday.AddDate(0, 0, 2), "day", PlainDayMin},
		{2, monday, "raw", 1},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("spans\n got %+v\nwant %+v", got, want)
	}
}

func TestGenerateStartAndPhases(t *testing.T) {
	spec := Spec{Start: monday, Span: 48 * time.Hour, Seed: 7, Phases: [][]Phase{
		{{From: 0, To: 12 * time.Hour, Cadence: time.Minute}, {From: 12 * time.Hour, To: 48 * time.Hour, Cadence: 30 * time.Second}},
		{{From: 24 * time.Hour, To: 48 * time.Hour, Cadence: time.Hour}},
	}}
	if spec.End() != monday.Add(48*time.Hour) {
		t.Fatalf("end %s", spec.End())
	}
	samples := Generate(spec)
	count := map[int64]map[time.Duration]int{}
	for i, s := range samples {
		if s.TS.Before(monday) || !s.TS.Before(spec.End()) {
			t.Fatalf("sample %d at %s, outside the span", i, s.TS)
		}
		if i > 0 && (s.SeriesID < samples[i-1].SeriesID || s.SeriesID == samples[i-1].SeriesID && s.TS.Before(samples[i-1].TS)) {
			t.Fatalf("sample %d out of order", i)
		}
		if count[s.SeriesID] == nil {
			count[s.SeriesID] = map[time.Duration]int{}
		}
		count[s.SeriesID][s.TS.Sub(monday)/(12*time.Hour)*12*time.Hour]++
	}
	// Each phase polls its own cadence, plus about 2% duplicate reports.
	near := func(got, polls int) bool { return got >= polls && got <= polls*106/100 }
	if c := count[1]; !near(c[0], 720) || !near(c[12*time.Hour], 1440) || !near(c[36*time.Hour], 1440) {
		t.Errorf("series 1 by half day %v", c)
	}
	if c := count[2]; c[0] != 0 || c[12*time.Hour] != 0 || !near(c[24*time.Hour], 12) {
		t.Errorf("series 2 by half day %v", c)
	}
	if len(count) != 2 {
		t.Errorf("%d series, want 2", len(count))
	}
}

func TestSpecLabel(t *testing.T) {
	if got := (Spec{Cadence: time.Hour}).Label(); got != "1h" {
		t.Errorf("label %q", got)
	}
	if got := (Spec{Cadence: time.Hour, Name: "168/wk"}).Label(); got != "168/wk" {
		t.Errorf("label %q", got)
	}
}

func TestWeekMinimum(t *testing.T) {
	rows := []SweepRow{{1, 110, 300}, {2, 108, 90}, {3, 106, 120}, {4, 105, 60}, {7, 104, 40}}
	if k, ok := WeekMinimum(rows); !ok || k != 4 {
		t.Fatalf("minimum %d %v, want 4: every count from it up is cheaper than raw", k, ok)
	}
	if _, ok := WeekMinimum([]SweepRow{{1, 100, 200}}); ok {
		t.Fatal("a block never cheaper than raw has no minimum")
	}
}

func TestSweepFromReport(t *testing.T) {
	ms := func(b int64) []Measure { return []Measure{{Samples: 100, Bytes: b}} }
	var r Report
	for _, k := range []int{1, 48} {
		spec := SweepSpec(k, 10, 7)
		r.Specs = append(r.Specs, spec)
		raw := Summarize("raw", spec.Cadence, ms(10000), false)
		raw.Cadence = spec.Label()
		pl := Summarize("F2p", spec.Cadence, ms(int64(20000/k)), true)
		pl.Cadence = spec.Label()
		r.Cells = append(r.Cells, raw, pl)
	}
	rows := Sweep(r)
	if len(rows) != 2 || rows[0] != (SweepRow{1, 100, 200}) || rows[1].PerWeek != 48 {
		t.Fatalf("rows %+v", rows)
	}
	if r.Specs[1].Cadence != 210*time.Minute || r.Specs[1].Start != monday {
		t.Fatalf("48 a week is %s from %s", r.Specs[1].Cadence, r.Specs[1].Start)
	}
	if out := SweepTable(rows); !strings.Contains(out, "| 48 |") || !strings.Contains(out, "48 samples a week") {
		t.Fatalf("table:\n%s", out)
	}
}

// A sample before the bucketing origin lands in the bucket that starts at or
// before it, as date_bin puts it, not the one after.
func TestBucketsBeforeEpoch(t *testing.T) {
	ts := Epoch.Add(-90 * time.Minute)
	got := Buckets([]Sample{{1, ts, 2}}, 1, ts, Epoch, time.Hour)
	if len(got) != 1 || !got[0].Start.Equal(Epoch.Add(-2*time.Hour)) {
		t.Fatalf("buckets %+v", got)
	}
}

func TestPlainTable(t *testing.T) {
	spec := Spec{Cadence: time.Hour}
	ms := func(b int64) []Measure {
		return []Measure{{Samples: 100, Bytes: b, ReadBuckets: 2 * time.Millisecond, Blocks: true, CompressedRows: 0.8}}
	}
	r := Report{Specs: []Spec{spec}, Cells: []Cell{
		Summarize("raw", time.Hour, ms(12000), false),
		Summarize("F2/week", time.Hour, ms(2600), true),
		Summarize("F2p", time.Hour, ms(1000), true),
		Summarize("F2p trend", time.Hour, ms(9000), true),
	}}
	out := PlainTable(r)
	for _, want := range []string{"| 1h | week | 26.00 | 10.00 | 26.04 | 10.4 | 90.00 |", "80.0%"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}
