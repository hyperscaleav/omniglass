//go:build linux

package node_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The connection fingerprint and the layered gate end to end (#1056 ruling,
// ADR-0157): Postgres, the bus, real nodes, and the simulated panel.

// gateStack is one test's server, panel and node.
type gateStack struct {
	t      *testing.T
	ctx    context.Context
	gw     *storage.PG
	all    scope.Set
	panel  *sim.Device
	panels map[string]*sim.Device // by loopback address
	ip     string
	apiURL string
	token  string
	stop   func()
}

// stackSeq gives every stack its own loopback address, so a rerun
// (-count) never binds where the last run's panel is still letting go.
var stackSeq atomic.Int32

func newGateStack(t *testing.T, ip, nodeName string, extraIPs ...string) *gateStack {
	t.Helper()
	if n := stackSeq.Add(1); n > 1 {
		parts := strings.Split(ip, ".")
		ip = fmt.Sprintf("127.%d.%d.%s", 90+int(n)/250, int(n)%250, parts[3])
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.PrivilegedPortShift(20000))
	t.Cleanup(engine.StopAll)
	snaps := []sim.DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip, ProfileName: nvpProfile(t, store), Version: 1}}
	for i, extra := range extraIPs {
		snaps = append(snaps, sim.DeviceSnapshot{DeviceID: fmt.Sprintf("panel-%d", i+2), Hostname: fmt.Sprintf("panel-%d", i+2),
			LoopbackIP: extra, ProfileName: nvpProfile(t, store), Version: 1})
	}
	if err := engine.Sync(snaps); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: nodeName}, all, all); err != nil {
		t.Fatal(err)
	}
	token := "enroll-token-" + nodeName
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", nodeName, hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Shutdown)
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	t.Cleanup(apiSrv.Close)
	s := &gateStack{t: t, ctx: ctx, gw: gw, all: all, ip: ip, apiURL: apiSrv.URL, token: token, panels: map[string]*sim.Device{}}
	for _, d := range engine.Devices() {
		s.panels[d.LoopbackIP.String()] = d
	}
	s.panel = s.panels[ip]
	t.Cleanup(func() {
		if s.stop != nil {
			s.stop()
		}
	})
	return s
}

// runNode starts the node, stopping one already running: a restart.
func (s *gateStack) runNode(name string) {
	if s.stop != nil {
		s.stop()
	}
	runCtx, cancel := context.WithCancel(s.ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: s.apiURL, Name: name, Token: s.token, HeartbeatEvery: 100 * time.Millisecond})
		ran <- err
	}()
	s.stop = func() {
		cancel()
		<-ran
		s.stop = nil
	}
}

func (s *gateStack) latest(comp, instance, key string) string {
	v, err := s.gw.LatestProperty(s.ctx, comp, key, instance)
	if err != nil || v == nil {
		return ""
	}
	return v.Value
}

func (s *gateStack) waitFor(comp, instance, key string, ok func(string) bool, what string) {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ok(s.latest(comp, instance, key)) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	s.t.Fatalf("%s on %s[%s] = %q, want %s", key, comp, instance, s.latest(comp, instance, key), what)
}

// settles waits for count to reach want, then holds a second to prove it
// stays there.
func (s *gateStack) settles(what string, count func() int64, want int64) {
	s.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for count() < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(time.Second)
	if got := count(); got != want {
		s.t.Fatalf("%s = %d, want %d", what, got, want)
	}
}

func is(want string) func(string) bool { return func(v string) bool { return v == want } }

func (s *gateStack) component(name string) {
	s.t.Helper()
	if _, err := s.gw.CreateComponent(s.ctx, "", storage.ComponentSpec{Name: name}, s.all, s.all, s.all, s.all); err != nil {
		s.t.Fatal(err)
	}
}

func (s *gateStack) attach(comp, drv, nodeName string, port int) *storage.Endpoint {
	s.t.Helper()
	ep, err := s.gw.CreateEndpoint(s.ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs:     map[string]string{"host": s.ip, "port": fmt.Sprint(port), "login": "panel-login"},
		SecretRead: s.all, CanAdmin: true,
	}, s.all)
	if err != nil {
		s.t.Fatalf("attach %s: %v", drv, err)
	}
	return ep
}

// TestTheGateLiftsOnlyWhenTheFingerprintChanges: the silent telnet face (it
// hangs up on a wrong password with no text) and a wrong password.
func TestTheGateLiftsOnlyWhenTheFingerprintChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	s := newGateStack(t, "127.89.16.5", "edge-i")
	s.panel.SetTelnetSilentReject(true)
	s.component("panel-1")
	login, err := s.gw.CreateSecret(s.ctx, "", storage.SecretSpec{
		Name: "panel-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "wrong"},
	}, s.all, true)
	if err != nil {
		t.Fatal(err)
	}
	official, err := s.gw.GetDriver(s.ctx, "newtron-nvp-telnet")
	if err != nil {
		t.Fatal(err)
	}
	custom, err := s.gw.CreateDriver(s.ctx, "", storage.Driver{Name: "panel-telnet", Label: "Panel Telnet", Version: "1.0.0", Spec: official.Spec})
	if err != nil {
		t.Fatal(err)
	}
	ep := s.attach("panel-1", "panel-telnet", "edge-i", 23+20000)
	logins := s.panel.TelnetLogins
	s.runNode("edge-i")

	// The login budget: three silent hang-ups, then rejected as auth.
	s.waitFor("panel-1", ep.Name, "session-state", is("rejected"), "rejected")
	s.waitFor("panel-1", ep.Name, "session-reason", func(v string) bool {
		return strings.HasPrefix(v, "auth: the component hung up after the login 3 times")
	}, "the login budget's refusal")
	s.waitFor("panel-1", ep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "authenticated auth ") }, "the published gate")
	s.settles("logins", logins, 3)

	// An unrelated edit moves nothing the fingerprint covers: no login.
	label := "Lobby panel"
	if _, err := s.gw.UpdateEndpoint(s.ctx, "", ep.ID, storage.EndpointPatch{Label: &label}, s.all, s.all); err != nil {
		t.Fatal(err)
	}
	s.settles("logins after a label edit", logins, 3)

	// A restarted node honours the gate it did not see happen: no login.
	s.runNode("edge-i")
	s.settles("logins after a node restart", logins, 3)

	// An operator's retry moves the fingerprint: a fresh login budget.
	if _, err := s.gw.RetryEndpoint(s.ctx, "", ep.ID, "", s.all, s.all); err != nil {
		t.Fatal(err)
	}
	s.settles("logins after a retry", logins, 6)

	// A change to the driver's login declaration, re-derived (#823).
	spec := strings.Replace(string(official.Spec), `"auth rejected"`, `"Login incorrect"`, 1)
	if _, err := s.gw.UpdateDriver(s.ctx, "", custom.ID, storage.DriverPatch{Spec: []byte(spec)}); err != nil {
		t.Fatal(err)
	}
	s.settles("logins after a login-declaration change", logins, 9)

	// The secret rotated in the store, the endpoint row untouched: up within
	// a tick, and the gate published as lifted.
	if _, err := s.gw.UpdateSecret(s.ctx, "", login.ID, storage.SecretPatch{Fields: map[string]string{"username": "admin", "password": "newtron"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	s.waitFor("panel-1", ep.Name, "session-state", is("up"), "up after the rotation")
	s.waitFor("panel-1", ep.Name, "endpoint-authenticated", is("yes"), "authenticated")
	s.waitFor("panel-1", ep.Name, "endpoint-rejection", is("none"), "the gate lifted")

	// A driver-attached endpoint's address, port and transport change by
	// re-attaching: the component reached over ssh holds a new session.
	if err := s.gw.DeleteEndpoint(s.ctx, "", ep.ID, s.all, s.all); err != nil {
		t.Fatal(err)
	}
	sshEp := s.attach("panel-1", "newtron-nvp-ssh", "edge-i", 22+20000)
	s.waitFor("panel-1", sshEp.Name, "session-state", is("up"), "up over ssh")
}

// TestAWrongPortIsUnreachableNeverAuth: the transport rung is the gate, so a
// port nothing listens on reads down at the transport, retryably, never as
// an auth refusal, and no login is ever tried.
func TestAWrongPortIsUnreachableNeverAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	s := newGateStack(t, "127.89.16.6", "edge-p")
	s.component("panel-2")
	if _, err := s.gw.CreateSecret(s.ctx, "", storage.SecretSpec{
		Name: "panel-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "newtron"},
	}, s.all, true); err != nil {
		t.Fatal(err)
	}
	ep := s.attach("panel-2", "newtron-nvp-telnet", "edge-p", 2399)
	s.runNode("edge-p")
	s.waitFor("panel-2", ep.Name, "session-state", is("down"), "down (the budget spent on refused connects)")
	if got := s.latest("panel-2", ep.Name, "session-reason"); !strings.Contains(got, "refused") || strings.HasPrefix(got, "auth") {
		t.Fatalf("session-reason = %q, want a refused connect, never auth", got)
	}
	if got := s.latest("panel-2", ep.Name, "endpoint-rejection"); got != "" && got != "none" {
		t.Fatalf("a wrong port published a gate: %q", got)
	}
	if n := s.panel.TelnetLogins(); n != 0 {
		t.Fatalf("a wrong port tried %d logins", n)
	}
}

// TestTheSSHProbeCredentialIsGated (#1060): a bare ssh endpoint whose params
// carry a wrong password is tried once; later probes still run the key
// exchange (responds stays observed) but send no credential while the
// fingerprint holds, across a node restart too. A params edit (a new
// fingerprint) tries again, and the right password authenticates.
func TestTheSSHProbeCredentialIsGated(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	s := newGateStack(t, "127.89.16.7", "edge-h")
	s.component("panel-3")
	comp, nodeName := "panel-3", "edge-h"
	params := func(pw string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"target":"%s:%d","username":"admin","password":%q}`, s.ip, 22+20000, pw))
	}
	ep, err := s.gw.CreateEndpoint(s.ctx, "", storage.EndpointSpec{Transport: "ssh", Component: &comp, Node: &nodeName, Params: params("wrong")}, s.all)
	if err != nil {
		t.Fatal(err)
	}
	s.runNode(nodeName)
	s.waitFor(comp, ep.Name, "endpoint-authenticated", is("no"), "no")
	s.waitFor(comp, ep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "authenticated auth ") }, "the published gate")
	s.waitFor(comp, ep.Name, "endpoint-responsive", is("up"), "responsive (the key exchange still runs)")
	s.settles("ssh passwords tried", s.panel.SSHPasswordTries, 1)
	s.runNode(nodeName)
	s.settles("ssh passwords tried after a restart", s.panel.SSHPasswordTries, 1)

	if _, err := s.gw.UpdateEndpoint(s.ctx, "", ep.ID, storage.EndpointPatch{Params: params("newtron")}, s.all, s.all); err != nil {
		t.Fatal(err)
	}
	s.waitFor(comp, ep.Name, "endpoint-authenticated", is("yes"), "yes on the new fingerprint")
	s.waitFor(comp, ep.Name, "endpoint-rejection", is("none"), "the gate lifted")
}

// TestADriverEndpointEditsItsAddressInPlace (#1061 ruling): no re-attach.
// The endpoint starts on a wrong port (down at the transport, no login
// tried); its port edited in place connects and authenticates within a
// tick; a label edit through the same route recycles nothing; its address
// edited in place moves the healthy session to the new component.
func TestADriverEndpointEditsItsAddressInPlace(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	n := 200 + stackSeq.Add(1)%50
	ipA, ipB := fmt.Sprintf("127.91.%d.8", n), fmt.Sprintf("127.91.%d.9", n)
	s := newGateStack(t, ipA, "edge-e", ipB)
	s.ip = s.panel.LoopbackIP.String()
	a := s.panel
	var b *sim.Device
	for addr, d := range s.panels {
		if d != a {
			b, ipB = d, addr
		}
	}
	s.component("panel-e")
	if _, err := s.gw.CreateSecret(s.ctx, "", storage.SecretSpec{
		Name: "panel-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "newtron"},
	}, s.all, true); err != nil {
		t.Fatal(err)
	}
	ep := s.attach("panel-e", "newtron-nvp-telnet", "edge-e", 2399)
	s.runNode("edge-e")
	edit := func(p storage.EndpointPatch) {
		t.Helper()
		p.SecretRead, p.CanAdmin = s.all, true
		if _, err := s.gw.UpdateEndpoint(s.ctx, "", ep.ID, p, s.all, s.all); err != nil {
			t.Fatalf("edit in place: %v", err)
		}
	}

	s.waitFor("panel-e", ep.Name, "session-state", is("down"), "down on the wrong port")
	if n := a.TelnetLogins(); n != 0 {
		t.Fatalf("a wrong port tried %d logins", n)
	}

	edit(storage.EndpointPatch{Inputs: map[string]string{"port": fmt.Sprint(23 + 20000)}})
	s.waitFor("panel-e", ep.Name, "session-state", is("up"), "up on the port edited in place")
	s.waitFor("panel-e", ep.Name, "endpoint-authenticated", is("yes"), "authenticated")
	logins := a.TelnetLogins()

	label := "Lobby panel"
	edit(storage.EndpointPatch{Label: &label})
	s.settles("logins after a label edit", a.TelnetLogins, logins)

	edit(storage.EndpointPatch{Inputs: map[string]string{"host": ipB}})
	s.settles("logins on the new address", b.TelnetLogins, 1)
	s.waitFor("panel-e", ep.Name, "session-state", is("up"), "up on the new address")
	if reg := a.TelnetSession(); reg != nil && reg.SessionCount() != 0 {
		t.Fatalf("the old address still holds %d sessions", reg.SessionCount())
	}
}
