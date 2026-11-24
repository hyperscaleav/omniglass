package labfootprint

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
)

// A settle whose ack floor does not move reads the receipts once, and again
// once per floor advance, however many polls it makes before the bound (#1947).
func TestSettleDeliveryReadsReceiptsOncePerFloorAdvance(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	w := delivery.Window{From: at, To: at.Add(5 * time.Minute)}
	clock := at.Add(10 * time.Minute)
	// The head is never reached, so the stream never clears and the loop runs to its bound.
	floors := []float64{10, 10, 10, 20, 20, 30, 30, 30}
	poll := 0
	var receiptReads int
	r := settleReads{
		receipts: func(context.Context) ([]delivery.Receipt, error) { receiptReads++; return nil, nil },
		backlog: func(context.Context) Backlog {
			f := floors[min(poll, len(floors)-1)]
			return Backlog{
				{At: w.To.Add(time.Second), Head: 100, SeenHead: true},
				{At: w.To.Add(2 * time.Second), Head: 100, Floor: f, SeenHead: true, SeenFloor: true},
			}
		},
		now:   func() time.Time { return clock },
		sleep: func(context.Context, time.Duration) error { poll++; clock = clock.Add(time.Second); return nil },
	}
	d, err := settleDelivery(context.Background(), w, time.Duration(len(floors)-1)*time.Second, time.Second, r)
	if err != nil {
		t.Fatal(err)
	}
	if poll != len(floors)-1 {
		t.Fatalf("polled %d times, want the loop to run to its bound (%d)", poll, len(floors)-1)
	}
	if receiptReads != 3 {
		t.Fatalf("read receipts %d times, want 3 (the first poll, and floors 20 and 30)", receiptReads)
	}
	if d.StreamClear {
		t.Fatal("the stream cleared with the floor short of the head")
	}
}
