package delivery_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/delivery"
)

func ptr(n int64) *int64 { return &n }

// TestLedgerCellsKeepAbsentApartFromZero: a kind declared at zero packs as
// a zero pair and an absent kind as a nil one, each in its own slot, and
// both read back as they went in.
func TestLedgerCellsKeepAbsentApartFromZero(t *testing.T) {
	d := delivery.Declared{
		delivery.Metric: {Count: 58, Before: 3_000_000_000},
		delivery.Log:    {Count: 0, Before: 9},
	}
	got := d.Cells()
	want := []*int64{ptr(58), ptr(3_000_000_000), nil, nil, nil, nil, ptr(0), ptr(9)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cells = %v, want %v", deref(got), deref(want))
	}
	back, err := delivery.DeclaredOf(got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, d) {
		t.Fatalf("read back %v, want %v", back, d)
	}
}

// TestLedgerCellsOfAnEmptyLedger: a ledger declaring nothing packs as every
// slot nil, never as no array, and reads back empty.
func TestLedgerCellsOfAnEmptyLedger(t *testing.T) {
	got := delivery.Declared{}.Cells()
	if len(got) != 8 {
		t.Fatalf("cells = %d, want 8", len(got))
	}
	for i, c := range got {
		if c != nil {
			t.Fatalf("cell %d = %d, want nil", i, *c)
		}
	}
	back, err := delivery.DeclaredOf(got)
	if err != nil || len(back) != 0 {
		t.Fatalf("read back %v, %v; want empty", back, err)
	}
}

// TestLedgerCellsSaturateAtInt64: a node's count past int64 packs at
// int64's ceiling rather than wrapping negative, so its receipt still reads.
func TestLedgerCellsSaturateAtInt64(t *testing.T) {
	got := delivery.Declared{delivery.Metric: {Count: math.MaxUint64, Before: math.MaxInt64 + 1}}.Cells()
	back, err := delivery.DeclaredOf(got)
	if err != nil {
		t.Fatal(err)
	}
	if want := (delivery.Declared{delivery.Metric: {Count: math.MaxInt64, Before: math.MaxInt64}}); !reflect.DeepEqual(back, want) {
		t.Fatalf("read back %v, want %v", back, want)
	}
}

// TestLedgerCellsLeaveAnUnknownKindOut: a kind a newer node declares has no
// slot here, and no reader counts it.
func TestLedgerCellsLeaveAnUnknownKindOut(t *testing.T) {
	got := delivery.Declared{"trace": {Count: 1}}.Cells()
	back, err := delivery.DeclaredOf(got)
	if err != nil || len(back) != 0 {
		t.Fatalf("read back %v, %v; want empty", back, err)
	}
}

// TestLedgerCellsReadAShorterArray: a row written before a slot was added
// declares none of the kinds past its end.
func TestLedgerCellsReadAShorterArray(t *testing.T) {
	back, err := delivery.DeclaredOf([]*int64{ptr(1), ptr(2)})
	if err != nil {
		t.Fatal(err)
	}
	if want := (delivery.Declared{delivery.Metric: {Count: 1, Before: 2}}); !reflect.DeepEqual(back, want) {
		t.Fatalf("read back %v, want %v", back, want)
	}
}

func TestLedgerCellsRefuseAMalformedArray(t *testing.T) {
	for name, cells := range map[string][]*int64{
		"half a pair": {ptr(1), nil},
		"odd":         {ptr(1)},
		"past a slot": make([]*int64, 10),
		"negative":    {ptr(-1), ptr(0)},
	} {
		if _, err := delivery.DeclaredOf(cells); err == nil {
			t.Errorf("%s: read back, want an error", name)
		}
	}
}

// TestKindSlotsArePinned: a stored ledger reads by position, so each kind's
// slot never moves. A new kind takes the next slot.
func TestKindSlotsArePinned(t *testing.T) {
	for slot, k := range []delivery.Kind{delivery.Metric, delivery.Property, delivery.Event, delivery.Log} {
		cells := delivery.Declared{k: {Count: 1, Before: 2}}.Cells()
		if cells[2*slot] == nil || *cells[2*slot] != 1 || *cells[2*slot+1] != 2 {
			t.Errorf("kind %s is not at slot %d: %v", k, slot, deref(cells))
		}
	}
}

// TestEveryKindHasASlot: the server tallies every kind it knows, and a tally
// whose kind has no slot cannot be stored, so a new kind takes its slot in
// the same change.
func TestEveryKindHasASlot(t *testing.T) {
	for _, k := range delivery.Kinds {
		var tl delivery.Tally
		tl.Add(k, delivery.Stored, 1)
		if _, err := tl.Cells(); err != nil {
			t.Errorf("kind %s: %v", k, err)
		}
	}
}

// outcomeCodes is the golden copy of every outcome's stored code. A stored
// tally reads by code, so a code never moves; a new outcome takes the next.
// The migration packing the jsonb outcome (#1699) froze this same list.
var outcomeCodes = map[delivery.Outcome]int32{
	delivery.Stored: 1, delivery.Coalesced: 2, delivery.Deduplicated: 3,
	delivery.RefusedUnregistered: 4, delivery.RefusedWriter: 5, delivery.RefusedInstance: 6,
	delivery.RefusedInvalid: 7, delivery.RefusedEmpty: 8, delivery.RefusedWrite: 9,
	delivery.Orphaned: 10, delivery.Unbound: 11, delivery.Unidentified: 12,
	delivery.RefusedOwner: 13, delivery.RefusedTask: 14, delivery.RefusedBatch: 15,
	delivery.RefusedMetadata: 16, delivery.Abandoned: 17, delivery.Missing: 18,
	delivery.Shipped: 19, delivery.SpoolOverflow: 20, delivery.SpoolExpired: 21,
	delivery.BusRefused: 22, delivery.SelfLogOverflow: 23, delivery.EncodeFailed: 24,
}

// TestEveryOutcomeHasItsPinnedCode: every item outcome packs, under its
// golden code, beside each kind's slot.
func TestEveryOutcomeHasItsPinnedCode(t *testing.T) {
	if len(outcomeCodes) != len(delivery.Outcomes) {
		t.Fatalf("golden codes = %d, outcomes = %d: a new outcome needs the next code", len(outcomeCodes), len(delivery.Outcomes))
	}
	for _, o := range delivery.Outcomes {
		want, ok := outcomeCodes[o]
		if !ok {
			t.Fatalf("outcome %s has no golden code", o)
		}
		var tl delivery.Tally
		tl.Add(delivery.Log, o, 5)
		got, err := tl.Cells()
		if err != nil {
			t.Fatalf("%s: %v", o, err)
		}
		if w := []int32{3<<8 | want, 5}; !reflect.DeepEqual(got, w) {
			t.Errorf("%s packs %v, want %v", o, got, w)
		}
	}
}

// TestTallyCellsRoundTrip: a tally across kinds and outcomes packs in slot
// then code order, zeros left out, and reads back equal.
func TestTallyCellsRoundTrip(t *testing.T) {
	var tl delivery.Tally
	tl.Add(delivery.Log, delivery.Stored, 1)
	tl.Add(delivery.Metric, delivery.Missing, 2)
	tl.Add(delivery.Metric, delivery.Stored, 58)
	tl.Add(delivery.Property, delivery.Coalesced, 0)
	got, err := tl.Cells()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int32{1, 58, 18, 2, 3<<8 | 1, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cells = %v, want %v", got, want)
	}
	back, err := delivery.TallyOf(got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, tl) {
		t.Fatalf("read back %+v, want %+v", back, tl)
	}
}

func TestTallyCellsOfAnEmptyTally(t *testing.T) {
	got, err := delivery.Tally{}.Cells()
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("cells = %v, %v; want an empty, non-nil array", got, err)
	}
	back, err := delivery.TallyOf(got)
	if err != nil || !back.Empty() {
		t.Fatalf("read back %+v, %v; want empty", back, err)
	}
}

func TestTallyCellsRefuseWhatHasNoCode(t *testing.T) {
	cases := map[string]func(*delivery.Tally){
		"unknown kind":    func(t *delivery.Tally) { t.Add("trace", delivery.Stored, 1) },
		"unknown outcome": func(t *delivery.Tally) { t.Add(delivery.Metric, delivery.Returned, 1) },
	}
	for name, add := range cases {
		var tl delivery.Tally
		add(&tl)
		if _, err := tl.Cells(); err == nil {
			t.Errorf("%s: packed, want an error", name)
		}
	}
}

// TestTallyCellsSaturateAtInt32: a count past int32, as a node's ledger
// declaring more than it could have sent leaves on its shortfall, packs at
// int32's ceiling: it still reads as loss, and the batch still lands.
func TestTallyCellsSaturateAtInt32(t *testing.T) {
	var tl delivery.Tally
	tl.Add(delivery.Metric, delivery.Missing, math.MaxInt32+5)
	got, err := tl.Cells()
	if err != nil {
		t.Fatal(err)
	}
	if want := []int32{18, math.MaxInt32}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cells = %v, want %v", got, want)
	}
}

func TestTallyCellsRefuseAMalformedArray(t *testing.T) {
	for name, cells := range map[string][]int32{
		"odd":           {1},
		"code 0":        {0, 1},
		"unknown code":  {99, 1},
		"unknown slot":  {9<<8 | 1, 1},
		"negative":      {1, -1},
		"negative code": {-1, 1},
	} {
		if _, err := delivery.TallyOf(cells); err == nil {
			t.Errorf("%s: read back, want an error", name)
		}
	}
}

func deref(cells []*int64) []any {
	out := make([]any, len(cells))
	for i, c := range cells {
		if c != nil {
			out[i] = *c
		}
	}
	return out
}
