//go:build linux

package node_test

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The line protocol as datagrams end to end (#1037): Postgres, the bus, a
// real node, and the simulated panel's NVP datagram face, on ephemeral
// ports, so two runs from one worktree never collide.

// closedUDPPort is a port nothing in a test binds: privileged, so no
// unprivileged process (a concurrent run's node or fixture) can hold it.
const closedUDPPort = 1

func (s *gateStack) attachUDP(comp, nodeName string, inputs map[string]string) *storage.Endpoint {
	s.t.Helper()
	drv := "newtron-nvp-udp"
	ep, err := s.gw.CreateEndpoint(s.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &nodeName, Inputs: inputs, SecretRead: s.all}, s.all)
	if err != nil {
		s.t.Fatalf("attach %s: %v", drv, err)
	}
	return ep
}

func (s *gateStack) owner() string {
	s.t.Helper()
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.gw.BootstrapOwner(s.ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		s.t.Fatal(err)
	}
	owner, err := s.gw.AuthenticateBearer(s.ctx, hash)
	if err != nil || owner == nil {
		s.t.Fatalf("the owner: %v", err)
	}
	return owner.ID
}

// TestNodeUDPEndpointExchangesAndListensInDatagrams: the shipped
// newtron-nvp-udp driver attached to the panel's datagram face, which drops
// 30% of datagrams each way. The request rung reads up through the loss
// and lands its timing, the status poll lands through its retries, and a
// command actuates; no tcp-open and no session-state land. A push from the
// panel lands with no poll, a datagram from another source to the
// endpoint's port is dropped, and a silent face and a closed port read not
// responsive.
func TestNodeUDPEndpointExchangesAndListensInDatagrams(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	s := newGateStack(t, "127.89.37.1", "edge-udp")
	s.panel.SetDatagramLoss(30)
	s.component("panel-1")
	port := s.port("udp")
	// No listen port: the node binds one of its own (a port picked and
	// released here could be taken by a concurrent run before the node binds
	// it), and the test reads it back from the panel, which learned it from
	// the subscribe.
	ep := s.attachUDP("panel-1", "edge-udp", map[string]string{"host": s.ip, "port": fmt.Sprint(port)})
	s.runNode("edge-udp")

	s.waitFor("panel-1", ep.Name, "endpoint-responsive", is("up"), "up, through the loss")
	s.waitFor("panel-1", ep.Name, "endpoint-reachable", is("up"), "up: the request rung is the verdict")
	s.waitFor("panel-1", ep.Name, "video-input", is("hdmi1"), "hdmi1, polled through the retries")
	if ms, ok := s.metric("panel-1", ep.Name, "udp-response-time"); !ok || ms <= 0 {
		t.Fatalf("udp-response-time = %v (%v), want a positive observation", ms, ok)
	}

	// A command actuates through the loss: the binding declares retries,
	// since SET INPUT names the input.
	owner := s.owner()
	if _, err := s.gw.IssueCommand(s.ctx, owner, "component", "panel-1", "set-input", "", json.RawMessage(`"hdmi3"`), nil); err != nil {
		t.Fatalf("issue set-input: %v", err)
	}
	s.waitFor("panel-1", ep.Name, "video-input", is("hdmi3"), "hdmi3, actuated over udp")
	if v := s.panel.ReadPath("status.video.input"); v != "hdmi3" {
		t.Fatalf("the panel shows %v, want hdmi3", v)
	}

	// Nothing connection-shaped lands for a datagram endpoint.
	if _, ok := s.metric("panel-1", ep.Name, "tcp-open"); ok {
		t.Fatal("a udp endpoint landed tcp-open")
	}
	if st := s.latest("panel-1", ep.Name, "session-state"); st != "" {
		t.Fatalf("a udp endpoint landed session-state %q", st)
	}

	// The push: another source selects dp1 on the panel, and the panel pushes
	// the change to the endpoint's port, its subscriber; it lands long before
	// the 30s poll would.
	s.panel.SetDatagramLoss(0)
	other, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	face := &net.UDPAddr{IP: net.ParseIP(s.ip), Port: port}
	if _, err := other.WriteToUDP([]byte("SET INPUT dp1\r\n"), face); err != nil {
		t.Fatal(err)
	}
	pushed := time.Now()
	s.waitFor("panel-1", ep.Name, "video-input", is("dp1"), "dp1, pushed")
	if took := time.Since(pushed); took > 10*time.Second {
		t.Fatalf("the change took %s, a poll's pace, not a push's", took)
	}

	// A stranger sends the endpoint's port a push of its own: dropped.
	var sources []string
	for _, src := range s.panel.DatagramSources() {
		if src != other.LocalAddr().String() {
			sources = append(sources, src)
		}
	}
	if len(sources) != 1 {
		t.Fatalf("the panel holds sources %v, want the endpoint's one socket", sources)
	}
	node, err := net.ResolveUDPAddr("udp4", sources[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.WriteToUDP([]byte("EVT INPUT usbc\r\n"), node); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if v := s.latest("panel-1", ep.Name, "video-input"); v != "dp1" {
		t.Fatalf("video-input = %q after a stranger's datagram, want dp1 still", v)
	}

	// The face falls silent: not responsive, and the verdict is down.
	s.panel.SetDatagramLoss(100)
	s.waitFor("panel-1", ep.Name, "endpoint-responsive", is("down"), "down: the request goes unanswered")
	s.waitFor("panel-1", ep.Name, "endpoint-reachable", is("down"), "down")
	s.panel.SetDatagramLoss(0)
	s.waitFor("panel-1", ep.Name, "endpoint-responsive", is("up"), "up again")

	// A closed port: a privileged one, which no test process can bind, so
	// the host refuses the request, which the poll's fault names.
	s.component("panel-2")
	ep2 := s.attachUDP("panel-2", "edge-udp", map[string]string{"host": s.ip, "port": fmt.Sprint(closedUDPPort)})
	s.waitFor("panel-2", ep2.Name, "endpoint-responsive", is("down"), "down: the port is refused")
	s.waitFor("panel-2", ep2.Name, "endpoint-reachable", is("down"), "down")
	deadline := time.Now().Add(10 * time.Second)
	for {
		evs, err := s.gw.ListComponentEvents(s.ctx, "panel-2", 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		refused := false
		for _, e := range evs {
			if e.Key == "collection-failed" && strings.Contains(e.Message, "connection refused") {
				refused = true
			}
		}
		if refused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no collection-failed naming the refusal on panel-2: %+v", evs)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
