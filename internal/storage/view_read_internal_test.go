package storage

import (
	"errors"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// The grid is aligned to the epoch: it opens on the bucket holding from and
// closes after the bucket holding the instant before to.
func TestViewGridAlignsToTheEpoch(t *testing.T) {
	t.Parallel()
	g, err := newViewGrid(at("2026-01-01T10:07:00Z"), at("2026-01-01T11:00:00Z"), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !g.lo.Equal(at("2026-01-01T10:00:00Z")) || !g.hi.Equal(at("2026-01-01T11:00:00Z")) || g.n() != 4 {
		t.Fatalf("grid %s..%s n=%d", g.lo, g.hi, g.n())
	}
	g, _ = newViewGrid(at("2026-01-01T10:07:00Z"), at("2026-01-01T10:52:30Z"), 15*time.Minute)
	if !g.hi.Equal(at("2026-01-01T11:00:00Z")) {
		t.Fatalf("a to inside a bucket closes after it: %s", g.hi)
	}
}

// A grid past ViewMaxBuckets, or a step that is not whole seconds, is
// refused rather than truncated.
func TestViewGridRefusesTooManyBuckets(t *testing.T) {
	t.Parallel()
	if _, err := newViewGrid(at("2026-01-01T00:00:00Z"), at("2026-02-01T00:00:00Z"), time.Second); !errors.Is(err, ErrViewTooLarge) {
		t.Fatalf("a month of seconds: %v", err)
	}
	if _, err := newViewGrid(at("2026-01-01T00:00:00Z"), at("2026-01-01T01:00:00Z"), 1500*time.Millisecond); !errors.Is(err, ErrViewTooLarge) {
		t.Fatalf("a fractional step: %v", err)
	}
}

// Inside the raw retention the raw rows answer; an hour-multiple step reaching
// past it takes the whole hours before the raw floor's next hour from the
// trends, back to the trend floor; a sub-hour step never does.
func TestSplitMetricReadPicksRawOrTrends(t *testing.T) {
	t.Parallel()
	now := at("2026-03-31T12:30:00Z")
	inside := splitMetricRead(at("2026-03-30T00:00:00Z"), now, time.Hour, 30, 730)
	if inside.trends || !inside.rawFrom.Equal(at("2026-03-30T00:00:00Z")) || !inside.covered.Equal(inside.rawFrom) {
		t.Fatalf("a range inside retention: %+v", inside)
	}
	past := splitMetricRead(at("2025-12-01T00:00:00Z"), now, 24*time.Hour, 30, 730)
	if !past.trends || !past.rawFrom.Equal(at("2026-03-01T13:00:00Z")) || !past.trendFrom.Equal(at("2025-12-01T00:00:00Z")) || !past.covered.Equal(past.trendFrom) {
		t.Fatalf("a range past retention: %+v", past)
	}
	fine := splitMetricRead(at("2025-12-01T00:00:00Z"), now, 15*time.Minute, 30, 730)
	if fine.trends || !fine.rawFrom.Equal(at("2026-03-01T12:30:00Z")) {
		t.Fatalf("a sub-hour step reads raw only: %+v", fine)
	}
	shallow := splitMetricRead(at("2025-12-01T00:00:00Z"), now, time.Hour, 30, 10)
	if shallow.trends {
		t.Fatalf("trends kept shorter than the raw rows add nothing: %+v", shallow)
	}
}

// Parts of one bucket merge: counts and sums add, min and max fold, and the
// later part's last wins; a bucket with no part stays empty.
func TestMergeMetricPartsStitchesTrendAndRaw(t *testing.T) {
	t.Parallel()
	g, _ := newViewGrid(at("2026-01-01T00:00:00Z"), at("2026-01-01T06:00:00Z"), 2*time.Hour)
	parts := []metricPart{
		{owner: "c1", b: at("2026-01-01T02:00:00Z"), count: 2, min: 1, max: 5, sum: 6, last: 5},
		{owner: "c1", b: at("2026-01-01T02:00:00Z"), count: 2, min: 0, max: 3, sum: 2, last: 0},
		{owner: "c2", b: at("2026-01-01T00:00:00Z"), count: 1, min: 7, max: 7, sum: 7, last: 7},
	}
	out := mergeMetricParts("cpu", g, parts)
	if len(out) != 2 || out[0].EntityID != "c1" || out[1].EntityID != "c2" {
		t.Fatalf("series: %+v", out)
	}
	b := out[0].Buckets
	if len(b) != 3 || b[0].Count != 0 || b[0].Avg != nil || b[2].Count != 0 {
		t.Fatalf("empty buckets: %+v", b)
	}
	m := b[1]
	if m.Count != 4 || *m.Min != 0 || *m.Max != 5 || *m.Avg != 2 || *m.Last != 0 {
		t.Fatalf("merged bucket: count=%d min=%v max=%v avg=%v last=%v", m.Count, *m.Min, *m.Max, *m.Avg, *m.Last)
	}
}

// A property grid counts each bucket's changes, keeps the value its own
// changes end on, and holds the value in force across empty buckets.
func TestPropertyGridHoldsAcrossEmptyBuckets(t *testing.T) {
	t.Parallel()
	g, _ := newViewGrid(at("2026-01-01T00:00:00Z"), at("2026-01-01T03:00:00Z"), time.Hour)
	on := "on"
	out := propertyGrid(g, []ViewPropertyPoint{
		{TS: at("2026-01-01T01:10:00Z"), Value: "standby"},
		{TS: at("2026-01-01T01:20:00Z"), Value: "off"},
	}, &on)
	if len(out) != 3 || *out[0].Held != "on" || out[0].Last != nil {
		t.Fatalf("carried in: %+v", out[0])
	}
	if out[1].Count != 2 || *out[1].Last != "off" || *out[2].Held != "off" || out[2].Count != 0 {
		t.Fatalf("changes and hold: %+v %+v", out[1], out[2])
	}
}

// A page cursor round-trips, and one that does not parse is refused.
func TestViewPageRoundTrips(t *testing.T) {
	t.Parallel()
	ts := at("2026-01-01T00:00:00Z").Add(123 * time.Microsecond)
	got, id, err := parseViewPage(viewPage(ts, 42))
	if err != nil || !got.Equal(ts) || id != 42 {
		t.Fatalf("round trip: %v %v %d", err, got, id)
	}
	for _, bad := range []string{"", "x", "1.y", "z.1"} {
		if _, _, err := parseViewPage(bad); !errors.Is(err, ErrViewPage) {
			t.Fatalf("page %q: %v", bad, err)
		}
	}
}
