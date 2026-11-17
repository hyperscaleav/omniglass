package node

import (
	"context"
	"maps"
	"net"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The ladder's informational ping is a host's, not an endpoint's (#1249):
// three endpoints of one device, each probed on its own lane, would each
// ping the one host every probe interval. pingShare sends one ping a host a
// probe slot (the probe interval, aligned to the clock) and hands every
// probe of that host in that slot the same answer, so the device sees one
// ping however many endpoints it has. The ping stays informational: a probe
// that waits on another's ping waits no longer than its own would take.

// pingKey is one host's ping in one probe slot.
type pingKey struct {
	host string
	slot time.Time
}

// pingCall is one ping in flight or done: done closes when dps holds its
// answer.
type pingCall struct {
	done chan struct{}
	dps  []collection.Sample
}

// pingShare holds the slot's pings, by host.
type pingShare struct {
	mu    sync.Mutex
	calls map[pingKey]*pingCall
}

// pingHost is the host a probe's informational ping goes to, and whether it
// pings at all: a ladder that asks for it, on a transport other than icmp
// (whose probe is the ping), on a node that can ping.
func pingHost(runner *collection.Runner, task collection.TaskSpec) (string, bool) {
	if task.Ladder == nil || !task.Ladder.Ping || task.Transport == "icmp" || runner.Ping == nil {
		return "", false
	}
	t, err := parseTCPTask(task)
	if err != nil {
		return "", false
	}
	host, _, err := net.SplitHostPort(t.Target)
	if err != nil {
		host = t.Target
	}
	return host, true
}

// pingSlot is the probe slot at now: now truncated to the interval, so every
// probe of a host started within one interval of the clock shares it.
func pingSlot(now time.Time, every time.Duration) time.Time {
	if every <= 0 {
		return now
	}
	n := now.UnixNano()
	return time.Unix(0, n-((n%int64(every))+int64(every))%int64(every)).UTC()
}

// sharedPing is the probe's informational ping, shared with every other
// probe of its host in its slot: the first probe there pings, the rest wait
// for its answer (or their own context) and land a copy of it.
func (c *collector) sharedPing(ctx context.Context, task collection.TaskSpec) []collection.Sample {
	host, ok := pingHost(c.runner, task)
	if !ok {
		return nil
	}
	key := pingKey{host: host, slot: pingSlot(c.clock(), c.every(task))}
	c.pings.mu.Lock()
	if c.pings.calls == nil {
		c.pings.calls = map[pingKey]*pingCall{}
	}
	call, waiting := c.pings.calls[key]
	if !waiting {
		// A slot gone by is never asked again: drop the older ones as a
		// new slot opens, so the map holds a slot's hosts at most.
		for k := range c.pings.calls {
			if k.slot.Before(key.slot) {
				delete(c.pings.calls, k)
			}
		}
		call = &pingCall{done: make(chan struct{})}
		c.pings.calls[key] = call
	}
	c.pings.mu.Unlock()
	if !waiting {
		call.dps = pingInformational(ctx, c.runner, task, nil)
		close(call.done)
	} else {
		select {
		case <-call.done:
		case <-ctx.Done():
			return nil
		}
	}
	out := make([]collection.Sample, len(call.dps))
	for i, s := range call.dps {
		s.Labels = maps.Clone(s.Labels)
		out[i] = s
	}
	return out
}
