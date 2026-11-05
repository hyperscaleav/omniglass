package compressbench

import (
	"math"
	"testing"
	"time"
)

func testSpec() Spec {
	return Spec{Cadence: 2 * time.Second, Series: 5, Span: 2 * time.Hour, Seed: 7}
}

func TestGenerateIsDeterministic(t *testing.T) {
	a, b := Generate(testSpec()), Generate(testSpec())
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("lengths %d and %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
	other := testSpec()
	other.Seed = 8
	c := Generate(other)
	same := len(c) == len(a)
	for i := 0; same && i < len(a); i++ {
		same = a[i] == c[i]
	}
	if same {
		t.Fatal("a different seed produced the same samples")
	}
}

func TestGenerateShape(t *testing.T) {
	spec := testSpec()
	samples := Generate(spec)
	start := spec.start()
	polls := int(spec.Span / spec.Cadence)
	perSeries := map[int64]int{}
	dupes := 0
	for i, s := range samples {
		perSeries[s.SeriesID]++
		if s.TS.Before(start) || !s.TS.Before(start.Add(spec.Span)) {
			t.Fatalf("sample %d at %s is outside the span", i, s.TS)
		}
		if s.TS.Sub(s.TS.Truncate(time.Millisecond)) != 0 {
			t.Fatalf("sample %d at %s is finer than a millisecond", i, s.TS)
		}
		if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
			t.Fatalf("sample %d value %v", i, s.Value)
		}
		if i == 0 {
			continue
		}
		p := samples[i-1]
		if p.SeriesID > s.SeriesID || (p.SeriesID == s.SeriesID && p.TS.After(s.TS)) {
			t.Fatalf("samples %d and %d are out of (series, ts) order", i-1, i)
		}
		if p.SeriesID == s.SeriesID && p.TS.Equal(s.TS) {
			dupes++
		}
	}
	if len(perSeries) != spec.Series {
		t.Fatalf("%d series, want %d", len(perSeries), spec.Series)
	}
	for id, n := range perSeries {
		if n < polls || n > polls*11/10 {
			t.Fatalf("series %d has %d samples for %d polls", id, n, polls)
		}
	}
	// ADR-0061: one poll cycle can report a series twice at one timestamp.
	if dupes == 0 {
		t.Fatal("no duplicate timestamps; the round trip would not exercise their order")
	}
}

func TestGenerateKinds(t *testing.T) {
	samples := Generate(Spec{Cadence: 30 * time.Second, Series: len(kinds), Span: 24 * time.Hour, Seed: 1})
	by := map[int64][]float64{}
	for _, s := range samples {
		by[s.SeriesID] = append(by[s.SeriesID], s.Value)
	}
	for i, k := range kinds {
		vs := by[int64(i+1)]
		distinct := map[float64]bool{}
		for _, v := range vs {
			distinct[v] = true
		}
		switch k {
		case kindConstant:
			if len(distinct) != 1 {
				t.Errorf("%s: %d distinct values, want 1", k, len(distinct))
			}
		case kindCounter:
			for j := 1; j < len(vs); j++ {
				if vs[j] < vs[j-1] {
					t.Fatalf("%s decreases at %d", k, j)
				}
			}
		case kindNoisy:
			if len(distinct) < len(vs)*9/10 {
				t.Errorf("%s: only %d distinct of %d", k, len(distinct), len(vs))
			}
		default:
			if len(distinct) < 2 || len(distinct) > len(vs)/2 {
				t.Errorf("%s: %d distinct of %d; a slow walk repeats values", k, len(distinct), len(vs))
			}
		}
	}
}

func TestRawWindowAndBuckets(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	samples := []Sample{
		{1, at(0), 4}, {1, at(30), 2}, {1, at(30), 9}, {1, at(70), 1},
		{2, at(10), 100},
		{1, at(130), 5},
	}
	got := RawWindow(samples, 1, at(30), at(130))
	want := []Point{{at(30), 2}, {at(30), 9}, {at(70), 1}}
	if len(got) != len(want) {
		t.Fatalf("raw window %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("raw window %v, want %v", got, want)
		}
	}

	buckets := Buckets(samples, 1, t0, at(180), time.Hour)
	wantB := []Bucket{
		{Start: at(0), Count: 3, Min: 2, Max: 9, Avg: 5},
		{Start: at(60), Count: 1, Min: 1, Max: 1, Avg: 1},
		{Start: at(120), Count: 1, Min: 5, Max: 5, Avg: 5},
	}
	if len(buckets) != len(wantB) {
		t.Fatalf("buckets %v, want %v", buckets, wantB)
	}
	for i := range wantB {
		if buckets[i] != wantB[i] {
			t.Fatalf("bucket %d %+v, want %+v", i, buckets[i], wantB[i])
		}
	}
}

func TestSamePoints(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	a := []Point{{t0, 1}, {t0, 2}}
	if err := SamePoints(a, []Point{{t0, 1}, {t0, 2}}); err != nil {
		t.Fatal(err)
	}
	if err := SamePoints(a, []Point{{t0, 2}, {t0, 1}}); err == nil {
		t.Fatal("swapped duplicates compared equal; order within a timestamp is the point")
	}
	if err := SamePoints(a, a[:1]); err == nil {
		t.Fatal("a missing sample compared equal")
	}
	if err := SamePoints(a, []Point{{t0.Add(time.Microsecond), 1}, {t0, 2}}); err == nil {
		t.Fatal("a shifted timestamp compared equal")
	}
}

func TestSameBuckets(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	a := []Bucket{{Start: t0, Count: 3, Min: 1, Max: 2, Avg: 1.0 / 3}}
	b := []Bucket{{Start: t0, Count: 3, Min: 1, Max: 2, Avg: 1.0/3 + 1e-17}}
	if err := SameBuckets(a, b); err != nil {
		t.Fatalf("summation order moved the average past tolerance: %v", err)
	}
	b[0].Max = 2.0000001
	if err := SameBuckets(a, b); err == nil {
		t.Fatal("a different max compared equal")
	}
	c := []Bucket{{Start: t0.AddDate(-30, 0, 0), Count: 3, Min: 1, Max: 2, Avg: 1.0 / 3}}
	if err := SameBuckets(a, c); err == nil {
		t.Fatal("a shifted bucket start compared equal")
	}
}

// TestHourlySummaries holds the reference per-hour summary to its samples:
// first and last in landing order within an instant, and an empty hour absent.
func TestHourlySummaries(t *testing.T) {
	h0, h2 := Epoch, Epoch.Add(2*time.Hour)
	samples := []Sample{
		{1, h0.Add(time.Minute), 3}, {1, h0.Add(time.Minute), 5}, {1, h0.Add(59 * time.Minute), 1},
		{1, h2, 7},
		{2, h0, -2},
	}
	got := HourlySummaries(samples)
	want := []Summary{
		{SeriesID: 1, Hour: h0, Count: 3, Min: 1, Max: 5, Sum: 9, First: 3, Last: 1},
		{SeriesID: 1, Hour: h2, Count: 1, Min: 7, Max: 7, Sum: 7, First: 7, Last: 7},
		{SeriesID: 2, Hour: h0, Count: 1, Min: -2, Max: -2, Sum: -2, First: -2, Last: -2},
	}
	if err := SameSummaries(want, got); err != nil {
		t.Fatal(err)
	}
	bad := append([]Summary(nil), want...)
	bad[0].Last = 5
	if SameSummaries(bad, got) == nil {
		t.Fatal("a wrong last value passed")
	}
	if SameSummaries(want[:2], got) == nil {
		t.Fatal("a missing hour passed")
	}
}

// TestLate is the late sample a merge lands: a second report at an instant
// the series already has, mid-span, so the block it lands in is closed and the
// sample must sort after the one already there.
func TestLate(t *testing.T) {
	spec := testSpec()
	samples := Generate(spec)
	late := Late(samples, 3)
	if late.SeriesID != 3 {
		t.Fatalf("late sample is series %d", late.SeriesID)
	}
	var at []int
	for i, s := range samples {
		if s.SeriesID == 3 && s.TS.Equal(late.TS) {
			at = append(at, i)
		}
	}
	if len(at) == 0 {
		t.Fatalf("late sample at %s matches no sample", late.TS)
	}
	merged := WithLate(samples, late)
	if len(merged) != len(samples)+1 {
		t.Fatalf("merged %d samples, want %d", len(merged), len(samples)+1)
	}
	last := at[len(at)-1]
	if merged[last+1] != late {
		t.Fatalf("sample %d is %+v, want the late sample after its instant's last", last+1, merged[last+1])
	}
	if merged[last] != samples[last] || merged[last+2] != samples[last+1] {
		t.Fatal("the samples around the late one moved")
	}
}
