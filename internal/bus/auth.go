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
	// The telemetry ingest consumer surface: resolve+confine a task's owner,
	// snapshot the datapoint registry (reject-not-project), and write the typed
	// metric rows through cp1's insert path.
	ResolveTaskOwner(ctx context.Context, taskID, nodeName string) (storage.TaskOwner, bool, error)
	ListPropertyTypes(ctx context.Context) ([]storage.PropertyType, error)
	InsertMetricDatapoints(ctx context.Context, evs []storage.MetricDatapointEvent) error
	// The state sink and its transition-only guard: a state datapoint routes here
	// (by registry kind), and LatestState lets the consumer skip a write whose
	// value equals the latest stored value for the series.
	InsertStateDatapoints(ctx context.Context, evs []storage.StateDatapointEvent) error
	LatestState(ctx context.Context, componentName, key, instance string) (*storage.StateDatapoint, error)
	// The log sink: a log-kind datapoint routes here (by registry kind) as an
	// occurrence, instead of being dropped.
	InsertEvents(ctx context.Context, evs []storage.EventOccurrence) error
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
