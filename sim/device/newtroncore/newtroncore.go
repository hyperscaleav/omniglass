// Package newtroncore is the Newtron control processor (#2833), a Go device
// family of one model. A room's control program runs on it: it starts and ends
// the room's meetings, drives the devices wired to its control ports and serves
// the touch panels connected to it. Its one face is the NVP line protocol
// (TCP, CRLF lines): everything a touch panel answers of the room (the input
// and the scene, the shared sim/surface/nvp surface) and the processor's own
// program, control ports, connected panels, room power, temperature and the
// last macro it ran.
//
// It follows the lab calendar: the room's system power is on from the wake lead
// before a booked meeting someone attends (at the start of a walk-in) to the
// cooldown after it, and standby otherwise; the start-meeting macro runs as it
// goes on and the end-meeting macro as it goes to standby. The room's climate
// warms with the people in it. The family replaced the newtron-core-3 YAML
// profile; wire parity with it is held by golden transcripts (sim/testdata/wire).
package newtroncore

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/nvp"
)

// Name is the family's id, the profile name its devices carry.
const Name = "newtron-core"

// Row is one model of the processor line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	// Firmware is the version it ships with.
	Firmware string
	// Ports is the control ports it starts with, each with its state, and
	// Panels the touch panels connected to it.
	Ports  string
	Panels int
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string { return fmt.Sprintf("Newtron %s, room control processor", r.Model) }

// Rows is the model table: the Core line, one model.
var Rows = []Row{
	{Product: "newtron-core-3", Model: "Core CP-3", Firmware: "4.1.2", Ports: "com1=connected com2=connected ir1=idle relay1=open", Panels: 3},
}

// The state paths of the processor.
const (
	pathUptime      = "status.device.uptime"
	pathBootEpoch   = "status.device.boot_epoch"
	pathProgram     = "status.control.program"
	pathPorts       = "status.control.ports"
	pathPanels      = "status.control.panels"
	pathRoomPower   = "status.control.room_power"
	pathMacro       = "status.control.macro"
	pathMacroAt     = "status.control.macro_at"
	pathTemperature = "status.room.temperature_c"
	pathClimate     = "status.room.climate_c"
	pathSetpoint    = "status.room.setpoint_c"
	pathLoad        = "status.room.load_c_per_person"
)

// The states the control program is in.
const (
	running = "running"
	stopped = "stopped"
)

// nvpFace is the interface the line protocol is served on.
const nvpFace = "nvp"

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device": map[string]any{"boot_epoch": 0},
			"panel":  map[string]any{"occupancy": false},
			"room": map[string]any{
				"scene": "idle", "source": "none",
				// The room's temperature in degrees Celsius, and the underlying value
				// the reading is taken from; the setpoint it settles at, and the
				// warmth each person in a held meeting adds toward it.
				"temperature_c": 21.5, "climate_c": 21.5, "setpoint_c": 21.5, "load_c_per_person": 0.25,
			},
			"diagnostics": map[string]any{"log": ""},
			"video":       map[string]any{"input": "hdmi1"},
			"control": map[string]any{
				"program": running, "ports": r.Ports, "panels": r.Panels,
				// The room's system power, on or standby, and the last macro the
				// program ran with the unix second it ran.
				"room_power": behave.DefaultMacros.Standby, "macro": "none", "macro_at": 0,
			},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Newtron"},
			"firmware": map[string]any{"version": r.Firmware},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Newtron", Model: "Core", DeviceType: "control-processor",
			Summary: "Newtron Core control processor",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{
				Product: r.Product, Model: r.Model, Summary: r.Summary(),
				State: map[string]any{
					"configuration": map[string]any{"device": map[string]any{"model": r.Model}, "firmware": map[string]any{"version": r.Firmware}},
					"status":        map[string]any{"control": map[string]any{"ports": r.Ports, "panels": r.Panels}},
				},
			}
		}),
		Interfaces: []profile.Interface{
			{
				Name: nvpFace, Protocol: "tcp", Port: 51325, Persistent: true, LineEnding: "\r\n",
				Doc: profile.Doc{Summary: "NVP line protocol: room control, program state, control ports and connected panels"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability"}},
		},
		State: stateFor(Rows[0]),
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{roomMeeting(), roomClimate()},
		Surface: surfaceOf,
	}
}

// roomMeeting starts and ends the room's meetings off the lab calendar: the
// room's power follows its use, and the macro that ran as it changed, with the
// second it ran, is held beside it (behave.RoomPower). A room with no calendar
// runs no macro and leaves the room as it was.
func roomMeeting() spec.Tick {
	return spec.Tick{
		Name: "room_meeting", Interval: bind.Seconds(30), OnStart: true,
		Writes: []string{pathMacro, pathMacroAt, pathRoomPower},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			next := behave.RoomPower(behave.ControlState{
				RoomPower: spec.String(d, pathRoomPower), Macro: spec.String(d, pathMacro), MacroAt: spec.Int64(d, pathMacroAt),
			}, c.Room, c.Now, behave.DefaultMacros)
			return []surface.Change{
				{Path: pathMacro, Value: next.Macro},
				{Path: pathMacroAt, Value: next.MacroAt},
				{Path: pathRoomPower, Value: next.RoomPower},
			}
		},
	}
}

// roomClimate moves the room's temperature: the underlying value approaches its
// target (the setpoint, plus the warmth of the people in a held meeting) and the
// reading is that value with a little noise (behave.Climate).
func roomClimate() spec.Tick {
	return spec.Tick{
		Name: "room_climate", Interval: bind.Seconds(30), OnStart: true,
		Writes: []string{pathTemperature, pathClimate},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			next := behave.Climate(behave.ClimateState{
				TemperatureC: spec.Float(d, pathTemperature), ClimateC: spec.Float(d, pathClimate),
				SetpointC: spec.Float(d, pathSetpoint), LoadCPerPerson: spec.Float(d, pathLoad),
			}, c.Room, behave.DefaultClimate, c.Rand)
			return []surface.Change{
				{Path: pathTemperature, Value: next.TemperatureC},
				{Path: pathClimate, Value: next.ClimateC},
			}
		},
	}
}

// program is the processor's own half of the line protocol: its control
// program, the ports, the panels, the room's power, temperature and macro.
type program struct{ d spec.Device }

var _ surface.Commander = program{}

// The lines the processor answers beyond the touch panel's, in the order a
// shell's help lists them.
func (program) Commands() []string {
	return []string{
		"GET PROGRAM", "SET PROGRAM " + running, "SET PROGRAM " + stopped,
		"GET PORTS", "GET PANELS", "GET ROOMPOWER", "GET ROOMTEMP", "GET MACRO",
		"SUBSCRIBE PROGRAM", "UNSUBSCRIBE PROGRAM",
	}
}

func (p program) Handle(line string) (surface.Reply, bool) {
	switch line {
	case "GET PROGRAM":
		return surface.Reply{Text: "PROGRAM " + spec.String(p.d, pathProgram)}, true
	case "SET PROGRAM " + running, "SET PROGRAM " + stopped:
		state := line[len("SET PROGRAM "):]
		return surface.Reply{Text: "PROGRAM " + state, Changes: []surface.Change{{Path: pathProgram, Value: state}}}, true
	case "GET PORTS":
		return surface.Reply{Text: "PORTS " + spec.String(p.d, pathPorts)}, true
	case "GET PANELS":
		return surface.Reply{Text: "PANELS " + spec.String(p.d, pathPanels)}, true
	// The room's system power (on or standby) and the last macro run, with the
	// unix second it ran: "MACRO start-meeting 1793700000", or "MACRO none 0"
	// before the first.
	case "GET ROOMPOWER":
		return surface.Reply{Text: "ROOMPOWER " + spec.String(p.d, pathRoomPower)}, true
	// The room's temperature in degrees Celsius: "ROOMTEMP 21.6".
	case "GET ROOMTEMP":
		return surface.Reply{Text: "ROOMTEMP " + spec.String(p.d, pathTemperature)}, true
	case "GET MACRO":
		return surface.Reply{Text: "MACRO " + spec.String(p.d, pathMacro) + " " + spec.String(p.d, pathMacroAt)}, true
	case "SUBSCRIBE PROGRAM":
		return surface.Reply{Text: "OK", Action: surface.ActionSubscribe, Subject: pathProgram, Token: "PROGRAM"}, true
	case "UNSUBSCRIBE PROGRAM":
		return surface.Reply{Text: "OK", Action: surface.ActionUnsubscribe, Subject: pathProgram}, true
	}
	return surface.Reply{}, false
}

// programSubject pushes the program's state as it changes: "EVT PROGRAM <state>".
func programSubject() surface.Subject {
	return surface.Subject{Match: pathProgram, Path: pathProgram, Token: "PROGRAM",
		Push: func(v any) string { return "EVT PROGRAM " + surface.Raw(v) }}
}

// surfaceOf is the line protocol: the touch panel's room (the shared NVP room
// surface) and the processor's own lines, with the subjects a session may
// subscribe to.
func surfaceOf(d spec.Device) spec.Surface {
	return spec.Surface{
		Commanders: []surface.Commander{bind.NVPRoom(d), program{d}},
		Subjects:   map[string][]surface.Subject{nvpFace: append([]surface.Subject{programSubject()}, nvp.Subjects()...)},
		ListedOn:   nvpFace,
	}
}
