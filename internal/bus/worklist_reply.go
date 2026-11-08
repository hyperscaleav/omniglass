package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go"
)

// DefaultMaxPayload is the largest message the embedded NATS server accepts,
// in bytes: the nats-server default (1 MB), set explicitly so the limit is a
// visible setting, not a hidden default (#1579). A worklist reply over it
// cannot be sent; the server tells the node so (#1577).
const DefaultMaxPayload = 1024 * 1024

// worklistNearLimit is the share of max payload past which a reply is warned
// about, so the risk shows before a reply is refused.
const worklistNearLimit = 0.75

// worklistNearWindow is how often one node's near-limit reply is warned about.
const worklistNearWindow = time.Hour

// worklistSizes records the size of each node's latest worklist reply and
// decides when a size is worth a warning (#1579). It is pure apart from its
// lock: every method takes the time it is asked about.
type worklistSizes struct {
	mu     sync.Mutex
	bytes  map[string]int
	warned map[string]time.Time
}

// observe records the size of a reply sent to node at now and reports whether
// it passes the near-limit share of maxPayload and no warning for node has been
// due in the last window; when it reports true the window restarts.
func (w *worklistSizes) observe(node string, size int, maxPayload int64, now time.Time) (warn bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.bytes == nil {
		w.bytes, w.warned = map[string]int{}, map[string]time.Time{}
	}
	w.bytes[node] = size
	if float64(size) <= worklistNearLimit*float64(maxPayload) {
		return false
	}
	if at, ok := w.warned[node]; ok && now.Sub(at) < worklistNearWindow {
		return false
	}
	w.warned[node] = now
	return true
}

// snapshot is each node's latest reply size in bytes.
func (w *worklistSizes) snapshot() map[string]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]int, len(w.bytes))
	for n, b := range w.bytes {
		out[n] = b
	}
	return out
}

// WorklistReplyBytes is the size in bytes of the latest worklist reply sent to
// each node. The server has no metrics endpoint, so this is the gauge: the
// same figure is logged at DEBUG on every reply.
func (s *Server) WorklistReplyBytes() map[string]int { return s.worklistSizes.snapshot() }

// worklistCounts counts how worklist pulls ended (#1577). The server has no
// metrics endpoint, so these are the counters a later one reads; each outcome
// other than served is also logged.
type worklistCounts struct {
	served, readFailed, oversize, respondFailed, expired, abandoned, busy atomic.Int64
}

// WorklistCounts is how many worklist pulls ended each way since the server
// started.
type WorklistCounts struct {
	// Served is the pulls answered with a worklist.
	Served int64
	// ReadFailed is the pulls whose worklist could not be read; the node was
	// told so.
	ReadFailed int64
	// Oversize is the pulls whose reply, compressed, was still over max
	// payload; the node was told so.
	Oversize int64
	// RespondFailed is the replies NATS would not send.
	RespondFailed int64
	// Expired is the pulls dropped unread because their node's budget had run
	// out while they queued.
	Expired int64
	// Abandoned is the pulls whose worklist was read, or begun, after their
	// node had given up, and so never sent.
	Abandoned int64
	// Busy is the pulls refused unread because the responder's queue was
	// full; the node was told so.
	Busy int64
}

// WorklistCounts reads the counts.
func (s *Server) WorklistCounts() WorklistCounts {
	c := &s.worklistCounts
	return WorklistCounts{Served: c.served.Load(), ReadFailed: c.readFailed.Load(), Oversize: c.oversize.Load(),
		RespondFailed: c.respondFailed.Load(), Expired: c.expired.Load(), Abandoned: c.abandoned.Load(), Busy: c.busy.Load()}
}

// respondFunc sends a worklist reply's body under its header.
type respondFunc func(data []byte, hdr nats.Header) error

// answerWorklist builds node's worklist under ctx (its deadline the node's
// budget) and sends it with respond, gzipped past
// collection.WorklistCompressAbove (#1577). A worklist that cannot be read,
// or is still over maxPayload compressed, is answered with the reason and no
// body, so the node keeps the worklist it has and retries; a pull whose node
// gave up before its worklist was ready is not answered. Each outcome is
// logged and counted (#1579). subject is what the node asked on; maxPayload
// is the connection's limit.
func (s *Server) answerWorklist(ctx context.Context, node, subject string, maxPayload int64, respond respondFunc, now time.Time) {
	reply, err := s.buildWorklistReply(ctx, node)
	if ctx.Err() != nil {
		s.worklistCounts.abandoned.Add(1)
		slog.Warn("worklist pull abandoned, its node stopped waiting before the worklist was read", "facility", "worklist",
			"node", node, "subject", subject, "error", ctx.Err().Error())
		return
	}
	if err != nil {
		s.worklistCounts.readFailed.Add(1)
		slog.Warn("worklist read failed, the node keeps its last worklist", "facility", "worklist",
			"node", node, "subject", subject, "error", err.Error())
		s.refuseWorklist(node, subject, "worklist read failed", respond)
		return
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		s.worklistCounts.readFailed.Add(1)
		slog.Warn("worklist reply could not be marshaled, the node keeps its last worklist", "facility", "worklist",
			"node", node, "subject", subject, "error", err.Error())
		s.refuseWorklist(node, subject, "worklist could not be encoded", respond)
		return
	}
	body, hdr, err := collection.EncodeWorklistBody(raw)
	if err != nil {
		s.worklistCounts.readFailed.Add(1)
		slog.Warn("worklist reply could not be compressed, the node keeps its last worklist", "facility", "worklist",
			"node", node, "subject", subject, "error", err.Error())
		s.refuseWorklist(node, subject, "worklist could not be encoded", respond)
		return
	}
	size := len(body)
	if int64(size) > maxPayload {
		s.worklistCounts.oversize.Add(1)
		slog.Error("worklist reply is over max payload and cannot be sent, the node keeps its last worklist", "facility", "worklist",
			"node", node, "subject", subject, "reply_bytes", size, "raw_bytes", len(raw), "max_payload", maxPayload, "tasks", len(reply.Tasks))
		s.refuseWorklist(node, subject, fmt.Sprintf("worklist is %d bytes, over max payload %d", size, maxPayload), respond)
		return
	}
	if err := respond(body, hdr); errors.Is(err, nats.ErrMaxPayload) {
		// The limit counts the header's wire form with the body, so a body
		// just under it can still be refused by the connection.
		s.worklistCounts.oversize.Add(1)
		slog.Error("worklist reply is over max payload with its header and cannot be sent, the node keeps its last worklist", "facility", "worklist",
			"node", node, "subject", subject, "reply_bytes", size, "raw_bytes", len(raw), "max_payload", maxPayload, "tasks", len(reply.Tasks))
		s.refuseWorklist(node, subject, fmt.Sprintf("worklist is %d bytes with its header, over max payload %d", size, maxPayload), respond)
		return
	} else if err != nil {
		s.worklistCounts.respondFailed.Add(1)
		slog.Warn("worklist reply could not be sent, the node will see a timeout", "facility", "worklist",
			"node", node, "subject", subject, "reply_bytes", size, "max_payload", maxPayload, "error", err.Error())
		return
	}
	s.worklistCounts.served.Add(1)
	slog.Debug("worklist reply sent", "facility", "worklist", "node", node, "reply_bytes", size, "raw_bytes", len(raw), "max_payload", maxPayload)
	if s.worklistSizes.observe(node, size, maxPayload, now) {
		slog.Warn("worklist reply is near max payload, a larger one will not be delivered", "facility", "worklist",
			"node", node, "subject", subject, "reply_bytes", size, "raw_bytes", len(raw), "max_payload", maxPayload,
			"share", float64(size)/float64(maxPayload), "tasks", len(reply.Tasks))
	}
}

// refuseWorklist tells node why it gets no worklist, so its pull fails at
// once rather than waiting out its budget, and it keeps the worklist it has.
func (s *Server) refuseWorklist(node, subject, reason string, respond respondFunc) {
	if err := respond(nil, collection.WorklistErrorReply(reason)); err != nil {
		s.worklistCounts.respondFailed.Add(1)
		slog.Warn("worklist refusal could not be sent, the node will see a timeout", "facility", "worklist",
			"node", node, "subject", subject, "error", err.Error())
	}
}
