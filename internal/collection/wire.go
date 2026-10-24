package collection

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
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
	// grant is an explicit allow-list of its own subjects).
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
// plus the placement-bound endpoint it runs over. EndpointID is the
// node-unique endpoint key (names are unique only per component), which a
// held session is keyed by (#816). EndpointParams and Spec are
// raw jsonb passed through from storage. Secrets carries a driver task's
// unsealed secret inputs (#814), resolved server-side at reply time; the
// per-node subject grant is the isolation boundary, so a node only ever
// receives the credentials of endpoints placed on it.
type TaskSpec struct {
	ID             string                       `json:"id"`
	Mode           string                       `json:"mode"`
	EndpointID     string                       `json:"endpoint_id,omitempty"`
	EndpointName   string                       `json:"endpoint_name"`
	Transport      string                       `json:"transport"`
	EndpointParams json.RawMessage              `json:"endpoint_params,omitempty"`
	Spec           json.RawMessage              `json:"spec,omitempty"`
	Secrets        map[string]map[string]string `json:"secrets,omitempty"`
	// Session is the endpoint's driver's session block (#1049): its auth
	// step, terminal replies and re-probe, read off the live driver spec.
	Session *driver.SessionSpec `json:"session,omitempty"`
	// SessionRetry counts the operator's :retry calls on the endpoint; a
	// rejected session redials once when it moves, and nothing else moves it.
	SessionRetry int64 `json:"session_retry,omitempty"`
	// HostKey is the ssh host key the endpoint's session pins (#1032): the
	// operator's declared key, else the key the node trusted on first use;
	// empty before first use.
	HostKey string `json:"host_key,omitempty"`
	// CertificatePin is the tls public key the endpoint pins (#1035): the
	// operator's declared key, else the key the node trusted on first use,
	// empty before first use. Delivered in every mode; only pin mode judges
	// a handshake by it.
	CertificatePin string `json:"certificate_pin,omitempty"`
	// Fingerprint is the connection fingerprint (#1056 ruling, ADR-0158):
	// a digest over every input a connection to the endpoint is made with.
	// A gate holds while it is unchanged. Never logged.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Rejection is the endpoint's last published endpoint-rejection, so a
	// restarted node honors a gate it did not see happen.
	Rejection string `json:"rejection,omitempty"`
	// Ladder is the endpoint's resolved reachability ladder (ADR-0158);
	// nil for a bare endpoint.
	Ladder *driver.EffectiveLadder `json:"ladder,omitempty"`
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
	// EndpointID is the endpoint the command dispatched to, so a node that
	// holds a session there actuates over it (#817); Reply is the binding's
	// declared reply shape, which picks the answer out of that session's
	// pushes.
	EndpointID string        `json:"endpoint_id,omitempty"`
	Reply      *driver.Reply `json:"reply,omitempty"`
	// SessionOnly says the command rides the endpoint's held session or
	// fails (#1049): its driver declares a session, or its transport has no
	// stateless line. A throwaway connection would skip the auth step, or
	// retry a credential the session was rejected for.
	SessionOnly bool `json:"session_only,omitempty"`
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
	// Reply is the device's answer to the command, which a binding that
	// settles from the response reads on the server (ADR-0141).
	Reply string `json:"reply,omitempty"`
}
