package simnode

import (
	"net"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

// An operation reaches the device it names and comes back as its answer; a
// device the simulator does not serve says so.
func TestAnswerRoutesToTheNamedDevice(t *testing.T) {
	a := sim.NewDevice("c1", "a", net.ParseIP("127.60.0.1"), "p", 1, map[string]any{"name": "first"})
	b := sim.NewDevice("c2", "b", net.ParseIP("127.60.0.2"), "p", 1, map[string]any{"name": "second"})
	for _, d := range []*sim.Device{a, b} {
		d.OIDMap = map[string]string{"1.3.6.1.2.1.1.5.0": "name"}
	}
	res := answer([]*sim.Device{a, b}, collection.SimOperation{ComponentID: "c2", Kind: "snmp", OID: "1.3.6.1.2.1.1.5.0"})
	if res.Error != "" || res.Body != "second" {
		t.Fatalf("result = %+v, want c2's sysName", res)
	}
	if res := answer([]*sim.Device{a, b}, collection.SimOperation{ComponentID: "c9", Kind: "snmp"}); res.Error == "" {
		t.Fatalf("an unserved device answered %+v", res)
	}
}
