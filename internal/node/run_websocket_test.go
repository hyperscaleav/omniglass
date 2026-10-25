//go:build linux

package node_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A websocket end to end (#1034): Postgres, the bus, a real node, and the
// simulated Kestrel bar's control socket over wss. The shipped kestrel-ws
// driver holds one socket: the subscription is armed on connect, a change
// on the bar is pushed and lands within a second, the polls and a command
// are answered by the id the node minted into each while pushes interleave
// (the command settling from its answer), a dropped socket recovers and
// arms again, a refused credential reads rejected after one upgrade, across
// a restart, until the secret is fixed in place, and a replaced bar's key is
// held beneath the certificate gate until :retry with certificate trusts it.

// facePort is the port a simulated component serves its face of protocol
// on.
func (s *httpStack) facePort(name, protocol string) int {
	s.t.Helper()
	for _, f := range s.devices[name].Faces() {
		if f.Protocol == protocol {
			return int(f.Addr.Port())
		}
	}
	s.t.Fatalf("%s serves no %s face: %+v", name, protocol, s.devices[name].Faces())
	return 0
}

func TestNodeWebSocketArmsPushesAnswersByIDAndRecovers(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var barProfile string
	s := newHTTPStack(t, "edge-ws", nil, func(store *profile.Store) []string {
		barProfile = driverProfile(t, store, "kestrel-api")
		return []string{barProfile}
	})
	bar := s.devices[barProfile]
	s.component("bar-ws")
	login := s.secret("bar-ws-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	ep := s.attachHTTP("bar-ws", "kestrel-ws", "edge-ws", map[string]string{
		"host": s.ips[barProfile], "port": fmt.Sprint(s.facePort(barProfile, "websocket")), "login": "bar-ws-login"})
	if ep.Name != "wss" {
		t.Fatalf("endpoint name %q, want wss", ep.Name)
	}
	s.runNode("edge-ws")

	s.waitFor("bar-ws", ep.Name, "session-state", is("up"), "up: the socket held")
	s.waitFor("bar-ws", ep.Name, "endpoint-authenticated", is("yes"), "yes: the upgrade's credential accepted")
	s.waitFor("bar-ws", ep.Name, "tls-certificate", is(spkiOf(bar.HTTPSCertificate().RawSubjectPublicKeyInfo)), "the bar's key, trusted on first use")
	if n := bar.WebSocketSubscribes(); n != 1 {
		t.Fatalf("subscribes = %d, want the arm sent on connect", n)
	}
	// The polls are answered by their ids: the power state, from a get.
	s.waitFor("bar-ws", ep.Name, "power-state", is(fmt.Sprint(bar.ReadPath("status.device.power"))), "the power state, polled over the socket")

	// A change on the bar is pushed and lands within a second.
	setVolume(t, bar, 37)
	if took := s.waitVolume("bar-ws", ep.Name, 37); took > time.Second {
		t.Fatalf("the change landed %v after it happened, want within a second", took)
	}

	// A command rides the socket: the bar pushes the change, then answers
	// the set by its id, and the answer settles the command.
	owner := s.owner()
	if _, err := s.gw.IssueCommand(s.ctx, owner, "component", "bar-ws", "set-volume", "", json.RawMessage(`42`), nil); err != nil {
		t.Fatalf("issue set-volume: %v", err)
	}
	s.waitVolume("bar-ws", ep.Name, 42)
	deadline := time.Now().Add(10 * time.Second)
	for s.commandStatus("set-volume") != "settled" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := s.commandStatus("set-volume"); got != "settled" {
		t.Fatalf("set-volume = %q, want settled from its answer", got)
	}
	if got := fmt.Sprint(bar.ReadPath("status.audio.volume")); got != "42" {
		t.Fatalf("the bar shows volume %s, want 42", got)
	}

	// The bar drops the socket: the session recovers, arms again, and
	// pushes land once more.
	if n := bar.DropWebSockets(); n != 1 {
		t.Fatalf("dropped %d sockets, want the node's one", n)
	}
	s.settles("subscribes after the drop", bar.WebSocketSubscribes, 2)
	setVolume(t, bar, 43)
	s.waitVolume("bar-ws", ep.Name, 43)
	if states := s.sessionStates("bar-ws", ep.Name); !slices.Contains(states, "recovering") || states[len(states)-1] != "up" {
		t.Fatalf("session states = %v, want a recovery back to up", states)
	}

	// A wrong password: the fingerprint moves, the socket upgrades once, is
	// refused, and reads rejected with the reason; it does not try again
	// under the fingerprint, across a node restart.
	refusals := bar.WebSocketRefusals
	before := refusals()
	if _, err := s.gw.UpdateSecret(s.ctx, "", login.ID, storage.SecretPatch{Fields: map[string]string{"password": "wrong-password"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	s.waitFor("bar-ws", ep.Name, "session-state", is("rejected"), "rejected: the upgrade's credential refused")
	s.waitFor("bar-ws", ep.Name, "session-reason", is("auth: the component refused the credential (401)"), "the refusal's reason")
	s.waitFor("bar-ws", ep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "authenticated auth ") }, "the auth gate")
	s.settles("upgrades refused with the wrong password", refusals, before+1)
	s.runNode("edge-ws")
	s.settles("upgrades refused after a node restart", refusals, before+1)

	// Fixed in place: the gate lifts and the socket comes back up.
	if _, err := s.gw.UpdateSecret(s.ctx, "", login.ID, storage.SecretPatch{Fields: map[string]string{"password": "kestrel"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	s.waitFor("bar-ws", ep.Name, "session-state", is("up"), "up after the fix")
	setVolume(t, bar, 44)
	s.waitVolume("bar-ws", ep.Name, 44)

	// The bar is replaced: its new key is refused at the handshake, the
	// socket's reconnect is held beneath the certificate gate, and no
	// upgrade reaches the bar until the operator trusts the key with :retry
	// and certificate.
	pinned := spkiOf(bar.HTTPSCertificate().RawSubjectPublicKeyInfo)
	if err := bar.RotateHTTPSKey(); err != nil {
		t.Fatal(err)
	}
	offered := spkiOf(bar.HTTPSCertificate().RawSubjectPublicKeyInfo)
	bar.DropWebSockets()
	s.waitFor("bar-ws", ep.Name, "endpoint-rejection", func(v string) bool {
		return strings.HasPrefix(v, "responsive certificate ") && strings.HasSuffix(v, pinned+" "+offered)
	}, "the certificate gate naming both keys")
	attempts := bar.WebSocketAttempts
	s.settles("upgrades beneath the certificate gate", attempts, attempts())
	if _, err := s.gw.RetryEndpoint(s.ctx, "", ep.ID, storage.RetryPin{Certificate: offered}, s.all, s.all); err != nil {
		t.Fatalf("retry with the offered key: %v", err)
	}
	s.waitFor("bar-ws", ep.Name, "endpoint-rejection", is("none"), "none, the key trusted")
	s.waitFor("bar-ws", ep.Name, "session-state", is("up"), "up on the trusted key")
	setVolume(t, bar, 45)
	s.waitVolume("bar-ws", ep.Name, 45)
}
