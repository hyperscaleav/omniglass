package ingress_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/ingress"
)

// A token bucket per key: a burst passes, the next is refused with how long
// until a token returns, and the bucket refills at its rate.
func TestABucketAdmitsItsBurstThenRefusesWithARetryAfter(t *testing.T) {
	b := ingress.NewBuckets(2, 4, 16) // two a second, a burst of four, sixteen keys
	for i := 0; i < 4; i++ {
		if ok, _ := b.Allow("203.0.113.9", now); !ok {
			t.Fatalf("post %d of the burst refused", i+1)
		}
	}
	ok, retry := b.Allow("203.0.113.9", now)
	if ok || retry <= 0 || retry > time.Second {
		t.Fatalf("past the burst = %v, retry %v; want refused, retry within a second", ok, retry)
	}
	// Another source has its own bucket.
	if ok, _ := b.Allow("198.51.100.4", now); !ok {
		t.Fatal("one source's flood refused another source")
	}
	// Half a second later one token has returned.
	if ok, _ := b.Allow("203.0.113.9", now.Add(500*time.Millisecond)); !ok {
		t.Fatal("a refilled token refused")
	}
}

// A flood of distinct keys (a spoofed source per post) cannot grow the
// buckets without bound: a full bucket is the same as a new one, so it is
// the one dropped, and past the cap with none full a new key is refused.
func TestBucketsAreBoundedUnderAFloodOfKeys(t *testing.T) {
	b := ingress.NewBuckets(1, 1, 4)
	for i := 0; i < 4; i++ {
		if ok, _ := b.Allow(fmt.Sprintf("10.0.0.%d", i), now); !ok {
			t.Fatalf("key %d refused under the cap", i)
		}
	}
	if ok, retry := b.Allow("10.0.0.99", now); ok || retry <= 0 {
		t.Fatalf("a fifth key with every bucket drained = %v; want refused", ok)
	}
	if b.Len() > 4 {
		t.Fatalf("buckets grew to %d past the cap of 4", b.Len())
	}
	// A second later every bucket has refilled, so one is dropped for it.
	if ok, _ := b.Allow("10.0.0.99", now.Add(time.Second)); !ok {
		t.Fatal("a new key refused though a refilled bucket could make room")
	}
	if b.Len() > 4 {
		t.Fatalf("buckets grew to %d past the cap of 4", b.Len())
	}
}

// A delivery id is remembered per endpoint for the replay window: the
// sender's redelivery is seen, another endpoint's same id is not, an id
// still publishing reads pending (so the redelivery is told to retry, never
// told it landed), and an id whose publish failed is forgotten so the retry
// lands.
func TestADeliveryLandsOncePerEndpointWithinTheWindow(t *testing.T) {
	d := ingress.NewDeduper(ingress.ReplayWindow, 8)
	if got := d.Begin("ep-1", "d-1", now); got != ingress.DeliveryNew {
		t.Fatalf("a first delivery = %v, want new", got)
	}
	if got := d.Begin("ep-1", "d-1", now.Add(time.Second)); got != ingress.DeliveryPending {
		t.Fatalf("a redelivery while the first still publishes = %v, want pending", got)
	}
	d.Commit("ep-1", "d-1")
	if got := d.Begin("ep-1", "d-1", now.Add(time.Minute)); got != ingress.DeliveryDone {
		t.Fatalf("a redelivery inside the window = %v, want done", got)
	}
	if got := d.Begin("ep-2", "d-1", now); got != ingress.DeliveryNew {
		t.Fatalf("another endpoint's delivery of the same id = %v, want new", got)
	}
	if got := d.Begin("ep-1", "d-1", now.Add(ingress.ReplayWindow+time.Second)); got != ingress.DeliveryNew {
		t.Fatalf("a delivery past the window = %v, want new", got)
	}
	if got := d.Begin("ep-1", "d-2", now); got != ingress.DeliveryNew {
		t.Fatalf("a fresh id = %v", got)
	}
	d.Abandon("ep-1", "d-2")
	if got := d.Begin("ep-1", "d-2", now); got != ingress.DeliveryNew {
		t.Fatal("an abandoned delivery (its publish failed) still remembered, so the retry would land nothing")
	}
	// An endpoint's memory is bounded: the oldest is let go first.
	b := ingress.NewDeduper(ingress.ReplayWindow, 3)
	for i := 0; i < 5; i++ {
		b.Begin("ep", fmt.Sprintf("d-%d", i), now.Add(time.Duration(i)*time.Second))
		b.Commit("ep", fmt.Sprintf("d-%d", i))
	}
	if b.Len("ep") > 3 {
		t.Fatalf("remembered %d ids past the cap of 3", b.Len("ep"))
	}
	if b.Begin("ep", "d-4", now.Add(10*time.Second)) != ingress.DeliveryDone {
		t.Fatal("the newest id was let go before the oldest")
	}
}

// What the listener refuses and what no endpoint claims are tallied per
// source between flushes: a count, the first and last heard, the reason, and
// for an unclaimed source its last message cut to 512 bytes; the unclaimed
// sources held at once are bounded.
func TestATallyAggregatesBetweenDrains(t *testing.T) {
	tl := ingress.NewTally(3)
	tl.Refused("ep-1", "203.0.113.9", ingress.ReasonBadSignature, now)
	tl.Refused("ep-1", "203.0.113.9", ingress.ReasonBadSignature, now.Add(time.Second))
	tl.Refused("ep-1", "203.0.113.9", ingress.ReasonStaleTimestamp, now.Add(2*time.Second))
	tl.Unclaimed("198.51.100.4", []byte("first"), now)
	tl.Unclaimed("198.51.100.4", []byte(strings.Repeat("é", 400)), now.Add(time.Second))
	for i := 0; i < 5; i++ {
		tl.Unclaimed(fmt.Sprintf("192.0.2.%d", i), []byte("x"), now)
	}
	refused, unclaimed, dropped := tl.Drain()
	if len(refused) != 2 {
		t.Fatalf("refusals = %+v, want one per (endpoint, source, reason)", refused)
	}
	for _, r := range refused {
		if r.Reason == ingress.ReasonBadSignature && (r.Count != 2 || !r.LastAt.Equal(now.Add(time.Second))) {
			t.Fatalf("bad signature = %+v, want two, last a second in", r)
		}
	}
	if len(unclaimed) != 3 || dropped != 3 {
		t.Fatalf("unclaimed = %d sources, %d dropped; want the cap of 3 and 3 dropped", len(unclaimed), dropped)
	}
	var first ingress.UnclaimedSource
	for _, u := range unclaimed {
		if u.Source == "198.51.100.4" {
			first = u
		}
	}
	if first.Count != 2 || !first.FirstAt.Equal(now) || !first.LastAt.Equal(now.Add(time.Second)) {
		t.Fatalf("aggregated source = %+v, want two, first and last heard kept", first)
	}
	if len(first.LastMessage) > ingress.MaxUnclaimedMessage || !strings.HasPrefix(first.LastMessage, "é") {
		t.Fatalf("last message = %d bytes, want at most %d and valid UTF-8", len(first.LastMessage), ingress.MaxUnclaimedMessage)
	}
	if r, u, d := tl.Drain(); len(r) != 0 || len(u) != 0 || d != 0 {
		t.Fatal("a drain did not reset the tally")
	}
}
