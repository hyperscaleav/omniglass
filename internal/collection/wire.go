package collection

import (
	"encoding/json"
	"strings"
	"time"
)

// The node-server control-plane wire, checkpoint 2: JSON over NATS. The
// telemetry Event (protobuf over JetStream) is checkpoint 3. Subjects encode the
// node name in their last token, and each node's NATS credential is permitted
// only its own subjects, so the subject IS the per-node isolation boundary.
const (
	// subjectPrefix namespaces every control-plane subject.
	subjectPrefix = "og.v1."
	// WorklistWildcard / HeartbeatWildcard are the server-side subscriptions
	// (single-token node wildcard), so a node name is always one subject token.
	WorklistWildcard  = subjectPrefix + "worklist.*"
	HeartbeatWildcard = subjectPrefix + "heartbeat.*"
	// TelemetryWildcard is the server-side JetStream stream subject: every node's
	// telemetry publish (single-token node wildcard), mirroring WorklistWildcard.
	TelemetryWildcard = subjectPrefix + "telemetry.*"
)

// WorklistSubject is where a node requests its worklist (request-reply).
func WorklistSubject(node string) string { return subjectPrefix + "worklist." + node }

// HeartbeatSubject is where a node publishes its liveness heartbeat.
func HeartbeatSubject(node string) string { return subjectPrefix + "heartbeat." + node }

// TelemetrySubject is reserved for the checkpoint-3 telemetry Event stream.
func TelemetrySubject(node string) string { return subjectPrefix + "telemetry." + node }

// WorklistChangedSubject is reserved: the server publishes here to nudge a node
// to re-pull when its config generation advances.
func WorklistChangedSubject(node string) string { return subjectPrefix + "worklist-changed." + node }

// InboxPrefix is a node's private request-reply inbox namespace, so a node's
// subscribe grant covers only its own reply inboxes, never another node's.
func InboxPrefix(node string) string { return "_INBOX." + node }

// NodeFromSubject extracts the node name (the last subject token) from a
// control-plane subject, e.g. og.v1.heartbeat.node-a -> node-a.
func NodeFromSubject(subject string) string {
	if i := strings.LastIndex(subject, "."); i >= 0 {
		return subject[i+1:]
	}
	return subject
}

// TaskSpec is one enabled task in a worklist reply: the content-addressed task
// plus the placement-bound interface it runs over. InterfaceParams and Spec are
// raw jsonb passed through from storage.
type TaskSpec struct {
	ID              string          `json:"id"`
	Mode            string          `json:"mode"`
	InterfaceName   string          `json:"interface_name"`
	InterfaceType   string          `json:"interface_type"`
	InterfaceParams json.RawMessage `json:"interface_params,omitempty"`
	Spec            json.RawMessage `json:"spec,omitempty"`
}

// WorklistReply is the server's response to a worklist request: the node's
// enabled tasks plus the config generation (advances at operator-config pace).
type WorklistReply struct {
	Tasks            []TaskSpec `json:"tasks"`
	ConfigGeneration int64      `json:"config_generation"`
}

// Heartbeat is a node's liveness publish payload.
type Heartbeat struct {
	Node string    `json:"node"`
	At   time.Time `json:"at"`
}
