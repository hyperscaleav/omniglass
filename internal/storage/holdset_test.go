package storage_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The hold-set resolver (#1648) is pure: it reads the dependency edges and
// endpoints a loader handed it and names the endpoints a disruptive command
// closes the gates of. These tests feed it rows directly, no database.

const holdNode = "node-a"

func ep(id, comp string, oob bool) storage.HoldEndpoint {
	return storage.HoldEndpoint{ID: id, ComponentID: comp, Node: holdNode, OutOfBand: oob}
}

func edge(dependent, upstream, kind, outlet string) storage.HoldEdge {
	return storage.HoldEdge{ComponentID: dependent, UpstreamID: upstream, Kind: kind, Outlet: outlet}
}

func assertHold(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if want == nil {
		want = []string{}
	}
	if got == nil {
		got = []string{}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("hold set = %v, want %v", got, want)
	}
}

// A reboot holds the target's in-band endpoints only: the BMC answers through
// the reboot, so closing its gate would hide a live device.
func TestHoldSetRebootSkipsOutOfBand(t *testing.T) {
	t.Parallel()
	got := storage.ResolveHoldSet(storage.HoldInput{
		Target: "codec", Node: holdNode,
		Endpoints: []storage.HoldEndpoint{ep("codec-api", "codec", false), ep("codec-bmc", "codec", true)},
	})
	assertHold(t, got, "codec-api")
}

// host propagates transitively: a codec, the touch panel it hosts and the
// panel's own hosted peripheral all go down with the codec.
func TestHoldSetRebootFollowsHostChain(t *testing.T) {
	t.Parallel()
	got := storage.ResolveHoldSet(storage.HoldInput{
		Target: "codec", Node: holdNode,
		Edges: []storage.HoldEdge{
			edge("panel", "codec", "host", ""),
			edge("cam", "panel", "host", ""),
		},
		Endpoints: []storage.HoldEndpoint{
			ep("codec-api", "codec", false), ep("codec-bmc", "codec", true),
			ep("panel-api", "panel", false), ep("panel-oob", "panel", true),
			ep("cam-api", "cam", false), ep("cam-oob", "cam", true),
		},
	})
	assertHold(t, got, "codec-api", "panel-api", "cam-api")
}

// Containment is not dependency, and a path dependent is #1652's: a reboot
// on a parent holds the parent alone.
func TestHoldSetRebootIgnoresContainmentAndPath(t *testing.T) {
	t.Parallel()
	got := storage.ResolveHoldSet(storage.HoldInput{
		Target: "chassis", Node: holdNode,
		Edges: []storage.HoldEdge{edge("behind-gw", "chassis", "path", "")},
		Endpoints: []storage.HoldEndpoint{
			ep("chassis-api", "chassis", false),
			ep("card-api", "card", false), // a child by parent_id, no edge
			ep("behind-api", "behind-gw", false),
		},
	})
	assertHold(t, got, "chassis-api")
}

// A power command on an outlet holds the fed component whole (its BMC loses
// power too) and its host dependents' in-band endpoints; the PDU itself and
// the component on another outlet keep running.
func TestHoldSetPowerFollowsOutlet(t *testing.T) {
	t.Parallel()
	got := storage.ResolveHoldSet(storage.HoldInput{
		Target: "pdu", Node: holdNode, Power: true, Outlet: "2",
		Edges: []storage.HoldEdge{
			edge("amp", "pdu", "power", "2"),
			edge("dsp", "pdu", "power", "3"),
			edge("amp-panel", "amp", "host", ""),
		},
		Endpoints: []storage.HoldEndpoint{
			ep("pdu-snmp", "pdu", false), ep("pdu-oob", "pdu", true),
			ep("amp-api", "amp", false), ep("amp-bmc", "amp", true),
			ep("amp-panel-api", "amp-panel", false), ep("amp-panel-oob", "amp-panel", true),
			ep("dsp-api", "dsp", false),
		},
	})
	assertHold(t, got, "amp-api", "amp-bmc", "amp-panel-api")
}

// A host cycle that slipped past the create check (two racing creates)
// terminates with each endpoint held once, and an endpoint placed on another
// node is that node's to gate.
func TestHoldSetHostCycleTerminatesAndStaysOnNode(t *testing.T) {
	t.Parallel()
	far := ep("b-far", "b", false)
	far.Node = "node-b"
	got := storage.ResolveHoldSet(storage.HoldInput{
		Target: "a", Node: holdNode,
		Edges: []storage.HoldEdge{
			edge("b", "a", "host", ""),
			edge("a", "b", "host", ""),
		},
		Endpoints: []storage.HoldEndpoint{ep("a-api", "a", false), ep("b-api", "b", false), far},
	})
	assertHold(t, got, "a-api", "b-api")
}

// A power command whose outlet param is missing feeds nothing, so it holds
// nothing: the PDU's own endpoints are never in a power hold.
func TestHoldSetPowerWithoutOutletHoldsNothing(t *testing.T) {
	t.Parallel()
	got := storage.ResolveHoldSet(storage.HoldInput{
		Target: "pdu", Node: holdNode, Power: true,
		Edges:     []storage.HoldEdge{edge("amp", "pdu", "power", "2")},
		Endpoints: []storage.HoldEndpoint{ep("pdu-snmp", "pdu", false), ep("amp-api", "amp", false)},
	})
	assertHold(t, got)
}

// The outlet comes off the command's params under the type's
// power_outlet_param, as a string whether the schema typed it a string or a
// number; anything else is no outlet.
func TestOutletParamReadsStringOrNumber(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		params, name, want string
	}{
		{`{"outlet":"2"}`, "outlet", "2"},
		{`{"outlet":12}`, "outlet", "12"},
		{`{"outlet":true}`, "outlet", ""},
		{`{"port":"2"}`, "outlet", ""},
		{``, "outlet", ""},
		{`{"outlet":"2"}`, "", ""},
	} {
		if got := storage.OutletParam([]byte(c.params), c.name); got != c.want {
			t.Errorf("OutletParam(%s, %q) = %q, want %q", c.params, c.name, got, c.want)
		}
	}
}

// An edge's shape is checked before any row is read: a power edge names its
// outlet (1 to 64 characters), host and path edges name none, and no other
// kind exists.
func TestValidateDependencyEdge(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		kind, outlet string
		ok           bool
	}{
		{"power", "1", true},
		{"power", "", false},
		{"power", strings.Repeat("o", 64), true},
		{"power", strings.Repeat("o", 65), false},
		{"host", "", true},
		{"host", "1", false},
		{"path", "", true},
		{"path", "a", false},
		{"contains", "", false},
	} {
		err := storage.ValidateDependencyEdge(c.kind, c.outlet)
		if c.ok != (err == nil) || (err != nil && !errors.Is(err, storage.ErrDependencyInvalid)) {
			t.Errorf("ValidateDependencyEdge(%q, %q) = %v, want ok=%v", c.kind, c.outlet, err, c.ok)
		}
	}
}
