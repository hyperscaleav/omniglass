package delivery

import (
	"context"
	"errors"
	"time"
)

// ErrTurnTimeout is a publish refused because the one ahead of it held the
// turn past the publisher's wait: the caller is answered at once, minting
// nothing, rather than queued behind every attempt of a stalled publish.
var ErrTurnTimeout = errors.New("delivery: publish turn not reached in time")

// Publisher is the push lane's ledger (#1696): the server is the lane's one
// publisher, so its process is the boot, and every batch it publishes takes
// the next stamp. Publishes go one at a time, each minted and sent before the
// next is minted, so seq and the offsets are dense in the order the stream
// stored them, and resolved is the seq below: the batch ahead had the
// stream's answer before this one was minted. Safe for concurrent use.
type Publisher struct {
	turn    chan struct{}
	ledger  *Ledger
	now     func() time.Time
	wait    time.Duration
	observe func(Turn)
}

// Turn is one caller's wait for the publish turn (#1746): how long it waited,
// and whether the wait ran out (ErrTurnTimeout) rather than ending in the
// turn. The server exports both, the wait as the turn's contention and the
// refusals as the pushes the lane answered 503 before minting anything.
type Turn struct {
	Wait    time.Duration
	Refused bool
}

// NewPublisher starts a publisher's boot; now is its clock, and wait is the
// longest a publish waits for its turn before ErrTurnTimeout.
func NewPublisher(boot string, now func() time.Time, wait time.Duration) *Publisher {
	return &Publisher{turn: make(chan struct{}, 1), ledger: NewLedger(boot), now: now, wait: wait}
}

// Observe sets f to hear every caller's Turn: one per caller that took its
// turn or was refused, none for a caller whose ctx ended first. f runs on the
// caller's goroutine, so it must not block. It returns p, to chain on
// NewPublisher.
func (p *Publisher) Observe(f func(Turn)) *Publisher {
	p.observe = f
	return p
}

// report hands t to the observer, if there is one.
func (p *Publisher) report(t Turn) {
	if p.observe != nil {
		p.observe(t)
	}
}

// Boot is the boot this publisher stamps.
func (p *Publisher) Boot() string { return p.ledger.Boot() }

// Publish waits its turn, stamps a batch of counts items by kind, and hands
// the stamp to send, which publishes the batch and reports whether the stream
// took it. A send that fails withdraws the stamp's items (Ledger.Withdraw):
// its publisher still holds them, so they are no loss of the lane's. A
// caller whose ctx ends, or whose wait passes the publisher's bound
// (ErrTurnTimeout), while it waits mints nothing.
func (p *Publisher) Publish(ctx context.Context, counts map[Kind]int, send func(Stamp) error) error {
	start := time.Now()
	bound := time.NewTimer(p.wait)
	defer bound.Stop()
	select {
	case p.turn <- struct{}{}:
		p.report(Turn{Wait: time.Since(start)})
	case <-ctx.Done():
		return ctx.Err()
	case <-bound.C:
		p.report(Turn{Wait: time.Since(start), Refused: true})
		return ErrTurnTimeout
	}
	defer func() { <-p.turn }()
	s := p.ledger.Mint(counts, p.now().UTC(), p.ledger.Last())
	if err := send(s); err != nil {
		p.ledger.Withdraw(s)
		return err
	}
	return nil
}
