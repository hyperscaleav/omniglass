//go:build linux

package node_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// snmp's reachability rung end to end (#821): Postgres, the bus, a real node,
// and the simulated panels' v2c faces, on ephemeral ports, so two runs from
// one worktree never collide.

func (s *gateStack) community(name, value string) *storage.Secret {
	s.t.Helper()
	sec, err := s.gw.CreateSecret(s.ctx, "", storage.SecretSpec{Name: name, SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": value}}, s.all, true)
	if err != nil {
		s.t.Fatalf("community %s: %v", name, err)
	}
	return sec
}

func (s *gateStack) attachSNMP(comp, nodeName, ip string, port int, community string) *storage.Endpoint {
	s.t.Helper()
	drv := "snmp-generic"
	ep, err := s.gw.CreateEndpoint(s.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &nodeName,
		Inputs: map[string]string{"host": ip, "port": fmt.Sprint(port), "community": community}, SecretRead: s.all, CanAdmin: true}, s.all)
	if err != nil {
		s.t.Fatalf("attach snmp-generic to %s: %v", comp, err)
	}
	return ep
}

// TestNodeSNMPRungIsAStandardGetAnswered: snmp-generic attached to three
// simulated panels. With the face's community the get answers every tick:
// responsive, the verdict, authenticated, and a timing, no tcp-open, and the
// poll still lands. A wrong community meets the face's silence: not
// responsive and never rejected. The face's refused community is answered
// with authorizationError: rejected, class auth, no further get under that
// fingerprint, and the secret rotated brings it up. A stopped agent reads not
// responsive.
func TestNodeSNMPRungIsAStandardGetAnswered(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	ipGood, ipWrong, ipRefused := "127.89.38.1", "127.89.38.2", "127.89.38.3"
	s := newGateStack(t, ipGood, "edge-snmp", ipWrong, ipRefused)
	s.community("panel-community", "public")
	s.community("panel-wrong", "private")
	revoked := s.community("panel-revoked", "revoked")
	for _, c := range []string{"panel-good", "panel-wrong", "panel-refused"} {
		s.component(c)
	}
	port := func(ip string) int { return panelPort(t, s.panels[ip], "snmp") }
	good := s.attachSNMP("panel-good", "edge-snmp", ipGood, port(ipGood), "panel-community")
	wrong := s.attachSNMP("panel-wrong", "edge-snmp", ipWrong, port(ipWrong), "panel-wrong")
	refused := s.attachSNMP("panel-refused", "edge-snmp", ipRefused, port(ipRefused), "panel-revoked")
	s.runNode("edge-snmp")

	s.waitFor("panel-good", good.Name, "endpoint-responsive", is("up"), "up: the get answered")
	s.waitFor("panel-good", good.Name, "endpoint-reachable", is("up"), "up: the rung is the verdict")
	s.waitFor("panel-good", good.Name, "endpoint-authenticated", is("yes"), "yes: the community was answered")
	if ms, ok := s.metric("panel-good", good.Name, "snmp-response-time"); !ok || ms <= 0 {
		t.Fatalf("snmp-response-time = %v (%v), want a positive observation", ms, ok)
	}
	if _, ok := s.metric("panel-good", good.Name, "tcp-open"); ok {
		t.Fatal("an snmp endpoint landed tcp-open")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, ok := s.metric("panel-good", good.Name, "uptime"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the snmp-generic poll landed no uptime beside the rung")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// A wrong v2c community: dropped silently, so not responsive, the
	// verdict down, and never a refusal.
	s.waitFor("panel-wrong", wrong.Name, "endpoint-responsive", is("down"), "down: the face drops a wrong community")
	s.waitFor("panel-wrong", wrong.Name, "endpoint-reachable", is("down"), "down")
	if r := s.latest("panel-wrong", wrong.Name, "endpoint-rejection"); r != "" && r != "none" {
		t.Fatalf("a wrong community gated the endpoint: %q", r)
	}
	if a := s.latest("panel-wrong", wrong.Name, "endpoint-authenticated"); a != "" {
		t.Fatalf("a silent drop said something of the community: %q", a)
	}

	// The refused community: answered with authorizationError, gated at the
	// session rung, and not sent again under that fingerprint.
	s.waitFor("panel-refused", refused.Name, "endpoint-authenticated", is("no"), "no: authorizationError")
	s.waitFor("panel-refused", refused.Name, "endpoint-rejection", func(v string) bool {
		return strings.HasPrefix(v, "authenticated auth ")
	}, "the session rung gated, class auth")
	agent := s.panels[ipRefused]
	time.Sleep(500 * time.Millisecond) // a get already in flight lands first
	s.settles("gets to the refusing agent under the refused community", agent.SNMPRequests, agent.SNMPRequests())

	// The community rotated in the secret store: up within a tick.
	if _, err := s.gw.UpdateSecret(s.ctx, "", revoked.ID, storage.SecretPatch{Fields: map[string]string{"community": "public"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	s.waitFor("panel-refused", refused.Name, "endpoint-authenticated", is("yes"), "yes after the rotation")
	s.waitFor("panel-refused", refused.Name, "endpoint-rejection", is("none"), "the gate lifted")

	// The agent stops: not responsive, the verdict down.
	s.panels[ipGood].StopSNMP()
	s.waitFor("panel-good", good.Name, "endpoint-responsive", is("down"), "down: the agent stopped")
	s.waitFor("panel-good", good.Name, "endpoint-reachable", is("down"), "down")
}
