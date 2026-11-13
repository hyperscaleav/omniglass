package delivery

import (
	"fmt"
	"math"
)

// The receipt stores its ledger and outcome as integer arrays, not JSON
// objects, because the receipt is one row a batch and its width is a fixed
// cost on every one (#1699). The kind names and outcome names a JSON object
// repeats on every row become positions and codes, fixed by the two lists
// below. Both are append-only: a stored row reads by position, so a kind or
// an outcome is added at the end and never reordered or removed.

// kindSlots is each kind's slot on a packed ledger, slot i holding the
// kind's count at 2i and its before at 2i+1. It is not Kinds, which is a
// display order free to change.
var kindSlots = []Kind{Metric, Property, Event, Log}

// outcomeCodes is each outcome's code on a packed tally, its index. Code 0
// is reserved, so a zero cell never reads as an outcome.
var outcomeCodes = []Outcome{
	"",
	Stored, Coalesced, Deduplicated,
	RefusedUnregistered, RefusedWriter, RefusedInstance, RefusedInvalid,
	RefusedEmpty, RefusedWrite, Orphaned, Unbound, Unidentified,
	RefusedOwner, RefusedTask, RefusedBatch, RefusedMetadata, Abandoned, Missing,
	Shipped, SpoolOverflow, SpoolExpired, BusRefused, SelfLogOverflow, EncodeFailed,
}

// Cells is the ledger as the receipt stores it: a (count, before) pair per
// kind slot, both nil for a kind the ledger does not declare, so an absent
// kind stays apart from one declared at zero. A kind with no slot is not
// stored: no reader counts a kind this server does not know. A count past
// int64 packs at int64's ceiling rather than wrapping negative. Pure.
func (d Declared) Cells() []*int64 {
	out := make([]*int64, 2*len(kindSlots))
	for i, k := range kindSlots {
		c, ok := d[k]
		if !ok {
			continue
		}
		n, before := int64(min(c.Count, math.MaxInt64)), int64(min(c.Before, math.MaxInt64))
		out[2*i], out[2*i+1] = &n, &before
	}
	return out
}

// DeclaredOf reads what Cells writes. A shorter array, written before a
// slot was added, declares none of the kinds past its end; a pair with one
// half nil, a negative cell or a cell past the last slot is an error. Pure.
func DeclaredOf(cells []*int64) (Declared, error) {
	if len(cells) > 2*len(kindSlots) || len(cells)%2 != 0 {
		return nil, fmt.Errorf("ledger of %d cells, want an even count up to %d", len(cells), 2*len(kindSlots))
	}
	d := Declared{}
	for i := 0; i < len(cells); i += 2 {
		n, before := cells[i], cells[i+1]
		if n == nil && before == nil {
			continue
		}
		k := kindSlots[i/2]
		if n == nil || before == nil {
			return nil, fmt.Errorf("ledger slot %s holds half a pair", k)
		}
		if *n < 0 || *before < 0 {
			return nil, fmt.Errorf("ledger slot %s is negative", k)
		}
		d[k] = Count{Count: uint64(*n), Before: uint64(*before)}
	}
	return d, nil
}

// Cells is the tally as the receipt stores it: (code, n) pairs, code the
// kind's slot << 8 | the outcome's code, in slot then code order, zeros
// left out. A kind or outcome with no code is an error: the server tallies
// only what it knows. A count past int32 packs at int32's ceiling, still
// loss, so a node's ledger declaring more than it sent cannot refuse the
// batch it shortchanged. Pure.
func (t Tally) Cells() ([]int32, error) {
	out := []int32{}
	for k, os := range t.m {
		if slot(k) < 0 {
			return nil, fmt.Errorf("tally kind %q has no slot", k)
		}
		for o := range os {
			if code(o) < 0 {
				return nil, fmt.Errorf("tally outcome %q has no code", o)
			}
		}
	}
	for s, k := range kindSlots {
		for c, o := range outcomeCodes {
			if n := t.m[k][o]; c > 0 && n > 0 {
				out = append(out, int32(s<<8|c), int32(min(n, math.MaxInt32)))
			}
		}
	}
	return out, nil
}

// TallyOf reads what Cells writes. An odd array, an unknown slot or code, or
// a negative count is an error. Pure.
func TallyOf(cells []int32) (Tally, error) {
	var t Tally
	if len(cells)%2 != 0 {
		return t, fmt.Errorf("outcome of %d cells, want pairs", len(cells))
	}
	for i := 0; i < len(cells); i += 2 {
		packed, n := cells[i], cells[i+1]
		s, c := int(packed>>8), int(packed&0xff)
		if packed < 0 || s >= len(kindSlots) || c == 0 || c >= len(outcomeCodes) {
			return Tally{}, fmt.Errorf("outcome code %d names no kind and outcome", packed)
		}
		if n < 0 {
			return Tally{}, fmt.Errorf("outcome code %d counts %d", packed, n)
		}
		t.Add(kindSlots[s], outcomeCodes[c], int(n))
	}
	return t, nil
}

func slot(k Kind) int {
	for i, s := range kindSlots {
		if s == k {
			return i
		}
	}
	return -1
}

func code(o Outcome) int {
	for i, c := range outcomeCodes {
		if i > 0 && c == o {
			return i
		}
	}
	return -1
}
