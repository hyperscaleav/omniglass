package simoutpost

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/outpost"
)

// blockingOutpost and blockingSim run until their context ends.
func blockingOutpost(ctx context.Context, _ outpost.Config) (collection.WorklistReply, error) {
	<-ctx.Done()
	return collection.WorklistReply{}, nil
}

func blockingSim(ctx context.Context, _ Config) error {
	<-ctx.Done()
	return nil
}

func TestRunWithOutpostStopsCleanlyWhenAsked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- runWithOutpost(ctx, outpost.Config{}, Config{}, blockingOutpost, blockingSim) }()
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("a stop asked for: %v, want a clean return", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pair did not stop within 5s of its context ending")
	}
}

// A simulator crash takes the outpost down with it, and says which half failed,
// so the container supervisor restarts the whole pair.
func TestRunWithOutpostStopsTheOutpostWhenTheSimulatorFails(t *testing.T) {
	boom := errors.New("catalog unreadable")
	outpostStopped := make(chan struct{})
	runOutpost := func(ctx context.Context, _ outpost.Config) (collection.WorklistReply, error) {
		<-ctx.Done()
		close(outpostStopped)
		return collection.WorklistReply{}, nil
	}
	runSim := func(context.Context, Config) error { return boom }
	err := runWithOutpost(context.Background(), outpost.Config{}, Config{}, runOutpost, runSim)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "simulator") {
		t.Fatalf("got %v, want the simulator's failure, named", err)
	}
	select {
	case <-outpostStopped:
	default:
		t.Fatal("the outpost was not stopped when its simulator failed")
	}
}

func TestRunWithOutpostStopsTheSimulatorWhenTheOutpostFails(t *testing.T) {
	boom := outpost.ErrClaimRejected
	simStopped := make(chan struct{})
	runOutpost := func(context.Context, outpost.Config) (collection.WorklistReply, error) {
		return collection.WorklistReply{}, boom
	}
	runSim := func(ctx context.Context, _ Config) error {
		<-ctx.Done()
		close(simStopped)
		return nil
	}
	err := runWithOutpost(context.Background(), outpost.Config{}, Config{}, runOutpost, runSim)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "outpost") {
		t.Fatalf("got %v, want the outpost's failure, named", err)
	}
	<-simStopped
}

// A half that returns nil with no stop asked for still breaks the pair: the
// process must exit non-zero so it is restarted, not linger half alive.
func TestRunWithOutpostReportsAHalfThatReturnsUnasked(t *testing.T) {
	runSim := func(context.Context, Config) error { return nil }
	err := runWithOutpost(context.Background(), outpost.Config{}, Config{}, blockingOutpost, runSim)
	if err == nil || !strings.Contains(err.Error(), "simulator stopped unexpectedly") {
		t.Fatalf("got %v, want the unexpected stop reported", err)
	}
}
