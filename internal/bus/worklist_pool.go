package bus

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go"
)

// DefaultWorklistWorkers is how many worklist pulls the server builds at once
// when it is not told otherwise (#1577). Measured against the 1,000-system
// lab, eight brought 18 nodes' pulls from 5.12 s in series to 1.30 s; past
// about eight the database is the limit.
const DefaultWorklistWorkers = 8

// worklistQueueDepth is how many pulls wait for a worker before a further
// pull is refused as busy. The subscription never blocks on the queue: a
// blocked callback would leave later pulls in NATS's own buffer, stamped on
// delivery rather than arrival, and the deadline would no longer be the
// node's.
const worklistQueueDepth = 256

// defaultWorklistBudget is the budget of a pull that states none.
const defaultWorklistBudget = 5 * time.Second

// worklistWorkers is the pool's size: want, or DefaultWorklistWorkers when
// want is not positive, held under the database pool less the one connection
// kept for ingest (no cap when dbPool is not positive), and never under one.
func worklistWorkers(want, dbPool int) int {
	if want <= 0 {
		want = DefaultWorklistWorkers
	}
	if dbPool > 0 && want > dbPool-1 {
		want = dbPool - 1
	}
	return max(want, 1)
}

// worklistDeadline is when a pull's node stops waiting: its arrival plus the
// budget its header states, or defaultWorklistBudget.
func worklistDeadline(hdr nats.Header, arrived time.Time) time.Time {
	budget, ok := collection.WorklistRequestTimeout(hdr)
	if !ok {
		budget = defaultWorklistBudget
	}
	return arrived.Add(budget)
}

// worklistPull is one node's pull, waiting for a worker.
type worklistPull struct {
	node     string
	subject  string
	respond  respondFunc
	deadline time.Time
}

// pullOf is msg, a node's pull on the worklist subject, as the pool holds it.
func pullOf(node string, msg *nats.Msg, deadline time.Time) worklistPull {
	return worklistPull{node: node, subject: msg.Subject, deadline: deadline, respond: func(data []byte, hdr nats.Header) error {
		return msg.RespondMsg(&nats.Msg{Subject: msg.Reply, Data: data, Header: hdr})
	}}
}

// startWorklistPool starts n workers that build and answer queued pulls. The
// subscription only stamps and queues a pull, so one large worklist never
// holds up the others, and a pull's deadline is counted from its arrival.
func (s *Server) startWorklistPool(n int) {
	ctx, cancel := context.WithCancel(context.Background())
	s.worklistCtx, s.stopWorklist = ctx, cancel
	s.worklistQueue = make(chan worklistPull, worklistQueueDepth)
	for range n {
		s.worklistDone.Add(1)
		go func() {
			defer s.worklistDone.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case p := <-s.worklistQueue:
					s.serveWorklistPull(ctx, p, time.Now())
				}
			}
		}()
	}
}

// queueWorklistPull hands a pull to the pool without waiting. A pull that
// finds the queue full is refused as busy, so its node fails fast and backs
// off rather than waiting out a budget the pool cannot meet; one that
// arrives once the pool has stopped is dropped.
func (s *Server) queueWorklistPull(p worklistPull) {
	select {
	case <-s.worklistCtx.Done():
	case s.worklistQueue <- p:
	default:
		s.worklistCounts.busy.Add(1)
		slog.Warn("worklist pull refused, the responder's queue is full", "facility", "worklist",
			"node", p.node, "subject", p.subject, "queued", len(s.worklistQueue))
		s.refuseWorklist(p.node, p.subject, "worklist responder is busy", p.respond)
	}
}

// serveWorklistPull answers one queued pull at now, under its node's
// deadline: one whose node stopped waiting while it queued is dropped unread.
func (s *Server) serveWorklistPull(ctx context.Context, p worklistPull, now time.Time) {
	if !now.Before(p.deadline) {
		s.worklistCounts.expired.Add(1)
		slog.Warn("worklist pull expired while it queued, its node stopped waiting", "facility", "worklist",
			"node", p.node, "subject", p.subject, "late", now.Sub(p.deadline).String())
		return
	}
	bctx, done := context.WithDeadline(ctx, p.deadline)
	defer done()
	s.answerWorklist(bctx, p.node, p.subject, s.nc.MaxPayload(), p.respond, now)
}
