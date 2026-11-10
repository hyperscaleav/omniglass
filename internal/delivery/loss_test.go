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
		delivery.Shipped, delivery.SpoolOverflow, delivery.SpoolExpired, delivery.BusRefused,
		delivery.SelfLogOverflow, delivery.EncodeFailed,
	} {
		if !seen[o] {
			t.Errorf("outcome %q missing from Outcomes", o)
		}
	}
	if !delivery.Stored.Out() || !delivery.Coalesced.Out() || !delivery.Unbound.Out() || !delivery.Deduplicated.Deduplicated() || !delivery.Missing.Lost() {
		t.Fatal("stored, coalesced and unbound are out, deduplicated is beside the ratio, missing is loss")
	}
	for _, o := range []delivery.Outcome{delivery.RefusedWriter, delivery.Orphaned, delivery.Unidentified, delivery.Missing} {
		if !o.Lost() {
			t.Errorf("outcome %q is not loss", o)
		}
	}
	if !delivery.Shipped.Out() {
		t.Error("shipped is the node hop's out")
	}
	for _, hop := range []delivery.Hop{delivery.Node, delivery.Ingest} {
		for _, o := range delivery.OutcomesOf(hop) {
			if !seen[o] {
				t.Errorf("hop %s outcome %q missing from Outcomes", hop, o)
			}
		}
	}
	if len(delivery.NodeOutcomes)+len(delivery.IngestOutcomes) != len(delivery.Outcomes) {
		t.Error("an outcome belongs to more than one hop, or to none")
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

// overflowed is receipt with the node's account riding it: the runs its
// spool dropped and what it lost before minting.
func overflowed(r delivery.Receipt, runs []delivery.Drop, lost map[delivery.Outcome]map[delivery.Kind]uint64) delivery.Receipt {
	r.Loss = delivery.NodeLoss{Dropped: runs, Lost: lost}
	return r
}

// TestTheNodeHopReconcilesSeparatelyFromIngest: a run the spool dropped
// reads as the node hop's loss under its cause and leaves the ingest row,
// a gap the node shipped reads as missing at ingest and not at the node, an
// item lost before minting is the node's alone, and end to end composes the
// two hops into the pipeline's whole loss.
func TestTheNodeHopReconcilesSeparatelyFromIngest(t *testing.T) {
	rs := []delivery.Receipt{
		receipt("b1", 1, 1, 0, 3),
		// seq 2 (2 items) and 3 (3 items) dropped at the spool's cap
		overflowed(receipt("b1", 4, 2, 8, 1),
			[]delivery.Drop{{Cause: delivery.SpoolOverflow, From: 2, To: 3, Items: map[delivery.Kind]uint64{delivery.Metric: 5}}},
			map[delivery.Outcome]map[delivery.Kind]uint64{delivery.EncodeFailed: {delivery.Metric: 2}}),
		// seq 5 (4 items) shipped and never landed
		receipt("b1", 6, 3, 13, 2),
	}
	node := delivery.ReconcileNode(window(0, 10), rs)[delivery.Metric]
	ingest := delivery.Reconcile(window(0, 10), rs)[delivery.Metric]
	if node.In != 17 || node.Out != 10 || node.Outcomes[delivery.SpoolOverflow] != 5 ||
		node.Outcomes[delivery.EncodeFailed] != 2 || node.Cause() != delivery.SpoolOverflow {
		t.Fatalf("node = %+v cause %q; want 17 in, 10 out, 5 spool-overflow, 2 encode-failed", node, node.Cause())
	}
	if ingest.In != node.Out || ingest.Out != 6 || ingest.Outcomes[delivery.Missing] != 4 {
		t.Fatalf("ingest = %+v; want in %d (the node's out), 6 out, 4 missing", ingest, node.Out)
	}
	nl, _ := node.Loss()
	il, _ := ingest.Loss()
	if got, want := delivery.EndToEnd(nl, il), float64(node.In-ingest.Out)/float64(node.In); math.Abs(got-want) > 1e-12 {
		t.Fatalf("end to end = %v; want %v, what went in at the node and never came out of ingest", got, want)
	}
}

// TestAGapTheNodeDidNotDropIsIngestsAlone: with no run on any receipt, the
// node shipped every item and reads 0, and the gap is missing at ingest.
func TestAGapTheNodeDidNotDropIsIngestsAlone(t *testing.T) {
	rs := []delivery.Receipt{receipt("b1", 1, 1, 0, 3), receipt("b1", 3, 2, 7, 1)}
	node := delivery.ReconcileNode(window(0, 10), rs)[delivery.Metric]
	if l, ok := node.Loss(); !ok || l != 0 || node.In != 8 {
		t.Fatalf("node = %+v; want 8 in, loss 0", node)
	}
	if got := delivery.Reconcile(window(0, 10), rs)[delivery.Metric]; got.Outcomes[delivery.Missing] != 4 {
		t.Fatalf("ingest missing = %d; want 4", got.Outcomes[delivery.Missing])
	}
}

// TestADroppedRunBelowABootsLowestReceiptIsTheNodes: the gap below the
// lowest landed receipt is owned as Reconcile owns it, and a run there is
// the node's.
func TestADroppedRunBelowABootsLowestReceiptIsTheNodes(t *testing.T) {
	rs := []delivery.Receipt{overflowed(receipt("b1", 3, 7, 5, 2),
		[]delivery.Drop{{Cause: delivery.SpoolExpired, From: 1, To: 2, Items: map[delivery.Kind]uint64{delivery.Metric: 5}}}, nil)}
	node := delivery.ReconcileNode(window(5, 10), rs)[delivery.Metric]
	if node.Outcomes[delivery.SpoolExpired] != 5 || node.In != 7 {
		t.Fatalf("node = %+v; want 5 spool-expired of 7", node)
	}
	if got := delivery.Reconcile(window(5, 10), rs)[delivery.Metric]; got.Outcomes[delivery.Missing] != 0 || got.In != 2 {
		t.Fatalf("ingest = %+v; want 2 in, none missing", got)
	}
}

// TestLostBeforeMintingCountsOnceAcrossWindows: the running totals each
// batch carries split by window, two adjacent windows summing to one over
// both.
func TestLostBeforeMintingCountsOnceAcrossWindows(t *testing.T) {
	lost := func(n uint64) map[delivery.Outcome]map[delivery.Kind]uint64 {
		return map[delivery.Outcome]map[delivery.Kind]uint64{delivery.SelfLogOverflow: {delivery.Log: n}}
	}
	rs := []delivery.Receipt{
		overflowed(receipt("b1", 1, 2, 0, 1), nil, lost(3)),
		overflowed(receipt("b1", 2, 7, 1, 1), nil, lost(5)),
	}
	w1 := delivery.ReconcileNode(window(0, 5), rs)[delivery.Log]
	w2 := delivery.ReconcileNode(window(5, 10), rs)[delivery.Log]
	both := delivery.ReconcileNode(window(0, 10), rs)[delivery.Log]
	if w1.Outcomes[delivery.SelfLogOverflow] != 3 || w2.Outcomes[delivery.SelfLogOverflow] != 2 || both.Outcomes[delivery.SelfLogOverflow] != 5 {
		t.Fatalf("self-log-overflow w1 %d, w2 %d, both %d; want 3, 2, 5", w1.Outcomes[delivery.SelfLogOverflow],
			w2.Outcomes[delivery.SelfLogOverflow], both.Outcomes[delivery.SelfLogOverflow])
	}
	if _, ok := delivery.Reconcile(window(0, 10), rs)[delivery.Log]; ok {
		t.Fatal("a line lost before minting reads on the ingest row")
	}
}

// TestALedgerCarriesADroppedRunUntilAnAckedBatchDoes: consecutive drops of
// one cause extend one run, every batch minted after carries it, and a batch
// minted after the run is acked retires it.
func TestALedgerCarriesADroppedRunUntilAnAckedBatchDoes(t *testing.T) {
	l := delivery.NewLedger("boot-1")
	l.Mint(map[delivery.Kind]int{delivery.Metric: 2}, t0, 0)
	l.Mint(map[delivery.Kind]int{delivery.Metric: 3}, t0, 0)
	l.Drop(delivery.SpoolOverflow, 1, map[delivery.Kind]int{delivery.Metric: 2})
	l.Drop(delivery.SpoolOverflow, 2, map[delivery.Kind]int{delivery.Metric: 3})
	l.Drop(delivery.BusRefused, 3, map[delivery.Kind]int{delivery.Log: 1})
	l.Lose(delivery.EncodeFailed, map[delivery.Kind]int{delivery.Event: 4})
	s3 := l.Mint(map[delivery.Kind]int{delivery.Metric: 1}, t0, 2)
	want := []delivery.Drop{
		{Cause: delivery.SpoolOverflow, From: 1, To: 2, Items: map[delivery.Kind]uint64{delivery.Metric: 5}},
		{Cause: delivery.BusRefused, From: 3, To: 3, Items: map[delivery.Kind]uint64{delivery.Log: 1}},
	}
	if got, _ := json.Marshal(s3.Loss.Dropped); string(got) != mustJSON(t, want) {
		t.Fatalf("seq 3 carries %s; want %s", got, mustJSON(t, want))
	}
	if back := delivery.FromProto(s3.Proto()); mustJSON(t, back.Loss) != mustJSON(t, s3.Loss) {
		t.Fatalf("the wire carried %s; want %s", mustJSON(t, back.Loss), mustJSON(t, s3.Loss))
	}
	l.Stored(2) // acked, but minted before the drops
	if s := l.Mint(nil, t0, 2); len(s.Loss.Dropped) != 2 {
		t.Fatalf("after an older ack, seq 4 carries %d runs; want 2", len(s.Loss.Dropped))
	}
	l.Stored(3)
	s5 := l.Mint(nil, t0, 4)
	if len(s5.Loss.Dropped) != 0 {
		t.Fatalf("after seq 3's ack, seq 5 carries %+v; want none", s5.Loss.Dropped)
	}
	if s5.Loss.Lost[delivery.EncodeFailed][delivery.Event] != 4 {
		t.Fatalf("seq 5 lost = %+v; want the running encode-failed total", s5.Loss.Lost)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
