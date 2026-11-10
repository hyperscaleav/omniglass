package delivery

import (
	"sync"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Counts is one hop's delivery over a window (#1659): the items per kind that
// arrived at it (in) and the items it handed on (out). An item counts once it
// settles: handed on, or lost on the way. The zero value is empty and ready.
type Counts struct {
	in, out map[Kind]uint64
}

func (c *Counts) add(k Kind, in, out int) {
	if in <= 0 && out <= 0 {
		return
	}
	if c.in == nil {
		c.in, c.out = map[Kind]uint64{}, map[Kind]uint64{}
	}
	if in > 0 {
		c.in[k] += uint64(in)
	}
	if out > 0 {
		c.out[k] += uint64(out)
	}
}

// Delivered counts items that arrived and were handed on. A redelivery the
// hop has already handled counts here too, once in and once out, so it reads
// as no loss.
func (c *Counts) Delivered(items map[Kind]int) {
	for k, n := range items {
		c.add(k, n, n)
	}
}

// Lost counts items that arrived and were not handed on: dropped, refused or
// never encoded.
func (c *Counts) Lost(items map[Kind]int) {
	for k, n := range items {
		c.add(k, n, 0)
	}
}

// Landed counts the store's outcome of one batch. Every item counted went
// in; stored, coalesced, unbound and deduplicated items came out (a
// deduplicated item is one an earlier copy landed, so a replay reads as no
// loss; an unbound one is in its conduit's inbox, awaiting the operator's
// binding); the rest, refused, orphaned or unidentified, were lost here.
func (c *Counts) Landed(t Tally) {
	for k, os := range t.m {
		for o, n := range os {
			out := 0
			if o.Out() || o.Deduplicated() {
				out = int(n)
			}
			c.add(k, int(n), out)
		}
	}
}

// Merge adds every count of o to c.
func (c *Counts) Merge(o Counts) {
	for k, n := range o.in {
		c.add(k, int(n), 0)
	}
	for k, n := range o.out {
		c.add(k, 0, int(n))
	}
}

// Reading is one delivery figure: a key, its kind as the instance, its value.
type Reading struct {
	Key      string
	Instance string
	Value    float64
}

// Readings is the window's figures, in, out and the loss ratio for every
// kind, a kind with no traffic included as zero and its ratio as 0, so a
// quiet window writes too and a past window's loss never stands as the
// latest reading.
func (c Counts) Readings() []Reading {
	out := make([]Reading, 0, 3*len(Kinds))
	for _, k := range Kinds {
		in, handed := c.in[k], c.out[k]
		loss, ok := Loss(in, handed)
		if !ok {
			loss = 0
		}
		out = append(out,
			Reading{Key: writerclass.DeliveryInCount, Instance: string(k), Value: float64(in)},
			Reading{Key: writerclass.DeliveryOutCount, Instance: string(k), Value: float64(handed)},
			Reading{Key: writerclass.DeliveryLossRatio, Instance: string(k), Value: loss},
		)
	}
	return out
}

// Flow is Counts many goroutines add to, taken a window at a time. The zero
// value is ready.
type Flow struct {
	mu sync.Mutex
	c  Counts
}

// Delivered counts items handed on, as Counts.Delivered.
func (f *Flow) Delivered(items map[Kind]int) {
	f.mu.Lock()
	f.c.Delivered(items)
	f.mu.Unlock()
}

// Lost counts items lost here, as Counts.Lost.
func (f *Flow) Lost(items map[Kind]int) {
	f.mu.Lock()
	f.c.Lost(items)
	f.mu.Unlock()
}

// Landed counts a batch's store outcome, as Counts.Landed.
func (f *Flow) Landed(t Tally) {
	f.mu.Lock()
	f.c.Landed(t)
	f.mu.Unlock()
}

// Take is the counts since the last take, and starts the next window.
func (f *Flow) Take() Counts {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.c
	f.c = Counts{}
	return c
}
