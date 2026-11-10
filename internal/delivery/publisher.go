package delivery

import (
	"context"
	"time"
)

// Publisher is the push lane's ledger (#1696): the server is the lane's one
// publisher, so its process is the boot, and every batch it publishes takes
// the next stamp. Publishes go one at a time, each minted and sent before the
// next is minted, so seq and the offsets are dense in the order the stream
// stored them, and resolved is the seq below: the batch ahead had the
// stream's answer before this one was minted. Safe for concurrent use.
type Publisher struct {
	turn   chan struct{}
	ledger *Ledger
	now    func() time.Time
}

// NewPublisher starts a publisher's boot; now is its clock.
func NewPublisher(boot string, now func() time.Time) *Publisher {
	return &Publisher{turn: make(chan struct{}, 1), ledger: NewLedger(boot), now: now}
}

// Boot is the boot this publisher stamps.
func (p *Publisher) Boot() string { return p.ledger.Boot() }

// Publish waits its turn, stamps a batch of counts items by kind, and hands
// the stamp to send, which publishes the batch and reports whether the stream
// took it. A send that fails withdraws the stamp's items (Ledger.Withdraw):
// its publisher still holds them, so they are no loss of the lane's. A
// caller whose ctx ends while it waits mints nothing.
func (p *Publisher) Publish(ctx context.Context, counts map[Kind]int, send func(Stamp) error) error {
	select {
	case p.turn <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.turn }()
	s := p.ledger.Mint(counts, p.now().UTC(), p.ledger.Last())
	if err := send(s); err != nil {
		p.ledger.Withdraw(s)
		return err
	}
	return nil
}
