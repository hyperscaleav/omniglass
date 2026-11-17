package node

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// subscribeTally answers the lab's request for what this node sent (#1639)
// on the node's own tally subject: the whole tally read now, named for the
// node. The read is lock-free counters, so it runs on the bus's goroutine.
func subscribeTally(nc *nats.Conn, node string, tally *collection.RequestTally) (*nats.Subscription, error) {
	return nc.Subscribe(collection.RequestTallySubject(node), func(m *nats.Msg) {
		snap := tally.Snapshot(time.Now())
		snap.Node = node
		b, err := json.Marshal(snap)
		if err != nil {
			return
		}
		_ = m.Respond(b)
	})
}
