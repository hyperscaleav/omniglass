package ingress

import (
	"strings"
	"sync"
	"time"
)

// Buckets is a token bucket per key (a source, an endpoint, the listener as
// a whole): rate tokens a second up to burst, refusing past it with how long
// until a token returns. The keys held at once are bounded, so a flood of
// distinct keys (a spoofed source per post) cannot grow it without limit.
type Buckets struct {
	rate, burst float64
	max         int

	mu sync.Mutex
	b  map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

// NewBuckets holds at most max keys, each refilling at rate a second up to
// burst.
func NewBuckets(rate, burst float64, max int) *Buckets {
	return &Buckets{rate: rate, burst: burst, max: max, b: map[string]*bucket{}}
}

// Allow takes a token for key at now, or refuses with the wait until one
// returns.
func (bs *Buckets) Allow(key string, now time.Time) (bool, time.Duration) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	b, ok := bs.b[key]
	if !ok {
		if len(bs.b) >= bs.max && !bs.evictFull(now) {
			// Every bucket is in use and none has refilled: a new key waits
			// as a drained bucket would.
			return false, bs.wait(0)
		}
		b = &bucket{tokens: bs.burst, at: now}
		bs.b[key] = b
	}
	b.tokens = min(bs.burst, b.tokens+now.Sub(b.at).Seconds()*bs.rate)
	b.at = now
	if b.tokens < 1 {
		return false, bs.wait(b.tokens)
	}
	b.tokens--
	return true, 0
}

// evictFull drops one bucket that has refilled to its burst (the same as a
// new one, so nothing is lost), reporting whether it found one.
func (bs *Buckets) evictFull(now time.Time) bool {
	for k, b := range bs.b {
		if b.tokens+now.Sub(b.at).Seconds()*bs.rate >= bs.burst {
			delete(bs.b, k)
			return true
		}
	}
	return false
}

func (bs *Buckets) wait(tokens float64) time.Duration {
	return time.Duration((1 - tokens) / bs.rate * float64(time.Second))
}

// Len is how many keys are held.
func (bs *Buckets) Len() int {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return len(bs.b)
}

// Deduper remembers each endpoint's delivery ids for the replay window, so
// a sender's redelivery of one delivery lands once. Each endpoint's memory
// is bounded, the oldest let go first.
type Deduper struct {
	window time.Duration
	max    int

	mu   sync.Mutex
	seen map[string]*deliveries
}

type deliveries struct {
	at    map[string]time.Time
	order []string // oldest first
}

// NewDeduper remembers ids for window, at most max per endpoint.
func NewDeduper(window time.Duration, max int) *Deduper {
	return &Deduper{window: window, max: max, seen: map[string]*deliveries{}}
}

// Begin reports whether endpoint's delivery id is new at now, and holds it
// as seen; a redelivery inside the window reports false.
func (d *Deduper) Begin(endpoint, id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ds := d.seen[endpoint]
	if ds == nil {
		ds = &deliveries{at: map[string]time.Time{}}
		d.seen[endpoint] = ds
	}
	ds.expire(now.Add(-d.window))
	if at, ok := ds.at[id]; ok && now.Sub(at) <= d.window {
		return false
	}
	for len(ds.order) >= d.max {
		delete(ds.at, ds.order[0])
		ds.order = ds.order[1:]
	}
	ds.at[id] = now
	ds.order = append(ds.order, id)
	return true
}

// Abandon forgets a delivery whose publish failed, so the sender's retry
// lands.
func (d *Deduper) Abandon(endpoint, id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ds := d.seen[endpoint]
	if ds == nil {
		return
	}
	delete(ds.at, id)
	for i, o := range ds.order {
		if o == id {
			ds.order = append(ds.order[:i:i], ds.order[i+1:]...)
			break
		}
	}
	if len(ds.order) == 0 {
		delete(d.seen, endpoint)
	}
}

// expire lets go of the ids first seen before cutoff.
func (ds *deliveries) expire(cutoff time.Time) {
	n := 0
	for n < len(ds.order) && ds.at[ds.order[n]].Before(cutoff) {
		delete(ds.at, ds.order[n])
		n++
	}
	ds.order = ds.order[n:]
}

// Len is how many ids endpoint holds.
func (d *Deduper) Len(endpoint string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ds := d.seen[endpoint]; ds != nil {
		return len(ds.order)
	}
	return 0
}

// MaxUnclaimedMessage bounds the last message an unclaimed source keeps: a
// stranger's body is untrusted data, so no more of it is stored (decision 4
// of #1146).
const MaxUnclaimedMessage = 512

// Refused is one (endpoint, source, reason) a listener refused, aggregated
// since the last drain.
type Refused struct {
	EndpointID, Source, Reason string
	Count                      int64
	LastAt                     time.Time
}

// UnclaimedSource is one source no endpoint claimed, aggregated since the
// last drain.
type UnclaimedSource struct {
	Source          string
	Count           int64
	FirstAt, LastAt time.Time
	LastMessage     string
}

// Tally aggregates what a listener refuses and what no endpoint claims
// between flushes, so a flood costs a host one write per source per flush,
// never one per message. The unclaimed sources held at once are bounded.
type Tally struct {
	maxSources int

	mu        sync.Mutex
	refused   map[[3]string]*Refused
	unclaimed map[string]*UnclaimedSource
	dropped   int64
}

// NewTally holds at most maxSources unclaimed sources between drains.
func NewTally(maxSources int) *Tally {
	return &Tally{maxSources: maxSources, refused: map[[3]string]*Refused{}, unclaimed: map[string]*UnclaimedSource{}}
}

// Refused counts a post an endpoint's scheme refused.
func (t *Tally) Refused(endpointID, source, reason string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := [3]string{endpointID, source, reason}
	r := t.refused[k]
	if r == nil {
		r = &Refused{EndpointID: endpointID, Source: source, Reason: reason}
		t.refused[k] = r
	}
	r.Count++
	r.LastAt = at
}

// Unclaimed counts a message no endpoint claimed, keeping its first bytes.
// A source past the bound is counted as dropped.
func (t *Tally) Unclaimed(source string, message []byte, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	u := t.unclaimed[source]
	if u == nil {
		if len(t.unclaimed) >= t.maxSources {
			t.dropped++
			return
		}
		u = &UnclaimedSource{Source: source, FirstAt: at}
		t.unclaimed[source] = u
	}
	u.Count++
	u.LastAt = at
	u.LastMessage = truncate(message, MaxUnclaimedMessage)
}

// Drain returns and resets everything tallied, with how many unclaimed
// sources were past the bound.
func (t *Tally) Drain() ([]Refused, []UnclaimedSource, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	refused := make([]Refused, 0, len(t.refused))
	for _, r := range t.refused {
		refused = append(refused, *r)
	}
	unclaimed := make([]UnclaimedSource, 0, len(t.unclaimed))
	for _, u := range t.unclaimed {
		unclaimed = append(unclaimed, *u)
	}
	dropped := t.dropped
	t.refused, t.unclaimed, t.dropped = map[[3]string]*Refused{}, map[string]*UnclaimedSource{}, 0
	return refused, unclaimed, dropped
}

// Pending reports whether anything waits for a drain.
func (t *Tally) Pending() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.refused) > 0 || len(t.unclaimed) > 0 || t.dropped > 0
}

// truncate cuts b to at most n bytes of valid UTF-8 with no NUL.
func truncate(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	s := strings.ToValidUTF8(string(b), "")
	return strings.ReplaceAll(s, "\x00", "")
}
