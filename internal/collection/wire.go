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
	// CommandWildcard / CommandStatusWildcard are the server-side subscriptions
	// for the command queue pull and the execution reports (#815).
	CommandWildcard       = subjectPrefix + "command.*"
	CommandStatusWildcard = subjectPrefix + "commandstatus.*"
	// TelemetryWildcard is the server-side JetStream stream subject: every node's
	// telemetry publish (single-token node wildcard), mirroring WorklistWildcard.
	TelemetryWildcard = subjectPrefix + "telemetry.*"
	// APITelemetrySubject is the trusted push lane: the API publishes a batch here
	// after it has authorized the caller against the declared owner, so the consumer
	// believes that owner. Only the server's own credential can publish here (a node
	// grant is an explicit allow-list of its three own subjects).
	//
	// It is deliberately NOT a token under TelemetryWildcard. That wildcard is
	// single-token and a node's grant covers og.v1.telemetry.<its-own-name>, so a
	// node named "api" (or whatever literal we reserved there) would be handed the
	// trusted subject and could publish a forged, pre-authorized owner. Putting the
	// lane in its own segment makes that structurally impossible rather than
	// dependent on nobody choosing an awkward node name.
	APITelemetrySubject = subjectPrefix + "api.telemetry"
)

// WorklistSubject is where a node requests its worklist (request-reply).
func WorklistSubject(node string) string { return subjectPrefix + "worklist." + node }

// HeartbeatSubject is where a node publishes its liveness heartbeat.
func HeartbeatSubject(node string) string { return subjectPrefix + "heartbeat." + node }

// TelemetrySubject is a node's telemetry publish subject, bound into the
// OG_TELEMETRY JetStream stream.
func TelemetrySubject(node string) string { return subjectPrefix + "telemetry." + node }

// WorklistChangedSubject is reserved: the server publishes here to nudge a node
// to re-pull when its config generation advances.
func WorklistChangedSubject(node string) string { return subjectPrefix + "worklist-changed." + node }

// CommandSubject is where a node pulls its pending command queue (#815), a
// request-reply mirroring the worklist: the reply is the commands resolved and
// rendered for this node, marked dispatched by the pull itself.
func CommandSubject(node string) string { return subjectPrefix + "command." + node }

// CommandStatusSubject is where a node reports a command's execution outcome.
func CommandStatusSubject(node string) string { return subjectPrefix + "commandstatus." + node }

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
// plus the placement-bound endpoint it runs over. EndpointParams and Spec are
// raw jsonb passed through from storage. Secrets carries a driver task's
// unsealed secret inputs (#814), resolved server-side at reply time; the
// per-node subject grant is the isolation boundary, so a node only ever
// receives the credentials of endpoints placed on it.
type TaskSpec struct {
	ID             string                       `json:"id"`
	Mode           string                       `json:"mode"`
	EndpointName   string                       `json:"endpoint_name"`
	Transport      string                       `json:"transport"`
	EndpointParams json.RawMessage              `json:"endpoint_params,omitempty"`
	Spec           json.RawMessage              `json:"spec,omitempty"`
	Secrets        map[string]map[string]string `json:"secrets,omitempty"`
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

// CommandDelivery is one command rendered for a node's queue (#815): what to
// send (the rendered line), over which transport, to which target.
type CommandDelivery struct {
	ID          int64  `json:"id"`
	CommandType string `json:"command_type"`
	Transport   string `json:"transport"`
	Target      string `json:"target"`
	Line        string `json:"line,omitempty"`
}

// CommandPullReply is the server's response to a command queue pull.
type CommandPullReply struct {
	Commands []CommandDelivery `json:"commands"`
}

// CommandStatus is a node's execution report for one command: an empty Error
// is a successful actuation. Settlement stays the server's separate judgment
// of observed against intended.
type CommandStatus struct {
	ID    int64  `json:"id"`
	Error string `json:"error,omitempty"`
}
