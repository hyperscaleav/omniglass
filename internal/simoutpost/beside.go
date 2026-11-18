package simoutpost

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/outpost"
)

// outpostRunner and simRunner are the two halves RunWithOutpost joins: outpost.Run and
// Run, injectable so the join's stop semantics are testable without a server.
type (
	outpostRunner func(context.Context, outpost.Config) (collection.WorklistReply, error)
	simRunner     func(context.Context, Config) error
)

// RunWithOutpost runs an outpost and its simulator in one process, under one context
// (`omniglass outpost run --simulate`). Both halves authenticate as the same outpost
// with the same enrollment token, so there is no second enrollment and no
// second grant: the simulator stays inside the outpost's subject grant
// (ADR-0142). Sharing the process means sharing its network namespace, so the
// simulated components sit on the loopback the outpost collects from by
// construction, with no namespace arrangement around it.
//
// The two live and die together: when either half returns, the other is
// stopped, and the first real failure is the one reported. A stop by the
// caller (ctx ending) is a clean return.
func RunWithOutpost(ctx context.Context, outpostCfg outpost.Config, simCfg Config) error {
	return runWithOutpost(ctx, outpostCfg, simCfg, outpost.Run, Run)
}

func runWithOutpost(ctx context.Context, outpostCfg outpost.Config, simCfg Config, runOutpost outpostRunner, runSim simRunner) error {
	inner, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		half string
		err  error
	}
	done := make(chan result, 2)
	go func() {
		_, err := runOutpost(inner, outpostCfg)
		done <- result{"outpost", err}
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
			return fmt.Errorf("outpost run --simulate: the %s stopped: %w", r.half, r.err)
		}
	}
	// One half returned cleanly with no stop asked for: the pair is no longer
	// whole, which the supervisor must see as a failure to restart it.
	return fmt.Errorf("outpost run --simulate: the %s stopped unexpectedly", first.half)
}
