package ferritepdu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

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

func call(t *testing.T, f *spectest.Fake, body string) spec.HTTPResponse {
	t.Helper()
	var parsed map[string]any
	_ = json.Unmarshal([]byte(body), &parsed)
	return route(t, "POST /api/outlets/state").Handle(f, spec.HTTPRequest{Method: "POST", Path: "/api/outlets/state", Body: parsed, Raw: []byte(body)})
}

// The document is the shape a networked PDU publishes: an agent block, the global
// measure, and one output a line, each with the state as a number.
func TestTheDocumentListsTheOutputsAOneLineEach(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	body := string(route(t, "GET /pdu.json").Handle(f, spec.HTTPRequest{}).Body.([]byte))
	for _, want := range []string{
		`"Model": "PDU 4C"`, `"Version": "3.1.0"`, `"VoltageMv": 230100`, `"TotalCurrent": 1140`, `"EnergyMwh": 0`,
		`    {"ID": 1, "Name": "Video switcher", "State": 1, "Current": 310},`,
		`    {"ID": 4, "Name": "Spare", "State": 0, "Current": 0}` + "\n  ]\n}\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the document lacks %q:\n%s", want, body)
		}
	}
	if err := json.Unmarshal([]byte(body), new(any)); err != nil {
		t.Errorf("the document is not JSON: %v", err)
	}
}

// An outlet is switched by its id, which may be a number or its text; the state
// lands on the outlet that has the id and the answer names what was asked.
func TestAnOutletIsSwitchedByItsId(t *testing.T) {
	for _, body := range []string{`{"outlet": 4, "state": "on"}`, `{"outlet": "4", "state": "on"}`, `{"outlet": 4.0, "state": "on"}`} {
		f := spectest.NewFake(Spec(), Name)
		res := call(t, f, body)
		if res.Status != 0 || f.Read("status.outlets.3.state") != "on" || f.Read("status.outlets.0.state") != "on" {
			t.Errorf("%s: status %d, outlet 4 %v", body, res.Status, f.Read("status.outlets.3.state"))
		}
	}
	f := spectest.NewFake(Spec(), Name)
	got := string(call(t, f, `{"outlet": 2, "state": "off"}`).Body.([]byte))
	if got != "{\"ok\": true, \"outlet\": 2, \"state\": \"off\"}\n" {
		t.Errorf("answer = %q", got)
	}
}

// A refused switch changes nothing, and says why in the engine's own words: a
// state outside on and off lists the values, an outlet the PDU does not have names
// the write it could not make.
func TestARefusedSwitchChangesNothing(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"a state outside the list", `{"outlet": 1, "state": "cycle"}`, `{"error":"validation failed","fields":[{"path":"status.outlets.0.state","value":"cycle","enum":["on","off"]}]}`},
		{"no state", `{"outlet": 1}`, `{"error":"validation failed","fields":[{"path":"status.outlets.0.state","value":"\u003cno value\u003e","enum":["on","off"]}]}`},
		{"an outlet the PDU does not have", `{"outlet": 9, "state": "on"}`, `"error":"mutation failed"`},
		{"no outlet", `{"state": "on"}`, `"error":"mutation failed"`},
		{"an empty object", `{}`, `{"error":"validation failed","fields":[{"path":"status.outlets..state","value":"\u003cno value\u003e","enum":["on","off"]}]}`},
		{"a body that is not an object", `[1]`, `the request body is not a JSON object`},
	} {
		f := spectest.NewFake(Spec(), Name)
		res := call(t, f, tc.body)
		if res.Status != 400 || len(f.Writes) != 0 || !strings.Contains(string(res.Body.([]byte)), tc.want) {
			t.Errorf("%s: status %d, %d writes, body %s; want a 400 naming %s", tc.name, res.Status, len(f.Writes), res.Body, tc.want)
		}
	}
}

// The load follows the room's day: an outlet reads the level its gear is at,
// an outlet that is off draws nothing, the fourth draws nothing in a room with a
// calendar, and the total and the energy follow.
func TestTheLoadFollowsTheRoomsDay(t *testing.T) {
	var tick spec.Tick
	for _, k := range Spec().Ticks {
		if k.Name == "load_drift" {
			tick = k
		}
	}
	run := func(f *spectest.Fake, room calendar.RoomState) {
		for _, ch := range tick.Run(f, spec.TickContext{Room: room, Now: f.Clock, Rand: behave.NewSource(f.ID)}) {
			_ = f.Write(ch.Path, ch.Value)
		}
	}
	idle := calendar.RoomState{Phase: calendar.PhaseIdle, HasCalendar: true}
	call := calendar.RoomState{HasCalendar: true, InUse: true, CallActive: true, Attendees: 3}
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write("status.outlets.3.state", "on")
	run(f, idle)
	if got := []any{f.Read("status.outlets.0.current_ma"), f.Read("status.outlets.1.current_ma"), f.Read("status.outlets.2.current_ma"), f.Read("status.outlets.3.current_ma")}; !equal(got, 150, 180, 120, 0) {
		t.Errorf("idle room: outlets draw %v, want 150 180 120 0", got)
	}
	run(f, call)
	if got := []any{f.Read("status.outlets.0.current_ma"), f.Read("status.outlets.1.current_ma"), f.Read("status.outlets.2.current_ma")}; !equal(got, 950, 300, 800) {
		t.Errorf("a call: outlets draw %v, want 950 300 800", got)
	}
	if f.Read(pathTotal) != int64(2050) {
		t.Errorf("total = %v, want the three outlets' 2050", f.Read(pathTotal))
	}
	// Each tick accrues its total's draw: 450 mA and then 2050 mA at 230.1 V.
	if f.Read(pathEnergy) != int64(863+3931) {
		t.Errorf("energy = %v, want the two ticks' 863 and 3931 mWh", f.Read(pathEnergy))
	}
	_ = f.Write("status.outlets.2.state", "off")
	run(f, call)
	if f.Read("status.outlets.2.current_ma") != int64(0) || f.Read(pathTotal) != int64(1250) {
		t.Errorf("an outlet that is off draws %v, total %v, want 0 and 1250", f.Read("status.outlets.2.current_ma"), f.Read(pathTotal))
	}
}

func equal(got []any, want ...int64) bool {
	for i, w := range want {
		if got[i] != w {
			return false
		}
	}
	return true
}

// SNMP reads every outlet's name, state and current beside the mains, as text,
// the way the agent has always answered them; only the system group is typed.
func TestSNMPServesTheOutletTableAsText(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	got := map[string]surface.OID{}
	for _, o := range Spec().Surface(f).OIDs {
		got[o.OID] = o
	}
	if len(got) != 2+3+12+2 {
		t.Fatalf("the agent serves %d OIDs, want 19", len(got))
	}
	for oid, want := range map[string]any{
		"1.3.6.1.4.1.55560.1.1.0": "230100", "1.3.6.1.4.1.55560.2.1.1.2": "Audio DSP",
		"1.3.6.1.4.1.55560.2.1.2.4": "off", "1.3.6.1.4.1.55560.2.1.3.3": "650", "1.3.6.1.4.1.55560.5.1.0": "3.1.0",
	} {
		if o := got[oid]; o.Get() != want || o.Kind != surface.KindOctetString {
			t.Errorf("OID %s = %v (kind %v), want %v as text", oid, o.Get(), o.Kind, want)
		}
	}
	if got["1.3.6.1.2.1.1.3.0"].Kind != surface.KindTimeTicks {
		t.Error("sysUpTime is not TimeTicks")
	}
}
