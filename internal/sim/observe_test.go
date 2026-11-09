package sim

import (
	"math"
	"sort"
	"sync"
	"testing"
	"time"
)

func newObservedDevice(id string) (*endpointObservation, *deviceObservation) {
	obs := newEndpointObservation()
	return obs, obs.device(id)
}

func TestObservationEachOutcomeMovesOneCounter(t *testing.T) {
	for _, out := range []Outcome{OutcomeAnswered, OutcomeRefused, OutcomeError, OutcomeDropped, OutcomeWithheld} {
		t.Run(out.String(), func(t *testing.T) {
			_, dev := newObservedDevice("d1")
			tok := dev.begin(obsHTTP, nil)
			if got := dev.counters(obsHTTP).InFlight; got != 1 {
				t.Fatalf("in flight after begin = %d, want 1", got)
			}
			tok.end(out)
			c := dev.counters(obsHTTP)
			if c.Received != 1 || c.InFlight != 0 {
				t.Fatalf("received %d in flight %d, want 1 and 0", c.Received, c.InFlight)
			}
			for _, o := range allOutcomes {
				want := int64(0)
				if o == out {
					want = 1
				}
				if got := c.Outcome(o); got != want {
					t.Errorf("%s = %d, want %d", o, got, want)
				}
			}
		})
	}
}

func TestObservationBeginWithoutEndStaysInFlight(t *testing.T) {
	_, dev := newObservedDevice("d1")
	dev.begin(obsSNMP, nil)
	c := dev.counters(obsSNMP)
	if c.Received != 1 || c.InFlight != 1 || c.Settled() != 0 {
		t.Fatalf("got %+v, want one received, one in flight, none settled", c)
	}
}

func TestObservationHighWaterHoldsPeakOfConcurrentBegins(t *testing.T) {
	obs, dev := newObservedDevice("d1")
	var begun, release sync.WaitGroup
	begun.Add(50)
	release.Add(1)
	var done sync.WaitGroup
	for range 50 {
		done.Add(1)
		go func() {
			defer done.Done()
			tok := dev.begin(obsTelnet, nil)
			begun.Done()
			release.Wait()
			tok.end(OutcomeAnswered)
		}()
	}
	begun.Wait()
	release.Done()
	done.Wait()
	snap := obs.snapshot(time.Now())
	if got := snap.Protocol("telnet").HighWater(time.Time{}, time.Now().Add(time.Minute)); got != 50 {
		t.Fatalf("in-flight high water = %d, want 50", got)
	}
	if c := dev.counters(obsTelnet); c.InFlight != 0 || c.Answered != 50 {
		t.Fatalf("after the ends: %+v", c)
	}
}

// The documented read order (received, then in flight, then outcomes) and the
// write order (begin raises in flight before received; end counts the outcome
// before lowering in flight) together mean no snapshot can show more received
// than in flight plus settled, whatever interleaving the writers take.
func TestObservationReadOrderNeverShowsAFalseGap(t *testing.T) {
	_, dev := newObservedDevice("d1")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				dev.begin(obsHTTP, nil).end(allOutcomes[(g+i)%len(allOutcomes)])
			}
		}()
	}
	for i := range 10_000 {
		c := dev.counters(obsHTTP)
		if c.Received > c.InFlight+c.Settled() {
			close(stop)
			wg.Wait()
			t.Fatalf("snapshot %d: received %d > in flight %d + settled %d", i, c.Received, c.InFlight, c.Settled())
		}
	}
	close(stop)
	wg.Wait()
}

func TestObservationBeginEndAllocatesNothing(t *testing.T) {
	_, dev := newObservedDevice("d1")
	face := dev.face("api", obsHTTP)
	if n := testing.AllocsPerRun(1000, func() {
		dev.begin(obsHTTP, face).end(OutcomeAnswered)
	}); n != 0 {
		t.Fatalf("begin and end allocate %v times, want 0", n)
	}
}

func TestObservationNilDeviceIsANoOp(t *testing.T) {
	var dev *deviceObservation
	dev.begin(obsHTTP, nil).end(OutcomeAnswered)
	dev.accept(obsTCP)
	dev.openStream(obsHTTP)()
}

func TestObservationConnectionsAndStreams(t *testing.T) {
	_, dev := newObservedDevice("d1")
	closeConn := dev.accept(obsSSH)
	if c := dev.counters(obsSSH); c.Accepted != 1 || c.OpenConnections != 1 {
		t.Fatalf("after accept: %+v", c)
	}
	closeConn()
	closeConn() // a second close does not lower the gauge again
	if c := dev.counters(obsSSH); c.Accepted != 1 || c.OpenConnections != 0 {
		t.Fatalf("after close: %+v", c)
	}
	closeStream := dev.openStream(obsHTTP)
	if c := dev.counters(obsHTTP); c.StreamsOpened != 1 || c.OpenStreams != 1 || c.InFlight != 0 || c.Received != 0 {
		t.Fatalf("a stream is not a request in flight: %+v", c)
	}
	closeStream()
	if c := dev.counters(obsHTTP); c.OpenStreams != 0 || c.StreamsOpened != 1 {
		t.Fatalf("after the stream closed: %+v", c)
	}
}

func TestObservationOverlapCountsOnlyTheSameFace(t *testing.T) {
	obs := newEndpointObservation()
	a, b := obs.device("a"), obs.device("b")
	aAPI, aCtl, bAPI := a.face("api", obsHTTP), a.face("control", obsHTTP), b.face("api", obsHTTP)

	t1 := a.begin(obsHTTP, aAPI)
	t2 := a.begin(obsHTTP, aCtl) // another face of the same device
	t3 := b.begin(obsHTTP, bAPI) // another device
	stream := a.openStream(obsHTTP)
	if a.counters(obsHTTP).Overlapped != 0 || b.counters(obsHTTP).Overlapped != 0 {
		t.Fatalf("different faces, devices and a stream counted an overlap")
	}
	t4 := a.begin(obsHTTP, aAPI) // the same face, while t1 is in flight
	if got := a.counters(obsHTTP).Overlapped; got != 1 {
		t.Fatalf("two begins on one face counted %d overlaps, want 1", got)
	}
	for _, tok := range []obsToken{t1, t2, t3, t4} {
		tok.end(OutcomeAnswered)
	}
	stream()
	a.begin(obsHTTP, aAPI).end(OutcomeAnswered) // sequential, no overlap
	if got := a.counters(obsHTTP).Overlapped; got != 1 {
		t.Fatalf("a sequential request counted an overlap: %d", got)
	}
	snap := obs.snapshot(time.Now())
	if len(snap.Faces) != 1 || snap.Faces[0].DeviceID != "a" || snap.Faces[0].Face != "api" || snap.Faces[0].Overlapped != 1 || snap.Faces[0].Received != 3 {
		t.Fatalf("faces = %+v, want only a/api with one overlap of three received", snap.Faces)
	}
}

func TestLatencyBucketsPlaceKnownDurations(t *testing.T) {
	for _, tc := range []struct {
		us   uint64
		want int
	}{
		{0, 0}, {1, 0}, {2, 8}, {3, 12}, {7, 22}, {8, 24}, {9, 25}, {15, 31}, {16, 32}, {17, 32}, {18, 33},
		{1000, 9*8 + 7}, {1 << 25, 25 * 8}, {1<<26 - 1, latencyBuckets - 1}, {1 << 40, latencyBuckets - 1},
	} {
		got := latencyBucket(tc.us)
		if got != tc.want {
			t.Errorf("bucket(%d us) = %d, want %d", tc.us, got, tc.want)
			continue
		}
		lo, hi := latencyBucketBounds(got)
		if tc.us < 1<<26 && tc.us > 0 && (float64(tc.us) < lo || float64(tc.us) >= hi) {
			t.Errorf("%d us outside its bucket [%v, %v)", tc.us, lo, hi)
		}
	}
}

func TestLatencyHistogramMergeEqualsOneFedBoth(t *testing.T) {
	var a, b, both latencyHistogram
	for i := range 500 {
		d := time.Duration(i*i) * time.Microsecond
		a.record(d)
		both.record(d)
	}
	for i := range 300 {
		d := time.Duration(i*977) * time.Microsecond
		b.record(d)
		both.record(d)
	}
	merged := MergeLatency(a.counts(), b.counts())
	want := both.counts()
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
	var h latencyHistogram
	var all []float64
	for i := 1; i <= 10_000; i++ {
		us := float64(i) * 37 // 37 us to 370 ms, uniform
		all = append(all, us)
		h.record(time.Duration(us) * time.Microsecond)
	}
	sort.Float64s(all)
	exact := all[int(math.Ceil(0.95*float64(len(all))))-1]
	got := LatencyQuantile(h.counts(), 0.95)
	lo, hi := latencyBucketBounds(latencyBucket(uint64(exact)))
	if math.Abs(got-exact) > hi-lo {
		t.Fatalf("p95 = %v us, exact %v us, bucket width %v", got, exact, hi-lo)
	}
	if LatencyQuantile(nil, 0.95) != 0 {
		t.Fatalf("an empty histogram has a quantile")
	}
}

func TestHighWaterRingKeepsTheMaxPerEpochAndForgetsOldOnes(t *testing.T) {
	var r highWaterRing
	r.observe(100, 3)
	r.observe(100, 7)
	r.observe(100, 5)
	r.observe(101, 2)
	r.observe(100+highWaterSlots, 4) // reuses epoch 100's slot
	got := map[int64]int64{}
	for _, e := range r.epochs() {
		got[e.Epoch] = e.Max
	}
	if got[100] != 0 || got[101] != 2 || got[100+highWaterSlots] != 4 {
		t.Fatalf("epochs = %v", got)
	}
	var s highWaterRing
	s.observe(5, 7)
	s.observe(5, 3)
	if e := s.epochs(); len(e) != 1 || e[0].Max != 7 {
		t.Fatalf("a lower reading replaced the max: %v", e)
	}
}
