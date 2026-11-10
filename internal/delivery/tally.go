package delivery

import (
	"encoding/json"
	"maps"
)

// Tally is the outcome counts per kind of one batch, the shape the receipt's
// outcome column stores: {"metric": {"stored": n, ...}, ...}. The zero value
// is empty and ready; Add on a nil *Tally counts nothing, so a caller with no
// accounting to do passes nil.
type Tally struct {
	m map[Kind]map[Outcome]uint64
}

// Add counts n items of kind k under outcome o. n <= 0 counts nothing.
func (t *Tally) Add(k Kind, o Outcome, n int) {
	if t == nil || n <= 0 {
		return
	}
	if t.m == nil {
		t.m = map[Kind]map[Outcome]uint64{}
	}
	if t.m[k] == nil {
		t.m[k] = map[Outcome]uint64{}
	}
	t.m[k][o] += uint64(n)
}

// Merge adds every count of o to t.
func (t *Tally) Merge(o Tally) {
	for k, os := range o.m {
		for out, n := range os {
			t.Add(k, out, int(n))
		}
	}
}

// Count is the items of kind k under outcome o.
func (t Tally) Count(k Kind, o Outcome) uint64 { return t.m[k][o] }

// Items is every item of kind k the tally counted, deduplicated aside.
func (t Tally) Items(k Kind) uint64 {
	var n uint64
	for o, c := range t.m[k] {
		if !o.Deduplicated() {
			n += c
		}
	}
	return n
}

// Kind is a copy of kind k's counts by outcome.
func (t Tally) Kind(k Kind) map[Outcome]uint64 { return maps.Clone(t.m[k]) }

// Empty reports whether the tally counted nothing.
func (t Tally) Empty() bool { return len(t.m) == 0 }

// MarshalJSON writes the tally as {kind: {outcome: n}}.
func (t Tally) MarshalJSON() ([]byte, error) {
	if t.m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(t.m)
}

// UnmarshalJSON reads what MarshalJSON writes.
func (t *Tally) UnmarshalJSON(b []byte) error {
	var m map[Kind]map[Outcome]uint64
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	t.m = nil
	for k, os := range m {
		for o, n := range os {
			if n > 0 {
				t.Add(k, o, int(n))
			}
		}
	}
	return nil
}
