package platformmetric_test

import (
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fixedClock is a clock a test moves by hand.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time { return c.t }

// byKey indexes a flush by key and instance.
func byKey(rows []storage.PlatformMetric) map[[2]string]storage.PlatformMetric {
	out := map[[2]string]storage.PlatformMetric{}
	for _, r := range rows {
		out[[2]string{r.Key, r.Instance}] = r
	}
	return out
}

// TestRecorderFoldsAWindowExactly: a window's count is the sum of what was
// counted, its mean the mean of what was observed, its max the largest, its
// gauge the last set; every sample carries the component and the flush's
// time, one per (key, instance).
func TestRecorderFoldsAWindowExactly(t *testing.T) {
	t.Parallel()
	clock := &fixedClock{t: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	r := platformmetric.NewRecorder(clock.now)
	for _, v := range []float64{10, 20, 60} {
		r.Count("req", "GET /a 2xx", 1)
		r.Mean("dur", "GET /a 2xx", v)
		r.Max("dur-max", "GET /a 2xx", v)
	}
	r.Count("req", "GET /b 5xx", 2)
	r.Gauge("pool", "", 3)
	r.Gauge("pool", "", 5)

	clock.t = clock.t.Add(time.Minute)
	rows := r.Flush("comp-1")
	got := byKey(rows)
	want := map[[2]string]float64{
		{"req", "GET /a 2xx"}:     3,
		{"dur", "GET /a 2xx"}:     30,
		{"dur-max", "GET /a 2xx"}: 60,
		{"req", "GET /b 5xx"}:     2,
		{"pool", ""}:              5,
	}
	if len(rows) != len(want) {
		t.Fatalf("flushed %d samples %+v, want one per (key, instance): %d", len(rows), rows, len(want))
	}
	for k, v := range want {
		row, ok := got[k]
		if !ok || row.Value != v {
			t.Errorf("%v = %+v, want %v", k, row, v)
			continue
		}
		if row.ComponentID != "comp-1" || !row.TS.Equal(clock.t) {
			t.Errorf("%v landed on %q at %v, want comp-1 at the flush %v", k, row.ComponentID, row.TS, clock.t)
		}
	}
}

// TestRecorderFlushResetsTheWindow: a flush starts the next window empty. A
// count that saw activity reads one zero in the window after it, then
// nothing while it stays quiet; a mean, a max or a gauge with nothing
// observed reads nothing rather than a zero that never happened.
func TestRecorderFlushResetsTheWindow(t *testing.T) {
	t.Parallel()
	clock := &fixedClock{t: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	r := platformmetric.NewRecorder(clock.now)
	r.Count("req", "GET /a 2xx", 4)
	r.Mean("dur", "GET /a 2xx", 12)
	r.Max("dur-max", "GET /a 2xx", 12)
	r.Gauge("pool", "", 1)
	if n := len(r.Flush("c")); n != 4 {
		t.Fatalf("first window flushed %d samples, want 4", n)
	}

	second := r.Flush("c")
	if len(second) != 1 || second[0].Key != "req" || second[0].Value != 0 {
		t.Fatalf("quiet window = %+v, want the count's one zero alone", second)
	}
	if third := r.Flush("c"); len(third) != 0 {
		t.Fatalf("second quiet window = %+v, want nothing", third)
	}

	r.Count("req", "GET /a 2xx", 1)
	if fourth := r.Flush("c"); len(fourth) != 1 || fourth[0].Value != 1 {
		t.Fatalf("a window after the quiet = %+v, want its own count 1, not a carried total", fourth)
	}
}

// TestRecorderIsSafeUnderConcurrentWriters: every request handler writes to
// one recorder at once; no count is lost.
func TestRecorderIsSafeUnderConcurrentWriters(t *testing.T) {
	t.Parallel()
	r := platformmetric.NewRecorder(time.Now)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				r.Count("req", "x", 1)
				r.Max("m", "x", 1)
			}
		}()
	}
	wg.Wait()
	got := byKey(r.Flush("c"))
	if v := got[[2]string{"req", "x"}].Value; v != 8000 {
		t.Errorf("count = %v, want 8000", v)
	}
}

// TestRecorderOrdersItsFlush: the flush reads in key then instance order, so
// the write it feeds and a test reading it are deterministic.
func TestRecorderOrdersItsFlush(t *testing.T) {
	t.Parallel()
	r := platformmetric.NewRecorder(time.Now)
	r.Count("b", "2", 1)
	r.Count("a", "z", 1)
	r.Count("b", "1", 1)
	rows := r.Flush("c")
	var got []string
	for _, row := range rows {
		got = append(got, row.Key+" "+row.Instance)
	}
	want := []string{"a z", "b 1", "b 2"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// TestRecorderFoldsAQuantileOfDurations (#1746): a quantile series counts
// the window's durations into the latency histogram's buckets and reads the
// asked quantile in milliseconds, within one bucket width of the truth; an
// empty window reads nothing, and the next window starts from no durations.
func TestRecorderFoldsAQuantileOfDurations(t *testing.T) {
	t.Parallel()
	clock := &fixedClock{t: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	r := platformmetric.NewRecorder(clock.now)
	for i := range 99 {
		r.Quantile("wait-p99", "", 0.99, time.Duration(i%10)*time.Millisecond)
	}
	r.Quantile("wait-p99", "", 0.99, 4*time.Second)
	r.Quantile("wait-p99", "", 0.99, 4*time.Second)
	got := byKey(r.Flush("c"))[[2]string{"wait-p99", ""}]
	if got.Value < 4000*7/8 || got.Value > 4000*9/8 {
		t.Fatalf("p99 over 99 short waits and two of 4 s = %v ms, want about 4000", got.Value)
	}

	r.Quantile("wait-p99", "", 0.99, 3*time.Millisecond)
	if got := byKey(r.Flush("c"))[[2]string{"wait-p99", ""}]; got.Value < 3*7.0/8 || got.Value > 3*9.0/8 {
		t.Fatalf("the next window's p99 of one 3 ms wait = %v ms, want about 3: the window carried the last one's waits", got.Value)
	}
	if rows := r.Flush("c"); len(rows) != 0 {
		t.Fatalf("a quantile with nothing observed flushed %+v, want nothing", rows)
	}
}
