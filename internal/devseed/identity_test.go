package devseed

import "testing"

// The generated-row index is the whole of how a second run recognises a row
// whose name the platform chose, so it is tested as the pure value it is, with
// no database in the way.

// TestGenIndexHandsOutAllocationOrder proves the index answers with the estate's
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
		t.Errorf("third take = %q, want \"\" (the estate is short of one, so the caller creates it)", got)
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

// TestGenIndexSeesTheRowsThisRunCreated proves a row created mid-run is visible
// to the next fixture row in the same slot, so two identical fixture rows create
// two estate rows on the first run and resolve to those same two on the second.
// Without it the second row would resolve to the first and the estate would come
// up one short, which no count assertion of a fresh run would notice.
func TestGenIndexSeesTheRowsThisRunCreated(t *testing.T) {
	room := genSlot{bucket: "location/room-1", class: "samsung-qm55"}
	idx := newGenIndex(nil)
	if got := idx.take(room); got != "" {
		t.Fatalf("first take on an empty estate = %q, want \"\"", got)
	}
	idx.grew(room, "created-1")
	if got := idx.take(room); got != "" {
		t.Errorf("second take = %q, want \"\" (a second fixture row wants a second estate row, not the one just created)", got)
	}
	idx.grew(room, "created-2")

	// A second run over the same estate: a fresh index built from what is now
	// there hands the two fixture rows back the two rows they created.
	next := newGenIndex([]genRow{
		{slot: room, ordinal: 1, id: "created-1"},
		{slot: room, ordinal: 2, id: "created-2"},
	})
	if got, want := next.take(room), "created-1"; got != want {
		t.Errorf("re-run first take = %q, want %q", got, want)
	}
	if got, want := next.take(room), "created-2"; got != want {
		t.Errorf("re-run second take = %q, want %q", got, want)
	}
}

// TestProductClassSpellsTheGenericTheGatewayResolves pins the one place the
// fixture and the estate could spell a classification differently: an omitted
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
