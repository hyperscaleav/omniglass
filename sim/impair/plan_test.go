package impair

import (
	"net/netip"
	"strings"
	"testing"
)

func TestImpairmentValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		imp  Impairment
		want string
	}{
		{"healthy", Impairment{}, ""},
		{"unplugged", Impairment{Unplugged: true}, ""},
		{"degraded link", Impairment{LatencyMs: 200, JitterMs: 50, LossPct: 5, RateKbit: 512}, ""},
		{"negative latency", Impairment{LatencyMs: -1}, "latency"},
		{"latency past ten seconds", Impairment{LatencyMs: 10001}, "latency"},
		{"jitter above latency", Impairment{LatencyMs: 10, JitterMs: 20}, "jitter"},
		{"loss above 100", Impairment{LossPct: 101}, "loss"},
		{"negative loss", Impairment{LossPct: -0.5}, "loss"},
		{"negative rate", Impairment{RateKbit: -1}, "rate"},
	} {
		err := tc.imp.Validate()
		if tc.want == "" && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: err %v, want one naming %s", tc.name, err, tc.want)
		}
	}
}

func TestPlanFor(t *testing.T) {
	a := netip.MustParseAddr("127.60.0.3")
	b := netip.MustParseAddr("127.60.0.1")
	c := netip.MustParseAddr("127.60.0.2")
	p := PlanFor([]Device{
		{Addr: a, Impairment: Impairment{LatencyMs: 100, LossPct: 10}},
		{Addr: b},
		{Addr: c, Impairment: Impairment{Unplugged: true, LatencyMs: 50}},
	})
	// Every served device, sorted, so ping can tell devices from empty addresses.
	if got := addrs(p.Devices); got != "127.60.0.1 127.60.0.2 127.60.0.3" {
		t.Errorf("devices = %s", got)
	}
	if got := addrs(p.Unplugged); got != "127.60.0.2" {
		t.Errorf("unplugged = %s", got)
	}
	// An unplugged device is dark, so shaping it would be moot.
	if len(p.Shaped) != 1 || p.Shaped[0].Addr != a || p.Shaped[0].LatencyMs != 100 || p.Shaped[0].LossPct != 10 {
		t.Errorf("shaped = %+v, want only the degraded device", p.Shaped)
	}
	if !p.Equal(PlanFor([]Device{{Addr: b}, {Addr: c, Impairment: Impairment{Unplugged: true, LatencyMs: 50}}, {Addr: a, Impairment: Impairment{LatencyMs: 100, LossPct: 10}}})) {
		t.Error("the same devices in another order planned differently")
	}
	if p.Equal(PlanFor([]Device{{Addr: a}, {Addr: b}, {Addr: c}})) {
		t.Error("a plan with impairments equals one without")
	}
}

func addrs(as []netip.Addr) string {
	var s []string
	for _, a := range as {
		s = append(s, a.String())
	}
	return strings.Join(s, " ")
}

// The rate is bounded where the column is: a value Postgres would refuse is
// refused as invalid instead.
func TestValidateBoundsTheRate(t *testing.T) {
	if err := (Impairment{RateKbit: maxRateKbit}).Validate(); err != nil {
		t.Fatalf("the maximum rate is refused: %v", err)
	}
	if err := (Impairment{RateKbit: maxRateKbit + 1}).Validate(); err == nil {
		t.Fatal("a rate above the maximum validated")
	}
}

// Loss is held to hundredths, as it is stored, so what an operator sets is
// what reads back, and a loss that rounds to nothing is no impairment.
func TestNormalizedRoundsLossToHundredths(t *testing.T) {
	if got := (Impairment{LossPct: 2.555}).Normalized().LossPct; got != 2.56 {
		t.Fatalf("2.555 normalized to %v, want 2.56", got)
	}
	if got := (Impairment{LossPct: 0.004}).Normalized(); got.Shaped() {
		t.Fatalf("0.004%% loss normalized to %+v, want healthy", got)
	}
}
