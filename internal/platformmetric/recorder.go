package platformmetric

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/obsstat"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fold is how a series folds its window's observations into one sample.
type fold int

const (
	foldCount    fold = iota // the sum of what was counted
	foldMean                 // the mean of what was observed
	foldMax                  // the largest observed
	foldGauge                // the last set
	foldQuantile             // a quantile of the durations observed, in ms
)

// seriesKey names one series: a platform key and its instance.
type seriesKey struct{ key, instance string }

// series is one series' window so far.
type series struct {
	fold  fold
	sum   float64
	n     int
	max   float64
	last  float64
	quiet bool     // a count that reported a window's activity owes one zero
	q     float64  // a quantile's rank, 0 to 1
	hist  []uint64 // a quantile's durations, in obsstat latency buckets
}

// Recorder folds a process's own observations into one sample per series per
// report window (#1627): the primitive any Omniglass process records its
// platform metrics through, safe for every request handler to write at once.
// A count reads the window's sum, then one zero in the window after its last
// activity, then nothing while it stays quiet; a mean, a max, a quantile or a
// gauge with nothing observed reads nothing rather than a zero that never
// happened.
type Recorder struct {
	now func() time.Time
	mu  sync.Mutex
	win map[seriesKey]*series
}

// NewRecorder is an empty recorder stamping its flushes with now.
func NewRecorder(now func() time.Time) *Recorder {
	return &Recorder{now: now, win: map[seriesKey]*series{}}
}

// Count adds n to the window's count of key at instance.
func (r *Recorder) Count(key, instance string, n float64) { r.observe(key, instance, foldCount, n) }

// Mean observes v toward the window's mean of key at instance.
func (r *Recorder) Mean(key, instance string, v float64) { r.observe(key, instance, foldMean, v) }

// Max observes v toward the window's largest of key at instance.
func (r *Recorder) Max(key, instance string, v float64) { r.observe(key, instance, foldMax, v) }

// Gauge sets key at instance to v, the window's reading unless set again.
func (r *Recorder) Gauge(key, instance string, v float64) { r.observe(key, instance, foldGauge, v) }

// Quantile observes d toward the window's quantile q (0 to 1) of key at
// instance, read in milliseconds from the latency histogram's buckets
// (obsstat), so within one bucket width of the truth however many durations
// the window saw. A series keeps the q it was first observed with.
func (r *Recorder) Quantile(key, instance string, q float64, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.series(seriesKey{key, instance}, foldQuantile)
	if s.hist == nil {
		s.q, s.hist = q, make([]uint64, obsstat.LatencyBuckets)
	}
	us := d.Microseconds()
	if us < 0 {
		us = 0
	}
	s.hist[obsstat.LatencyBucket(uint64(us))]++
	s.n++
}

// series is k's window, started with fold f if the window has none; the
// caller holds r.mu.
func (r *Recorder) series(k seriesKey, f fold) *series {
	s := r.win[k]
	if s == nil {
		s = &series{fold: f}
		r.win[k] = s
	}
	return s
}

func (r *Recorder) observe(key, instance string, f fold, v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.series(seriesKey{key, instance}, f)
	if s.n == 0 || v > s.max {
		s.max = v
	}
	s.sum += v
	s.last = v
	s.n++
}

// Flush closes the window: one sample per series that has something to say,
// on componentID at the flush's time, in key then instance order. The next
// window starts empty.
func (r *Recorder) Flush(componentID string) []storage.PlatformMetric {
	ts := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []storage.PlatformMetric
	for k, s := range r.win {
		v, ok := s.value()
		if ok {
			out = append(out, storage.PlatformMetric{ComponentID: componentID, Key: k.key, Instance: k.instance, Value: v, TS: ts})
		}
		if s.fold == foldCount && s.n > 0 {
			*s = series{fold: foldCount, quiet: true}
			continue
		}
		delete(r.win, k)
	}
	slices.SortFunc(out, func(a, b storage.PlatformMetric) int {
		return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.Instance, b.Instance))
	})
	return out
}

// value is the series' window sample, and false when it has nothing to say.
func (s *series) value() (float64, bool) {
	if s.n == 0 {
		return 0, s.fold == foldCount && s.quiet
	}
	switch s.fold {
	case foldCount:
		return s.sum, true
	case foldMean:
		return s.sum / float64(s.n), true
	case foldMax:
		return s.max, true
	case foldQuantile:
		return obsstat.LatencyQuantile(s.hist, s.q) / 1000, true
	default:
		return s.last, true
	}
}
