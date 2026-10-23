package node

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The node's ssh sessions (#1032): the transport's own credential comes from
// the secret input the driver names, the host key pin rides the worklist and
// is trusted on first use, a pin the operator changes recycles the session,
// and a key trusted on first use publishes as ssh-host-key without
// recycling anything.

func sshPollTask(id, endpointID, hostKey string, secrets map[string]map[string]string) collection.TaskSpec {
	return collection.TaskSpec{
		ID: id, Mode: "poll", Transport: "ssh", EndpointID: endpointID, EndpointName: "ssh",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:22"}`),
		Spec: json.RawMessage(`{"driver":"newtron-nvp-ssh","function":"status","schedule":{"every":"30s"},
			"request":{"line":"GET INPUT"},
			"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^INPUT (\\S+)$"}}]}`),
		Secrets: secrets,
		Session: &driver.SessionSpec{Credential: "login", PTY: true},
		HostKey: hostKey,
	}
}

func TestSSHSessionAuthComesFromTheNamedSecret(t *testing.T) {
	task := sshPollTask("p1", "ep-1", "SHA256:pinned", map[string]map[string]string{"login": {"username": "admin", "password": "newtron"}})
	want, refused := desiredSessions([]collection.TaskSpec{task}, map[string]collection.SessionDialer{"ssh": &authFakeDialer{}})
	if len(refused) != 0 {
		t.Fatalf("refused = %v", refused)
	}
	w := want["ep-1"]
	if w.auth == nil || w.auth.SSH == nil || w.auth.SSH.Username != "admin" || w.auth.SSH.Password != "newtron" {
		t.Fatalf("ssh auth = %+v", w.auth)
	}
	if w.hostKey != "SHA256:pinned" || !w.channel.PTY {
		t.Fatalf("pin %q, channel %+v", w.hostKey, w.channel)
	}

	task.Secrets = map[string]map[string]string{"login": {"username": "admin"}}
	_, refused = desiredSessions([]collection.TaskSpec{task}, map[string]collection.SessionDialer{"ssh": &authFakeDialer{}})
	if r := refused["ep-1"]; !strings.Contains(r, "login") || strings.Contains(r, "admin") {
		t.Fatalf("refused = %q, want the missing credential named by reference", r)
	}
}

// sshFakeDialer trusts a host key as the real dialer does, reporting it.
type sshFakeDialer struct {
	heldFakeDialer
	mu       sync.Mutex
	connects int
	offered  string
}

func (d *sshFakeDialer) Connect(_ context.Context, target string, _ time.Duration) (collection.PendingConn, error) {
	d.mu.Lock()
	d.connects++
	d.mu.Unlock()
	return &sshFakePending{d: d, target: target}, nil
}

func (d *sshFakeDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connects
}

type sshFakePending struct {
	d      *sshFakeDialer
	target string
}

func (p *sshFakePending) Authenticate(_ context.Context, auth *collection.SessionAuth, _ collection.Classifier, _ time.Duration) error {
	pin := auth.SSH.Pin
	if pinned := pin.Get(); pinned != "" && pinned != p.d.offered {
		return &collection.TerminalError{Class: driver.ClassHostKey, Reason: "the host key changed"}
	}
	pin.Trust(p.d.offered)
	return nil
}

func (p *sshFakePending) Open(in collection.Inbound, _ collection.ChannelOptions) (collection.HeldConn, error) {
	return p.d.heldFakeDialer.Dial(context.Background(), p.target, 0, in)
}

func (p *sshFakePending) Close() error { return nil }

func TestSSHHostKeyTrustedOnFirstUsePublishesAndAPinChangeRecycles(t *testing.T) {
	dialer := &sshFakeDialer{offered: "SHA256:first"}
	log := &transitionLog{}
	var mu sync.Mutex
	var keys []string
	m := newSessionManager(map[string]collection.SessionDialer{"ssh": dialer}, sessionHooks{
		transition: log.record,
		hostKey: func(_, _ string, fp string) {
			mu.Lock()
			keys = append(keys, fp)
			mu.Unlock()
		},
	})
	defer m.closeAll()
	creds := map[string]map[string]string{"login": {"username": "admin", "password": "newtron"}}

	m.reconcile([]collection.TaskSpec{sshPollTask("p1", "ep-1", "", creds)})
	log.waitFor(t, "p1", collection.SessionUp)
	mu.Lock()
	got := append([]string(nil), keys...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "SHA256:first" {
		t.Fatalf("published keys = %v, want the key trusted on first use", got)
	}
	// The server now delivers the trusted key as the pin: nothing recycles.
	m.reconcile([]collection.TaskSpec{sshPollTask("p1", "ep-1", "SHA256:first", creds)})
	time.Sleep(20 * time.Millisecond)
	if n := dialer.count(); n != 1 {
		t.Fatalf("delivering the trusted pin recycled the session: %d connects", n)
	}

	// The component is replaced and the operator accepts its new key: the
	// changed pin recycles the session onto it.
	dialer.mu.Lock()
	dialer.offered = "SHA256:second"
	dialer.mu.Unlock()
	m.reconcile([]collection.TaskSpec{sshPollTask("p1", "ep-1", "SHA256:second", creds)})
	deadline := time.Now().Add(3 * time.Second)
	for dialer.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if n := dialer.count(); n != 2 {
		t.Fatalf("an operator's new pin did not recycle the session: %d connects", n)
	}
}

// Review of #1032, finding 1: the trusted key reaches the server even when
// its first publish is lost (it republishes while the worklist delivers no
// pin), and a session recycled before the server holds the pin keeps the
// key it trusted rather than trusting whatever is offered next.
func TestSSHPinSurvivesALostPublishAndARecycle(t *testing.T) {
	dialer := &sshFakeDialer{offered: "SHA256:first"}
	log := &transitionLog{}
	var mu sync.Mutex
	published := 0
	m := newSessionManager(map[string]collection.SessionDialer{"ssh": dialer}, sessionHooks{
		transition: log.record,
		hostKey: func(_, _ string, fp string) {
			mu.Lock()
			published++
			mu.Unlock()
		},
	})
	defer m.closeAll()
	creds := map[string]map[string]string{"login": {"username": "admin", "password": "newtron"}}
	m.reconcile([]collection.TaskSpec{sshPollTask("p1", "ep-1", "", creds)})
	log.waitFor(t, "p1", collection.SessionUp)

	// The server never got it: the next tick still delivers no pin, and the
	// node publishes it again.
	m.reconcile([]collection.TaskSpec{sshPollTask("p1", "ep-1", "", creds)})
	mu.Lock()
	n := published
	mu.Unlock()
	if n < 2 {
		t.Fatalf("published the trusted key %d times with the server lacking it, want a republish", n)
	}

	// A credential edit recycles the session before the server holds the
	// pin, and something else now answers: the carried pin refuses it.
	dialer.mu.Lock()
	dialer.offered = "SHA256:mitm"
	dialer.mu.Unlock()
	edited := map[string]map[string]string{"login": {"username": "admin", "password": "rotated"}}
	m.reconcile([]collection.TaskSpec{sshPollTask("p1", "ep-1", "", edited)})
	log.waitFor(t, "p1", collection.SessionRejected)
}
