package bus

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"time"

	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/nats-io/nats-server/v2/server"
)

// Store is the narrow gateway surface the bus needs: authenticate a node
// connection, resolve a node's worklist, and record a heartbeat. Kept small so
// the auth callback and handlers can be unit-tested with a fake.
type Store interface {
	AuthenticateNode(ctx context.Context, name, tokenHashHex string) (bool, error)
	NodeWorklist(ctx context.Context, name string) (storage.Worklist, error)
	// NodeSimDevices lists the components a simulator beside the node serves.
	NodeSimDevices(ctx context.Context, name string, read scope.Set) ([]storage.SimDevice, error)
	// NodeSimWebhooks reads the webhook subscriptions of the components the
	// simulator serves (ADR-1370), for its device list alone.
	NodeSimWebhooks(ctx context.Context, name string) (map[string]storage.SimWebhook, error)
	RecordHeartbeat(ctx context.Context, name string) error
	// NodeCheckins and RecordPlatformMetrics are the check-in age timer's
	// read and write (#1625): every claimed node's last word, and the age
	// written on its platform component.
	NodeCheckins(ctx context.Context) ([]storage.NodeCheckin, error)
	RecordPlatformMetrics(ctx context.Context, rows []storage.PlatformMetric) error
	// CommandQueueHealth is the command queue health timer's read (#1626):
	// every node's queue, and the window's counts since the previous read.
	CommandQueueHealth(ctx context.Context, since time.Time) (storage.CommandQueueRead, error)
	// RecordOrphanedBatches counts a node's batches the confinement fence
	// orphaned (#1200), once per flush rather than per batch.
	RecordOrphanedBatches(ctx context.Context, name string, batches int64, at time.Time) error
	// The command wire (#815): resolve-and-dispatch a node's pending command
	// queue, and record its execution reports.
	PendingNodeCommands(ctx context.Context, name string) ([]storage.CommandDelivery, error)
	RecordCommandExecution(ctx context.Context, name string, commandID int64, execErr, reply string) error
	SweepCommandSettlements(ctx context.Context) error
	// SweepEventRules acts on event rule streaks held for their sustain.
	SweepEventRules(ctx context.Context, now time.Time) error
	// ReconcileEventRules heals rule state that disagrees with a series'
	// newest sample, reading samples past from.
	ReconcileEventRules(ctx context.Context, st *storage.RuleReconcileState) error
	// DrainCalcPending serves up to limit pending calc rule marks (#996),
	// returning how many it served.
	DrainCalcPending(ctx context.Context, limit int) (storage.CalcDrain, error)
	// The telemetry ingest consumer surface: resolve+confine a tick's task
	// owners, snapshot the sample registry (reject-not-project), and write the
	// typed rows through one LandBatch. The catalog is two lanes (#587):
	// metric_type names route to the metric sink, property_type names to property.
	// ResolveTaskOwners resolves every task a node's tick names in one read
	// (#1582), each confined to the node; a task that is not the node's is
	// absent from the map, an orphan.
	ResolveTaskOwners(ctx context.Context, nodeName string, taskIDs []string) (map[string]storage.TaskOwner, error)
	// ResolveIdentityBindings resolves the identities one batch names on each
	// conduit against that conduit's bindings, every conduit in one read
	// (#1282, #1582), in system mode.
	ResolveIdentityBindings(ctx context.Context, ids map[string][]storage.IdentityKey) (map[string]*storage.IdentityResolution, error)
	ListMetricTypes(ctx context.Context) ([]storage.MetricType, error)
	ListPropertyTypes(ctx context.Context) ([]storage.PropertyType, error)
	// The event-type registry snapshot: with the log kind gone from property_type,
	// a registered event_type name routes a natively-published occurrence (an xAPI
	// event, a trap) to the event sink (kind "event") as a caught event (ADR-0066).
	ListEventTypes(ctx context.Context) ([]storage.EventType, error)
	// CatalogFingerprint digests the rows the registry is built from, so the
	// consumer reuses its registry until a catalog changes (#1279).
	CatalogFingerprint(ctx context.Context) (string, error)
	// CommandTargetedProperties is the property type names some command type
	// targets, kept beside the registry and carried on each batch (#1299).
	CommandTargetedProperties(ctx context.Context) (map[string]bool, error)
	// LandBatch writes one batch's every sink (the raw log lanes, ADR-0066 and
	// #589, and the three typed sample lanes) in one transaction under the
	// batch's receipt (#311): all or nothing, and an id that already landed
	// writes nothing. The property lane is transition-only, its guard held in
	// the insert (#455). A node tick's groups land or drop one by one
	// (#1582): a refused group is reported in the result, its siblings land.
	LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error)
	// RecordTermedBatch writes the receipt of a batch the consumer Terms
	// whole (#1691), its declared items under the Term's cause, so they read
	// as that cause rather than missing. A later copy that lands replaces it.
	RecordTermedBatch(ctx context.Context, b storage.TermedBatch) error
	// PruneTelemetryReceipts deletes the receipts of batches landed before
	// before, past the horizon a copy of the batch could still arrive in.
	PruneTelemetryReceipts(ctx context.Context, before time.Time, batch int) (int64, error)
	// RetentionWindows and PruneSamples are the sample retention pass's (#417):
	// the windows from the settings store, and one bounded chunk of one lane.
	RetentionWindows(ctx context.Context) (settings.RetentionSettings, error)
	PruneSamples(ctx context.Context, lane string, before time.Time, limit int) (int64, error)
	// EnsureMetricPartitions and DropMetricPartitions are the metric table's
	// daily roll (#420, #1527): three days ahead at boot and on every
	// retention pass, and each day past every window dropped whole.
	EnsureMetricPartitions(ctx context.Context) ([]string, error)
	DropMetricPartitions(ctx context.Context, before time.Time) ([]string, error)
	// RetentionStatus is the storage read after each pass (#1629): the
	// metric partitions held and the stored tables' and database's bytes.
	RetentionStatus(ctx context.Context) (storage.RetentionStatus, error)
	// CompactMetricTrends and PruneMetricTrends are the trend tier's (#1276):
	// hours closed into trends on every pass, and the trend window kept.
	CompactMetricTrends(ctx context.Context, maxHours, laneDays int) (storage.TrendCompaction, error)
	PruneMetricTrends(ctx context.Context, before time.Time, limit int) (int64, error)
	// CompactMetricBlocks is the compacted tier's fold (#1528): closed days
	// folded into lossless blocks on every pass, beside the raw rows.
	CompactMetricBlocks(ctx context.Context, f storage.BlockFold) (storage.BlockCompaction, error)
	// PruneLogs is one chunk of one log table's severity band (#417).
	PruneLogs(ctx context.Context, table, band string, before time.Time, limit int) (int64, error)
	// RetentionOverrides and PruneSamplesOfType are the types with retention
	// windows of their own, and one chunk of one such type (#417).
	RetentionOverrides(ctx context.Context) ([]storage.TypeRetention, error)
	PruneSamplesOfType(ctx context.Context, lane, typeID string, days int, before time.Time, limit int) (int64, error)
	// StampEndpointsSeen records that a batch an endpoint's task delivered
	// landed, whatever it carried: an endpoint's freshness (#933).
	StampEndpointsSeen(ctx context.Context, endpointIDs []string) error
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
