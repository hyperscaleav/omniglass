package obsstat

import (
	"math"
	"sort"
	"testing"
	"time"
)

func TestLatencyBucketsPlaceKnownDurations(t *testing.T) {
	for _, tc := range []struct {
		us   uint64
		want int
	}{
		{0, 0}, {1, 0}, {2, 8}, {3, 12}, {7, 22}, {8, 24}, {9, 25}, {15, 31}, {16, 32}, {17, 32}, {18, 33},
		{1000, 9*8 + 7}, {1 << 25, 25 * 8}, {1<<26 - 1, LatencyBuckets - 1}, {1 << 40, LatencyBuckets - 1},
	} {
		got := LatencyBucket(tc.us)
		if got != tc.want {
			t.Errorf("bucket(%d us) = %d, want %d", tc.us, got, tc.want)
			continue
		}
		lo, hi := LatencyBucketBounds(got)
		if tc.us < 1<<26 && tc.us > 0 && (float64(tc.us) < lo || float64(tc.us) >= hi) {
			t.Errorf("%d us outside its bucket [%v, %v)", tc.us, lo, hi)
		}
	}
}

func TestLatencyHistogramMergeEqualsOneFedBoth(t *testing.T) {
	var a, b, both LatencyHistogram
	for i := range 500 {
		d := time.Duration(i*i) * time.Microsecond
		a.Record(d)
		both.Record(d)
	}
	for i := range 300 {
		d := time.Duration(i*977) * time.Microsecond
		b.Record(d)
		both.Record(d)
	}
	merged := MergeLatency(a.Counts(), b.Counts())
	want := both.Counts()
	if len(merged) != len(want) {
		t.Fatalf("merged has %d buckets, one histogram fed both has %d", len(merged), len(want))
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Fatalf("bucket %d: merged %d, fed both %d", i, merged[i], want[i])
		}
	}
}

func TestLatencyQuantileWithinOneBucket(t *testing.T) {
	var h LatencyHistogram
	var all []float64
	for i := 1; i <= 10_000; i++ {
		us := float64(i) * 37 // 37 us to 370 ms, uniform
		all = append(all, us)
		h.Record(time.Duration(us) * time.Microsecond)
	}
	sort.Float64s(all)
	exact := all[int(math.Ceil(0.95*float64(len(all))))-1]
	got := LatencyQuantile(h.Counts(), 0.95)
	lo, hi := LatencyBucketBounds(LatencyBucket(uint64(exact)))
	if math.Abs(got-exact) > hi-lo {
		t.Fatalf("p95 = %v us, exact %v us, bucket width %v", got, exact, hi-lo)
	}
	if LatencyQuantile(nil, 0.95) != 0 {
		t.Fatalf("an empty histogram has a quantile")
	}
}

func TestHighWaterRingKeepsTheMaxPerEpochAndForgetsOldOnes(t *testing.T) {
	var r HighWaterRing
	r.Observe(100, 3)
	r.Observe(100, 7)
	r.Observe(100, 5)
	r.Observe(101, 2)
	r.Observe(100+HighWaterSlots, 4) // reuses epoch 100's slot
	got := map[int64]int64{}
	for _, e := range r.Epochs() {
		got[e.Epoch] = e.Max
	}
	if got[100] != 0 || got[101] != 2 || got[100+HighWaterSlots] != 4 {
		t.Fatalf("epochs = %v", got)
	}
	var s HighWaterRing
	s.Observe(5, 7)
	s.Observe(5, 3)
	if e := s.Epochs(); len(e) != 1 || e[0].Max != 7 {
		t.Fatalf("a lower reading replaced the max: %v", e)
	}
}
