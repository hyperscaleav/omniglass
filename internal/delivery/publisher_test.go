package delivery_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
)

var pubAt = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func pubClock() time.Time { return pubAt }

// publish runs one batch of counts through p, sending it with err, and
// returns the stamp send saw.
func publish(t *testing.T, p *delivery.Publisher, counts map[delivery.Kind]int, err error) delivery.Stamp {
	t.Helper()
	var sent delivery.Stamp
	got := p.Publish(context.Background(), counts, func(s delivery.Stamp) error {
		sent = s
		return err
	})
	if !errors.Is(got, err) {
		t.Fatalf("Publish = %v, want %v", got, err)
	}
	return sent
}

// TestPublisherStampsADenseSeqAndCumulativeOffsets is the push lane's ledger
// (#1696): one boot per publisher, seq dense from 1, every kind's before the
// items of that kind the boot published ahead of it, and resolved the seq
// below, since the batch ahead had its answer from the stream before this one
// was minted.
func TestPublisherStampsADenseSeqAndCumulativeOffsets(t *testing.T) {
	p := delivery.NewPublisher("push-boot", pubClock, time.Minute)
	if p.Boot() != "push-boot" {
		t.Fatalf("Boot = %q", p.Boot())
	}
	a := publish(t, p, map[delivery.Kind]int{delivery.Metric: 3, delivery.Property: 1}, nil)
	b := publish(t, p, map[delivery.Kind]int{delivery.Metric: 2}, nil)
	c := publish(t, p, map[delivery.Kind]int{delivery.Property: 4, delivery.Log: 1}, nil)

	for i, s := range []delivery.Stamp{a, b, c} {
		if s.Boot != "push-boot" || s.Seq != uint64(i+1) || s.Resolved != uint64(i) || !s.Minted.Equal(pubAt) {
			t.Fatalf("stamp %d = %+v, want boot push-boot, seq %d, resolved %d, minted %v", i, s, i+1, i, pubAt)
		}
	}
	want := []delivery.Declared{
		{delivery.Metric: {Count: 3}, delivery.Property: {Count: 1}},
		{delivery.Metric: {Count: 2, Before: 3}, delivery.Property: {Before: 1}},
		{delivery.Metric: {Before: 5}, delivery.Property: {Count: 4, Before: 1}, delivery.Log: {Count: 1}},
	}
	for i, s := range []delivery.Stamp{a, b, c} {
		if len(s.Kinds) != len(want[i]) {
			t.Fatalf("stamp %d kinds = %v, want %v", i, s.Kinds, want[i])
		}
		for k, w := range want[i] {
			if s.Kinds[k] != w {
				t.Fatalf("stamp %d %s = %+v, want %+v", i, k, s.Kinds[k], w)
			}
		}
	}
}

// TestPublisherWithdrawsARefusedBatchsItems: a publish the stream never acked
// is a 503 to the pusher, which still holds its items, so they are not the
// lane's loss. Its seq stays spent (a copy the stream stored with its ack
// lost lands under that seq, never a second batch's), and its items come
// off the offsets, so the hole it leaves holds nothing to read as missing.
func TestPublisherWithdrawsARefusedBatchsItems(t *testing.T) {
	p := delivery.NewPublisher("push-boot", pubClock, time.Minute)
	refused := errors.New("stream gone")
	a := publish(t, p, map[delivery.Kind]int{delivery.Metric: 3}, nil)
	publish(t, p, map[delivery.Kind]int{delivery.Metric: 5}, refused)
	c := publish(t, p, map[delivery.Kind]int{delivery.Metric: 2}, nil)

	if c.Seq != 3 || c.Resolved != 2 {
		t.Fatalf("after a refusal: seq %d resolved %d, want 3 and 2", c.Seq, c.Resolved)
	}
	if got := c.Kinds[delivery.Metric]; got != (delivery.Count{Count: 2, Before: 3}) {
		t.Fatalf("after a refusal: metric %+v, want the refused batch's 5 withdrawn (before 3)", got)
	}
	// Reconciled with the refused batch absent, the lane lost nothing.
	w := delivery.Window{From: pubAt.Add(-time.Minute), To: pubAt}
	stored := func(s delivery.Stamp, n int) delivery.Receipt {
		var o delivery.Tally
		o.Add(delivery.Metric, delivery.Stored, n)
		return delivery.Receipt{Boot: s.Boot, Seq: s.Seq, Minted: s.Minted, Resolved: s.Resolved, Deliveries: 1, Declared: s.Kinds, Outcome: o}
	}
	tot := delivery.Reconcile(w, []delivery.Receipt{stored(a, 3), stored(c, 2)})[delivery.Metric]
	if tot.In != 5 || tot.Out != 5 {
		t.Fatalf("reconciled around a refused batch: %+v, want in 5 out 5", tot)
	}
}

// TestPublisherWaitsItsTurn: concurrent publishes go one at a time, so seq
// and the offsets stay dense in the order the stream stored them; a caller
// that gives up while waiting mints nothing.
func TestPublisherWaitsItsTurn(t *testing.T) {
	p := delivery.NewPublisher("push-boot", pubClock, time.Minute)
	const n = 50
	var mu sync.Mutex
	var order []delivery.Stamp
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			err := p.Publish(context.Background(), map[delivery.Kind]int{delivery.Metric: 1}, func(s delivery.Stamp) error {
				mu.Lock()
				order = append(order, s)
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("Publish: %v", err)
			}
		})
	}
	wg.Wait()
	for i, s := range order {
		if s.Seq != uint64(i+1) || s.Kinds[delivery.Metric].Before != uint64(i) {
			t.Fatalf("send %d carried seq %d before %d: the publishes interleaved", i, s.Seq, s.Kinds[delivery.Metric].Before)
		}
	}

	hold, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = p.Publish(context.Background(), nil, func(delivery.Stamp) error {
			close(hold)
			<-release
			return nil
		})
	}()
	<-hold
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.Publish(ctx, map[delivery.Kind]int{delivery.Metric: 1}, func(delivery.Stamp) error {
		t.Fatal("a cancelled caller was sent")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled wait = %v, want context.Canceled", err)
	}
	close(release)
	next := publish(t, p, map[delivery.Kind]int{delivery.Metric: 1}, nil)
	if next.Seq != n+2 {
		t.Fatalf("after a cancelled wait: seq %d, want %d (the cancelled caller minted none)", next.Seq, n+2)
	}
}

// TestPublisherBoundsItsTurn: a turn held past the publisher's wait (a stream
// that stalls each attempt to its ack timeout) refuses the caller behind it
// with ErrTurnTimeout instead of queueing it for the head's whole retry, so
// a pusher behind a stalled publish hears 503 in a bounded time, as it did
// when publishes ran side by side. The refused caller mints nothing.
func TestPublisherBoundsItsTurn(t *testing.T) {
	p := delivery.NewPublisher("push-boot", pubClock, 20*time.Millisecond)
	hold, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = p.Publish(context.Background(), nil, func(delivery.Stamp) error {
			close(hold)
			<-release
			return nil
		})
	}()
	<-hold
	done := make(chan error, 1)
	go func() {
		done <- p.Publish(context.Background(), map[delivery.Kind]int{delivery.Metric: 1}, func(delivery.Stamp) error {
			t.Error("a caller refused its turn was sent")
			return nil
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, delivery.ErrTurnTimeout) {
			t.Fatalf("a wait past the bound = %v, want ErrTurnTimeout", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("a caller behind a held turn waited past the publisher's bound")
	}
	close(release)
	next := publish(t, p, map[delivery.Kind]int{delivery.Metric: 1}, nil)
	if next.Seq != 2 || next.Kinds[delivery.Metric].Before != 0 {
		t.Fatalf("after a refused turn: seq %d before %d, want 2 and 0 (the refused caller minted none)", next.Seq, next.Kinds[delivery.Metric].Before)
	}
}
