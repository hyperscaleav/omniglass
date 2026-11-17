// Package bus is the embedded NATS server the control-plane hosts in-process and
// the server-side handlers for the outpost-server protocol (checkpoint 2: worklist
// request-reply and heartbeat, both JSON over core NATS; JetStream is enabled for
// the checkpoint-3 telemetry stream). Per-outpost isolation is enforced by NATS
// subject permissions: an in-process CustomClientAuthentication callback resolves
// each connecting outpost's credential and registers a user whose publish/subscribe
// grants are scoped to that outpost's own subjects. An outpost therefore cannot publish
// or pull as another outpost, which is the invariant this slice keeps real.
package bus

import (
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats-server/v2/server"
)

// simopTimeout is how long the server waits for a simulator's answer to an
// operation, and so how long the simulator's one reply stays permitted.
const simopTimeout = 5 * time.Second

// outpostPermissions is the subject grant for one outpost: it may publish only its own
// worklist request, heartbeat, telemetry, and simulator subjects, and subscribe
// only to its own worklist-changed signal, its own request-reply inbox
// namespace, and the operations the server asks of its simulator. This is the per-outpost credential isolation, expressed as subject
// permissions. Pure: no I/O, unit-testable.
func outpostPermissions(outpost string) *server.Permissions {
	return &server.Permissions{
		Publish: &server.SubjectPermission{Allow: []string{
			collection.WorklistSubject(outpost),
			collection.HeartbeatSubject(outpost),
			collection.TelemetrySubject(outpost),
			collection.CommandSubject(outpost),
			collection.CommandStatusSubject(outpost),
			// A simulator beside the outpost shares its credential.
			collection.SimlistSubject(outpost),
			collection.SimHitSubject(outpost),
		}},
		Subscribe: &server.SubjectPermission{Allow: []string{
			collection.WorklistChangedSubject(outpost),
			collection.InboxPrefix(outpost) + ".>",
			// The simulator beside the outpost takes the server's operation
			// requests for it.
			collection.SimopSubject(outpost),
			// The outpost takes the server's on-demand runs for it (#1163).
			collection.RunSubject(outpost),
			// The lab's endpoint observation (#1639): the simulator answers
			// what its endpoints observed, the outpost what it tallied sending.
			collection.SimobsSubject(outpost),
			collection.RequestTallySubject(outpost),
		}},
		// One reply to each request the outpost received, so the simulator can
		// answer an operation, and the outpost a run, without a grant to publish
		// anywhere else. A run may take its whole budget, so the reply stays
		// permitted that long.
		Response: &server.ResponsePermission{MaxMsgs: 1, Expires: max(simopTimeout, runTimeout+runReplySlack)},
	}
}

// fullPermissions is the grant for the server's own internal client (the
// worklist responder and heartbeat sink). It is never handed to an outpost.
func fullPermissions() *server.Permissions {
	return &server.Permissions{
		Publish:   &server.SubjectPermission{Allow: []string{">"}},
		Subscribe: &server.SubjectPermission{Allow: []string{">"}},
	}
}
