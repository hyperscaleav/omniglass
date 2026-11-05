// Package compressbench measures candidate storage formats for the metric lane
// against real Postgres 18 (#1516): hour and day blocks of arrays in benchmark
// tables, pg_deltax and TimescaleDB, each on the same seeded series at a
// product cadence. It is a diagnostic, never a gate (ADR-0094): it reports
// bytes per sample, compression time and read latency, and it fails only when a
// candidate does not give back exactly what it was given.
//
// This file is the pure core: the series generator and the reference reads that
// every candidate's result is held to.
package compressbench

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"time"
)

// Sample is one metric row in the order it landed. Its position in the slice
// Generate returns is its id: within one series and timestamp, the later
// sample is the current one (ADR-0061).
type Sample struct {
	SeriesID int64
	TS       time.Time
	Value    float64
}

// Point is a sample as a single-series read returns it.
type Point struct {
	TS    time.Time
	Value float64
}

// Spec is one cadence's dataset.
type Spec struct {
	Cadence time.Duration
	Series  int
	Span    time.Duration
	Seed    uint64
}

// Epoch is where every dataset starts. It is well before any run, so every
// day partition and chunk is sealed by the time a candidate compresses it.
var Epoch = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func (s Spec) start() time.Time { return Epoch }

// End is the instant after the dataset's last possible sample.
func (s Spec) End() time.Time { return s.start().Add(s.Span) }

// kind is the shape of a series, after the lab simulator's devices: a probe
// that holds one value, a monotonic counter, slow random walks rounded the way
// the simulator rounds them, and full-precision noise.
type kind string

const (
	kindConstant  kind = "constant"
	kindCounter   kind = "counter"
	kindWalkTenth kind = "walk-round1"
	kindWalkSig3  kind = "walk-sigfig3"
	kindNoisy     kind = "noisy"
)

// kinds is the cycle series ids walk through, so any series count carries
// every shape in close to equal measure.
var kinds = []kind{kindConstant, kindCounter, kindWalkTenth, kindWalkSig3, kindNoisy}

// dupeRate is the share of polls that report a series twice at one timestamp.
const dupeRate = 0.02

// Generate is spec's samples, ordered by series, then timestamp, then landing
// order. It is deterministic in spec. Timestamps carry millisecond precision:
// a poll lands on its cadence plus a jitter, never on a whole second only.
func Generate(spec Spec) []Sample {
	polls := int(spec.Span / spec.Cadence)
	jitter := min(spec.Cadence/8, 250*time.Millisecond)
	out := make([]Sample, 0, spec.Series*polls*51/50)
	for i := 0; i < spec.Series; i++ {
		id := int64(i + 1)
		rng := rand.New(rand.NewPCG(spec.Seed, uint64(id)))
		k := kinds[i%len(kinds)]
		g := newGen(k, rng, spec.Cadence)
		phase := time.Duration(rng.Int64N(int64(spec.Cadence/time.Millisecond))) * time.Millisecond
		// The phase and jitter never carry a poll into the next one, so the
		// series stays in timestamp order without a sort.
		phase = min(phase, spec.Cadence-2*jitter-time.Millisecond)
		for p := 0; p < polls; p++ {
			var j time.Duration
			if jitter >= time.Millisecond {
				j = time.Duration(rng.Int64N(int64(jitter/time.Millisecond))) * time.Millisecond
			}
			ts := spec.start().Add(time.Duration(p)*spec.Cadence + phase + j)
			out = append(out, Sample{id, ts, g.next()})
			if rng.Float64() < dupeRate {
				out = append(out, Sample{id, ts, g.next()})
			}
		}
	}
	return out
}

type gen struct {
	k    kind
	rng  *rand.Rand
	v    float64
	step float64
}

func newGen(k kind, rng *rand.Rand, cadence time.Duration) *gen {
	g := &gen{k: k, rng: rng}
	switch k {
	case kindConstant:
		g.v = 1
	case kindCounter:
		g.v = math.Round(rng.Float64()*20000*1e4) / 1e4
		g.step = cadence.Hours()
	case kindWalkTenth:
		g.v = -20 + rng.Float64()*10
		g.step = 0.05
	case kindWalkSig3:
		g.v = 900 + rng.Float64()*600
		g.step = 2
	case kindNoisy:
		g.v = 8
	}
	return g
}

func (g *gen) next() float64 {
	switch g.k {
	case kindConstant:
		return g.v
	case kindCounter:
		g.v += g.step
		return math.Round(g.v*1e4) / 1e4
	case kindWalkTenth:
		g.v = clamp(g.v+g.rng.NormFloat64()*g.step, -60, 0)
		return math.Round(g.v*10) / 10
	case kindWalkSig3:
		g.v = clamp(g.v+g.rng.NormFloat64()*g.step, 0, 4000)
		return sigfig(g.v, 3)
	default:
		return g.v + g.rng.ExpFloat64()*3
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func sigfig(v float64, n int) float64 {
	if v == 0 {
		return 0
	}
	p := math.Pow(10, float64(n)-math.Ceil(math.Log10(math.Abs(v))))
	return math.Round(v*p) / p
}

// RawWindow is the reference 24 h raw read: series' samples in [from, to), in
// timestamp then landing order.
func RawWindow(samples []Sample, series int64, from, to time.Time) []Point {
	var out []Point
	for _, s := range samples {
		if s.SeriesID == series && !s.TS.Before(from) && s.TS.Before(to) {
			out = append(out, Point{s.TS, s.Value})
		}
	}
	return out
}

// Bucket is one row of a bucketed read.
type Bucket struct {
	Start time.Time
	Count int64
	Min   float64
	Max   float64
	Avg   float64
}

// Buckets is the reference bucketed read: series' samples in [from, to),
// binned by width from Epoch.
func Buckets(samples []Sample, series int64, from, to time.Time, width time.Duration) []Bucket {
	idx := map[time.Time]int{}
	var out []Bucket
	sums := []float64{}
	for _, s := range samples {
		if s.SeriesID != series || s.TS.Before(from) || !s.TS.Before(to) {
			continue
		}
		b := Epoch.Add(s.TS.Sub(Epoch) / width * width)
		i, ok := idx[b]
		if !ok {
			i = len(out)
			idx[b] = i
			out = append(out, Bucket{Start: b, Min: s.Value, Max: s.Value})
			sums = append(sums, 0)
		}
		out[i].Count++
		out[i].Min = math.Min(out[i].Min, s.Value)
		out[i].Max = math.Max(out[i].Max, s.Value)
		sums[i] += s.Value
	}
	for i := range out {
		out[i].Avg = sums[i] / float64(out[i].Count)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Start.Before(out[b].Start) })
	return out
}

// SamePoints is nil when got is want exactly: every sample, in order, bit for
// bit, duplicates in their landing order.
func SamePoints(want, got []Point) error {
	if len(got) != len(want) {
		return fmt.Errorf("got %d samples, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].TS.Equal(want[i].TS) || math.Float64bits(got[i].Value) != math.Float64bits(want[i].Value) {
			return fmt.Errorf("sample %d: got %s %v, want %s %v", i, got[i].TS.UTC().Format(time.RFC3339Nano), got[i].Value, want[i].TS.UTC().Format(time.RFC3339Nano), want[i].Value)
		}
	}
	return nil
}

// SameBuckets is nil when got matches want: starts, counts, minimums and
// maximums exactly, the average to a relative 1e-12 (Postgres sums in its own
// order).
func SameBuckets(want, got []Bucket) error {
	if len(got) != len(want) {
		return fmt.Errorf("got %d buckets, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if !g.Start.Equal(w.Start) || g.Count != w.Count || g.Min != w.Min || g.Max != w.Max ||
			math.Abs(g.Avg-w.Avg) > 1e-12*math.Max(1, math.Abs(w.Avg)) {
			return fmt.Errorf("bucket %d: got %+v, want %+v", i, g, w)
		}
	}
	return nil
}

// Summary is one series' samples in one UTC hour: what a block carries for
// each hour it spans, so an hourly read never opens the sample arrays.
type Summary struct {
	SeriesID int64
	Hour     time.Time
	Count    int64
	Min      float64
	Max      float64
	Sum      float64
	First    float64
	Last     float64
}

// HourlySummaries is the reference summary of every hour that has a sample,
// by series then hour. samples is in series, timestamp and landing order, so
// the first and last of an hour are its first and last in that order.
func HourlySummaries(samples []Sample) []Summary {
	var out []Summary
	for _, s := range samples {
		h := s.TS.UTC().Truncate(time.Hour)
		if n := len(out); n > 0 && out[n-1].SeriesID == s.SeriesID && out[n-1].Hour.Equal(h) {
			x := &out[n-1]
			x.Count++
			x.Min = math.Min(x.Min, s.Value)
			x.Max = math.Max(x.Max, s.Value)
			x.Sum += s.Value
			x.Last = s.Value
			continue
		}
		out = append(out, Summary{s.SeriesID, h, 1, s.Value, s.Value, s.Value, s.Value, s.Value})
	}
	return out
}

// SameSummaries is nil when got matches want: every hour, its count, minimum,
// maximum, first and last exactly, its sum to a relative 1e-12.
func SameSummaries(want, got []Summary) error {
	if len(got) != len(want) {
		return fmt.Errorf("got %d hourly summaries, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.SeriesID != w.SeriesID || !g.Hour.Equal(w.Hour) || g.Count != w.Count || g.Min != w.Min || g.Max != w.Max ||
			g.First != w.First || g.Last != w.Last || math.Abs(g.Sum-w.Sum) > 1e-12*math.Max(1, math.Abs(w.Sum)) {
			return fmt.Errorf("hourly summary %d: got %+v, want %+v", i, g, w)
		}
	}
	return nil
}

// lateValue is the late sample's value: one no series kind generates.
const lateValue = 4242.4242

// Late is the late sample a merge lands for series: a second report at the
// instant of the series' middle sample, so the block it lands in is closed
// and already holds a sample at that instant, which the late one follows.
func Late(samples []Sample, series int64) Sample {
	var at []int
	for i, s := range samples {
		if s.SeriesID == series {
			at = append(at, i)
		}
	}
	return Sample{series, samples[at[len(at)/2]].TS, lateValue}
}

// WithLate is samples with late landed after every sample at its series and
// instant: the order a round trip must give back after the merge.
func WithLate(samples []Sample, late Sample) []Sample {
	i := sort.Search(len(samples), func(i int) bool {
		s := samples[i]
		return s.SeriesID > late.SeriesID || s.SeriesID == late.SeriesID && s.TS.After(late.TS)
	})
	out := make([]Sample, 0, len(samples)+1)
	out = append(out, samples[:i]...)
	out = append(out, late)
	return append(out, samples[i:]...)
}
