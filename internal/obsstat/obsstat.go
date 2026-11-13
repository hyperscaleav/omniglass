// Package obsstat holds the two summaries both ends of a request keep about
// it (#1639): a latency histogram that merges across nodes by summing
// buckets, and an in-flight high-water ring a reader takes the max over for
// its own window. Both are fixed-size arrays of atomics, so recording is a
// few atomic operations and no allocation, and neither resets on read.
package obsstat

import (
	"math/bits"
	"sync/atomic"
	"time"
)

// The latency histogram: fixed log-linear buckets, eight per power of two of
// microseconds, from 1 us to about 67 s, so a quantile read from it is
// within one bucket width (an eighth of its power of two) of the truth, and
// two histograms merge by summing buckets. A sketch that cannot be summed
// across nodes (a t-digest) is refused for that reason.
const (
	latencySubBuckets = 8
	latencyOctaves    = 26
	// LatencyBuckets is the histogram's bucket count.
	LatencyBuckets = latencySubBuckets * latencyOctaves
)

// LatencyHistogram counts durations into the fixed buckets.
type LatencyHistogram struct {
	buckets [LatencyBuckets]atomic.Uint64
}

// LatencyBucket is the bucket a duration in microseconds falls in: octave e
// is the value's highest set bit, the sub-bucket its next three bits.
func LatencyBucket(us uint64) int {
	if us == 0 {
		return 0
	}
	e := bits.Len64(us) - 1
	if e >= latencyOctaves {
		return LatencyBuckets - 1
	}
	var sub uint64
	if e >= 3 {
		sub = (us >> (e - 3)) & 7
	} else {
		sub = (us << (3 - e)) & 7
	}
	return e*latencySubBuckets + int(sub)
}

// LatencyBucketBounds is a bucket's range in microseconds, [lo, hi).
func LatencyBucketBounds(i int) (lo, hi float64) {
	e, sub := i/latencySubBuckets, i%latencySubBuckets
	base := float64(uint64(1) << e)
	return base * (1 + float64(sub)/latencySubBuckets), base * (1 + float64(sub+1)/latencySubBuckets)
}

// Record counts one duration.
func (h *LatencyHistogram) Record(d time.Duration) {
	us := d.Microseconds()
	if us < 0 {
		us = 0
	}
	h.buckets[LatencyBucket(uint64(us))].Add(1)
}

// Counts reads the buckets, trailing empty ones trimmed.
func (h *LatencyHistogram) Counts() []uint64 {
	last := -1
	var out [LatencyBuckets]uint64
	for i := range out {
		out[i] = h.buckets[i].Load()
		if out[i] != 0 {
			last = i
		}
	}
	return append([]uint64(nil), out[:last+1]...)
}

// MergeLatency sums histograms bucket by bucket.
func MergeLatency(hs ...[]uint64) []uint64 {
	var out []uint64
	for _, h := range hs {
		for len(out) < len(h) {
			out = append(out, 0)
		}
		for i, n := range h {
			out[i] += n
		}
	}
	return out
}

// SubtractLatency is end minus start, bucket by bucket, for a window; a
// bucket that went backwards (a restart) reads zero.
func SubtractLatency(end, start []uint64) []uint64 {
	out := append([]uint64(nil), end...)
	for i := range out {
		if i < len(start) {
			if start[i] > out[i] {
				out[i] = 0
			} else {
				out[i] -= start[i]
			}
		}
	}
	return out
}

// LatencyQuantile reads quantile q (0 to 1) from bucket counts, in
// microseconds: the midpoint of the bucket the q-th observation falls in.
func LatencyQuantile(counts []uint64, q float64) float64 {
	var total uint64
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return 0
	}
	rank := uint64(q * float64(total))
	if float64(rank) < q*float64(total) || rank == 0 {
		rank++
	}
	var seen uint64
	for i, n := range counts {
		seen += n
		if seen >= rank {
			lo, hi := LatencyBucketBounds(i)
			if i == 0 {
				lo = 0
			}
			return (lo + hi) / 2
		}
	}
	lo, hi := LatencyBucketBounds(len(counts) - 1)
	return (lo + hi) / 2
}

// The high-water ring: one slot per 10 s epoch over the last ten minutes,
// each holding its epoch and the largest reading seen in it, packed into one
// word so a reading is one compare-and-swap. A reader takes the max over the
// epochs its own window covers.
const (
	// EpochSeconds is one epoch's length.
	EpochSeconds = 10
	// HighWaterSlots is the epochs the ring holds.
	HighWaterSlots = 60
)

// EpochOf is the epoch a time falls in.
func EpochOf(t time.Time) int64 { return t.Unix() / EpochSeconds }

// HighWaterRing holds the largest reading per epoch.
type HighWaterRing struct {
	slots [HighWaterSlots]atomic.Uint64
}

// Observe records a reading in an epoch; a lower reading, or one for an
// epoch older than the slot holds, changes nothing.
func (r *HighWaterRing) Observe(epoch, n int64) {
	if n < 0 {
		n = 0
	}
	if n > 1<<32-1 {
		n = 1<<32 - 1
	}
	slot := &r.slots[epoch%HighWaterSlots]
	want := uint64(uint32(epoch))<<32 | uint64(n)
	for {
		cur := slot.Load()
		curEpoch := uint32(cur >> 32)
		if curEpoch > uint32(epoch) || (curEpoch == uint32(epoch) && int64(uint32(cur)) >= n) {
			return
		}
		if slot.CompareAndSwap(cur, want) {
			return
		}
	}
}

// EpochHigh is one epoch's high water. Epoch is unix seconds over ten.
type EpochHigh struct {
	Epoch int64 `json:"e"`
	Max   int64 `json:"m"`
}

// Epochs reads the ring, the newest epoch's last ten minutes only.
func (r *HighWaterRing) Epochs() []EpochHigh {
	var raw [HighWaterSlots]uint64
	var newest uint32
	for i := range raw {
		raw[i] = r.slots[i].Load()
		if raw[i] != 0 && uint32(raw[i]>>32) > newest {
			newest = uint32(raw[i] >> 32)
		}
	}
	var out []EpochHigh
	for _, v := range raw {
		ep := uint32(v >> 32)
		if v == 0 || newest-ep >= HighWaterSlots {
			continue
		}
		out = append(out, EpochHigh{Epoch: int64(ep), Max: int64(uint32(v))})
	}
	return out
}

// HighWater is the largest reading in any epoch overlapping [from, to].
func HighWater(epochs []EpochHigh, from, to time.Time) int64 {
	lo, hi := EpochOf(from), EpochOf(to)
	var m int64
	for _, e := range epochs {
		if e.Epoch >= lo && e.Epoch <= hi && e.Max > m {
			m = e.Max
		}
	}
	return m
}
