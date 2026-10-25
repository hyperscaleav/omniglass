// Package bus is the embedded NATS server the control-plane hosts in-process and
// the server-side handlers for the node-server protocol (checkpoint 2: worklist
// request-reply and heartbeat, both JSON over core NATS; JetStream is enabled for
// the checkpoint-3 telemetry stream). Per-node isolation is enforced by NATS
// subject permissions: an in-process CustomClientAuthentication callback resolves
// each connecting node's credential and registers a user whose publish/subscribe
// grants are scoped to that node's own subjects. A node therefore cannot publish
// or pull as another node, which is the invariant this slice keeps real.
package bus

import (
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats-server/v2/server"
)

// simopTimeout is how long the server waits for a simulator's answer to an
// operation, and so how long the simulator's one reply stays permitted.
const simopTimeout = 5 * time.Second

// nodePermissions is the subject grant for one node: it may publish only its own
// worklist request, heartbeat, telemetry, and simulator subjects, and subscribe
// only to its own worklist-changed signal, its own request-reply inbox
// namespace, and the operations the server asks of its simulator. This is the per-node credential isolation, expressed as subject
// permissions. Pure: no I/O, unit-testable.
func nodePermissions(node string) *server.Permissions {
	return &server.Permissions{
		Publish: &server.SubjectPermission{Allow: []string{
			collection.WorklistSubject(node),
			collection.HeartbeatSubject(node),
			collection.TelemetrySubject(node),
			collection.CommandSubject(node),
			collection.CommandStatusSubject(node),
			// A simulator beside the node shares its credential.
			collection.SimlistSubject(node),
			collection.SimHitSubject(node),
		}},
		Subscribe: &server.SubjectPermission{Allow: []string{
			collection.WorklistChangedSubject(node),
			collection.InboxPrefix(node) + ".>",
			// The simulator beside the node takes the server's operation
			// requests for it.
			collection.SimopSubject(node),
			// The node takes the server's on-demand runs for it (#1163).
			collection.RunSubject(node),
		}},
		// One reply to each request the node received, so the simulator can
		// answer an operation, and the node a run, without a grant to publish
		// anywhere else. A run may take its whole budget, so the reply stays
		// permitted that long.
		Response: &server.ResponsePermission{MaxMsgs: 1, Expires: max(simopTimeout, runTimeout+runReplySlack)},
	}
}

// fullPermissions is the grant for the server's own internal client (the
// worklist responder and heartbeat sink). It is never handed to a node.
func fullPermissions() *server.Permissions {
	return &server.Permissions{
		Publish:   &server.SubjectPermission{Allow: []string{">"}},
		Subscribe: &server.SubjectPermission{Allow: []string{">"}},
	}
}
