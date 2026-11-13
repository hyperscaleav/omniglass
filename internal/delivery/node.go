package delivery

import (
	"cmp"
	"maps"
	"slices"
	"time"
)

// The node hop (#1692): what a node minted against what its spool handed to
// the stream. A batch the spool gives up on leaves a gap in its boot's seqs,
// which alone reads as missing at ingest; the node names the gap's cause on
// the batches it mints after, so the read can tell the node's loss from the
// ingest's. Items lost before any batch carried them have no seq at all, so
// the node keeps a running count of them by cause, carried by every batch.

// Drop is a run of consecutive seqs of one boot the node's spool dropped for
// one cause, From and To both included, and the items of each kind they
// carried.
type Drop struct {
	Cause Outcome         `json:"cause"`
	From  uint64          `json:"from"`
	To    uint64          `json:"to"`
	Items map[Kind]uint64 `json:"items"`
}

// NodeLoss is the node's own account a batch carries: the runs its spool
// dropped that no acked batch has carried yet, and the items lost before any
// batch carried them, running totals since the boot began by cause and kind.
type NodeLoss struct {
	Dropped []Drop                      `json:"dropped,omitempty"`
	Lost    map[Outcome]map[Kind]uint64 `json:"lost,omitempty"`
}

// Empty reports whether the node has lost nothing it accounts for.
func (n NodeLoss) Empty() bool { return len(n.Dropped) == 0 && len(n.Lost) == 0 }

// maxPendingDrops bounds the runs a ledger carries. Drops of the oldest
// queued batch come in seq order and extend one run, so it takes a drop
// pattern no spool makes to reach it; past it the oldest run is forgotten,
// and its items read as missing at ingest rather than as the node's loss.
const maxPendingDrops = 256

// pendingDrop is a run not yet known to be in the stream: noted is the last
// seq minted when the run last grew, so any batch above it carries the run.
type pendingDrop struct {
	Drop
	noted uint64
}

// Drop notes that the spool dropped the batch at seq, carrying items, for
// cause. It extends the latest run when it is the same cause and the next
// seq; otherwise it starts a run. Every batch minted from now on carries the
// run until one of them is acked (Stored).
func (l *Ledger) Drop(cause Outcome, seq uint64, items map[Kind]int) {
	n := len(l.drops)
	if n == 0 || l.drops[n-1].Cause != cause || l.drops[n-1].To+1 != seq {
		if n >= maxPendingDrops {
			l.drops = slices.Delete(l.drops, 0, 1)
		}
		l.drops = append(l.drops, pendingDrop{Drop: Drop{Cause: cause, From: seq, To: seq, Items: map[Kind]uint64{}}})
		n = len(l.drops)
	} else {
		l.drops[n-1].To = seq
	}
	d := &l.drops[n-1]
	d.noted = l.seq
	for k, c := range items {
		if c > 0 {
			d.Items[k] += uint64(c)
		}
	}
}

// Lose notes items lost before any batch carried them, for cause: every
// batch minted from now on carries the running total.
func (l *Ledger) Lose(cause Outcome, items map[Kind]int) {
	for k, c := range items {
		if c <= 0 {
			continue
		}
		if l.lost == nil {
			l.lost = map[Outcome]map[Kind]uint64{}
		}
		if l.lost[cause] == nil {
			l.lost[cause] = map[Kind]uint64{}
		}
		l.lost[cause][k] += uint64(c)
	}
}

// Stored notes that the stream acked the batch at seq: every run noted
// before it was minted rides it, so no later batch need carry them.
func (l *Ledger) Stored(seq uint64) {
	l.drops = slices.DeleteFunc(l.drops, func(d pendingDrop) bool { return d.noted < seq })
}

// loss is the ledger's node account as of now, a copy the stamp can keep.
func (l *Ledger) loss() NodeLoss {
	var n NodeLoss
	for _, d := range l.drops {
		n.Dropped = append(n.Dropped, Drop{Cause: d.Cause, From: d.From, To: d.To, Items: maps.Clone(d.Items)})
	}
	if len(l.lost) > 0 {
		n.Lost = map[Outcome]map[Kind]uint64{}
		for c, ks := range l.lost {
			n.Lost[c] = maps.Clone(ks)
		}
	}
	return n
}

// dropsIn is the items of each kind the boot's runs account for strictly
// between seqs lo and hi, both landed: a run is wholly inside such a gap or
// wholly outside it, since every seq in a run was dropped and neither end
// was. Pure.
func dropsIn(runs []Drop, lo, hi uint64) map[Outcome]map[Kind]uint64 {
	var out map[Outcome]map[Kind]uint64
	for _, d := range runs {
		if d.From <= lo || d.To >= hi {
			continue
		}
		for k, n := range d.Items {
			if n == 0 {
				continue
			}
			if out == nil {
				out = map[Outcome]map[Kind]uint64{}
			}
			if out[d.Cause] == nil {
				out[d.Cause] = map[Kind]uint64{}
			}
			out[d.Cause][k] += n
		}
	}
	return out
}

// bootDrops is every run a boot's receipts carry, each once: a run grows
// while batches carry it, so of the copies of a run (keyed by its first seq)
// the longest is kept. In seq order.
func bootDrops(rs []Receipt) []Drop {
	by := map[uint64]Drop{}
	for _, r := range rs {
		for _, d := range r.Loss.Dropped {
			if have, ok := by[d.From]; !ok || d.To > have.To {
				by[d.From] = d
			}
		}
	}
	out := slices.Collect(maps.Values(by))
	slices.SortFunc(out, func(a, b Drop) int { return cmp.Compare(a.From, b.From) })
	return out
}

// gapDrops is, per cause and kind, the node-dropped items of the gap rs[i]
// owns: the seqs between it and the next receipt, and for the boot's lowest
// receipt the seqs below it too. Capped per kind at the gap's items, so the
// node never accounts for more than the offsets say went missing.
func gapDrops(rs []Receipt, runs []Drop, i int, k Kind) (map[Outcome]uint64, uint64) {
	var gap uint64
	got := map[Outcome]uint64{}
	add := func(lo, hi, items uint64) {
		gap += items
		left := items
		in := dropsIn(runs, lo, hi)
		for _, c := range NodeOutcomes {
			n := min(in[c][k], left)
			if n > 0 {
				got[c] += n
				left -= n
			}
		}
	}
	if i+1 < len(rs) {
		add(rs[i].Seq, rs[i+1].Seq, gapAbove(rs, i, k))
	}
	if i == 0 {
		add(0, rs[0].Seq, rs[0].Declared[k].Before)
	}
	return got, gap
}

// gapMissing is the items of kind k of the gap rs[i] owns that the node
// does not account for: what it shipped and the ingest never landed.
func gapMissing(rs []Receipt, runs []Drop, i int, k Kind) uint64 {
	got, gap := gapDrops(rs, runs, i, k)
	for _, n := range got {
		gap -= n
	}
	return gap
}

// ReconcileNode is the node hop over a window: per kind, in is what the
// node minted into the window's batches and the gaps the window owns (as
// Reconcile owns them), plus what it lost before minting in the window; out
// is what it shipped. A gap's items the node accounts for are its loss under
// their cause, the rest it shipped and the ingest hop reads as missing. So
// the node's out is exactly the ingest's in, and the two compose end to end.
//
// What a boot lost before minting reads from the running totals: the most
// any in-window receipt carries, less the most any receipt below the window
// carries.
func ReconcileNode(w Window, receipts []Receipt) map[Kind]Total {
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
		lostIn, lostBelow := map[Outcome]map[Kind]uint64{}, map[Outcome]map[Kind]uint64{}
		inWindow := false
		for i, r := range rs {
			if !w.Contains(r.Minted) {
				if !r.Minted.After(w.To) {
					maxInto(lostBelow, r.Loss.Lost)
				}
				continue
			}
			inWindow = true
			maxInto(lostIn, r.Loss.Lost)
			for _, k := range Kinds {
				if _, ok := r.Declared[k]; ok {
					if _, seen := out[k]; !seen {
						out[k] = Total{}
					}
				}
				add(k, Shipped, max(r.Declared[k].Count, r.Outcome.Items(k)))
				got, gap := gapDrops(rs, runs, i, k)
				var dropped uint64
				for _, c := range NodeOutcomes {
					add(k, c, got[c])
					dropped += got[c]
				}
				add(k, Shipped, gap-dropped)
			}
		}
		if !inWindow {
			continue
		}
		for _, c := range NodeOutcomes {
			for _, k := range Kinds {
				if n, below := lostIn[c][k], lostBelow[c][k]; n > below {
					add(k, c, n-below)
				}
			}
		}
	}
	return out
}

// maxInto raises each of into's counts to src's where src's is higher.
func maxInto(into, src map[Outcome]map[Kind]uint64) {
	for c, ks := range src {
		for k, n := range ks {
			if into[c] == nil {
				into[c] = map[Kind]uint64{}
			}
			into[c][k] = max(into[c][k], n)
		}
	}
}

// Ended is a boot that will never settle because its node started again: its
// spool, and every batch it still held, went with the process. Last is the
// highest seq of it that landed and LastMinted when; Next is the boot that
// followed and NextMinted its first landed batch. Whatever the old boot
// minted after Last never shipped, and nothing landed says how much: counting
// it needs the next boot to report the old one's watermark (#1733).
type Ended struct {
	Boot
	Last       uint64
	LastMinted time.Time
	Next       string
	NextMinted time.Time
}

// EndedOf is each of the boots named that its node followed with another
// boot: a receipt of a different boot of the same node minted after the
// boot's last landed batch. The earliest such boot is Next. A boot not
// followed is not ended, and stays unsettled. Pure.
func EndedOf(named []Boot, receipts []Receipt) map[Boot]Ended {
	all := boots(receipts)
	byBoot := map[Boot][]Receipt{}
	for _, rs := range all {
		byBoot[Boot{Node: rs[0].Node, Boot: rs[0].Boot}] = rs
	}
	out := map[Boot]Ended{}
	for _, b := range named {
		rs := byBoot[b]
		if len(rs) == 0 {
			continue
		}
		last := rs[len(rs)-1]
		e := Ended{Boot: b, Last: last.Seq, LastMinted: last.Minted}
		for _, other := range all {
			if other[0].Node != b.Node || other[0].Boot == b.Boot {
				continue
			}
			first := other[0]
			if first.Minted.After(last.Minted) && (e.Next == "" || first.Minted.Before(e.NextMinted)) {
				e.Next, e.NextMinted = first.Boot, first.Minted
			}
		}
		if e.Next != "" {
			out[b] = e
		}
	}
	return out
}
