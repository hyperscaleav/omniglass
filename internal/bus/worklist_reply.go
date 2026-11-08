package bus

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// DefaultMaxPayload is the largest message the embedded NATS server accepts,
// in bytes: the nats-server default (1 MB), set explicitly so the limit is a
// visible setting, not a hidden default (#1579). A worklist reply over it
// cannot be sent, and the node sees a timeout.
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

// answerWorklist builds node's worklist and sends it with respond, saying so
// when it cannot: a failure here is otherwise a timeout at the node, with
// nothing on the server to explain it (#1579). subject is what the node asked
// on; maxPayload is the connection's limit.
func (s *Server) answerWorklist(node, subject string, maxPayload int64, respond func([]byte) error, now time.Time) {
	reply, err := s.buildWorklistReply(node)
	if err != nil {
		slog.Warn("worklist read failed, the node will see a timeout", "facility", "worklist",
			"node", node, "subject", subject, "error", err.Error())
		return
	}
	b, err := json.Marshal(reply)
	if err != nil {
		slog.Warn("worklist reply could not be marshaled, the node will see a timeout", "facility", "worklist",
			"node", node, "subject", subject, "error", err.Error())
		return
	}
	size := len(b)
	if int64(size) > maxPayload {
		slog.Error("worklist reply is over max payload and cannot be sent, the node will see a timeout", "facility", "worklist",
			"node", node, "subject", subject, "reply_bytes", size, "max_payload", maxPayload, "tasks", len(reply.Tasks))
		return
	}
	if err := respond(b); err != nil {
		slog.Warn("worklist reply could not be sent, the node will see a timeout", "facility", "worklist",
			"node", node, "subject", subject, "reply_bytes", size, "max_payload", maxPayload, "error", err.Error())
		return
	}
	slog.Debug("worklist reply sent", "facility", "worklist", "node", node, "reply_bytes", size, "max_payload", maxPayload)
	if s.worklistSizes.observe(node, size, maxPayload, now) {
		slog.Warn("worklist reply is near max payload, a larger one will not be delivered", "facility", "worklist",
			"node", node, "subject", subject, "reply_bytes", size, "max_payload", maxPayload,
			"share", float64(size)/float64(maxPayload), "tasks", len(reply.Tasks))
	}
}
