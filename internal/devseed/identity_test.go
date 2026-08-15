package devseed

import "testing"

// The generated-row index is the whole of how a second run recognises a row
// whose name the platform chose, so it is tested as the pure value it is, with
// no database in the way.

// TestGenIndexHandsOutAllocationOrder proves the index answers with the fleet's
// rows in the order the generator allocated them, whatever order they were read
// in. Sorting on the stored ordinal rather than the name is the point: a
// suppressed first ordinal ("boardroom" is 1, "boardroom-2" is 2) sorts
// correctly here and would sort backwards by name.
func TestGenIndexHandsOutAllocationOrder(t *testing.T) {
	room := genSlot{bucket: "location/room-1", class: "board"}
	idx := newGenIndex([]genRow{
		{slot: room, ordinal: 2, id: "second"},
		{slot: room, ordinal: 1, id: "first"},
	})
	if got := idx.take(room); got != "first" {
		t.Errorf("first take = %q, want first (the lowest ordinal, whatever order the rows were read in)", got)
	}
	if got := idx.take(room); got != "second" {
		t.Errorf("second take = %q, want second", got)
	}
	if got := idx.take(room); got != "" {
		t.Errorf("third take = %q, want \"\" (the fleet is short of one, so the caller creates it)", got)
	}
}

// TestGenIndexKeepsSlotsApart proves two fixture rows in different ordinal
// spaces never resolve to each other's row: the bucket and the classification
// together are the space, so the same room's displays and video bars are
// separate runs of ordinals, and the same product in another room is another.
func TestGenIndexKeepsSlotsApart(t *testing.T) {
	displays := genSlot{bucket: "location/room-1", class: "samsung-qm55"}
	bars := genSlot{bucket: "location/room-1", class: "cisco-room-bar"}
	elsewhere := genSlot{bucket: "location/room-2", class: "samsung-qm55"}
	idx := newGenIndex([]genRow{
		{slot: displays, ordinal: 1, id: "display-here"},
		{slot: bars, ordinal: 1, id: "bar-here"},
		{slot: elsewhere, ordinal: 1, id: "display-there"},
	})
	for _, tc := range []struct {
		slot genSlot
		want string
	}{
		{displays, "display-here"},
		{bars, "bar-here"},
		{elsewhere, "display-there"},
	} {
		if got := idx.take(tc.slot); got != tc.want {
			t.Errorf("take(%+v) = %q, want %q", tc.slot, got, tc.want)
		}
	}
}

// TestGenIndexAdvancesEvenWhenTheFleetIsShort is why nothing has to record the
// rows a run creates. Two identical fixture rows on an empty fleet must each be
// told "not here" so each is created; if the slot only advanced on a hit, the
// second would resolve to the first and the fleet would come up one short, which
// a fresh run's counts would never notice.
func TestGenIndexAdvancesEvenWhenTheFleetIsShort(t *testing.T) {
	room := genSlot{bucket: "location/room-1", class: "samsung-qm55"}
	idx := newGenIndex([]genRow{{slot: room, ordinal: 1, id: "already-here"}})
	if got, want := idx.take(room), "already-here"; got != want {
		t.Fatalf("first take = %q, want %q", got, want)
	}
	if got := idx.take(room); got != "" {
		t.Errorf("second take = %q, want \"\" (the fleet holds one and the fixture declares two)", got)
	}
	if got := idx.take(room); got != "" {
		t.Errorf("third take = %q, want \"\" (a slot never hands the same row out twice)", got)
	}

	// The next run over the fleet those creates left behind resolves all three.
	next := newGenIndex([]genRow{
		{slot: room, ordinal: 1, id: "already-here"},
		{slot: room, ordinal: 2, id: "created-2"},
		{slot: room, ordinal: 3, id: "created-3"},
	})
	for _, want := range []string{"already-here", "created-2", "created-3"} {
		if got := next.take(room); got != want {
			t.Errorf("re-run take = %q, want %q", got, want)
		}
	}
}

// TestProductClassSpellsTheGenericTheGatewayResolves pins the one place the
// fixture and the fleet could spell a classification differently: an omitted
// product is not "unclassified", the gateway resolves it to generic-device, and
// that is what the row then reports back.
func TestProductClassSpellsTheGenericTheGatewayResolves(t *testing.T) {
	if got := productClass(""); got != "generic-device" {
		t.Errorf("productClass(\"\") = %q, want generic-device (what CreateComponent's own COALESCE resolves)", got)
	}
	if got := productClass("samsung-qm55"); got != "samsung-qm55" {
		t.Errorf("productClass(samsung-qm55) = %q, want it unchanged", got)
	}
}

// TestBucketOfPrefersTheParent pins the placement bucket's precedence against
// the scoped-name unique indexes it mirrors: a parent wins over a location, and
// a row with neither is unplaced.
func TestBucketOfPrefersTheParent(t *testing.T) {
	if got, want := bucketOf("p", "l"), "parent/p"; got != want {
		t.Errorf("bucketOf(parent, location) = %q, want %q (a parent wins)", got, want)
	}
	if got, want := bucketOf("", "l"), "location/l"; got != want {
		t.Errorf("bucketOf(no parent, location) = %q, want %q", got, want)
	}
	if got, want := bucketOf("", ""), "unplaced"; got != want {
		t.Errorf("bucketOf(nothing) = %q, want %q", got, want)
	}
}
