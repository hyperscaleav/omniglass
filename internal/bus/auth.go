package bus

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/nats-io/nats-server/v2/server"
)

// Store is the narrow gateway surface the bus needs: authenticate a node
// connection, resolve a node's worklist, and record a heartbeat. Kept small so
// the auth callback and handlers can be unit-tested with a fake.
type Store interface {
	AuthenticateNode(ctx context.Context, name, tokenHashHex string) (bool, error)
	NodeWorklist(ctx context.Context, name string) (storage.Worklist, error)
	RecordHeartbeat(ctx context.Context, name string) error
	// The command wire (#815): resolve-and-dispatch a node's pending command
	// queue, and record its execution reports.
	PendingNodeCommands(ctx context.Context, name string) ([]storage.CommandDelivery, error)
	RecordCommandExecution(ctx context.Context, name string, commandID int64, execErr string) error
	// The telemetry ingest consumer surface: resolve+confine a task's owner,
	// snapshot the sample registry (reject-not-project), and write the typed
	// metric rows through cp1's insert path. The catalog is two lanes (#587):
	// metric_type names route to the metric sink, property_type names to property.
	ResolveTaskOwner(ctx context.Context, taskID, nodeName string) (storage.TaskOwner, bool, error)
	ListMetricTypes(ctx context.Context) ([]storage.MetricType, error)
	ListPropertyTypes(ctx context.Context) ([]storage.PropertyType, error)
	// The event-type registry snapshot: with the log kind gone from property_type,
	// a registered event_type name routes a natively-published occurrence (an xAPI
	// event, a trap) to the event sink (kind "event") as a caught event (ADR-0066).
	ListEventTypes(ctx context.Context) ([]storage.EventType, error)
	InsertMetricSamples(ctx context.Context, evs []storage.MetricSampleWrite) error
	// The property sink and its transition-only guard: a state-kind sample
	// routes here (by registry kind), and LatestProperty lets the consumer skip
	// a write whose value equals the latest stored value for the series.
	InsertPropertySamples(ctx context.Context, evs []storage.PropertySampleWrite) error
	LatestProperty(ctx context.Context, componentName, key, instance string) (*storage.PropertySample, error)
	// The log sink: a log-kind sample routes here (by registry kind) as an
	// occurrence, instead of being dropped.
	InsertEvents(ctx context.Context, evs []storage.EventWrite) error
	// The raw log sinks (ADR-0066): untyped arrival off the ingest lane, no
	// registry gate, split by origin (#589): a push's component-owned lines
	// land on log_line, a node's self-logs on node_log.
	InsertLogLines(ctx context.Context, lines []storage.LogLineWrite) error
	InsertNodeLogs(ctx context.Context, lines []storage.NodeLogWrite) error
}

// nodeAuth implements server.Authentication (the in-process
// CustomClientAuthentication callback). It admits two kinds of client: the
// server's own internal client (token = the boot secret, full permissions) and a
// node (username = node.name, password = enrollment token), which it validates
// against the store and registers with node-scoped subject permissions.
type nodeAuth struct {
	store         Store
	internalToken string
}

// Check authenticates a connecting client and, on success, registers the user's
// subject permissions. A node password is hashed and matched against the stored
// enrollment token; the cleartext never leaves this function.
func (a *nodeAuth) Check(c server.ClientAuthentication) bool {
	opts := c.GetOpts()

	// The server's own internal client authenticates with the boot token.
	if opts.Token != "" {
		if subtle.ConstantTimeCompare([]byte(opts.Token), []byte(a.internalToken)) == 1 {
			c.RegisterUser(&server.User{Permissions: fullPermissions()})
			return true
		}
		return false
	}

	// A node authenticates with username = node.name, password = enrollment token.
	name, pass := opts.Username, opts.Password
	if name == "" || pass == "" {
		return false
	}
	sum := sha256.Sum256([]byte(pass))
	ok, err := a.store.AuthenticateNode(context.Background(), name, hex.EncodeToString(sum[:]))
	if err != nil || !ok {
		return false
	}
	c.RegisterUser(&server.User{Username: name, Permissions: nodePermissions(name)})
	return true
}
