package simnode

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
)

// nodeRunner and simRunner are the two halves RunWithNode joins: node.Run and
// Run, injectable so the join's stop semantics are testable without a server.
type (
	nodeRunner func(context.Context, node.Config) (collection.WorklistReply, error)
	simRunner  func(context.Context, Config) error
)

// RunWithNode runs a node and its simulator in one process, under one context
// (`omniglass node run --simulate`). Both halves authenticate as the same node
// with the same enrollment token, so there is no second enrollment and no
// second grant: the simulator stays inside the node's subject grant
// (ADR-0142). Sharing the process means sharing its network namespace, so the
// simulated components sit on the loopback the node collects from by
// construction, with no namespace arrangement around it.
//
// The two live and die together: when either half returns, the other is
// stopped, and the first real failure is the one reported. A stop by the
// caller (ctx ending) is a clean return.
func RunWithNode(ctx context.Context, nodeCfg node.Config, simCfg Config) error {
	return runWithNode(ctx, nodeCfg, simCfg, node.Run, Run)
}

func runWithNode(ctx context.Context, nodeCfg node.Config, simCfg Config, runNode nodeRunner, runSim simRunner) error {
	inner, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		half string
		err  error
	}
	done := make(chan result, 2)
	go func() {
		_, err := runNode(inner, nodeCfg)
		done <- result{"node", err}
	}()
	go func() {
		done <- result{"simulator", runSim(inner, simCfg)}
	}()
	first := <-done
	cancel()
	second := <-done
	if ctx.Err() != nil {
		return nil
	}
	for _, r := range []result{first, second} {
		if r.err != nil && !errors.Is(r.err, context.Canceled) {
			return fmt.Errorf("node run --simulate: the %s stopped: %w", r.half, r.err)
		}
	}
	// One half returned cleanly with no stop asked for: the pair is no longer
	// whole, which the supervisor must see as a failure to restart it.
	return fmt.Errorf("node run --simulate: the %s stopped unexpectedly", first.half)
}
