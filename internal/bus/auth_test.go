package bus

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"testing"

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
func (f fakeStore) NodeWorklist(context.Context, string) (storage.Worklist, error) {
	return storage.Worklist{}, nil
}
func (f fakeStore) RecordHeartbeat(context.Context, string) error { return nil }
func (f fakeStore) ResolveTaskOwner(context.Context, string, string) (storage.TaskOwner, bool, error) {
	return storage.TaskOwner{}, false, nil
}
func (f fakeStore) ListProperties(context.Context) ([]storage.Property, error) {
	return nil, nil
}
func (f fakeStore) InsertMetricDatapoints(context.Context, []storage.MetricDatapointEvent) error {
	return nil
}
func (f fakeStore) InsertStateDatapoints(context.Context, []storage.StateDatapointEvent) error {
	return nil
}
func (f fakeStore) LatestState(context.Context, string, string, string) (*storage.StateDatapoint, error) {
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
