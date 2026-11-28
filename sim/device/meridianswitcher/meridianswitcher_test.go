package meridianswitcher

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name {
			t.Fatalf("%s resolves to %+v, want the %s family", r.Product, res.Definition, Name)
		}
		if res.Identity.Model != r.Model || res.Identity.DeviceType != "presentation-switcher" || res.Identity.Summary != r.Summary() {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
	}
}

func route(t *testing.T, match string) spec.HTTPRoute {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r
		}
	}
	t.Fatalf("no route %s", match)
	return spec.HTTPRoute{}
}

// A route change lands the signal on the new input at once, in the one change
// the acknowledgment reads back; an input the switcher does not have changes
// nothing.
func TestARouteLandsTheSignalOnTheNewInputAtOnce(t *testing.T) {
	r := route(t, "POST /api/route")
	for input, signal := range map[string]string{"hdmi1": "present", "hdmi2": "present", "hdmi3": "present", "dp1": "absent"} {
		f := spectest.NewFake(Spec(), "meridian-mx-84")
		res := r.Handle(f, spec.HTTPRequest{Body: map[string]any{"input": input}})
		want := fmt.Sprintf("{\"ok\": true, \"input\": %q, \"signal\": %q}\n", input, signal)
		if string(res.Body.([]byte)) != want || len(f.Writes) != 1 || len(f.Writes[0]) != 2 {
			t.Errorf("%s: %s, writes %+v; want %s in one change of two paths", input, res.Body, f.Writes, want)
		}
	}
	f := spectest.NewFake(Spec(), "meridian-mx-84")
	if res := r.Handle(f, spec.HTTPRequest{Body: map[string]any{"input": "vga"}}); res.Status != 400 || len(f.Writes) != 0 {
		t.Errorf("an input the switcher lacks: %d, %d writes", res.Status, len(f.Writes))
	}
	_ = f.Write("status.switcher.inputs.hdmi2.signal", "absent")
	res := r.Handle(f, spec.HTTPRequest{Body: map[string]any{"input": "hdmi2"}})
	if !strings.Contains(string(res.Body.([]byte)), `"signal": "absent"`) {
		t.Errorf("a source pulled from an input is read when it is routed: %s", res.Body)
	}
}

// The status lists every input's signal beside the routed one's, the inputs by
// name in order.
func TestTheStatusDocumentListsEveryInput(t *testing.T) {
	f := spectest.NewFake(Spec(), "meridian-mx-84")
	got := string(statusRoute(f, spec.HTTPRequest{}).Body.([]byte))
	want := `"switcher": {"input":"hdmi1","inputs":{"dp1":{"signal":"absent"},"hdmi1":{"signal":"present"},"hdmi2":{"signal":"present"},"hdmi3":{"signal":"present"}},"signal":"present"}`
	if !strings.Contains(got, want) {
		t.Fatalf("status = %s, want it to carry %s", got, want)
	}
}

// SNMP: the routed input and its signal at .1.1.0 and .1.2.0, the table of each
// input's signal at .1.10.1.2.<n>, inputs 1 to 4, and the identity arc.
func TestSNMPServesTheSwitcherBranchTheInputTableAndTheIdentityArc(t *testing.T) {
	f := spectest.NewFake(Spec(), "meridian-mx-84")
	got := map[string]any{}
	for _, o := range surfaceOf(f).OIDs {
		got[o.OID] = o.Get()
	}
	for oid, want := range map[string]any{
		"1.3.6.1.4.1.55550.1.1.0": "hdmi1", "1.3.6.1.4.1.55550.1.2.0": "present",
		"1.3.6.1.4.1.55550.1.10.1.2.1": "present", "1.3.6.1.4.1.55550.1.10.1.2.2": "present",
		"1.3.6.1.4.1.55550.1.10.1.2.3": "present", "1.3.6.1.4.1.55550.1.10.1.2.4": "absent",
		"1.3.6.1.4.1.55550.5.1.0": "3.2.0", "1.3.6.1.2.1.1.1.0": "MX 84",
	} {
		if got[oid] != want {
			t.Errorf("OID %s = %v, want %v", oid, got[oid], want)
		}
	}
	if len(got) != 10 {
		t.Errorf("serves %d OIDs, want 10", len(got))
	}
}

// The route is an edge: an operator's route inside a meeting holds until the
// meeting asks for another input, and the signal follows the routed input.
func TestAnOperatorsRouteHoldsUntilTheMeetingAsksForAnother(t *testing.T) {
	f := spectest.NewFake(Spec(), "meridian-mx-84")
	meeting, follow := Spec().Ticks[0], Spec().Ticks[1]
	step := func(tick spec.Tick, room calendar.RoomState) {
		for _, ch := range tick.Run(f, spec.TickContext{Room: room}) {
			_ = f.Write(ch.Path, ch.Value)
		}
	}
	call := calendar.RoomState{HasCalendar: true, CallActive: true}
	share := calendar.RoomState{HasCalendar: true, CallActive: true, Sharing: true}
	idle := calendar.RoomState{HasCalendar: true}
	for _, tc := range []struct {
		room  calendar.RoomState
		input string
	}{{idle, "dp1"}, {call, "hdmi2"}, {call, "hdmi2"}, {share, "hdmi1"}, {call, "hdmi2"}, {idle, "dp1"}} {
		step(meeting, tc.room)
		if got := spec.String(f, pathInput); got != tc.input {
			t.Fatalf("routed %q, the meeting wants %q", got, tc.input)
		}
	}
	step(meeting, call) // the meeting wants hdmi2
	_ = f.Write(pathInput, "hdmi3")
	step(meeting, call)
	if spec.String(f, pathInput) != "hdmi3" {
		t.Fatalf("the operator's route was undone inside the meeting: %v", f.Read(pathInput))
	}
	_ = f.Write("status.switcher.inputs.hdmi3.signal", "absent")
	step(follow, call)
	if f.Read(pathSignal) != "absent" {
		t.Fatalf("a source pulled from the routed input left the signal at %v", f.Read(pathSignal))
	}
	step(meeting, spectest.IdleRoom())
	if spec.String(f, pathInput) != "hdmi3" {
		t.Fatalf("a room with no calendar moved the route to %v", f.Read(pathInput))
	}
}
