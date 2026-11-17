package simnode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

// A simulator started before its server is reachable keeps trying rather
// than exiting, and stops trying when cancelled.
func TestRetryUntilKeepsTryingAndHonorsCancel(t *testing.T) {
	calls := 0
	err := retryUntil(context.Background(), time.Millisecond, 4*time.Millisecond, func() error {
		calls++
		if calls < 4 {
			return errors.New("server not up")
		}
		return nil
	})
	if err != nil || calls != 4 {
		t.Fatalf("err %v after %d calls, want success on the fourth", err, calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := retryUntil(ctx, time.Millisecond, time.Millisecond, func() error { return errors.New("down") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled retry returned %v", err)
	}
}

// The hit buffer holds at most maxBufferedHits between flushes and counts
// what it dropped, so a publish outage cannot grow it without bound.
func TestHitBufferIsBounded(t *testing.T) {
	var b hitBuffer
	for i := 0; i < maxBufferedHits+7; i++ {
		b.add(sim.Hit{DeviceID: "c1", Protocol: "tcp"})
	}
	hits, dropped := b.drain()
	if len(hits) != maxBufferedHits || dropped != 7 {
		t.Fatalf("drained %d with %d dropped, want %d and 7", len(hits), dropped, maxBufferedHits)
	}
	if hits, dropped := b.drain(); len(hits) != 0 || dropped != 0 {
		t.Fatal("drain did not reset the buffer")
	}
}

// A wrong token is final: Run reports the refusal instead of retrying.
func TestRunStopsOnARefusedClaim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := Run(ctx, Config{ServerURL: srv.URL, Name: "edge", Token: "wrong"})
	if !errors.Is(err, node.ErrClaimRejected) {
		t.Fatalf("Run with a refused claim returned %v", err)
	}
}
