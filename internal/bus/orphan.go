package bus

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// orphanWindow is how often one node's orphaned batches are warned about: the
// first in a window is logged, the rest are counted, and the next warning says
// how many went unlogged (#1200). orphanFlushEvery is how often the counts
// reach the store. Vars so a test can set them.
var (
	orphanWindow     = time.Minute
	orphanFlushEvery = time.Minute
)

// orphanFlushTimeout bounds one flush's store writes.
const orphanFlushTimeout = 10 * time.Second

// orphanCount is one node's orphaned batches not yet written to the store, and
// when the newest was.
type orphanCount struct {
	Batches int64
	Last    time.Time
}

// orphanTally is the node lane's record of the batches its confinement fence
// orphans (#1200): a warning per node per window, so a node stuck publishing
// for a task it no longer holds is never silent and never floods, and a count
// per node, drained to the store on a timer rather than written per batch. It
// is pure apart from its lock: every method takes the time it is asked about.
type orphanTally struct {
	mu     sync.Mutex
	window time.Duration
	nodes  map[string]*orphanNode
}

type orphanNode struct {
	windowStart time.Time
	suppressed  int
	pending     orphanCount
}

func newOrphanTally(window time.Duration) *orphanTally {
	return &orphanTally{window: window, nodes: map[string]*orphanNode{}}
}

// observe records one orphaned batch from node at now. It reports whether this
// one is to be warned about (the first in the node's window) and, when it is,
// how many of the node's batches since its last warning were counted without
// one.
func (t *orphanTally) observe(node string, now time.Time) (warn bool, suppressed int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, ok := t.nodes[node]
	if !ok {
		n = &orphanNode{}
		t.nodes[node] = n
	}
	n.pending.Batches++
	if now.After(n.pending.Last) {
		n.pending.Last = now
	}
	if ok && now.Sub(n.windowStart) < t.window {
		n.suppressed++
		return false, 0
	}
	suppressed, n.suppressed = n.suppressed, 0
	n.windowStart = now
	return true, suppressed
}

// drain hands over every node's uncounted batches and clears them, so each
// orphaned batch reaches the store once.
func (t *orphanTally) drain() map[string]orphanCount {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]orphanCount{}
	for name, n := range t.nodes {
		if n.pending.Batches > 0 {
			out[name] = n.pending
			n.pending = orphanCount{}
		}
	}
	return out
}

// restore puts back counts a flush could not write, so a store hiccup delays
// a count rather than losing it.
func (t *orphanTally) restore(counts map[string]orphanCount) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, c := range counts {
		n, ok := t.nodes[name]
		if !ok {
			n = &orphanNode{}
			t.nodes[name] = n
		}
		n.pending.Batches += c.Batches
		if c.Last.After(n.pending.Last) {
			n.pending.Last = c.Last
		}
	}
}

// forgetQuiet drops a node whose window has closed with nothing left to
// drain, so the tally holds the nodes orphaning now, not every node that ever
// did. Its next orphan opens a fresh window and is warned about.
func (t *orphanTally) forgetQuiet(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, n := range t.nodes {
		if n.pending.Batches == 0 && now.Sub(n.windowStart) >= t.window {
			delete(t.nodes, name)
		}
	}
}

func (t *orphanTally) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.nodes)
}

// recordOrphan is the node lane's answer to a batch the confinement fence
// orphans: count it, and warn about it when it is the first from its node in
// the window, naming the node, the task and what was dropped.
func (s *Server) recordOrphan(node, task string, samples, taskLogs int) {
	warn, suppressed := s.orphans.observe(node, time.Now())
	if !warn {
		return
	}
	slog.Warn("node batch orphaned by the confinement fence, samples dropped",
		"facility", "ingest", "node", node, "task", task,
		"samples", samples, "task_logs", taskLogs,
		"suppressed_since_last_warning", suppressed,
		"window", s.orphans.window.String())
}

// flushOrphans writes the drained counts to the store, one statement per node
// with anything to count, puts back any it could not write, and drops a
// deleted node's.
func (s *Server) flushOrphans(ctx context.Context) {
	counts := s.orphans.drain()
	failed := map[string]orphanCount{}
	for name, c := range counts {
		if err := s.store.RecordOrphanedBatches(ctx, name, c.Batches, c.Last); err != nil {
			// A node deleted since its batches arrived has no row to count
			// on, and never will: drop the count rather than retry it every
			// period, so the node is forgotten like any quiet one.
			if errors.Is(err, storage.ErrNodeNotFound) {
				slog.Info("orphaned batch count dropped: the node no longer exists",
					"facility", "ingest", "node", name, "batches", c.Batches)
				continue
			}
			failed[name] = c
			if ctx.Err() == nil {
				slog.Warn("orphaned batch count not recorded, kept for the next flush",
					"facility", "ingest", "node", name, "batches", c.Batches, "error", err.Error())
			}
		}
	}
	if len(failed) > 0 {
		s.orphans.restore(failed)
	}
	s.orphans.forgetQuiet(time.Now())
}

// startOrphanFlush drains the orphan tally to the store every
// orphanFlushEvery, so a count is never held past one period; Shutdown writes
// the last of it once the consumer has stopped.
func (s *Server) startOrphanFlush() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopOrphanFlush = cancel
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		t := time.NewTicker(orphanFlushEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pass, done := context.WithTimeout(ctx, orphanFlushTimeout)
				s.flushOrphans(pass)
				done()
			}
		}
	}()
}
