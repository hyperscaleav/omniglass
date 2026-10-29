package bus

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/nats-io/nats-server/v2/server"
)

// fakeStore admits exactly one (node, token-hash) pair, so the auth callback can
// be unit-tested without a database.
type fakeStore struct {
	validName    string
	validHashHex string
}

func (f fakeStore) AuthenticateNode(_ context.Context, name, hashHex string) (bool, error) {
	return name == f.validName && hashHex == f.validHashHex, nil
}
func (f fakeStore) PendingNodeCommands(context.Context, string) ([]storage.CommandDelivery, error) {
	return nil, nil
}
func (f fakeStore) RecordCommandExecution(context.Context, string, int64, string, string) error {
	return nil
}
func (f fakeStore) SweepCommandSettlements(context.Context) error    { return nil }
func (f fakeStore) SweepEventRules(context.Context, time.Time) error { return nil }
func (f fakeStore) DrainCalcPending(context.Context, int) (storage.CalcDrain, error) {
	return storage.CalcDrain{}, nil
}
func (f fakeStore) ReconcileEventRules(context.Context, *storage.RuleReconcileState) error {
	return nil
}
func (f fakeStore) NodeSimDevices(context.Context, string, scope.Set) ([]storage.SimDevice, error) {
	return nil, nil
}
func (f fakeStore) NodeWorklist(context.Context, string) (storage.Worklist, error) {
	return storage.Worklist{}, nil
}
func (f fakeStore) RecordHeartbeat(context.Context, string) error { return nil }
func (f fakeStore) ResolveTaskOwner(context.Context, string, string) (storage.TaskOwner, bool, error) {
	return storage.TaskOwner{}, false, nil
}
func (f fakeStore) ResolveIdentityBindings(context.Context, string, []storage.IdentityKey) (*storage.IdentityResolution, error) {
	return nil, nil
}
func (f fakeStore) ListMetricTypes(context.Context) ([]storage.MetricType, error) {
	return nil, nil
}
func (f fakeStore) ListPropertyTypes(context.Context) ([]storage.PropertyType, error) {
	return nil, nil
}
func (f fakeStore) StampEndpointsSeen(context.Context, []string) error { return nil }
func (f fakeStore) RecordOrphanedBatches(context.Context, string, int64, time.Time) error {
	return nil
}

func (f fakeStore) LandBatch(context.Context, storage.IngestBatch) (bool, error) {
	return true, nil
}
func (f fakeStore) RetentionWindows(context.Context) (settings.RetentionSettings, error) {
	return settings.RetentionSettings{MetricDays: 14, PropertyDays: 90, TrendDays: 730,
		DebugLogDays: 3, InfoLogDays: 14, WarningLogDays: 30, ErrorLogDays: 90}, nil
}
func (f fakeStore) RetentionOverrides(context.Context) ([]storage.TypeRetention, error) {
	return nil, nil
}
func (f fakeStore) PruneSamplesOfType(context.Context, string, string, int, time.Time, int) (int64, error) {
	return 0, nil
}
func (f fakeStore) PruneLogs(context.Context, string, string, time.Time, int) (int64, error) {
	return 0, nil
}
func (f fakeStore) PruneSamples(context.Context, string, time.Time, int) (int64, error) {
	return 0, nil
}
func (f fakeStore) PruneTelemetryReceipts(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}
func (f fakeStore) ListEventTypes(context.Context) ([]storage.EventType, error) {
	return nil, nil
}
func (f fakeStore) CatalogFingerprint(context.Context) (string, error) { return "", nil }
func (f fakeStore) CommandTargetedProperties(context.Context) (map[string]bool, error) {
	return nil, nil
}

// fakeClientAuth is a minimal server.ClientAuthentication that carries the
// presented options and captures the RegisterUser call.
type fakeClientAuth struct {
	opts       *server.ClientOpts
	registered *server.User
}

func (f *fakeClientAuth) GetOpts() *server.ClientOpts                 { return f.opts }
func (f *fakeClientAuth) RegisterUser(u *server.User)                 { f.registered = u }
func (f *fakeClientAuth) GetTLSConnectionState() *tls.ConnectionState { return nil }
func (f *fakeClientAuth) RemoteAddress() net.Addr                     { return nil }
func (f *fakeClientAuth) GetNonce() []byte                            { return nil }
func (f *fakeClientAuth) Kind() int                                   { return 0 }
func (f *fakeClientAuth) GetID() uint64                               { return 0 }

func TestAuthCheck(t *testing.T) {
	pass := "enroll-secret"
	sum := sha256.Sum256([]byte(pass))
	store := fakeStore{validName: "node-a", validHashHex: hex.EncodeToString(sum[:])}
	a := &nodeAuth{store: store, internalToken: "boot-secret"}

	// Internal client: right token -> full permissions.
	c := &fakeClientAuth{opts: &server.ClientOpts{Token: "boot-secret"}}
	if !a.Check(c) || c.registered == nil || c.registered.Permissions.Publish.Allow[0] != ">" {
		t.Fatalf("internal token: want admit with full perms, got %+v", c.registered)
	}
	// Wrong internal token -> reject.
	c = &fakeClientAuth{opts: &server.ClientOpts{Token: "nope"}}
	if a.Check(c) {
		t.Fatalf("wrong internal token: want reject")
	}

	// Node: right creds -> admit with node-scoped perms.
	c = &fakeClientAuth{opts: &server.ClientOpts{Username: "node-a", Password: pass}}
	if !a.Check(c) || c.registered == nil {
		t.Fatalf("node right creds: want admit")
	}
	if got := c.registered.Permissions.Publish.Allow; got[1] != "og.v1.heartbeat.node-a" {
		t.Fatalf("node perms not scoped to node-a: %v", got)
	}
	// Node: wrong password -> reject, no registration.
	c = &fakeClientAuth{opts: &server.ClientOpts{Username: "node-a", Password: "wrong"}}
	if a.Check(c) || c.registered != nil {
		t.Fatalf("node wrong password: want reject")
	}
	// Empty creds -> reject.
	c = &fakeClientAuth{opts: &server.ClientOpts{}}
	if a.Check(c) {
		t.Fatalf("empty creds: want reject")
	}
}
