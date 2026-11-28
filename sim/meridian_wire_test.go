//go:build linux

package sim

import "time"

// The Meridian products' wire scripts (#2833): the switcher's and the
// extender's JSON APIs behind the Meridian key, their SNMP arc, and the
// behavior walks every golden transcript of the two holds.

const (
	meridianSwitcherProduct = "meridian-mx-84"
	meridianExtenderProduct = "meridian-xl-1"
)

// meridianSwitcherScript is the switcher's control API: the status, every
// route valid and refused, and the reboot.
func meridianSwitcherScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/api/status"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/route"},
		{Name: "the status with no key", Method: "GET", Path: "/api/status", NoKey: true},
		{Name: "a route with no key", Method: "POST", Path: "/api/route", Body: `{"input": "hdmi2"}`, NoKey: true},
		wirePost("route hdmi2", "/api/route", `{"input": "hdmi2"}`),
		wirePost("route hdmi3", "/api/route", `{"input": "hdmi3"}`),
		wirePost("route dp1, which has no signal", "/api/route", `{"input": "dp1"}`),
		wireGet("the status on dp1", "/api/status"),
		wirePost("route hdmi1", "/api/route", `{"input": "hdmi1"}`),
		{Name: "route hdmi2 with the signal gone", Method: "POST", Path: "/api/route", Body: `{"input": "hdmi2"}`, Snapshot: true, Hand: map[string]any{"status.switcher.inputs.hdmi2.signal": "absent"}},
		wireGet("the status with a signal gone", "/api/status"),
		{Name: "route hdmi3 with the dp1 cable plugged", Method: "POST", Path: "/api/route", Body: `{"input": "dp1"}`, Snapshot: true, Hand: map[string]any{"status.switcher.inputs.dp1.signal": "present"}},
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the status after a reboot", "/api/status"),
		{Name: "factory reset is not on a switcher", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`},
		{Name: "reboot with the wrong method", Method: "GET", Path: "/api/admin/reboot"},
		// The requests the profile refused with the engine's own words, a second
		// path's refusal beside the first or a template's error, come last.
		quirkRoute("route refused", `{"input": "vga"}`, quirkSecondPath),
		quirkRoute("route an input in another case", `{"input": "HDMI1"}`, quirkSecondPath),
		quirkRoute("route with no body", ``, quirkTemplate),
		quirkRoute("route with no field", `{}`, quirkTemplate),
		quirkRoute("route with a number", `{"input": 2}`, quirkTemplate),
		quirkRoute("route with a list", `{"input": ["hdmi2"]}`, quirkTemplate),
		quirkRoute("route with an array body", `["hdmi2"]`, quirkTemplate),
		quirkRoute("route with a body that is not JSON", `input=hdmi2`, quirkTemplate),
	}
}

const (
	quirkSecondPath = "the refusal named the signal path beside the input, which the template resolved to <no value>"
	quirkTemplate   = "the refusal was the template engine's own error text"
)

func quirkRoute(name, body, why string) wireStep {
	return wireStep{Name: name, Method: "POST", Path: "/api/route", Body: body, Snapshot: true, Quirk: why}
}

func meridianSwitcherWrites() []wireStep {
	return []wireStep{
		wirePost("route dp1", "/api/route", `{"input": "dp1"}`),
		{Name: "the hdmi3 source pulled", Method: "POST", Path: "/api/route", Body: `{"input": "hdmi3"}`, Snapshot: true, Hand: map[string]any{"status.switcher.inputs.hdmi3.signal": "absent"}},
	}
}

var meridianSwitcherOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
	"1.3.6.1.4.1.55550.1.1.0", "1.3.6.1.4.1.55550.1.2.0",
	"1.3.6.1.4.1.55550.1.10.1.2.1", "1.3.6.1.4.1.55550.1.10.1.2.2", "1.3.6.1.4.1.55550.1.10.1.2.3", "1.3.6.1.4.1.55550.1.10.1.2.4", "1.3.6.1.4.1.55550.1.10.1.2.5",
	"1.3.6.1.4.1.55550.2.1.0", "1.3.6.1.4.1.55550.2.2.0",
	"1.3.6.1.4.1.55550.5.1.0", "1.3.6.1.4.1.55550.5.2.0", "1.3.6.1.4.1.55550.9.9.0",
}

var meridianWalks = []string{"1.3.6.1.4.1.55550", "1.3.6.1.2.1.1"}

func meridianSwitcherTicks() []tickPlan {
	return []tickPlan{
		{"signal_follow", []string{"status.switcher.signal"}},
		{"room_meeting", []string{"status.switcher.input", "status.switcher.signal", "status.sim.want", "status.sim.prev"}},
	}
}

func meridianSwitcherRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.switcher.input", "status.switcher.signal", "status.sim.want", "status.sim.prev")
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "07:30:00": // a source unplugged from the idle input
			handPath(d, "status.switcher.inputs.dp1.signal", "present")
		case "09:50:00": // an operator routes inside a meeting: it holds until the meeting asks for another
			handPath(d, "status.switcher.input", "hdmi3")
		case "10:20:00":
			handPath(d, "status.switcher.inputs.hdmi2.signal", "absent")
		case "10:40:00":
			handPath(d, "status.switcher.inputs.hdmi2.signal", "present")
		}
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) {
		d.TickBehavior("room_meeting")
		d.TickBehavior("signal_follow")
	}, operator)
	return w.out
}

// meridianExtenderScript is the extender's status and reboot.
func meridianExtenderScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/api/status"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the status with no key", Method: "GET", Path: "/api/status", NoKey: true},
		{Name: "a route is not on an extender", Method: "POST", Path: "/api/route", Body: `{"input": "hdmi2"}`},
		{Name: "the status with the link down", Method: "GET", Path: "/api/status", Hand: map[string]any{"status.extender.link": "down", "status.extender.signal": "absent"}},
		wirePost("reboot retrains the link", "/api/admin/reboot", `{}`),
		wireGet("the status after a reboot", "/api/status"),
		{Name: "reboot with the wrong method", Method: "GET", Path: "/api/admin/reboot"},
		{Name: "factory reset is not on an extender", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`},
	}
}

func meridianExtenderWrites() []wireStep {
	return []wireStep{
		{Name: "the link down", Method: "GET", Path: "/api/status", Hand: map[string]any{"status.extender.link": "down", "status.extender.signal": "absent"}},
	}
}

var meridianExtenderOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
	"1.3.6.1.4.1.55550.1.1.0", "1.3.6.1.4.1.55550.2.1.0", "1.3.6.1.4.1.55550.2.2.0",
	"1.3.6.1.4.1.55550.5.1.0", "1.3.6.1.4.1.55550.5.2.0", "1.3.6.1.4.1.55550.9.9.0",
}

func meridianExtenderTicks() []tickPlan {
	return []tickPlan{{"room_meeting", []string{"status.extender.signal"}}}
}

func meridianExtenderRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.extender.link", "status.extender.signal")
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "07:30:00": // the cable pulled: nothing undoes it
			handPath(d, "status.extender.link", "down")
		case "10:20:00":
			handPath(d, "status.extender.signal", "absent")
		}
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) { d.TickBehavior("room_meeting") }, operator)
	return w.out
}

const wireMeridianSource = "recorded from the YAML profile before its deletion (#2833); see sim/testdata/wire/README.md"

var (
	meridianSwitcherWire = wireFamily{
		Name: "meridian-switcher", Products: []string{meridianSwitcherProduct}, Engine: shippedEngineOf, Source: wireMeridianSource,
		Script: meridianSwitcherScript(), Writes: meridianSwitcherWrites(), OIDs: meridianSwitcherOIDs, Walks: meridianWalks,
		Ticks: meridianSwitcherTicks(), RoomDay: meridianSwitcherRoomDay,
	}
	meridianExtenderWire = wireFamily{
		Name: "meridian-extender", Products: []string{meridianExtenderProduct}, Engine: shippedEngineOf, Source: wireMeridianSource,
		Script: meridianExtenderScript(), Writes: meridianExtenderWrites(), OIDs: meridianExtenderOIDs, Walks: meridianWalks,
		Ticks: meridianExtenderTicks(), RoomDay: meridianExtenderRoomDay,
	}
)

func init() { wireFamilies = append(wireFamilies, meridianSwitcherWire, meridianExtenderWire) }
