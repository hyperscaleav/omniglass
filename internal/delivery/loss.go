package delivery

import (
	"cmp"
	"maps"
	"slices"
	"time"
)

// Loss is (in - out) / in, absent (ok false) when nothing went in: a kind
// with nothing to lose has no ratio, never a fake 0.
func Loss(in, out uint64) (loss float64, ok bool) {
	if in == 0 {
		return 0, false
	}
	if out >= in {
		return 0, true
	}
	return float64(in-out) / float64(in), true
}

// EndToEnd composes per-hop losses into the whole pipeline's: an item
// survives only if it survives every hop, so loss = 1 - prod(1 - loss_hop).
func EndToEnd(hops ...float64) float64 {
	survive := 1.0
	for _, l := range hops {
		survive *= 1 - l
	}
	return 1 - survive
}

// Receipt is one landed batch as its receipt records it.
type Receipt struct {
	Node       string
	Boot       string
	Seq        uint64
	Minted     time.Time
	Resolved   uint64
	Deliveries int
	Declared   Declared
	Outcome    Tally
	// Loss is the node's own account the batch carried (#1692).
	Loss NodeLoss
}

// Window is a stretch of node time, (From, To]: a receipt is in it when its
// batch was minted in it.
type Window struct{ From, To time.Time }

// Contains reports whether t falls in (From, To].
func (w Window) Contains(t time.Time) bool { return t.After(w.From) && !t.After(w.To) }

// Total is one kind's reconciled count for a hop.
type Total struct {
	In           uint64             `json:"in"`
	Out          uint64             `json:"out"`
	Deduplicated uint64             `json:"deduplicated"`
	Outcomes     map[Outcome]uint64 `json:"outcomes"`
}

// Loss is the total's ratio, absent when nothing went in.
func (t Total) Loss() (float64, bool) { return Loss(t.In, t.Out) }

// Cause is the largest loss outcome behind the total, "" when nothing was
// lost. A tie goes to the outcome listed first in Outcomes, then SimOutcomes,
// then the schedule hop's Skipped (its Made is out, never a cause).
func (t Total) Cause() Outcome {
	var cause Outcome
	var most uint64
	for _, o := range slices.Concat(Outcomes, SimOutcomes, []Outcome{Skipped}) {
		if o.Lost() && t.Outcomes[o] > most {
			cause, most = o, t.Outcomes[o]
		}
	}
	return cause
}

func (t *Total) add(o Outcome, n uint64) {
	if n == 0 {
		return
	}
	if t.Outcomes == nil {
		t.Outcomes = map[Outcome]uint64{}
	}
	t.Outcomes[o] += n
	switch {
	case o.Deduplicated():
		t.Deduplicated += n
	case o.Out():
		t.In += n
		t.Out += n
	default:
		t.In += n
	}
}

// boots groups receipts by (node, boot), each boot's receipts sorted by seq
// with duplicates of a seq dropped, so arrival order and a receipt read twice
// change nothing. A receipt with no ledger (no boot or seq) is skipped; the
// push lane's receipts key on the server's boot with no node (#1696).
func boots(receipts []Receipt) [][]Receipt {
	type bootKey struct{ node, boot string }
	by := map[bootKey][]Receipt{}
	for _, r := range receipts {
		if r.Boot == "" || r.Seq == 0 {
			continue
		}
		k := bootKey{r.Node, r.Boot}
		by[k] = append(by[k], r)
	}
	keys := slices.SortedFunc(maps.Keys(by), func(a, b bootKey) int {
		return cmp.Or(cmp.Compare(a.node, b.node), cmp.Compare(a.boot, b.boot))
	})
	out := make([][]Receipt, 0, len(keys))
	for _, k := range keys {
		rs := by[k]
		slices.SortStableFunc(rs, func(a, b Receipt) int { return cmp.Compare(a.Seq, b.Seq) })
		rs = slices.CompactFunc(rs, func(a, b Receipt) bool { return a.Seq == b.Seq })
		out = append(out, rs)
	}
	return out
}

// Reconcile is the ingest hop over a window: per kind, in is the declared
// items of the window's receipts plus the items of the gaps the window owns
// that the node shipped, out is their stored, coalesced and unbound items,
// and missing is those gap items plus any shortfall the ingest counted
// between declared and decoded. A gap's items the node's runs account for
// (dropped at the spool, refused by the bus) are the node hop's loss
// (ReconcileNode), never the ingest's.
//
// Every gap has exactly one owner whatever order its batches arrived in: a
// gap between two present receipts belongs to the window of its lower
// neighbor, and a gap below a boot's lowest present receipt to that
// receipt's window. A gap across a window edge so counts once, in the earlier
// window, and two adjacent windows sum to one window over both.
//
// A kind appears only if some receipt in the window declares it or the
// window owns a gap of it.
func Reconcile(w Window, receipts []Receipt) map[Kind]Total {
	out := map[Kind]Total{}
	add := func(k Kind, o Outcome, n uint64) {
		t, ok := out[k]
		if !ok && n == 0 {
			return
		}
		t.add(o, n)
		out[k] = t
	}
	for _, rs := range boots(receipts) {
		runs := bootDrops(rs)
		for i, r := range rs {
			if !w.Contains(r.Minted) {
				continue
			}
			for _, k := range Kinds {
				declared := r.Declared[k].Count
				counted := r.Outcome.Items(k)
				if _, ok := r.Declared[k]; ok {
					if _, seen := out[k]; !seen {
						out[k] = Total{}
					}
				}
				for _, o := range IngestOutcomes {
					if !o.Deduplicated() {
						add(k, o, r.Outcome.Count(k, o))
					}
				}
				if declared > counted {
					// Declared but never counted: the ingest read less than
					// the node said it sent.
					add(k, Missing, declared-counted)
				}
				if r.Deliveries > 1 {
					add(k, Deduplicated, uint64(r.Deliveries-1)*max(declared, counted))
				}
				add(k, Missing, gapMissing(rs, runs, i, k))
			}
		}
	}
	return out
}

// gapAbove is the items of kind k in the seqs between rs[i] and the next
// present receipt: none when there is no next one (nothing yet shows how
// many there were) or no seq between them.
func gapAbove(rs []Receipt, i int, k Kind) uint64 {
	if i+1 >= len(rs) || rs[i+1].Seq == rs[i].Seq+1 {
		return 0
	}
	lo, hi := rs[i].Declared[k], rs[i+1].Declared[k]
	end := lo.Before + lo.Count
	if hi.Before <= end {
		return 0
	}
	return hi.Before - end
}

// Boot names one node boot.
type Boot struct{ Node, Boot string }

// Unsettled is every boot with a receipt in the window whose batches are not
// all known yet: none of its landed receipts was minted after the window, or
// the latest resolved of its receipts does not reach the last seq minted in
// the window. Until it settles, a gap at its tail could still fill from its
// spool, so a ratio read from it would be a guess.
func Unsettled(w Window, receipts []Receipt) []Boot {
	proven := Proven(w, receipts)
	var out []Boot
	for _, rs := range boots(receipts) {
		b := Boot{Node: rs[0].Node, Boot: rs[0].Boot}
		if _, ok := proven[b]; !ok && slices.ContainsFunc(rs, func(r Receipt) bool { return w.Contains(r.Minted) }) {
			out = append(out, b)
		}
	}
	return out
}

// Proven is each boot with a receipt in the window that Unsettled does not
// name, with when it was known to have published every batch it minted in
// the window: the mint of its earliest receipt whose resolved reaches the seq
// below its lowest landed seq minted after the window. A receipt's resolved
// is read as it is minted, so by then each of those batches had left the
// boot's spool, into the stream or dropped (#1758). Pure.
func Proven(w Window, receipts []Receipt) map[Boot]time.Time {
	out := map[Boot]time.Time{}
	for _, rs := range boots(receipts) {
		if !slices.ContainsFunc(rs, func(r Receipt) bool { return w.Contains(r.Minted) }) {
			continue
		}
		var after uint64 // the lowest seq landed minted after the window
		for _, r := range rs {
			if r.Minted.After(w.To) && (after == 0 || r.Seq < after) {
				after = r.Seq
			}
		}
		if after == 0 {
			continue
		}
		var at time.Time
		for _, r := range rs {
			if r.Resolved >= after-1 && (at.IsZero() || r.Minted.Before(at)) {
				at = r.Minted
			}
		}
		if !at.IsZero() {
			out[Boot{Node: rs[0].Node, Boot: rs[0].Boot}] = at
		}
	}
	return out
}
