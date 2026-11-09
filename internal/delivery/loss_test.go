package delivery_test

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
)

func TestLossIsZeroWhenEverythingArrived(t *testing.T) {
	l, ok := delivery.Loss(10, 10)
	if !ok || l != 0 {
		t.Fatalf("Loss(10, 10) = %v, %v; want 0, true", l, ok)
	}
	l, ok = delivery.Loss(4, 3)
	if !ok || l != 0.25 {
		t.Fatalf("Loss(4, 3) = %v, %v; want 0.25, true", l, ok)
	}
}

func TestLossIsAbsentWhenNothingWentIn(t *testing.T) {
	if l, ok := delivery.Loss(0, 0); ok {
		t.Fatalf("Loss(0, 0) = %v, true; want absent", l)
	}
}

func TestEveryOutcomeIsClassedExactlyOnce(t *testing.T) {
	seen := map[delivery.Outcome]bool{}
	for _, o := range delivery.Outcomes {
		if seen[o] {
			t.Fatalf("outcome %q listed twice", o)
		}
		seen[o] = true
		n := 0
		for _, c := range []bool{o.Out(), o.Lost(), o.Deduplicated()} {
			if c {
				n++
			}
		}
		if n != 1 {
			t.Errorf("outcome %q is in %d classes; want exactly 1", o, n)
		}
	}
	for _, o := range []delivery.Outcome{
		delivery.Stored, delivery.Coalesced, delivery.Deduplicated,
		delivery.RefusedUnregistered, delivery.RefusedWriter, delivery.RefusedInstance,
		delivery.RefusedInvalid, delivery.RefusedEmpty, delivery.RefusedWrite,
		delivery.Orphaned, delivery.Unbound, delivery.Unidentified, delivery.Missing,
	} {
		if !seen[o] {
			t.Errorf("outcome %q missing from Outcomes", o)
		}
	}
	if !delivery.Stored.Out() || !delivery.Coalesced.Out() || !delivery.Deduplicated.Deduplicated() || !delivery.Missing.Lost() {
		t.Fatal("stored and coalesced are out, deduplicated is beside the ratio, missing is loss")
	}
}

// receipt is a landed batch of boot b at seq, minted at minute m, carrying
// count metrics with before metrics minted ahead of it, every one stored.
func receipt(b string, seq uint64, m int, before, count uint64) delivery.Receipt {
	r := delivery.Receipt{
		Node:       "node-a",
		Boot:       b,
		Seq:        seq,
		Minted:     t0.Add(time.Duration(m) * time.Minute),
		Deliveries: 1,
		Declared:   delivery.Declared{delivery.Metric: {Count: count, Before: before}},
	}
	r.Outcome.Add(delivery.Metric, delivery.Stored, int(count))
	return r
}

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func window(from, to int) delivery.Window {
	return delivery.Window{From: t0.Add(time.Duration(from) * time.Minute), To: t0.Add(time.Duration(to) * time.Minute)}
}

func TestReconcileReadsAGapAsMissingItems(t *testing.T) {
	rs := []delivery.Receipt{
		receipt("b1", 1, 1, 0, 3),
		receipt("b1", 2, 2, 3, 4),
		// seq 3 (5 items) and 4 (2 items) never landed
		receipt("b1", 5, 5, 14, 1),
	}
	got := delivery.Reconcile(window(0, 10), rs)[delivery.Metric]
	if got.Outcomes[delivery.Missing] != 7 || got.In != 15 || got.Out != 8 {
		t.Fatalf("got in %d out %d missing %d; want 15, 8, 7", got.In, got.Out, got.Outcomes[delivery.Missing])
	}
	if l, ok := got.Loss(); !ok || math.Abs(l-7.0/15) > 1e-12 {
		t.Fatalf("loss = %v, %v; want 7/15", l, ok)
	}
}

func TestReconcileIsOrderInsensitiveAndCountsADuplicateOnce(t *testing.T) {
	rs := []delivery.Receipt{
		receipt("b1", 1, 1, 0, 3),
		receipt("b1", 2, 2, 3, 4),
		receipt("b1", 5, 5, 14, 1),
		receipt("b2", 2, 3, 6, 2),
		receipt("b2", 4, 4, 9, 2),
	}
	want := delivery.Reconcile(window(0, 10), rs)
	for i := 0; i < 20; i++ {
		shuffled := append([]delivery.Receipt(nil), rs...)
		shuffled = append(shuffled, rs[i%len(rs)])
		rand.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		got := delivery.Reconcile(window(0, 10), shuffled)
		if !sameTotals(got, want) {
			t.Fatalf("shuffle %d: %+v; want %+v", i, got[delivery.Metric], want[delivery.Metric])
		}
	}
	m := want[delivery.Metric]
	// b1: 7 missing; b2: 6 below seq 2, and seq 3 holds 9-6-2 = 1.
	if m.Outcomes[delivery.Missing] != 14 {
		t.Fatalf("missing = %d; want 14", m.Outcomes[delivery.Missing])
	}
}

func sameTotals(a, b map[delivery.Kind]delivery.Total) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func TestAGapAcrossAWindowEdgeCountsOnceInTheEarlierWindow(t *testing.T) {
	rs := []delivery.Receipt{
		receipt("b1", 1, 2, 0, 3), // window 1
		// seq 2 and 3 (6 items) never landed
		receipt("b1", 4, 7, 9, 1), // window 2
	}
	w1 := delivery.Reconcile(window(0, 5), rs)[delivery.Metric]
	w2 := delivery.Reconcile(window(5, 10), rs)[delivery.Metric]
	both := delivery.Reconcile(window(0, 10), rs)[delivery.Metric]
	if w1.Outcomes[delivery.Missing] != 6 || w2.Outcomes[delivery.Missing] != 0 {
		t.Fatalf("missing w1 %d, w2 %d; want 6, 0", w1.Outcomes[delivery.Missing], w2.Outcomes[delivery.Missing])
	}
	if w1.In+w2.In != both.In || w1.Out+w2.Out != both.Out ||
		w1.Outcomes[delivery.Missing]+w2.Outcomes[delivery.Missing] != both.Outcomes[delivery.Missing] {
		t.Fatalf("w1 %+v + w2 %+v != both %+v", w1, w2, both)
	}
}

func TestAGapBelowABootsLowestReceiptCountsInThatReceiptsWindow(t *testing.T) {
	rs := []delivery.Receipt{receipt("b1", 3, 7, 5, 2)}
	if got := delivery.Reconcile(window(5, 10), rs)[delivery.Metric]; got.Outcomes[delivery.Missing] != 5 || got.In != 7 {
		t.Fatalf("got %+v; want 5 missing of 7", got)
	}
	if got, ok := delivery.Reconcile(window(0, 5), rs)[delivery.Metric]; ok {
		t.Fatalf("earlier window got %+v; want no metric row", got)
	}
}

func TestADuplicateDeliveryReadsBesideTheRatio(t *testing.T) {
	r := receipt("b1", 1, 1, 0, 4)
	r.Deliveries = 3
	got := delivery.Reconcile(window(0, 5), []delivery.Receipt{r})[delivery.Metric]
	if got.Deduplicated != 8 || got.In != 4 || got.Out != 4 {
		t.Fatalf("got %+v; want 8 deduplicated, 4 in, 4 out", got)
	}
	if l, ok := got.Loss(); !ok || l != 0 {
		t.Fatalf("loss = %v, %v; want 0", l, ok)
	}
}

func TestReconcileCountsRefusalsAsLoss(t *testing.T) {
	r := receipt("b1", 1, 1, 0, 0)
	r.Declared = delivery.Declared{delivery.Property: {Count: 5}}
	r.Outcome = delivery.Tally{}
	r.Outcome.Add(delivery.Property, delivery.Stored, 2)
	r.Outcome.Add(delivery.Property, delivery.Coalesced, 1)
	r.Outcome.Add(delivery.Property, delivery.RefusedUnregistered, 2)
	got := delivery.Reconcile(window(0, 5), []delivery.Receipt{r})[delivery.Property]
	if got.In != 5 || got.Out != 3 || got.Cause() != delivery.RefusedUnregistered {
		t.Fatalf("got %+v cause %q; want 5 in, 3 out, refused-unregistered", got, got.Cause())
	}
}

func TestEndToEndComposesPerHopSurvival(t *testing.T) {
	if got := delivery.EndToEnd(0.5, 0.5); got != 0.75 {
		t.Fatalf("EndToEnd(0.5, 0.5) = %v; want 0.75", got)
	}
	if got := delivery.EndToEnd(0.25); got != 0.25 {
		t.Fatalf("EndToEnd(0.25) = %v; want 0.25", got)
	}
	if got := delivery.EndToEnd(0, 0, 0); got != 0 {
		t.Fatalf("EndToEnd(0, 0, 0) = %v; want 0", got)
	}
}

func TestUnsettledNamesABootWithNothingResolvedPastTheWindow(t *testing.T) {
	a := receipt("b1", 1, 2, 0, 1)
	b := receipt("b1", 2, 7, 1, 1)
	b.Resolved = 1
	c := receipt("b2", 1, 3, 0, 1)
	c.Node = "node-b" // nothing after the window yet
	got := delivery.Unsettled(window(0, 5), []delivery.Receipt{a, b, c})
	if len(got) != 1 || got[0].Node != "node-b" || got[0].Boot != "b2" {
		t.Fatalf("unsettled = %+v; want node-b's b2", got)
	}
	b.Resolved = 0 // seq 1 was still queued when seq 2 was minted
	if got := delivery.Unsettled(window(0, 5), []delivery.Receipt{a, b}); len(got) != 1 {
		t.Fatalf("unsettled = %+v; want b1 while seq 1 is unresolved", got)
	}
}

func TestTallyRoundTripsAsJSON(t *testing.T) {
	var tl delivery.Tally
	tl.Add(delivery.Metric, delivery.Stored, 3)
	tl.Add(delivery.Property, delivery.Coalesced, 1)
	tl.Add(delivery.Property, delivery.Stored, 0)
	b, err := json.Marshal(tl)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"metric":{"stored":3},"property":{"coalesced":1}}` {
		t.Fatalf("json = %s", b)
	}
	var back delivery.Tally
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Count(delivery.Metric, delivery.Stored) != 3 || back.Count(delivery.Property, delivery.Coalesced) != 1 {
		t.Fatalf("round trip = %+v", back)
	}
	var nilTally *delivery.Tally
	nilTally.Add(delivery.Metric, delivery.Stored, 1) // a nil tally counts nothing
}

func TestAStampRoundTripsThroughTheWire(t *testing.T) {
	l := delivery.NewLedger("boot-1")
	l.Mint(map[delivery.Kind]int{delivery.Metric: 2}, t0, 0)
	s := l.Mint(map[delivery.Kind]int{delivery.Log: 3}, t0.Add(time.Second), 1)
	back := delivery.FromProto(s.Proto())
	if back.Boot != "boot-1" || back.Seq != 2 || back.Resolved != 1 || !back.Minted.Equal(s.Minted) {
		t.Fatalf("round trip = %+v; want %+v", back, s)
	}
	if back.Kinds[delivery.Metric] != (delivery.Count{Count: 0, Before: 2}) || back.Kinds[delivery.Log] != (delivery.Count{Count: 3}) {
		t.Fatalf("kinds = %+v", back.Kinds)
	}
	if _, ok := back.Kinds[delivery.Event]; ok {
		t.Fatal("a kind the boot never minted rides the stamp")
	}
}

// TestDeclaredAgainst: a kind a batch decoded fewer items of than its
// ledger declared is short by the difference, one it decoded more of is
// over, and a kind that matches is neither.
func TestDeclaredAgainst(t *testing.T) {
	d := delivery.Declared{delivery.Metric: {Count: 5}, delivery.Property: {Count: 2}, delivery.Log: {Count: 1}}
	short, over := d.Against(map[delivery.Kind]int{delivery.Metric: 3, delivery.Property: 2, delivery.Event: 4, delivery.Log: 1})
	if len(short) != 1 || short[delivery.Metric] != 2 {
		t.Errorf("short = %v, want metric 2", short)
	}
	if len(over) != 1 || over[delivery.Event] != 4 {
		t.Errorf("over = %v, want event 4", over)
	}
}
