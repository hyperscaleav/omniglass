package outpost

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// subscribeTally answers the lab's request for what this outpost sent (#1639)
// on the outpost's own tally subject: the whole tally read now, named for the
// outpost. The read is lock-free counters, so it runs on the bus's goroutine.
func subscribeTally(nc *nats.Conn, outpost string, tally *collection.RequestTally) (*nats.Subscription, error) {
	return nc.Subscribe(collection.RequestTallySubject(outpost), func(m *nats.Msg) {
		snap := tally.Snapshot(time.Now())
		snap.Outpost = outpost
		b, err := json.Marshal(snap)
		if err != nil {
			return
		}
		_ = m.Respond(b)
	})
}
