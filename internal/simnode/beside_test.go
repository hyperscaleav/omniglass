package simnode

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
)

// blockingNode and blockingSim run until their context ends.
func blockingNode(ctx context.Context, _ node.Config) (collection.WorklistReply, error) {
	<-ctx.Done()
	return collection.WorklistReply{}, nil
}

func blockingSim(ctx context.Context, _ Config) error {
	<-ctx.Done()
	return nil
}

func TestRunWithNodeStopsCleanlyWhenAsked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- runWithNode(ctx, node.Config{}, Config{}, blockingNode, blockingSim) }()
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

// A simulator crash takes the node down with it, and says which half failed,
// so the container supervisor restarts the whole pair.
func TestRunWithNodeStopsTheNodeWhenTheSimulatorFails(t *testing.T) {
	boom := errors.New("catalog unreadable")
	nodeStopped := make(chan struct{})
	runNode := func(ctx context.Context, _ node.Config) (collection.WorklistReply, error) {
		<-ctx.Done()
		close(nodeStopped)
		return collection.WorklistReply{}, nil
	}
	runSim := func(context.Context, Config) error { return boom }
	err := runWithNode(context.Background(), node.Config{}, Config{}, runNode, runSim)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "simulator") {
		t.Fatalf("got %v, want the simulator's failure, named", err)
	}
	select {
	case <-nodeStopped:
	default:
		t.Fatal("the node was not stopped when its simulator failed")
	}
}

func TestRunWithNodeStopsTheSimulatorWhenTheNodeFails(t *testing.T) {
	boom := node.ErrClaimRejected
	simStopped := make(chan struct{})
	runNode := func(context.Context, node.Config) (collection.WorklistReply, error) {
		return collection.WorklistReply{}, boom
	}
	runSim := func(ctx context.Context, _ Config) error {
		<-ctx.Done()
		close(simStopped)
		return nil
	}
	err := runWithNode(context.Background(), node.Config{}, Config{}, runNode, runSim)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "node") {
		t.Fatalf("got %v, want the node's failure, named", err)
	}
	<-simStopped
}

// A half that returns nil with no stop asked for still breaks the pair: the
// process must exit non-zero so it is restarted, not linger half alive.
func TestRunWithNodeReportsAHalfThatReturnsUnasked(t *testing.T) {
	runSim := func(context.Context, Config) error { return nil }
	err := runWithNode(context.Background(), node.Config{}, Config{}, blockingNode, runSim)
	if err == nil || !strings.Contains(err.Error(), "simulator stopped unexpectedly") {
		t.Fatalf("got %v, want the unexpected stop reported", err)
	}
}
