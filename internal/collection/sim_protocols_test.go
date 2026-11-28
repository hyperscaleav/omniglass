package collection

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// A hit's protocol is one the hub accepts (SimProtocols), and a simulator that
// gains a protocol must not have its hits dropped by a hub that was never told
// (#1366: websocket was, for as long as the hub kept its own list). The
// vocabulary leaves out two protocols a face can name that never report a hit:
// icmp, which the kernel answers so the simulator sees nothing, and https,
// which no face serves (https is http with tls, and reports as http). The
// simulator's own sources are held to the vocabulary in sim
// (TestEveryProtocolTheSimulatorReportsAndServesIsOneTheHubCounts).
func TestSimProtocolsAreTheOnesAHitIsReportedBy(t *testing.T) {
	want := []string{"http", "snmp", "ssh", "tcp", "telnet", "udp", "websocket"}
	got := SimProtocols()
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("SimProtocols() = %v, want %v", got, want)
	}
	for _, p := range want {
		if !IsSimProtocol(p) {
			t.Errorf("IsSimProtocol(%q) = false", p)
		}
	}
	for _, p := range []string{"", "https", "icmp", "HTTP", "made-up"} {
		if IsSimProtocol(p) {
			t.Errorf("IsSimProtocol(%q) = true, want only a protocol a device reports a hit by", p)
		}
	}
}

func TestSimProtocolsIsACopy(t *testing.T) {
	a := SimProtocols()
	a[0] = "mutated"
	if SimProtocols()[0] == "mutated" || IsSimProtocol("mutated") {
		t.Error("SimProtocols shares its backing array")
	}
}

// Every transport an outpost dials reaches the simulator as one of its protocols,
// except icmp, which the kernel answers so the simulator never sees it, and a
// push transport, which no outpost dials. A transport added to the registry fails
// here until it is placed.
func TestSimProtocolOfPlacesEveryDialedTransport(t *testing.T) {
	want := map[string]string{
		"http": "http", "https": "http", "websocket": "websocket", "wss": "websocket",
		"tcp": "tcp", "tls": "tcp", "ssh": "ssh", "telnet": "telnet", "udp": "udp", "snmp": "snmp",
	}
	for _, tr := range transport.All() {
		got, ok := SimProtocolOf(tr.Name)
		switch {
		case tr.Name == "icmp" || tr.Push:
			if ok {
				t.Errorf("SimProtocolOf(%q) = %q, want none", tr.Name, got)
			}
		case !ok || got != want[tr.Name]:
			t.Errorf("SimProtocolOf(%q) = %q, %v, want %q", tr.Name, got, ok, want[tr.Name])
		}
		if ok && !IsSimProtocol(got) {
			t.Errorf("SimProtocolOf(%q) = %q, not a protocol the simulator counts", tr.Name, got)
		}
	}
	if _, ok := SimProtocolOf("made-up"); ok {
		t.Error("an unknown transport maps to a protocol")
	}
}
