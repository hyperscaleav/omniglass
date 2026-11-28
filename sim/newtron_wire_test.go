//go:build linux

package sim

import (
	"strings"
	"time"
)

// The Newtron products' wire scripts (#2833): the requests, line exchanges,
// SNMP reads and behavior walks every golden transcript of the control
// processor, the touch panel and the scheduling panel holds. The recorder ran
// them against the YAML profiles; the parity tests run them against the Go
// families.

const (
	newtronCoreProduct  = "newtron-core-3"
	newtronPanelProduct = "newtron-panel-7"
	newtronSlateProduct = "newtron-slate-5"
)

// nvpRoomLines is the room surface every Newtron NVP device with a touch
// panel's room answers: the input and the scene, valid, refused and
// subscribed, the pushes a change draws.
func nvpRoomLines() []wireLine {
	return []wireLine{
		wireLine1("the input at rest", "GET INPUT"),
		wireLineSnap("input dp1", "SET INPUT dp1"),
		wireLineSnap("input hdmi2", "SET INPUT hdmi2"),
		wireLineSnap("input hdmi3", "SET INPUT hdmi3"),
		wireLineSnap("input usbc", "SET INPUT usbc"),
		wireLineSnap("input hdmi1", "SET INPUT hdmi1"),
		wireLine1("input refused", "SET INPUT vga"),
		wireLine1("input with no value", "SET INPUT"),
		wireLine1("input in another case", "SET INPUT HDMI1"),
		wireLine1("the input after", "GET INPUT"),
		wireLine1("the scene at rest", "GET SCENE"),
		wireLineSnap("scene presentation", "SET SCENE presentation"),
		wireLineSnap("scene vc", "SET SCENE vc"),
		wireLineSnap("scene idle", "SET SCENE idle"),
		wireLine1("scene refused", "SET SCENE cleanup"),
		wireLine1("the scene after", "GET SCENE"),
		wireLine1("a lower-case verb", "get input"),
		wireLine1("a verb with trailing space", "GET INPUT "),
		wireLine1("an unknown verb", "NOPE"),
		wireLine{Name: "a blank line", Line: "  ", NoReply: true},
		wireLine1("subscribe input", "SUBSCRIBE INPUT"),
		wireLineSnap("a change is pushed", "SET INPUT dp1"),
		wireLineSnap("the same input again", "SET INPUT dp1"),
		wireLine1("subscribe log", "SUBSCRIBE LOG"),
		wireLine1("subscribe scene", "SUBSCRIBE SCENE"),
		wireLineSnap("a scene is pushed", "SET SCENE vc"),
		wireLine1("unsubscribe scene", "UNSUBSCRIBE SCENE"),
		wireLineSnap("a scene after unsubscribe", "SET SCENE presentation"),
		wireLine1("unsubscribe input", "UNSUBSCRIBE INPUT"),
		wireLineSnap("an input after unsubscribe", "SET INPUT usbc"),
		wireLine1("unsubscribe log", "UNSUBSCRIBE LOG"),
	}
}

// coreLines is the processor's line protocol: a touch panel's room and the
// processor's own program, ports, panels, room power, temperature and macro.
func coreLines() []wireLine {
	lines := nvpRoomLines()
	return append(lines,
		wireLine1("the program at rest", "GET PROGRAM"),
		wireLineSnap("stop the program", "SET PROGRAM stopped"),
		wireLine1("the program stopped", "GET PROGRAM"),
		wireLineSnap("start the program", "SET PROGRAM running"),
		wireLine1("program refused", "SET PROGRAM paused"),
		wireLine1("subscribe program", "SUBSCRIBE PROGRAM"),
		wireLineSnap("the program stops, pushed", "SET PROGRAM stopped"),
		wireLineSnap("the program starts, pushed", "SET PROGRAM running"),
		wireLine1("unsubscribe program", "UNSUBSCRIBE PROGRAM"),
		wireLineSnap("the program after unsubscribe", "SET PROGRAM stopped"),
		wireLine1("the control ports", "GET PORTS"),
		wireLine1("the connected panels", "GET PANELS"),
		wireLine1("the room power at rest", "GET ROOMPOWER"),
		wireLine1("the room temperature at rest", "GET ROOMTEMP"),
		wireLine1("the macro at rest", "GET MACRO"),
		wireLine{Name: "the room is on and the macro ran", Line: "GET ROOMPOWER", Hand: map[string]any{
			"status.control.room_power": "on", "status.control.macro": "start-meeting", "status.control.macro_at": int64(1793700000),
			"status.room.temperature_c": 23.4, "status.control.ports": "com1=connected com2=error ir1=idle relay1=closed", "status.control.panels": 5,
		}},
		wireLine1("the macro after", "GET MACRO"),
		wireLine1("the room temperature after", "GET ROOMTEMP"),
		wireLine1("the ports after", "GET PORTS"),
		wireLine1("the panels after", "GET PANELS"),
		wireCmd("a command through the table", "GET MACRO"),
		wireCmd("a subscribe through the table", "SUBSCRIBE PROGRAM"),
		wireCmd("an unsubscribe through the table", "UNSUBSCRIBE PROGRAM"),
		wireCmd("an unknown command through the table", "GET NOTHING"),
	)
}

func coreTicks() []tickPlan {
	return []tickPlan{
		{"room_climate", []string{"status.room.temperature_c", "status.room.climate_c"}},
		{"room_meeting", []string{"status.control.room_power", "status.control.macro", "status.control.macro_at"}},
	}
}

func coreRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.control.room_power", "status.control.macro", "status.control.macro_at", "status.room.temperature_c", "status.room.climate_c")
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "07:30:00":
			handPath(d, "status.control.room_power", "on") // on with no meeting: the end macro runs
		case "09:50:00":
			handPath(d, "status.control.room_power", "standby") // standby in a meeting: the start macro runs
		}
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) {
		d.TickBehavior("room_meeting")
		d.TickBehavior("room_climate")
	}, operator)
	return w.out
}

// panelScript is the touch panel's web API behind its sign-in page, every
// route valid and refused.
func panelScript() []wireStep {
	return []wireStep{
		wireGet("the dashboard at rest", "/"),
		wireGet("the status document", "/api/status"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the removed room read", Method: "GET", Path: "/api/room"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/room/scene"},
		{Name: "a head request on the root", Method: "HEAD", Path: "/"},
		{Name: "the root with no session", Method: "GET", Path: "/", NoKey: true},
		{Name: "the status with no session", Method: "GET", Path: "/api/status", NoKey: true},
		{Name: "a write with no session", Method: "POST", Path: "/api/room/scene", Body: `{"scene": "vc"}`, NoKey: true},
		{Name: "the sign-in page", Method: "GET", Path: "/login", NoKey: true},
		wirePost("scene presentation", "/api/room/scene", `{"scene": "presentation"}`),
		wirePost("scene vc", "/api/room/scene", `{"scene": "vc"}`),
		wirePost("scene cleanup", "/api/room/scene", `{"scene": "cleanup"}`),
		wirePost("scene a word the panel never listed", "/api/room/scene", `{"scene": "karaoke"}`),
		wirePost("scene with a space", "/api/room/scene", `{"scene": "movie night"}`),
		wirePost("scene idle", "/api/room/scene", `{"scene": "idle"}`),
		wirePost("source laptop", "/api/room/source", `{"source": "laptop"}`),
		wirePost("source room pc", "/api/room/source", `{"source": "room-pc"}`),
		wirePost("source a word the panel never listed", "/api/room/source", `{"source": "projector-cart"}`),
		wirePost("source none", "/api/room/source", `{"source": "none"}`),
		wirePost("brightness 0", "/api/panel/brightness", `{"brightness": 0}`),
		wirePost("brightness 100", "/api/panel/brightness", `{"brightness": 100}`),
		wirePost("brightness 55", "/api/panel/brightness", `{"brightness": 55}`),
		wirePost("brightness a fraction", "/api/panel/brightness", `{"brightness": 72.5}`),
		wirePost("brightness as text", "/api/panel/brightness", `{"brightness": "40"}`),
		wirePost("brightness out of range", "/api/panel/brightness", `{"brightness": 250}`),
		wirePost("brightness negative", "/api/panel/brightness", `{"brightness": -5}`),
		wirePost("log a line", "/api/diagnostics/log", `{"line": "WARN backlight driver reset"}`),
		wirePost("log a line with spaces", "/api/diagnostics/log", `{"line": "INFO  two   spaces  "}`),
		wirePost("log refused for a newline", "/api/diagnostics/log", `{"line": "a\nb"}`),
		wirePost("log refused for being empty", "/api/diagnostics/log", `{"line": ""}`),
		wirePost("log refused for being long", "/api/diagnostics/log", `{"line": "`+strings.Repeat("x", 201)+`"}`),
		wirePost("log a line of the longest length", "/api/diagnostics/log", `{"line": "`+strings.Repeat("y", 200)+`"}`),
		{Name: "set it all away from default", Method: "POST", Path: "/api/room/scene", Body: `{"scene": "vc"}`, Snapshot: true, Hand: map[string]any{"status.panel.occupancy": true, "status.panel.awake": true, "status.panel.page": "in-call"}},
		wireGet("the status away from default", "/api/status"),
		wireGet("the dashboard away from default", "/"),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the status after a reboot", "/api/status"),
		{Name: "set it away again", Method: "POST", Path: "/api/room/source", Body: `{"source": "laptop"}`, Snapshot: true, Hand: map[string]any{"status.panel.occupancy": true}},
		wirePost("brightness again", "/api/panel/brightness", `{"brightness": 12}`),
		wirePost("factory reset", "/api/admin/factory-reset", `{}`),
		wireGet("the dashboard after a factory reset", "/"),
		{Name: "reboot with the wrong method", Method: "GET", Path: "/api/admin/reboot"},
		// The requests the profile answered with an accident of its templates
		// come last, so the state they left does not carry into the rest.
		{Name: "scene missing", Method: "POST", Path: "/api/room/scene", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
		{Name: "scene a number", Method: "POST", Path: "/api/room/scene", Body: `{"scene": 7}`, Snapshot: true, Quirk: "the template wrote the number into state as a scene"},
		{Name: "source missing", Method: "POST", Path: "/api/room/source", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
		{Name: "brightness a word", Method: "POST", Path: "/api/panel/brightness", Body: `{"brightness": "abc"}`, Snapshot: true, Quirk: "the template wrote the word into state and the status document was not JSON"},
		{Name: "brightness missing", Method: "POST", Path: "/api/panel/brightness", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
		{Name: "log missing", Method: "POST", Path: "/api/diagnostics/log", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into the log"},
	}
}

// panelWrites move the SNMP-visible state away from rest before the second
// SNMP read.
func panelWrites() []wireStep {
	return []wireStep{
		wirePost("reboot", "/api/admin/reboot", `{}`),
	}
}

var panelOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
	"1.3.6.1.4.1.55510.1.1.0", "1.3.6.1.4.1.55510.2.1.0", "1.3.6.1.4.1.55510.5.1.0", "1.3.6.1.4.1.55510.5.2.0",
}

var panelWalks = []string{"1.3.6.1.4.1.55510", "1.3.6.1.2.1.1"}

// panelLines is the panel's line protocol: the NVP room on its raw face, with
// the pushes a change made over the web API draws, and the shell's verbs.
func panelLines() []wireLine {
	lines := nvpRoomLines()
	return append(lines,
		wireLine1("the program is not the panel's", "GET PROGRAM"),
		wireLine1("the booking is not the panel's", "GET BOOKING"),
		wireLine{Name: "subscribe, then a log line over the web API", Line: "SUBSCRIBE LOG", Then: &wireStep{Name: "log", Method: "POST", Path: "/api/diagnostics/log", Body: `{"line": "WARN backlight driver reset"}`}, Snapshot: true},
		wireLine{Name: "a scene over the web API is pushed", Line: "SUBSCRIBE SCENE", Then: &wireStep{Name: "scene", Method: "POST", Path: "/api/room/scene", Body: `{"scene": "vc"}`}, Snapshot: true},
		wireLine{Name: "an input over the line, a log over the web API", Line: "SUBSCRIBE INPUT", Then: &wireStep{Name: "log", Method: "POST", Path: "/api/diagnostics/log", Body: `{"line": "INFO  second"}`}},
		wireLine{Name: "only the web API acts", Then: &wireStep{Name: "scene", Method: "POST", Path: "/api/room/scene", Body: `{"scene": "idle"}`}},
		wireCmd("show status", "show status"),
		wireCmd("show room", "show room"),
		wireLine{Name: "show room with an occupant", Line: "show room", Direct: true, Hand: map[string]any{"status.panel.occupancy": true, "status.room.source": "laptop"}},
		wireCmd("an unknown show", "show nothing"),
		wireCmd("an nvp verb in the shell", "GET INPUT"),
		wireCmd("an nvp set in the shell", "SET INPUT dp1"),
		wireCmd("an nvp subscribe in the shell", "SUBSCRIBE INPUT"),
		wireCmd("reboot", "reboot"),
		wireLine{Name: "factory reset after changes", Line: "factory-reset", Direct: true, Snapshot: true, Hand: map[string]any{
			"status.panel.brightness": 20, "status.panel.occupancy": true, "status.room.scene": "vc", "status.room.source": "laptop",
		}},
		wireCmd("reboot with no hand", "reboot"),
		wireCmd("an unknown command", "frobnicate"),
	)
}

func panelTicks() []tickPlan {
	return []tickPlan{{"room_meeting", []string{"status.panel.awake", "status.panel.page"}}}
}

func panelRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.panel.awake", "status.panel.page")
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "07:30:00":
			handPath(d, "status.panel.page", "in-call") // a page left on, in a room with no meeting
			handPath(d, "status.panel.awake", true)
		case "09:50:00":
			handPath(d, "status.panel.page", "idle")
		}
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) { d.TickBehavior("room_meeting") }, operator)
	return w.out
}

// slateLines is the scheduling panel's line protocol: the room's booking, its
// buttons and the meetings it shows.
func slateLines() []wireLine {
	return []wireLine{
		wireLine1("the booking at rest", "GET BOOKING"),
		wireLine1("the next meeting at rest", "GET NEXT"),
		wireLine1("the current meeting at rest", "GET CURRENT"),
		wireLineSnap("book the room", "BOOK"),
		wireLine1("the booking after", "GET BOOKING"),
		wireLineSnap("release the room", "RELEASE"),
		wireLine1("the booking released", "GET BOOKING"),
		wireLine1("a lower-case verb", "book"),
		wireLine1("an unknown verb", "NOPE"),
		wireLine1("a touch panel's verb is not the slate's", "GET INPUT"),
		wireLine1("a touch panel's set is not the slate's", "SET SCENE idle"),
		wireLine1("a processor's verb is not the slate's", "GET PROGRAM"),
		wireLine{Name: "a meeting holds the room", Line: "GET CURRENT", Hand: map[string]any{
			"status.room.current_meeting": "1793700000 1793703600 ada@example.com", "status.room.next_meeting": "1793707200 1793709000 grace@example.com",
		}},
		wireLine1("the next meeting then", "GET NEXT"),
		wireLine1("subscribe booking", "SUBSCRIBE BOOKING"),
		wireLineSnap("a booking is pushed", "BOOK"),
		wireLineSnap("the same booking again", "BOOK"),
		wireLineSnap("a release is pushed", "RELEASE"),
		wireLine1("unsubscribe booking", "UNSUBSCRIBE BOOKING"),
		wireLineSnap("a booking after unsubscribe", "BOOK"),
		wireCmd("a command through the table", "GET BOOKING"),
		wireCmd("a subscribe through the table", "SUBSCRIBE BOOKING"),
		wireCmd("an unknown command through the table", "GET NOTHING"),
	}
}

func slateTicks() []tickPlan {
	return []tickPlan{{"room_bookings", []string{"status.room.booking", "status.room.cal_booking", "status.room.current_meeting", "status.room.next_meeting"}}}
}

func slateRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.room.booking", "status.room.cal_booking", "status.room.current_meeting", "status.room.next_meeting")
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "09:50:00": // an operator books the free room from the panel: it holds
			handPath(d, "status.room.booking", "booked")
		case "11:30:00": // and releases a booked one
			handPath(d, "status.room.booking", "free")
		}
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) { d.TickBehavior("room_bookings") }, operator)
	return w.out
}

// The room control and switching families, as the harness records and replays
// them. Their goldens were recorded from the YAML profiles the families
// replaced, so they are frozen: recording one from the family would make the
// hold on the profile vacuous.
const wireRoomFrozen = "recorded from the YAML profile before its deletion (#2833); re-recording from the Go family would hold it to itself"

const wireNewtronSource = "recorded from the YAML profile before its deletion (#2833); see sim/testdata/wire/README.md"

var (
	newtronCoreWire = wireFamily{
		Name: "newtron-core", Products: []string{newtronCoreProduct}, Engine: shippedEngineOf, Source: wireNewtronSource, Frozen: wireRoomFrozen,
		Lines: coreLines(), Ticks: coreTicks(), RoomDay: coreRoomDay,
	}
	newtronPanelWire = wireFamily{
		Name: "newtron-panel", Products: []string{newtronPanelProduct}, Engine: shippedEngineOf, Source: wireNewtronSource, Frozen: wireRoomFrozen,
		Script: panelScript(), Writes: panelWrites(), OIDs: panelOIDs, Walks: panelWalks,
		Lines: panelLines(), Ticks: panelTicks(), RoomDay: panelRoomDay,
	}
	newtronSlateWire = wireFamily{
		Name: "newtron-slate", Products: []string{newtronSlateProduct}, Engine: shippedEngineOf, Source: wireNewtronSource, Frozen: wireRoomFrozen,
		Lines: slateLines(), Ticks: slateTicks(), RoomDay: slateRoomDay,
	}
)

func init() { wireFamilies = append(wireFamilies, newtronCoreWire, newtronPanelWire, newtronSlateWire) }
