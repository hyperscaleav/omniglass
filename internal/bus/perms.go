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
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats-server/v2/server"
)

// nodePermissions is the subject grant for one node: it may publish only its own
// worklist request, heartbeat, and (reserved) telemetry subjects, and subscribe
// only to its own worklist-changed signal and its own request-reply inbox
// namespace. This is the per-node credential isolation, expressed as subject
// permissions. Pure: no I/O, unit-testable.
func nodePermissions(node string) *server.Permissions {
	return &server.Permissions{
		Publish: &server.SubjectPermission{Allow: []string{
			collection.WorklistSubject(node),
			collection.HeartbeatSubject(node),
			collection.TelemetrySubject(node),
		}},
		Subscribe: &server.SubjectPermission{Allow: []string{
			collection.WorklistChangedSubject(node),
			collection.InboxPrefix(node) + ".>",
		}},
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
