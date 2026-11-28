// Package meridianswitcher is the Meridian MX 84 presentation switcher (#2833),
// a Go device family of one model: four video inputs (three HDMI, one
// DisplayPort) routed to one output, the box a lectern's laptop, a wall plate
// and a wireless gateway plug into and the display hangs off. An operator wants
// two answers from it: which input is routed, and whether that input has a
// signal.
//
// Its monitoring face is SNMP on the Meridian enterprise arc, the switcher's
// branch at .1: the routed input, the signal on it and a table of the signal on
// each input. Its control face is a small HTTP JSON API behind a Meridian
// header key: the route, and a reboot. The extender (meridianextender) shares
// the arc and the key but has a different API (a status and a reboot, no route),
// so it is a family of its own.
//
// Its route follows the room's meetings off the lab calendar: the codec's feed
// while a meeting is held, the lectern laptop through the stretch content is
// shared, and the input nothing is plugged into between meetings and through a
// no-show. The family replaced the meridian-mx-84 YAML profile; wire parity
// with it is held by golden transcripts (sim/testdata/wire).
package meridianswitcher

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry.
const Name = "meridian-switcher"

// Row is one model of the switcher line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	// Firmware is the version it ships with.
	Firmware string
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string { return fmt.Sprintf("Meridian %s, presentation switcher", r.Model) }

// Rows is the model table: the MX line, one model.
var Rows = []Row{
	{Product: "meridian-mx-84", Model: "MX 84", Firmware: "3.2.0"},
}

// The inputs the switcher has, in the order its SNMP table numbers them (1 to
// 4), and which of them have a source plugged in: the lectern laptop (hdmi1),
// the wall plate (hdmi2) and the wireless gateway (hdmi3) are live; nothing is
// on the DisplayPort.
var inputs = []string{"hdmi1", "hdmi2", "hdmi3", "dp1"}

var plugged = map[string]bool{"hdmi1": true, "hdmi2": true, "hdmi3": true}

// The state paths of the switcher.
const (
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
	pathInput     = "status.switcher.input"
	pathSignal    = "status.switcher.signal"
	pathWant      = "status.sim.want"
	pathPrev      = "status.sim.prev"
	pathModel     = "configuration.device.model"
	pathSerial    = "configuration.device.serial"
	pathFirmware  = "configuration.firmware.version"
)

// inputSignal is the path of the signal on one input.
func inputSignal(input string) string { return "status.switcher.inputs." + input + ".signal" }

// The signal an input reads.
const (
	present = "present"
	absent  = "absent"
)

// The enterprise number of Meridian's (fictional) arc, shared with the extender:
// the switcher's branch is .1, the extender's .2, identity .5.
const enterprise = 55550

func stateFor(r Row) map[string]any {
	table := map[string]any{}
	for _, in := range inputs {
		signal := absent
		if plugged[in] {
			signal = present
		}
		table[in] = map[string]any{"signal": signal}
	}
	return map[string]any{
		"status": map[string]any{
			"device":   map[string]any{"boot_epoch": 0},
			"switcher": map[string]any{"input": inputs[0], "signal": present, "inputs": table},
			// The input the meeting last asked for, and the one before it.
			"sim":     map[string]any{"want": "", "prev": ""},
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Meridian"},
			"firmware": map[string]any{"version": r.Firmware},
			"network":  map[string]any{"hostname": ""},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Meridian", Model: "MX", DeviceType: "presentation-switcher",
			Summary: "Meridian MX presentation switcher",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{
				Product: r.Product, Model: r.Model, Summary: r.Summary(),
				State: map[string]any{"configuration": map[string]any{"device": map[string]any{"model": r.Model}, "firmware": map[string]any{"version": r.Firmware}}},
			}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, Doc: wire.Doc{Summary: "Meridian JSON control API"},
				Auth: wire.Auth{Type: "header", Header: "X-Meridian-Key", APIKey: "meridian-lab-key", Enforce: true},
			},
			{
				Name: "snmp", Protocol: "snmp", Port: 161, Doc: wire.Doc{Summary: "SNMP monitoring"},
				Auth: wire.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability"}},
		},
		State:   stateFor(Rows[0]),
		HTTP:    routes(),
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{roomMeeting(), signalFollow()},
		Surface: surfaceOf,
	}
}

// signals is the signal on each input, by input.
func signals(d spec.Device) map[string]string {
	out := make(map[string]string, len(inputs))
	for _, in := range inputs {
		out[in] = spec.String(d, inputSignal(in))
	}
	return out
}

// roomMeeting routes what the room's meeting wants. The route is an edge, not a
// level: the wanted input is written when it changes, so an operator's route
// inside a meeting holds until the meeting asks for another (behave.RouteFollowsRoom).
func roomMeeting() spec.Tick {
	return spec.Tick{
		Name: "room_meeting", Interval: bind.Seconds(30), OnStart: true,
		Writes: []string{pathPrev, pathWant, pathInput, pathSignal},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			next := behave.RouteFollowsRoom(behave.RouteState{
				Input: spec.String(d, pathInput), Signal: spec.String(d, pathSignal), Want: spec.String(d, pathWant), Prev: spec.String(d, pathPrev),
			}, c.Room, signals(d), behave.DefaultRoute)
			return []surface.Change{
				{Path: pathPrev, Value: next.Prev}, {Path: pathWant, Value: next.Want},
				{Path: pathInput, Value: next.Input}, {Path: pathSignal, Value: next.Signal},
			}
		},
	}
}

// signalFollow keeps the signal the switcher reports for its routed input as
// whatever that input's own signal reads: a source unplugged from the routed
// input drops it on the next tick. A scenario drives it by setting an input's
// signal to absent.
func signalFollow() spec.Tick {
	return spec.Tick{
		Name: "signal_follow", Interval: bind.Seconds(10), Writes: []string{pathSignal},
		Run: func(d spec.Device, _ spec.TickContext) []surface.Change {
			return []surface.Change{{Path: pathSignal, Value: behave.SignalOnInput(spec.String(d, pathSignal), spec.String(d, pathInput), signals(d))}}
		},
	}
}

// statusDoc is GET /api/status after the identity block: the routed input, the
// signal on each input and the signal on the routed one.
type statusDoc struct {
	Input  string              `json:"input"`
	Inputs map[string]inputDoc `json:"inputs"`
	Signal string              `json:"signal"`
}

type inputDoc struct {
	Signal string `json:"signal"`
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.Identity{Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware), Uptime: spec.Int64(d, pathUptime)}
	doc := statusDoc{Input: spec.String(d, pathInput), Inputs: map[string]inputDoc{}, Signal: spec.String(d, pathSignal)}
	for in, s := range signals(d) {
		doc.Inputs[in] = inputDoc{s}
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Status(id, httpapi.ModelFirst, httpapi.Inline("switcher", doc))}
}

type (
	routeAck struct {
		OK     bool   `json:"ok"`
		Input  string `json:"input"`
		Signal string `json:"signal"`
	}
	actionAck struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
	}
)

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /api/status", Summary: "Switcher identity, routed input and the signal on each input", Handle: statusRoute},
		// A route change lands the signal on the new input at once; signal_follow
		// keeps it current afterward, as a source is plugged or pulled.
		httpapi.Control{
			Match: "POST /api/route", Summary: "Route an input to the output (hdmi1, hdmi2, hdmi3, dp1)", Example: "{\"input\": \"hdmi2\"}\n",
			Fields: []httpapi.Field{{Path: pathInput, Name: "input", Kind: httpapi.Choice, Values: inputs}},
			Also: func(d spec.Device, set map[string]any) []surface.Change {
				return []surface.Change{{Path: pathSignal, Value: spec.String(d, inputSignal(set[pathInput].(string)))}}
			},
			Ack: func(d spec.Device) any {
				return routeAck{true, spec.String(d, pathInput), spec.String(d, pathSignal)}
			},
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot switcher (clears uptime, routes the first input)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, pathInput, pathSignal)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
	}
}

// surfaceOf is the SNMP agent: the MIB-II system group, then the Meridian arc.
// The switcher is its branch .1: the routed input at .1.1.0, the signal on it at
// .1.2.0 and a table of the signal on each input at .1.10.1.<column>.<input>
// (inputs 1 to 4, column 2 the signal). The extender's branch .2 is not served
// here, and identity is at .5.
func surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	arc := func(suffix string) string { return fmt.Sprintf("1.3.6.1.4.1.%d.%s", enterprise, suffix) }
	text := func(suffix, path string) surface.OID {
		return surface.Text(arc(suffix), func() string { return spec.String(d, path) }).At(path)
	}
	oids = append(oids, text("1.1.0", pathInput), text("1.2.0", pathSignal))
	for i, in := range inputs {
		oids = append(oids, text(fmt.Sprintf("1.10.1.2.%d", i+1), inputSignal(in)))
	}
	oids = append(oids, snmp.Identity{
		Enterprise: enterprise,
		Firmware:   func() string { return spec.String(d, pathFirmware) }, FirmwarePath: pathFirmware,
		Serial: func() string { return spec.String(d, pathSerial) }, SerialPath: pathSerial,
	}.OIDs()...)
	return spec.Surface{OIDs: oids}
}
