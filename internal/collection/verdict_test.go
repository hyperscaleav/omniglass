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
		dps    []Datapoint
		wantUp bool
		wantOk bool
	}{
		{"single tcp open", []Datapoint{{Name: DatapointTCPOpen, Value: 1}, {Name: DatapointTCPConnectTime, Value: 3}}, true, true},
		{"single tcp closed", []Datapoint{{Name: DatapointTCPOpen, Value: 0}}, false, true},
		{"single icmp up", []Datapoint{{Name: DatapointICMPReachable, Value: 1}, {Name: DatapointICMPRTTAvg, Value: 2}}, true, true},
		{"single icmp down", []Datapoint{{Name: DatapointICMPReachable, Value: 0}}, false, true},
		{"and of two up", []Datapoint{{Name: DatapointTCPOpen, Value: 1}, {Name: DatapointICMPReachable, Value: 1}}, true, true},
		{"and one down", []Datapoint{{Name: DatapointTCPOpen, Value: 1}, {Name: DatapointICMPReachable, Value: 0}}, false, true},
		{"no reachability metric", []Datapoint{{Name: DatapointTCPConnectTime, Value: 3}}, false, false},
		{"empty", nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			up, ok := InterfaceVerdict(c.dps)
			if up != c.wantUp || ok != c.wantOk {
				t.Fatalf("InterfaceVerdict = (%v,%v), want (%v,%v)", up, ok, c.wantUp, c.wantOk)
			}
		})
	}
}
