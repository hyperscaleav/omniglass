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

// observed is a publisher whose observer records every turn it reports.
func observed(bound time.Duration) (*delivery.Publisher, func() []delivery.Turn) {
	var mu sync.Mutex
	var turns []delivery.Turn
	p := delivery.NewPublisher("push-boot", pubClock, bound).Observe(func(tn delivery.Turn) {
		mu.Lock()
		turns = append(turns, tn)
		mu.Unlock()
	})
	return p, func() []delivery.Turn {
		mu.Lock()
		defer mu.Unlock()
		return append([]delivery.Turn(nil), turns...)
	}
}

// holdTurn takes p's turn and keeps it until the returned release is
// called; release waits for the holder's publish to end.
func holdTurn(p *delivery.Publisher) (release func()) {
	hold, let, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = p.Publish(context.Background(), nil, func(delivery.Stamp) error {
			close(hold)
			<-let
			return nil
		})
	}()
	<-hold
	return func() { close(let); <-done }
}

// TestPublisherReportsEveryTurn (#1746): the publisher tells its observer
// how long each caller waited for its turn and whether the wait ran out, so
// the server can export the turn's wait and its refusals. A caller that
// takes its turn reports the wait it took, a caller refused reports a wait
// of at least the bound, and a caller that gave up on its own (its ctx
// ended) reports nothing: it was neither served nor refused.
func TestPublisherReportsEveryTurn(t *testing.T) {
	const bound = 40 * time.Millisecond
	p, got := observed(bound)
	publish(t, p, map[delivery.Kind]int{delivery.Metric: 1}, nil)
	if ts := got(); len(ts) != 1 || ts[0].Refused || ts[0].Wait < 0 || ts[0].Wait >= bound {
		t.Fatalf("an uncontended turn reported %+v, want one taken turn with a wait under the bound", ts)
	}

	release := holdTurn(p)
	if err := p.Publish(context.Background(), map[delivery.Kind]int{delivery.Metric: 1}, func(delivery.Stamp) error {
		return nil
	}); !errors.Is(err, delivery.ErrTurnTimeout) {
		t.Fatalf("a wait past the bound = %v, want ErrTurnTimeout", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Publish(ctx, nil, func(delivery.Stamp) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled wait = %v, want context.Canceled", err)
	}
	release()
	ts := got()
	if len(ts) != 3 || ts[1].Refused || !ts[2].Refused || ts[2].Wait < bound {
		t.Fatalf("turns %+v, want the holder's taken turn, then one refusal that waited the bound and nothing for the cancelled caller", ts)
	}
}

// TestPublisherReportsTheWaitForATakenTurn (#1746): a caller queued behind a
// held turn reports, once it takes its turn, at least how long it was held.
func TestPublisherReportsTheWaitForATakenTurn(t *testing.T) {
	p, got := observed(time.Minute)
	release := holdTurn(p)
	waited := make(chan error, 1)
	start := make(chan time.Time, 1)
	go func() {
		start <- time.Now()
		waited <- p.Publish(context.Background(), map[delivery.Kind]int{delivery.Metric: 1}, func(delivery.Stamp) error { return nil })
	}()
	began := <-start
	time.Sleep(20 * time.Millisecond)
	held := time.Since(began)
	release()
	if err := <-waited; err != nil {
		t.Fatalf("a wait under the bound = %v, want its turn", err)
	}
	ts := got()
	if len(ts) != 2 || ts[1].Refused || ts[1].Wait < held-5*time.Millisecond {
		t.Fatalf("turns %+v, want a taken turn that waited about the %v the turn was held", ts, held)
	}
}
