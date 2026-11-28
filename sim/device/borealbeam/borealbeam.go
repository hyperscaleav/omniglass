// Package borealbeam is the Boreal Beam laser installation projector (#2832),
// a Go device family of one model. A projector is not a display: its light
// source wears out by the hour, and it carries a shutter that blanks the image
// without powering down; those are what an operator watches it for. Control is
// the Boreal web admin behind HTTP Digest (power, the shutter, the input) and
// monitoring is SNMP on the Boreal enterprise arc; the projector follows its
// room's meetings like the Edge display, and its light source ages only while
// it is on.
//
// The family replaced the boreal-beam-340 YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package borealbeam

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
const Name = "boreal-beam"

// Row is one model of the projector line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	// Firmware is the version it ships with.
	Firmware string
	// LightSourceHours are the hours on its laser source when it starts: a
	// source is rated for about 20,000, and this one is a few years into a
	// lecture hall's timetable.
	LightSourceHours float64
}

// Rows is the model table: the Beam line, one model.
var Rows = []Row{
	{Product: "boreal-beam-340", Model: "Beam 340", Firmware: "1.12.0", LightSourceHours: 12480.5},
}

// The state paths of the projector.
const (
	pathPower       = bind.PathPower
	pathUptime      = "status.device.uptime"
	pathBootEpoch   = "status.device.boot_epoch"
	pathHours       = "status.projector.light_source_hours"
	pathShutter     = "status.projector.shutter"
	pathInput       = "status.projector.input"
	pathTemperature = "status.projector.temperature"
	pathModel       = "configuration.device.model"
	pathSerial      = "configuration.device.serial"
	pathFirmware    = "configuration.firmware.version"
)

// The choices the projector offers: it has no off, only standby.
var (
	powers   = []string{"on", "standby"}
	shutters = []string{"open", "closed"}
	inputs   = []string{"hdmi1", "hdmi2"}
)

const enterprise = 55520

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device": map[string]any{"power": "on", "boot_epoch": 0},
			"projector": map[string]any{
				"light_source_hours": r.LightSourceHours, "shutter": "open", "input": "hdmi1", "temperature": 36,
			},
			"sim":     bind.RoomDefaults(),
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Boreal"},
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
			Manufacturer: "Boreal", Model: "Beam", DeviceType: "projector",
			Summary: "Boreal Beam laser installation projector",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, Summary: fmt.Sprintf("Boreal %s laser installation projector", r.Model)}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "web-admin", Protocol: "http", Port: 80, Doc: wire.Doc{Summary: "Web-based management"},
				Auth: wire.Auth{
					Type: "digest", DefaultUser: "admin", DefaultPassword: "boreal", Realm: "Boreal Beam Admin",
					Algorithm: "MD5", Enforce: true, ExpirePath: "/api/admin/sessions/expire",
				},
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
		Ticks: []spec.Tick{
			bind.PowerFollowsRoom(pathInput),
			// The light source ages only while it is lit: a minute on adds a
			// minute's hours, standby adds none.
			bind.Wear("light_source_aging", bind.Seconds(60), pathHours, behave.DefaultLightSource),
			bind.Drift("temperature_drift", bind.Seconds(60), pathTemperature, behave.Band{Lo: 30, Hi: 48, Step: 1}, behave.SigFigs(3)),
		},
		Surface: surfaceOf,
	}
}

// statusDoc is GET /api/status: identity, power, and the projector's own
// readings.
type statusDoc struct {
	Device struct {
		Model    string `json:"model"`
		Serial   string `json:"serial"`
		Firmware string `json:"firmware"`
		Uptime   int64  `json:"uptime"`
		Power    string `json:"power"`
	} `json:"device"`
	Projector struct {
		LightSourceHours float64 `json:"light_source_hours"`
		Shutter          string  `json:"shutter"`
		Input            string  `json:"input"`
		Temperature      float64 `json:"temperature"`
	} `json:"projector"`
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	var doc statusDoc
	doc.Device.Model, doc.Device.Serial, doc.Device.Firmware = spec.String(d, pathModel), spec.String(d, pathSerial), spec.String(d, pathFirmware)
	doc.Device.Uptime, doc.Device.Power = spec.Int64(d, pathUptime), spec.String(d, pathPower)
	doc.Projector.LightSourceHours, doc.Projector.Shutter = spec.Float(d, pathHours), spec.String(d, pathShutter)
	doc.Projector.Input, doc.Projector.Temperature = spec.String(d, pathInput), spec.Float(d, pathTemperature)
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(doc)}
}

type (
	powerAck struct {
		OK    bool   `json:"ok"`
		Power string `json:"power"`
	}
	shutterAck struct {
		OK      bool   `json:"ok"`
		Shutter string `json:"shutter"`
	}
	inputAck struct {
		OK    bool   `json:"ok"`
		Input string `json:"input"`
	}
	actionAck struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
	}
)

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /api/status", Summary: "Projector status (identity, power, light source, shutter, input)", Handle: statusRoute},
		httpapi.Control{
			Match: "POST /api/projector/power", Summary: "Set projector power (on, standby)", Example: "{\"power\": \"standby\"}\n",
			Fields: []httpapi.Field{{Path: pathPower, Name: "power", Kind: httpapi.Choice, Values: powers}},
			Ack:    func(d spec.Device) any { return powerAck{true, spec.String(d, pathPower)} },
		}.Route(),
		// The shutter blanks the image with the light source still lit: the
		// projector stays on and warm, so the picture is back at once.
		httpapi.Control{
			Match: "POST /api/projector/shutter", Summary: "Open or close the shutter (open, closed)", Example: "{\"shutter\": \"closed\"}\n",
			Fields: []httpapi.Field{{Path: pathShutter, Name: "shutter", Kind: httpapi.Choice, Values: shutters}},
			Ack:    func(d spec.Device) any { return shutterAck{true, spec.String(d, pathShutter)} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/projector/input", Summary: "Switch the projector input (hdmi1, hdmi2)", Example: "{\"input\": \"hdmi2\"}\n",
			Fields: []httpapi.Field{{Path: pathInput, Name: "input", Kind: httpapi.Choice, Values: inputs}},
			Ack:    func(d spec.Device) any { return inputAck{true, spec.String(d, pathInput)} },
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot projector (clears uptime, opens the shutter)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, pathShutter)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
	}
}

// surfaceOf is the SNMP agent: the MIB-II system group, then the Boreal arc.
// The power state (.1.1.1.0) and the serial (.5.2.0) sit where the Edge keeps
// them, firmware at .5.1.0 is the Beam's own (the Edge withholds it), and the
// projector's own branch is .6: light-source hours, shutter, input,
// temperature.
func surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	arc := func(suffix string) string { return fmt.Sprintf("1.3.6.1.4.1.%d.%s", enterprise, suffix) }
	text := func(suffix, path string) surface.OID {
		return surface.Text(arc(suffix), func() string { return spec.String(d, path) }).At(path)
	}
	oids = append(oids,
		text("1.1.1.0", pathPower),
		text("6.1.0", pathHours),
		text("6.2.0", pathShutter),
		text("6.3.0", pathInput),
		surface.Integer(arc("6.4.0"), func() int { return spec.Int(d, pathTemperature) }).At(pathTemperature),
	)
	oids = append(oids, snmp.Identity{
		Enterprise: enterprise,
		Firmware:   func() string { return spec.String(d, pathFirmware) }, FirmwarePath: pathFirmware,
		Serial: func() string { return spec.String(d, pathSerial) }, SerialPath: pathSerial,
	}.OIDs()...)
	return spec.Surface{OIDs: oids}
}
