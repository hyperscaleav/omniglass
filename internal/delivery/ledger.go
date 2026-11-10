package delivery

import "time"

// Count is one kind's place on a batch's ledger: the items of that kind the
// batch carries, and the items of that kind its boot minted before it.
type Count struct {
	Count  uint64 `json:"count"`
	Before uint64 `json:"before"`
}

// Declared is a batch's ledger counts by kind, the shape the receipt's
// ledger column stores: {"metric": {"count": n, "before": m}, ...}. A kind
// absent from it is one its boot had minted none of.
type Declared map[Kind]Count

// Stamp is the ledger one batch carries.
type Stamp struct {
	Boot     string
	Seq      uint64
	Minted   time.Time
	Resolved uint64
	Kinds    Declared
	// Loss is the node's account of what it dropped or lost, as of the mint
	// (#1692).
	Loss NodeLoss
}

// Ledger is a node boot's running count, held by the node's spool: the next
// sequence and every kind's running offset. Not safe for concurrent use; the
// spool calls it under its own lock, which is what makes seq and the offsets
// dense in queue order.
type Ledger struct {
	boot   string
	seq    uint64
	minted map[Kind]uint64
	drops  []pendingDrop               // runs dropped that no acked batch carried yet
	lost   map[Outcome]map[Kind]uint64 // lost before minting, since the boot began
}

// NewLedger starts a boot's count.
func NewLedger(boot string) *Ledger {
	return &Ledger{boot: boot, minted: map[Kind]uint64{}}
}

// Boot is the boot this ledger counts.
func (l *Ledger) Boot() string { return l.boot }

// Last is the seq last minted, 0 before the first.
func (l *Ledger) Last() uint64 { return l.seq }

// Mint stamps the next batch: it carries counts items by kind, was minted at
// at, and every batch of this boot at or below resolved had left the spool.
// A kind the boot has minted any of rides the stamp, at count 0 when this
// batch carries none.
func (l *Ledger) Mint(counts map[Kind]int, at time.Time, resolved uint64) Stamp {
	l.seq++
	s := Stamp{Boot: l.boot, Seq: l.seq, Minted: at, Resolved: resolved, Kinds: Declared{}, Loss: l.loss()}
	for _, k := range Kinds {
		n := uint64(max(counts[k], 0))
		before := l.minted[k]
		if n == 0 && before == 0 {
			continue
		}
		s.Kinds[k] = Count{Count: n, Before: before}
		l.minted[k] = before + n
	}
	return s
}

// Against compares what a batch's ledger declared with what the batch
// decoded to, kind by kind: short is each kind it decoded fewer items of
// (those items are missing), over each kind it decoded more of (a node
// that miscounted, warned but never counted as loss). Pure.
func (d Declared) Against(decoded map[Kind]int) (short, over map[Kind]int) {
	for _, k := range Kinds {
		declared, got := int(d[k].Count), decoded[k]
		switch {
		case got < declared:
			if short == nil {
				short = map[Kind]int{}
			}
			short[k] = declared - got
		case got > declared:
			if over == nil {
				over = map[Kind]int{}
			}
			over[k] = got - declared
		}
	}
	return short, over
}
