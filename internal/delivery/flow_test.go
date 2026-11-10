package delivery_test

import (
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// read indexes readings by key and kind.
func read(t *testing.T, rs []delivery.Reading) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, r := range rs {
		k := r.Key + "/" + r.Instance
		if _, dup := out[k]; dup {
			t.Fatalf("%s read twice in one window", k)
		}
		out[k] = r.Value
	}
	return out
}

// TestAQuietWindowReadsZeroForEveryKind: the in and out counts are written
// every window, zero when quiet, and the loss ratio too, as 0 when nothing
// went in, so a hop's latest reading is never a past window's loss left
// standing.
func TestAQuietWindowReadsZeroForEveryKind(t *testing.T) {
	var c delivery.Counts
	got := read(t, c.Readings())
	if len(got) != 3*len(delivery.Kinds) {
		t.Fatalf("a quiet window read %d values, want in, out and ratio for each of %d kinds: %v", len(got), len(delivery.Kinds), got)
	}
	for _, k := range delivery.Kinds {
		for _, key := range []string{writerclass.DeliveryInCount, writerclass.DeliveryOutCount, writerclass.DeliveryLossRatio} {
			if v, ok := got[key+"/"+string(k)]; !ok || v != 0 {
				t.Errorf("%s/%s = %v (written %v), want 0", key, k, v, ok)
			}
		}
	}
}

func TestADroppedBatchReadsALossAboveZero(t *testing.T) {
	var c delivery.Counts
	c.Delivered(map[delivery.Kind]int{delivery.Metric: 3, delivery.Log: 2})
	c.Lost(map[delivery.Kind]int{delivery.Metric: 1})
	got := read(t, c.Readings())
	if got[writerclass.DeliveryInCount+"/metric"] != 4 || got[writerclass.DeliveryOutCount+"/metric"] != 3 {
		t.Fatalf("metric in %v out %v, want 4 and 3", got[writerclass.DeliveryInCount+"/metric"], got[writerclass.DeliveryOutCount+"/metric"])
	}
	if l := got[writerclass.DeliveryLossRatio+"/metric"]; l != 0.25 {
		t.Fatalf("metric loss = %v, want 0.25", l)
	}
	if l := got[writerclass.DeliveryLossRatio+"/log"]; l != 0 {
		t.Fatalf("log loss = %v, want 0: both its lines arrived", l)
	}
}

// TestARedeliveryReadsNoLoss: a batch delivered twice (the first ack lost)
// counts once in and once out each time, so it reads 0 loss, never as an
// item lost or a ratio below zero.
func TestARedeliveryReadsNoLoss(t *testing.T) {
	var c delivery.Counts
	items := map[delivery.Kind]int{delivery.Metric: 2, delivery.Event: 1}
	c.Delivered(items)
	c.Delivered(items)
	got := read(t, c.Readings())
	if got[writerclass.DeliveryInCount+"/metric"] != 4 || got[writerclass.DeliveryOutCount+"/metric"] != 4 {
		t.Fatalf("metric in %v out %v, want 4 and 4", got[writerclass.DeliveryInCount+"/metric"], got[writerclass.DeliveryOutCount+"/metric"])
	}
	for _, k := range delivery.Kinds {
		if l := got[writerclass.DeliveryLossRatio+"/"+string(k)]; l != 0 {
			t.Errorf("%s loss = %v, want 0", k, l)
		}
	}
}

// TestALandedTallyCountsDeduplicatedAsHandled: the server's side of a hop
// reads the store's outcome. Stored, coalesced and deduplicated items are out
// (a deduplicated item is one already landed, so a replay reads 0 loss); a
// refused item went in and did not come out.
func TestALandedTallyCountsDeduplicatedAsHandled(t *testing.T) {
	var tl delivery.Tally
	tl.Add(delivery.Metric, delivery.Stored, 5)
	tl.Add(delivery.Metric, delivery.Coalesced, 1)
	tl.Add(delivery.Metric, delivery.Deduplicated, 2)
	tl.Add(delivery.Metric, delivery.RefusedWriter, 2)
	tl.Add(delivery.Property, delivery.Deduplicated, 3)
	var c delivery.Counts
	c.Landed(tl)
	got := read(t, c.Readings())
	if got[writerclass.DeliveryInCount+"/metric"] != 10 || got[writerclass.DeliveryOutCount+"/metric"] != 8 {
		t.Fatalf("metric in %v out %v, want 10 and 8", got[writerclass.DeliveryInCount+"/metric"], got[writerclass.DeliveryOutCount+"/metric"])
	}
	if l := got[writerclass.DeliveryLossRatio+"/metric"]; l != 0.2 {
		t.Fatalf("metric loss = %v, want 0.2", l)
	}
	if got[writerclass.DeliveryInCount+"/property"] != 3 || got[writerclass.DeliveryLossRatio+"/property"] != 0 {
		t.Fatalf("an all-deduplicated replay read property in %v loss %v, want 3 and 0",
			got[writerclass.DeliveryInCount+"/property"], got[writerclass.DeliveryLossRatio+"/property"])
	}
}

func TestCountsMergeAddsEveryKind(t *testing.T) {
	var a, b delivery.Counts
	a.Delivered(map[delivery.Kind]int{delivery.Metric: 1})
	b.Lost(map[delivery.Kind]int{delivery.Metric: 1, delivery.Log: 2})
	a.Merge(b)
	got := read(t, a.Readings())
	if got[writerclass.DeliveryInCount+"/metric"] != 2 || got[writerclass.DeliveryOutCount+"/metric"] != 1 || got[writerclass.DeliveryInCount+"/log"] != 2 {
		t.Fatalf("merged %v", got)
	}
}

// TestAFlowTakesEachItemOnce: the flow is the counts many goroutines add to,
// and each take is the window since the last, so an item counts in exactly
// one window.
func TestAFlowTakesEachItemOnce(t *testing.T) {
	var f delivery.Flow
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				f.Delivered(map[delivery.Kind]int{delivery.Metric: 1})
			}
		})
	}
	wg.Wait()
	first := read(t, f.Take().Readings())
	if first[writerclass.DeliveryInCount+"/metric"] != 800 || first[writerclass.DeliveryOutCount+"/metric"] != 800 {
		t.Fatalf("first window %v, want 800 in and out", first)
	}
	second := read(t, f.Take().Readings())
	if second[writerclass.DeliveryInCount+"/metric"] != 0 {
		t.Fatalf("second window read %v in, want 0: each item counts once", second[writerclass.DeliveryInCount+"/metric"])
	}
}
