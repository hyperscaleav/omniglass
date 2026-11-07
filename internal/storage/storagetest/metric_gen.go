package storagetest

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

// The compacted tier's measured series (#1522, #1525): a deterministic
// generator of metric samples in the shapes the lab simulator's devices
// report, at a product cadence or through a cadence change, and the span rule
// (ADR-1528) stated over them in Go, so a block test holds the fold in
// Postgres to a reference it did not compute.

// GenSample is one generated metric sample. Its position in the slice
// Generate returns is its landing order: within one series and timestamp, the
// later sample is the current one (ADR-0061).
type GenSample struct {
	Series int
	TS     time.Time
	Value  float64
}

// GenSpec is one generated dataset: Series series polled every Cadence for
// Span from Start, or, when Phases is set, each series through its own
// phases (the series count being len(Phases)).
type GenSpec struct {
	Cadence time.Duration
	Series  int
	Start   time.Time
	Span    time.Duration
	Seed    uint64
	Phases  [][]GenPhase
}

// GenPhase is a stretch of one series polled at one cadence, From and To
// being offsets from the dataset's start.
type GenPhase struct {
	From, To time.Duration
	Cadence  time.Duration
}

// GenSeed is the seed #1525 measured the span rule's footprint with.
const GenSeed = 1516

// MixedSpec is #1522's cadence change over the ISO week from start, a
// Monday: series 1 polls 60 s and turns to 30 s at Wednesday noon, series 2
// polls 1 h and turns to 30 s at Thursday noon, and series 3 reports every
// 56 h, three times the week. Its days cross every span the rule has.
func MixedSpec(start time.Time, seed uint64) GenSpec {
	h := time.Hour
	week := 7 * 24 * h
	return GenSpec{Start: start, Span: week, Seed: seed, Phases: [][]GenPhase{
		{{From: 0, To: 60 * h, Cadence: time.Minute}, {From: 60 * h, To: week, Cadence: 30 * time.Second}},
		{{From: 0, To: 84 * h, Cadence: h}, {From: 84 * h, To: week, Cadence: 30 * time.Second}},
		{{From: 0, To: week, Cadence: 56 * h}},
	}}
}

// genKind is the shape of a series, after the lab simulator's devices: a
// probe that holds one value, a monotonic counter, slow random walks rounded
// the way the simulator rounds them, and full-precision noise.
type genKind int

const (
	genConstant genKind = iota
	genCounter
	genWalkTenth
	genWalkSig3
	genNoisy
	genKinds
)

// genDupeRate is the share of polls that report a series twice at one
// timestamp.
const genDupeRate = 0.02

// Generate is spec's samples, by series (1 up), then timestamp, then landing
// order. It is deterministic in spec, and it gives #1525's samples for its
// specs: timestamps are whole milliseconds, a poll landing on its cadence plus
// a phase and a jitter.
func Generate(spec GenSpec) []GenSample {
	phases := spec.Phases
	if len(phases) == 0 {
		for range spec.Series {
			phases = append(phases, []GenPhase{{From: 0, To: spec.Span, Cadence: spec.Cadence}})
		}
	}
	var out []GenSample
	for i, ps := range phases {
		id := i + 1
		rng := rand.New(rand.NewPCG(spec.Seed, uint64(id)))
		k := genKind(i % int(genKinds))
		var g *gen
		for _, ph := range ps {
			if g == nil {
				g = newGen(k, rng, ph.Cadence)
			} else if k == genCounter {
				g.step = ph.Cadence.Hours()
			}
			jitter := min(ph.Cadence/8, 250*time.Millisecond)
			phase := time.Duration(rng.Int64N(int64(ph.Cadence/time.Millisecond))) * time.Millisecond
			// The phase and jitter never carry a poll into the next one, so
			// the series stays in timestamp order without a sort.
			phase = min(phase, ph.Cadence-2*jitter-time.Millisecond)
			for p := range int((ph.To - ph.From) / ph.Cadence) {
				var j time.Duration
				if jitter >= time.Millisecond {
					j = time.Duration(rng.Int64N(int64(jitter/time.Millisecond))) * time.Millisecond
				}
				ts := spec.Start.Add(ph.From + time.Duration(p)*ph.Cadence + phase + j)
				out = append(out, GenSample{id, ts, g.next()})
				if rng.Float64() < genDupeRate {
					out = append(out, GenSample{id, ts, g.next()})
				}
			}
		}
	}
	return out
}

type gen struct {
	k    genKind
	rng  *rand.Rand
	v    float64
	step float64
}

func newGen(k genKind, rng *rand.Rand, cadence time.Duration) *gen {
	g := &gen{k: k, rng: rng}
	switch k {
	case genConstant:
		g.v = 1
	case genCounter:
		g.v = math.Round(rng.Float64()*20000*1e4) / 1e4
		g.step = cadence.Hours()
	case genWalkTenth:
		g.v = -20 + rng.Float64()*10
		g.step = 0.05
	case genWalkSig3:
		g.v = 900 + rng.Float64()*600
		g.step = 2
	case genNoisy:
		g.v = 8
	}
	return g
}

func (g *gen) next() float64 {
	switch g.k {
	case genConstant:
		return g.v
	case genCounter:
		g.v += g.step
		return math.Round(g.v*1e4) / 1e4
	case genWalkTenth:
		g.v = math.Max(-60, math.Min(0, g.v+g.rng.NormFloat64()*g.step))
		return math.Round(g.v*10) / 10
	case genWalkSig3:
		g.v = math.Max(0, math.Min(4000, g.v+g.rng.NormFloat64()*g.step))
		return sigfig(g.v, 3)
	default:
		return g.v + g.rng.ExpFloat64()*3
	}
}

func sigfig(v float64, n int) float64 {
	if v == 0 {
		return 0
	}
	p := math.Pow(10, float64(n)-math.Ceil(math.Log10(math.Abs(v))))
	return math.Round(v*p) / p
}

// The span rule's thresholds, restated here rather than imported so the
// reference is not the code it checks: 170 samples cross Postgres's 2 KB
// compression threshold.
const (
	spanDayMin  = 170
	spanHourMin = 24 * spanDayMin
)

// DaySpan is the span one series-day's samples fold at.
type DaySpan struct {
	Series int
	Day    time.Time
	Span   string
	Count  int
}

// DaySpans is the span rule over samples, which are in series then timestamp
// order: a series-day of at least 4,080 samples is cut into hour blocks, one
// of at least 170 is a day block, and a sparser one waits for its ISO week's
// block.
func DaySpans(samples []GenSample) []DaySpan {
	var out []DaySpan
	for _, s := range samples {
		day := s.TS.UTC().Truncate(24 * time.Hour)
		if n := len(out); n > 0 && out[n-1].Series == s.Series && out[n-1].Day.Equal(day) {
			out[n-1].Count++
			continue
		}
		out = append(out, DaySpan{Series: s.Series, Day: day, Count: 1})
	}
	for i, d := range out {
		switch {
		case d.Count >= spanHourMin:
			out[i].Span = "hour"
		case d.Count >= spanDayMin:
			out[i].Span = "day"
		default:
			out[i].Span = "week"
		}
	}
	return out
}

// GenBlock is one block the span rule gives: its series, span and start, and
// its samples in landing order.
type GenBlock struct {
	Series    int
	Span      string
	SpanStart time.Time
	Samples   []GenSample
}

// Blocks is every block the span rule gives samples (in series then
// timestamp order), by series, then span start, then span: what a fold of
// them must write.
func Blocks(samples []GenSample) []GenBlock {
	spans := map[[2]int64]string{}
	for _, d := range DaySpans(samples) {
		spans[[2]int64{int64(d.Series), d.Day.Unix()}] = d.Span
	}
	type key struct {
		series int
		start  int64
		span   string
	}
	idx := map[key]int{}
	var out []GenBlock
	for _, s := range samples {
		day := s.TS.UTC().Truncate(24 * time.Hour)
		span := spans[[2]int64{int64(s.Series), day.Unix()}]
		start := day
		switch span {
		case "hour":
			start = s.TS.UTC().Truncate(time.Hour)
		case "week":
			start = day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
		}
		k := key{s.Series, start.Unix(), span}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, GenBlock{Series: s.Series, Span: span, SpanStart: start})
		}
		out[i].Samples = append(out[i].Samples, s)
	}
	sortBlocks(out)
	return out
}

// sortBlocks orders blocks by series, span start, then span, as a read of
// metric_block ordered by its key does.
func sortBlocks(b []GenBlock) {
	slices.SortFunc(b, func(x, y GenBlock) int {
		return cmp.Or(cmp.Compare(x.Series, y.Series), x.SpanStart.Compare(y.SpanStart), cmp.Compare(x.Span, y.Span))
	})
}
