//go:build linux

package node_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The line protocol over tls end to end (#1035): Postgres, the bus, a real
// node, and the simulated panel's TLS face, on ephemeral ports.

// panelKey is the public key fingerprint of the certificate the panel's tls
// face serves now, computed independently of the code under test.
func panelKey(t *testing.T, panel *sim.Device) string {
	t.Helper()
	c := panel.TLSCertificate()
	if c == nil {
		t.Fatal("the panel serves no tls certificate")
	}
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// tenDayCertificates gives the panel's tls face a ten-day window.
func tenDayCertificates(store *profile.Store) {
	for _, p := range store.Devices {
		for i := range p.Interfaces {
			if p.Interfaces[i].TLS && p.Interfaces[i].Protocol == "tcp" {
				p.Interfaces[i].TLSValidityDays = 10
			}
		}
	}
}

func (s *gateStack) attachTLS(comp, nodeName string, port int, extra map[string]string) *storage.Endpoint {
	s.t.Helper()
	drv := "newtron-nvp-tls"
	inputs := map[string]string{"host": s.ip, "port": fmt.Sprint(port)}
	for k, v := range extra {
		inputs[k] = v
	}
	ep, err := s.gw.CreateEndpoint(s.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &nodeName, Inputs: inputs, SecretRead: s.all}, s.all)
	if err != nil {
		s.t.Fatalf("attach %s: %v", drv, err)
	}
	return ep
}

func (s *gateStack) metric(comp, instance, key string) (float64, bool) {
	m, err := s.gw.LatestMetricInstance(s.ctx, comp, key, instance)
	if err != nil || m == nil {
		return 0, false
	}
	return m.Value, true
}

// TestNodeTLSSessionPinsTheKeyAndRefusesAChange: the shipped newtron-nvp-tls
// driver (pin on first use) attached to the panel's TLS face. The session
// trusts the panel's key, polls, actuates a command, receives a push, and
// recovers after the face drops it; the probe lands the handshake time and
// the certificate's expiry and days left. The panel is replaced (a new key):
// the endpoint reads rejected, class certificate, naming both keys, only the
// TCP connect runs beneath the gate (no handshake reaches the new key), and
// a restarted node runs none either. The operator accepts the offered key
// with :retry: the session comes up on it.
func TestNodeTLSSessionPinsTheKeyAndRefusesAChange(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	s := newGateStackWith(t, "127.89.16.8", "edge-tls", tenDayCertificates)
	s.component("panel-1")
	port := s.port("tls")
	ep := s.attachTLS("panel-1", "edge-tls", port, nil)
	s.runNode("edge-tls")

	first := panelKey(t, s.panel)
	s.waitFor("panel-1", ep.Name, "session-state", is("up"), "up")
	s.waitFor("panel-1", ep.Name, "tls-certificate", is(first), "the panel's key, trusted on first use")
	s.waitFor("panel-1", ep.Name, "video-input", is("hdmi1"), "hdmi1, polled over tls")

	// The worklist delivers the trusted key back as the pin.
	deadline := time.Now().Add(5 * time.Second)
	for {
		wl, err := s.gw.NodeWorklist(s.ctx, "edge-tls")
		if err != nil {
			t.Fatal(err)
		}
		pinned := ""
		for _, task := range wl.Tasks {
			if task.CertificatePin != "" {
				pinned = task.CertificatePin
			}
		}
		if pinned == first {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worklist pin = %q, want %q", pinned, first)
		}
		time.Sleep(25 * time.Millisecond)
	}

	// The probe: the handshake rung's timing, and a certificate that expires
	// in ten days reads ten days left, its not-after the expiry property.
	notAfter := s.panel.TLSCertificate().NotAfter.UTC().Format(time.RFC3339)
	s.waitFor("panel-1", ep.Name, "tls-certificate-expiry", is(notAfter), "the certificate's not-after")
	s.waitFor("panel-1", ep.Name, "endpoint-responsive", is("up"), "responsive")
	if days, ok := s.metric("panel-1", ep.Name, "tls-certificate-days-left"); !ok || days < 9.9 || days > 10 {
		t.Fatalf("tls-certificate-days-left = %v (%v), want 10", days, ok)
	}
	if ms, ok := s.metric("panel-1", ep.Name, "tls-handshake-time"); !ok || ms <= 0 {
		t.Fatalf("tls-handshake-time = %v (%v), want a positive observation", ms, ok)
	}

	// A command actuates over the held tls session, and the push it causes
	// lands through the listener.
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.gw.BootstrapOwner(s.ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	owner, err := s.gw.AuthenticateBearer(s.ctx, hash)
	if err != nil || owner == nil {
		t.Fatalf("the owner: %v", err)
	}
	if _, err := s.gw.IssueCommand(s.ctx, owner.ID, "component", "panel-1", "set-input", "", json.RawMessage(`"hdmi2"`), nil); err != nil {
		t.Fatalf("issue set-input: %v", err)
	}
	s.waitFor("panel-1", ep.Name, "video-input", is("hdmi2"), "hdmi2, actuated over tls")

	// The face drops: the session recovers onto the same key.
	s.panel.StopTCPSession()
	s.waitFor("panel-1", ep.Name, "session-state", func(v string) bool { return v == "recovering" || v == "down" }, "recovering")
	if err := s.panel.StartTLSSession(port); err != nil {
		t.Fatal(err)
	}
	s.waitFor("panel-1", ep.Name, "session-state", is("up"), "up again on the same key")

	// The panel is replaced: a new key at the same address.
	s.panel.StopTCPSession()
	if err := s.panel.RotateTLSKey(); err != nil {
		t.Fatal(err)
	}
	if err := s.panel.StartTLSSession(port); err != nil {
		t.Fatal(err)
	}
	second := panelKey(t, s.panel)
	s.waitFor("panel-1", ep.Name, "session-state", is("rejected"), "rejected")
	s.waitFor("panel-1", ep.Name, "session-reason", func(v string) bool {
		return strings.HasPrefix(v, "certificate: ") && strings.Contains(v, first) && strings.Contains(v, second)
	}, "certificate naming both keys")
	s.waitFor("panel-1", ep.Name, "endpoint-rejection", func(v string) bool {
		f := strings.Fields(v)
		return len(f) == 5 && f[0] == "responsive" && f[1] == "certificate" && f[3] == first && f[4] == second
	}, "the gate at the transport rung, carrying both keys")
	if got := s.latest("panel-1", ep.Name, "tls-certificate"); got != first {
		t.Fatalf("a refused key replaced the pin: %q", got)
	}

	// Beneath the gate only the TCP connect runs: no handshake (and so no
	// poll, command or arm) reaches the new key, tick after tick, across a
	// node restart too.
	handshakes := func() int64 { return s.panel.TLSHandshakes() }
	time.Sleep(500 * time.Millisecond) // let the refused handshakes in flight finish
	held := handshakes()
	s.settles("handshakes beneath the gate", handshakes, held)
	s.runNode("edge-tls")
	s.settles("handshakes after a node restart", handshakes, held)

	// The operator accepts the offered key: the session comes up on it.
	if _, err := s.gw.RetryEndpoint(s.ctx, "", ep.ID, storage.RetryPin{Certificate: second}, s.all, s.all); err != nil {
		t.Fatalf("retry with the offered key: %v", err)
	}
	s.waitFor("panel-1", ep.Name, "session-state", is("up"), "up on the accepted key")
	s.waitFor("panel-1", ep.Name, "tls-certificate", is(second), "the accepted key")
	s.waitFor("panel-1", ep.Name, "endpoint-rejection", is("none"), "the gate lifted")
}

// TestNodeTLSModesVerifyAndSkip: the mode is a driver input edited in place.
// Verify against the panel's self-signed certificate is a certificate
// refusal with its reason, never a timeout; skip connects, and the endpoint
// reads that verification is skipped; each edit moves the fingerprint, so a
// gate lifts without a retry.
func TestNodeTLSModesVerifyAndSkip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	s := newGateStack(t, "127.89.16.9", "edge-tlsm")
	s.component("panel-1")
	ep := s.attachTLS("panel-1", "edge-tlsm", s.port("tls"), map[string]string{"tls-mode": "verify"})
	s.runNode("edge-tlsm")

	s.waitFor("panel-1", ep.Name, "session-state", is("rejected"), "rejected under verify")
	s.waitFor("panel-1", ep.Name, "session-reason", func(v string) bool {
		return strings.HasPrefix(v, "certificate: the certificate failed verification") && strings.Contains(v, "unknown authority")
	}, "certificate, the verification's reason")
	s.waitFor("panel-1", ep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "responsive certificate ") }, "the gate")
	if got := s.latest("panel-1", ep.Name, "tls-certificate"); got != "" {
		t.Fatalf("verify trusted a key on first use: %q", got)
	}

	edit := func(mode string) {
		t.Helper()
		if _, err := s.gw.UpdateEndpoint(s.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"tls-mode": mode}, SecretRead: s.all}, s.all, s.all); err != nil {
			t.Fatalf("edit the mode to %s: %v", mode, err)
		}
	}
	edit("skip")
	s.waitFor("panel-1", ep.Name, "session-state", is("up"), "up, verification skipped")
	s.waitFor("panel-1", ep.Name, "endpoint-rejection", is("none"), "the gate lifted by the edit")
	s.waitFor("panel-1", ep.Name, "video-input", is("hdmi1"), "hdmi1 polled with verification skipped")
	got, err := s.gw.GetEndpoint(s.ctx, ep.ID, s.all)
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]string
	_ = json.Unmarshal(got.Params, &params)
	if params["tls-mode"] != "skip" {
		t.Fatalf("the endpoint reads mode %q, want skip visible on it", params["tls-mode"])
	}
}
