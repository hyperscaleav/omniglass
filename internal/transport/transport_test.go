package transport_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// The transport registry is code, not a table (ADR-0073): the set of wires the
// platform can speak over is a build-time fact, so the registry is the one
// source the storage validator, the API route, and the node's probe dispatch
// all read. These tests pin the contract the rest of slice 1 hangs off.

func TestRegistryCarriesTheEightTransports(t *testing.T) {
	want := map[string]struct{ held, built bool }{
		"icmp":   {held: false, built: true},
		"tcp":    {held: true, built: true}, // the line protocol holds a session over it (#817)
		"tls":    {held: true, built: true}, // the line protocol over a tls handshake (#1035)
		"udp":    {held: false, built: false},
		"telnet": {held: true, built: true}, // the line protocol behind a login (#1033)
		"ssh":    {held: true, built: true},
		"http":   {held: true, built: true},
		"snmp":   {held: true, built: false},
	}
	all := transport.All()
	if len(all) != len(want) {
		t.Fatalf("registry carries %d transports, want %d", len(all), len(want))
	}
	for _, tr := range all {
		w, ok := want[tr.Name]
		if !ok {
			t.Fatalf("unexpected transport %q", tr.Name)
		}
		if tr.Held != w.held {
			t.Errorf("%s: Held = %v, want %v", tr.Name, tr.Held, w.held)
		}
		if tr.Built != w.built {
			t.Errorf("%s: Built = %v, want %v", tr.Name, tr.Built, w.built)
		}
		if tr.Description == "" {
			t.Errorf("%s: empty description", tr.Name)
		}
	}
}

func TestRegistryOrderIsStable(t *testing.T) {
	// The console picker and the generated docs render the registry in order,
	// so the order is part of the contract: a map-iteration order would flap
	// the UI and the docs diff on every build.
	var names []string
	for _, tr := range transport.All() {
		names = append(names, tr.Name)
	}
	want := []string{"icmp", "tcp", "tls", "udp", "telnet", "ssh", "http", "snmp"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("order = %v, want %v", names, want)
		}
	}
}

func TestByName(t *testing.T) {
	tr, ok := transport.ByName("ssh")
	if !ok || tr.Name != "ssh" {
		t.Fatalf("ByName(ssh) = %+v, %v", tr, ok)
	}
	if _, ok := transport.ByName("carrier-pigeon"); ok {
		t.Fatal("an unknown transport must not resolve")
	}
	if _, ok := transport.ByName(""); ok {
		t.Fatal("the empty name must not resolve")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	// A caller mutating the returned slice must not corrupt the registry.
	first := transport.All()
	first[0].Name = "mutated"
	if again := transport.All(); again[0].Name != "icmp" {
		t.Fatal("All() exposes the registry's backing array")
	}
}

// The transport rung each transport's ladder defaults to (ADR-0158): a
// connect where an open port can be detected, the protocol's handshake for
// ssh, a named request where it cannot (udp, snmp); icmp has none.
func TestRegistryCarriesTheTransportRung(t *testing.T) {
	want := map[string]string{"icmp": "", "tcp": "connect", "tls": "handshake", "udp": "request", "telnet": "connect", "ssh": "handshake", "http": "connect", "snmp": "request"}
	for _, tr := range transport.All() {
		if tr.Rung != want[tr.Name] {
			t.Errorf("%s: rung %q, want %q", tr.Name, tr.Rung, want[tr.Name])
		}
	}
}

// tls is its own transport over the shared TLS primitive (#1035, decision 1
// of #1076): its endpoint is named tls, so a component can hold a raw control
// port and a TLS one side by side, and its transport rung is the handshake.
// The line protocol rides it, so a driver's polls, commands and listeners
// speak lines over its held session.
func TestTLSIsAHeldLineTransportWhoseRungIsTheHandshake(t *testing.T) {
	tr, ok := transport.ByName("tls")
	if !ok {
		t.Fatal("the registry has no tls transport")
	}
	if !tr.Held || !tr.Built || !tr.Line || tr.Rung != "handshake" {
		t.Fatalf("tls = %+v, want held, built, line, rung handshake", tr)
	}
}
