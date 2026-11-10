package labfootprint

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/client"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// endpointReads bounds how many nodes' observations are read at once.
const endpointReads = 8

// drainPoll is how often the marks ask whether the ingest lane has drained,
// and drainBound how long they wait for it: a lane further behind than the
// bound leaves the closed loop unmeasured rather than read short.
const (
	drainPoll  = time.Second
	drainBound = 3 * time.Minute
)

// endpointMark is one end of the closed loop's window: the database's clock
// and every node's bracketed observation, read after it at the start and
// before it at the end, so the receipts the clock bounds fall inside the
// reads; and Drained, the database's clock once the ingest lane has landed
// every batch the bus acknowledged before the mark's reads.
type endpointMark struct {
	At      time.Time
	Drained time.Time
	Obs     map[string]collection.EndpointObservation
}

// markStart reads the database's clock, then every node's observation, then
// waits for the lane to drain past the reads: what it landed until then can
// be batches from before the window.
func (h *harness) markStart(ctx context.Context, l scaleLab) (endpointMark, error) {
	at, err := h.dbClock(ctx)
	if err != nil {
		return endpointMark{}, err
	}
	obs, err := h.readEndpoints(ctx, l)
	if err != nil {
		return endpointMark{}, err
	}
	read, err := h.dbClock(ctx)
	if err != nil {
		return endpointMark{}, err
	}
	drained, err := h.drainedPast(ctx, read)
	return endpointMark{At: at, Drained: drained, Obs: obs}, err
}

// markEnd reads every node's observation, then the database's clock, then
// waits for the lane to drain past it, so every batch acknowledged inside
// the window has landed before its receipts are counted.
func (h *harness) markEnd(ctx context.Context, l scaleLab) (endpointMark, error) {
	obs, err := h.readEndpoints(ctx, l)
	if err != nil {
		return endpointMark{}, err
	}
	at, err := h.dbClock(ctx)
	if err != nil {
		return endpointMark{}, err
	}
	drained, err := h.drainedPast(ctx, at)
	return endpointMark{At: at, Drained: drained, Obs: obs}, err
}

// drainedPast waits for the ingest lane to drain past at (drainedQuery) and
// returns the database's clock when it had.
func (h *harness) drainedPast(ctx context.Context, at time.Time) (time.Time, error) {
	deadline := time.Now().Add(drainBound)
	for {
		out, err := h.psql(ctx, drainedQuery(at))
		if err != nil {
			return time.Time{}, fmt.Errorf("read the ingest lane's drain: %w", err)
		}
		drained, clock, err := parseDrained(out)
		if err != nil {
			return time.Time{}, err
		}
		if drained {
			return clock, nil
		}
		if time.Now().After(deadline) {
			return time.Time{}, fmt.Errorf("the ingest lane landed no sample a node probed after %s within %s", at.Format(time.RFC3339), drainBound)
		}
		if err := sleep(ctx, drainPoll); err != nil {
			return time.Time{}, err
		}
	}
}

// dbClock is the database's clock now, the clock a receipt lands on.
func (h *harness) dbClock(ctx context.Context) (time.Time, error) {
	out, err := h.psql(ctx, `select extract(epoch from clock_timestamp())`)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the database's clock: %w", err)
	}
	return parseEpoch(out)
}

// parseEpoch reads seconds since the epoch, as psql prints them.
func parseEpoch(out string) (time.Time, error) {
	s := strings.TrimSpace(out)
	whole, frac, _ := strings.Cut(s, ".")
	sec, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("the database's clock %q: %w", s, err)
	}
	var nsec int64
	if frac != "" {
		frac = (frac + "000000000")[:9]
		if nsec, err = strconv.ParseInt(frac, 10, 64); err != nil {
			return time.Time{}, fmt.Errorf("the database's clock %q: %w", s, err)
		}
	}
	return time.Unix(sec, nsec).UTC(), nil
}

// readEndpoints reads each node's endpoint observation through the API, a
// few at a time. A scale lab's node service is named for its node.
func (h *harness) readEndpoints(ctx context.Context, l scaleLab) (map[string]collection.EndpointObservation, error) {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		out  = map[string]collection.EndpointObservation{}
		errs []error
		sem  = make(chan struct{}, endpointReads)
	)
	for node := range l.nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var obs collection.EndpointObservation
			err := h.get(ctx, h.env["LAB_HOST_URL"]+"/api/v1/nodes/"+url.PathEscape(node)+"/simulation/observation", &obs)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s's endpoint observation: %w", node, err))
				return
			}
			out[node] = obs
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// nodeCadences reads each node container's heartbeat and probe interval off
// its command.
func (h *harness) nodeCadences(ctx context.Context, l scaleLab) (map[string]NodeCadence, error) {
	out := map[string]NodeCadence{}
	for node, id := range l.nodes {
		r, err := h.dc.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("read %s's command: %w", node, err)
		}
		var cmd []string
		if r.Container.Config != nil {
			cmd = r.Container.Config.Cmd
		}
		out[node] = ParseNodeCadence(cmd)
	}
	return out, nil
}

// endpointWindow is the span between the marks, refused past the receipts'
// horizon (the earliest receipts may be pruned) and when the database's
// clock ran backwards.
func endpointWindow(start, end endpointMark) (time.Duration, error) {
	w := end.At.Sub(start.At)
	switch {
	case w <= 0:
		return 0, fmt.Errorf("the database's clock read %s at the end, not after its %s at the start", end.At, start.At)
	case w > maxEndpointWindow:
		return 0, fmt.Errorf("the window is %s, past the %s the server keeps receipts for", w.Round(time.Second), maxEndpointWindow)
	}
	return w, nil
}

// endpointBrackets closes the loop on every node: the two marks, the
// receipts each node's batches left between them, and the poll targets each
// node held.
func (h *harness) endpointBrackets(ctx context.Context, l scaleLab, start, end endpointMark) ([]NodeBracket, error) {
	window, err := endpointWindow(start, end)
	if err != nil {
		return nil, err
	}
	cadence, err := h.nodeCadences(ctx, l)
	if err != nil {
		return nil, err
	}
	out, err := h.psql(ctx, pollTargetsQuery)
	if err != nil {
		return nil, fmt.Errorf("read the poll targets: %w", err)
	}
	targets, err := parsePollTargets(out, cadence)
	if err != nil {
		return nil, err
	}
	var brackets []NodeBracket
	for node := range l.nodes {
		out, err := h.psql(ctx, receiptCountQuery(node, start.At, start.Drained, end.At))
		if err != nil {
			return nil, fmt.Errorf("count %s's receipts: %w", node, err)
		}
		receipts, err := parseReceiptCount(out)
		if err != nil {
			return nil, err
		}
		brackets = append(brackets, NodeBracket{Node: node, Start: start.Obs[node], End: end.Obs[node],
			Receipts: receipts, Targets: targets[node], Window: window})
	}
	sort.Slice(brackets, func(i, j int) bool { return brackets[i].Node < brackets[j].Node })
	return brackets, nil
}
