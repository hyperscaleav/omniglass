// Package ferritepdu is the Ferrite PDU 4C (#2836), a Go device family of one:
// a networked rack power conditioner with four switched, metered outlets, the box
// a room's AV rack plugs into. An operator watches it for the mains it is fed,
// what the rack draws, and which outlets are on.
//
// Its monitoring face is SNMP on the Ferrite enterprise arc (1.3.6.1.4.1.55560):
// the mains voltage, the total current and the energy at .1, an outlet table at
// .2.1.<column>.<outlet> (column 1 the name, 2 the state, 3 the current), and
// identity at .5. Agents report whole numbers, so the voltage is in millivolts
// and the currents in milliamps. It also answers a read-only JSON document, the
// same state in the shape a networked PDU publishes it (an agent block, a global
// measure, an array of outputs), and its HTTP face switches an outlet by its id.
// Switching an outlet off models only the outlet: no simulated device is wired to
// it, so what it feeds keeps answering.
//
// In a room whose scenario declares a lab calendar the load follows the room's
// day (behave.OutletCurrent): outlet 1 the video chain, 2 the processors, 3 the
// amplifier, 4 nothing plugged in. A PDU with no calendar random-walks.
//
// The family replaced the ferrite-pdu-4c YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package ferritepdu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
)

// Name is the family's id, the profile name its devices carry: the product it is
// a family of one for.
const Name = "ferrite-pdu-4c"

// Row is one model of the line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
}

// Rows is the model table: the PDU 4C, one model.
var Rows = []Row{{Product: Name, Model: "PDU 4C", Firmware: "3.1.0"}}

// The enterprise number of Ferrite's (fictional) arc.
const enterprise = 55560

// The state paths of the PDU.
const (
	pathBootEpoch = "status.device.boot_epoch"
	pathUptime    = "status.device.uptime"
	pathVoltage   = "status.mains.voltage_mv"
	pathTotal     = "status.load.total_current_ma"
	pathEnergy    = "status.load.energy_mwh"
	pathOutlets   = "status.outlets"
	pathModel     = "configuration.device.model"
	pathSerial    = "configuration.device.serial"
	pathFirmware  = "configuration.firmware.version"
)

func outletPath(i int, field string) string { return fmt.Sprintf("%s.%d.%s", pathOutlets, i, field) }

// outlet is one of the PDU's outlets as it starts: its id on the front panel and
// in the SNMP table, its name, its state and what it draws.
type outlet struct {
	ID        int
	Name      string
	On        bool
	CurrentMA int
}

// outlets are the four of the PDU, numbered by id 1 to 4. The spare is off, so the
// rack reads a mix; the total is what the three on draw (310 + 180 + 650).
var outlets = []outlet{
	{1, "Video switcher", true, 310}, {2, "Audio DSP", true, 180}, {3, "Amplifier", true, 650}, {4, "Spare", false, 0},
}

// outletParams is what each outlet's gear draws, in milliamps, by the room's
// day: video 150 in standby, 700 awake, 950 in a call; processors 180 and 300;
// amplifier 120, 500 and 800; and nothing in the fourth in a room with a calendar.
var outletParams = []behave.OutletParams{
	{Idle: 150, InUse: 700, InCall: 950, Walk: behave.DefaultOutletWalk},
	{Idle: 180, InUse: 300, Walk: behave.DefaultOutletWalk},
	{Idle: 120, InUse: 500, InCall: 800, Walk: behave.DefaultOutletWalk},
	{CalendarOff: true, Walk: behave.DefaultOutletWalk},
}

// The mains wobble around the nominal 230 V, in millivolts.
var mains = behave.Band{Lo: 226000, Hi: 234000, Step: 600}

// energyDivisor turns milliamps times millivolts (microwatts) into the
// milliwatt-hours of a 30 s tick: 30 s of a microwatt is 30/3600/1000 mWh.
const energyDivisor = 120000.0

func stateFor(r Row) map[string]any {
	list := make([]any, len(outlets))
	for i, o := range outlets {
		state := "off"
		if o.On {
			state = "on"
		}
		list[i] = map[string]any{"id": o.ID, "name": o.Name, "state": state, "current_ma": o.CurrentMA}
	}
	return map[string]any{
		"status": map[string]any{
			"device":  map[string]any{"boot_epoch": 0},
			"mains":   map[string]any{"voltage_mv": 230100},
			"load":    map[string]any{"total_current_ma": 1140, "energy_mwh": 0},
			"outlets": list,
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Ferrite"},
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
			Manufacturer: "Ferrite", Model: "PDU 4C", DeviceType: "power-device",
			Summary: "Ferrite PDU 4C, networked rack power conditioner",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, APISurface: "ferrite-pdu",
				Doc:  profile.Doc{Summary: "Ferrite JSON status and outlet control", Tags: []string{"management", "api"}},
				Auth: profile.Auth{Type: "basic", DefaultUser: "ferrite", DefaultPassword: "ferrite", Realm: "Ferrite PDU", Enforce: true},
			},
			{
				Name: "snmp", Protocol: "snmp", Port: 161, APISurface: "ferrite-pdu",
				Doc:  profile.Doc{Summary: "SNMP monitoring", Tags: []string{"monitoring"}},
				Auth: profile.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: stateFor(Rows[0]),
		HTTP: []spec.HTTPRoute{
			{Match: "GET /pdu.json", Summary: "Agent, mains measure and outputs", Handle: statusRoute},
			{Match: "POST /api/outlets/state", Summary: "Switch an outlet on or off by its id", Example: "{\"outlet\": 2, \"state\": \"off\"}\n", Handle: switchRoute},
		},
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks: []spec.Tick{
			bind.DriftWhole("mains_drift", bind.Seconds(30), pathVoltage, mains),
			loadTick(),
		},
		Surface: surfaceOf,
	}
}

// outletsOf is the outlets as state holds them.
func outletsOf(d spec.Device) []map[string]any {
	list, _ := d.Read(pathOutlets).([]any)
	out := make([]map[string]any, 0, len(list))
	for _, o := range list {
		m, _ := o.(map[string]any)
		out = append(out, m)
	}
	return out
}

// text is a state value as a document prints it: the value itself.
func text(v any) string { return fmt.Sprint(v) }

// quote is a string as the document writes it, markup escaped.
func quote(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// plain is a string as the document writes the agent's identity, inside quotes.
func plain(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

// statusRoute is the JSON document a networked PDU publishes: an agent block, the
// global measure and an array of outputs, one outlet to a line.
func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"Agent\": {\n    \"Model\": %s,\n    \"SerialNumber\": %s,\n    \"Version\": %s,\n    \"Uptime\": %d\n  },\n",
		plain(spec.String(d, pathModel)), plain(spec.String(d, pathSerial)), plain(spec.String(d, pathFirmware)), spec.Int64(d, pathUptime))
	fmt.Fprintf(&b, "  \"GlobalMeasure\": {\n    \"VoltageMv\": %s,\n    \"TotalCurrent\": %s,\n    \"EnergyMwh\": %s\n  },\n",
		text(d.Read(pathVoltage)), text(d.Read(pathTotal)), text(d.Read(pathEnergy)))
	b.WriteString("  \"Outputs\": [")
	for i, o := range outletsOf(d) {
		if i > 0 {
			b.WriteString(",")
		}
		on := 0
		if o["state"] == "on" {
			on = 1
		}
		fmt.Fprintf(&b, "\n    {\"ID\": %s, \"Name\": %s, \"State\": %d, \"Current\": %s}", text(o["id"]), quote(o["name"]), on, text(o["current_ma"]))
	}
	b.WriteString("\n  ]\n}\n")
	return spec.HTTPResponse{ContentType: "application/json", Body: []byte(b.String())}
}

// ack is the document a switch answers: the outlet and the state as the request
// named them.
type ack struct {
	OK     bool   `json:"ok"`
	Outlet any    `json:"outlet"`
	State  string `json:"state"`
}

// The states an outlet takes.
var states = []string{"on", "off"}

// switchRoute switches an outlet on or off by its id. The outlet is the one whose
// id prints as the request's outlet: an id the PDU does not have names no outlet
// and the write is refused, as is a state other than on or off. A refused request
// changes nothing, and a body that is not a JSON object is refused as one.
func switchRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if r.Body == nil && len(bytes.TrimSpace(r.Raw)) > 0 {
		return httpapi.Unreadable()
	}
	index := ""
	if want, named := r.Body["outlet"]; named {
		for i, o := range outletsOf(d) {
			if text(o["id"]) == text(want) {
				index = fmt.Sprint(i)
				break
			}
		}
	}
	path := fmt.Sprintf("%s.%s.state", pathOutlets, index)
	state := httpapi.FieldText(r.Body["state"])
	if state != "on" && state != "off" {
		return httpapi.Reject(httpapi.Invalid{Path: path, Value: state, Enum: states})
	}
	if index == "" {
		return httpapi.MutationFailed(httpapi.FailedMutation{Path: path, Error: fmt.Sprintf("setting %q: segment 2 (\"\"): \"\" is not an integer index", path)})
	}
	if err := d.Write(path, state); err != nil {
		return spec.Failure(500, err.Error())
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Spaced(ack{true, r.Body["outlet"], state})}
}

// loadTick is what the rack draws and what it has drawn: each outlet a level from
// the room's calendar (a walk with none), the total the outlets add up to, and the
// energy the total's draw accrues over the tick. An outlet that is off draws
// nothing.
func loadTick() spec.Tick {
	writes := []string{pathTotal, pathEnergy}
	for i := range outletParams {
		writes = append(writes, outletPath(i, "current_ma"))
	}
	return spec.Tick{
		Name: "load_drift", Interval: bind.Seconds(30), Writes: writes,
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			var out []surface.Change
			var total int64
			for i, o := range outletsOf(d) {
				on := o["state"] == "on"
				prev := int(spec.Int64(d, outletPath(i, "current_ma")))
				n := int64(behave.OutletCurrent(prev, on, c.Room, outletParams[i], c.Rand))
				total += n
				out = append(out, surface.Change{Path: outletPath(i, "current_ma"), Value: n})
			}
			energy := spec.Int64(d, pathEnergy) + int64(behave.Round(0, float64(total*spec.Int64(d, pathVoltage))/energyDivisor))
			return append(out, surface.Change{Path: pathTotal, Value: total}, surface.Change{Path: pathEnergy, Value: energy})
		},
	}
}

// surfaceOf is the SNMP agent: the MIB-II system group, then the Ferrite arc.
// Every scalar past the system group reads as text, as the agent has always
// reported them.
func surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	arc := func(suffix string) string { return fmt.Sprintf("1.3.6.1.4.1.%d.%s", enterprise, suffix) }
	text := func(suffix, path string) surface.OID {
		return surface.Text(arc(suffix), func() string { return spec.String(d, path) }).At(path)
	}
	oids = append(oids, text("1.1.0", pathVoltage), text("1.2.0", pathTotal), text("1.3.0", pathEnergy))
	for _, col := range []struct{ column, field string }{{"1", "name"}, {"2", "state"}, {"3", "current_ma"}} {
		for i := range outletParams {
			oids = append(oids, text(fmt.Sprintf("2.1.%s.%d", col.column, i+1), outletPath(i, col.field)))
		}
	}
	oids = append(oids, text("5.1.0", pathFirmware), text("5.2.0", pathSerial))
	return spec.Surface{OIDs: oids}
}
