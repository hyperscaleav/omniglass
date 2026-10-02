package collection

import "testing"

// TestInterfaceVerdict proves the per-interface verdict is the AND of the
// interface's probe results: up iff every present reachability metric reads 1,
// down if any reads 0, and ok=false when the interface produced no reachability
// metric to judge (nothing to emit). Generalizable to N probes; the tcp/icmp
// case today is the degenerate single probe.
func TestInterfaceVerdict(t *testing.T) {
	cases := []struct {
		name   string
		dps    []Sample
		wantUp bool
		wantOk bool
	}{
		{"single tcp open", []Sample{{Name: SignalTCPOpen, Value: 1}, {Name: SignalTCPConnectTime, Value: 3}}, true, true},
		{"single tcp closed", []Sample{{Name: SignalTCPOpen, Value: 0}}, false, true},
		{"single icmp up", []Sample{{Name: SignalICMPReachable, Value: 1}, {Name: SignalICMPRTTAvg, Value: 2}}, true, true},
		{"single icmp down", []Sample{{Name: SignalICMPReachable, Value: 0}}, false, true},
		{"and of two up", []Sample{{Name: SignalTCPOpen, Value: 1}, {Name: SignalICMPReachable, Value: 1}}, true, true},
		{"and one down", []Sample{{Name: SignalTCPOpen, Value: 1}, {Name: SignalICMPReachable, Value: 0}}, false, true},
		{"no reachability metric", []Sample{{Name: SignalTCPConnectTime, Value: 3}}, false, false},
		{"empty", nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			up, ok := EndpointVerdict(c.dps)
			if up != c.wantUp || ok != c.wantOk {
				t.Fatalf("EndpointVerdict = (%v,%v), want (%v,%v)", up, ok, c.wantUp, c.wantOk)
			}
		})
	}
}
