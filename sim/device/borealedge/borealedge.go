// Package borealedge is the Boreal Edge commercial display (#2832), the first
// Go device family: one wire API, implemented once, whose five sizes are rows of
// a typed table. The web admin (an HTML dashboard and a JSON API behind HTTP
// Digest) and the SNMP agent on the Boreal enterprise arc answer from one live
// state; the panel follows its room's meetings through the shared behavior
// primitives.
//
// The family replaced the boreal-edge-86 YAML profile and its `products:` overrides. Its wire
// matched that profile's, proven by transcripts the profile recorded before it was deleted (#2826);
// the driver contract test now holds it.
package borealedge

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
const Name = "boreal-edge"

// Row is one size of the line: the typed row the model table is written in,
// and what a handler reads back for the product it answers for.
type Row struct {
	// Product is the catalog product the row simulates.
	Product string
	// Diagonal is the panel's size in inches.
	Diagonal int
	// Resolution is the panel's native resolution, width x height.
	Resolution string
}

// ModelName is how the panel names itself: SNMP's sysDescr, its status document
// and its dashboard.
func (r Row) ModelName() string { return fmt.Sprintf("Edge %d", r.Diagonal) }

// Summary describes the product where the console lists it.
func (r Row) Summary() string {
	return fmt.Sprintf("Boreal Edge %d-inch 4K commercial display", r.Diagonal)
}

// Rows is the model table: the Edge line, one row a size.
var Rows = []Row{
	{Product: "boreal-edge-55", Diagonal: 55, Resolution: "3840x2160"},
	{Product: "boreal-edge-65", Diagonal: 65, Resolution: "3840x2160"},
	{Product: "boreal-edge-75", Diagonal: 75, Resolution: "3840x2160"},
	{Product: "boreal-edge-86", Diagonal: 86, Resolution: "3840x2160"},
	{Product: "boreal-edge-98", Diagonal: 98, Resolution: "3840x2160"},
}

// base is the row a device of the family's own name (no product chosen) reads
// as: the 86, the size the family began as.
func base() Row { return Rows[3] }

// The state paths of the panel.
const (
	pathPower       = bind.PathPower
	pathUptime      = "status.device.uptime"
	pathBootEpoch   = "status.device.boot_epoch"
	pathInput       = "status.display.input"
	pathBrightness  = "status.display.brightness"
	pathContrast    = "status.display.contrast"
	pathTemperature = "status.display.temperature"
	pathResolution  = "status.display.resolution"
	pathVolume      = "status.audio.volume"
	pathMute        = "status.audio.mute"
	pathAddress     = "status.network.ipv4.address"
	pathMAC         = "status.network.ipv4.mac"
	pathModel       = "configuration.device.model"
	pathSerial      = "configuration.device.serial"
	pathFirmware    = "configuration.firmware.version"
)

// The choices the panel offers.
var (
	powers = []string{"on", "off", "standby"}
	inputs = []string{"hdmi1", "hdmi2", "hdmi3", "dp1", "usbc"}
)

// The enterprise number of Boreal's (fictional) arc.
const enterprise = 55520

func stateFor(r Row) map[string]any {
	sim := bind.RoomDefaults()
	return map[string]any{
		"status": map[string]any{
			"device": map[string]any{
				"power": "on",
				// The anchor of the uptime a read computes; 0 is unset, and the
				// engine seeds it to the device's start.
				"boot_epoch": 0,
			},
			"display": map[string]any{
				"input": "hdmi1", "brightness": 80, "contrast": 70, "temperature": 38, "resolution": r.Resolution,
			},
			"sim":   sim,
			"audio": map[string]any{"volume": 20, "mute": false},
			"network": map[string]any{
				"ipv4": map[string]any{"address": "", "mac": ""},
			},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.ModelName(), "manufacturer": "Boreal"},
			"firmware": map[string]any{"version": "3.4.1"},
			"network":  map[string]any{"hostname": ""},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Boreal", Model: "Edge", DeviceType: "display",
			Summary: "Boreal Edge 4K commercial display",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{
				Product: r.Product, Model: r.ModelName(), Summary: r.Summary(),
				State: map[string]any{
					"configuration": map[string]any{"device": map[string]any{"model": r.ModelName()}},
					"status":        map[string]any{"display": map[string]any{"resolution": r.Resolution}},
				},
			}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "web-admin", Protocol: "http", Port: 80, Doc: wire.Doc{Summary: "Web-based management"},
				// HTTP Digest (RFC 7616): the nonce goes stale when the console
				// expires the sessions (a reboot's effect), which a client
				// answers afresh rather than reads as a refusal.
				Auth: wire.Auth{
					Type: "digest", DefaultUser: "admin", DefaultPassword: "boreal", Realm: "Boreal Edge Admin",
					Algorithm: "MD5", Enforce: true, ExpirePath: "/api/admin/sessions/expire",
				},
			},
			{
				Name: "snmp", Protocol: "snmp", Port: 161, Doc: wire.Doc{Summary: "SNMP monitoring"},
				// A community the agent knows and refuses answers
				// authorizationError, where any other is dropped silently.
				Auth: wire.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability"}, ReachableWhen: "status.device.power != 'off'"},
		},
		State: stateFor(base()),
		HTTP:  routes(),
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks: []spec.Tick{
			bind.PowerFollowsRoom(pathInput),
			bind.Drift("temperature_drift", bind.Seconds(60), pathTemperature, behave.Band{Lo: 34, Hi: 54, Step: 1}, behave.SigFigs(3)),
		},
		Surface: surfaceOf,
	}
}

// statusDoc is GET /api/status: identity only, deliberately. The telemetry
// (power, input, volume, brightness, temperature) is the SNMP agent's, so SNMP
// is the one way in for the course's display datapoints, and firmware is only
// on the dashboard.
type statusDoc struct {
	Device struct {
		Model  string `json:"model"`
		Serial string `json:"serial"`
		Uptime int64  `json:"uptime"`
	} `json:"device"`
	Network struct {
		Address string `json:"address"`
	} `json:"network"`
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	var doc statusDoc
	doc.Device.Model, doc.Device.Serial, doc.Device.Uptime = spec.String(d, pathModel), spec.String(d, pathSerial), spec.Int64(d, pathUptime)
	doc.Network.Address = spec.String(d, pathAddress)
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(doc)}
}

// A control route answers ok and the one thing it set, read back from state.
type (
	powerAck struct {
		OK    bool   `json:"ok"`
		Power string `json:"power"`
	}
	inputAck struct {
		OK    bool   `json:"ok"`
		Input string `json:"input"`
	}
	brightnessAck struct {
		OK         bool    `json:"ok"`
		Brightness float64 `json:"brightness"`
	}
	volumeAck struct {
		OK     bool    `json:"ok"`
		Volume float64 `json:"volume"`
	}
	muteAck struct {
		OK   bool `json:"ok"`
		Mute bool `json:"mute"`
	}
	actionAck struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
	}
)

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /", Summary: "Display dashboard", Handle: dashboardRoute},
		{Match: "GET /api/status", Summary: "Device identity (model, serial, uptime, network)", Handle: statusRoute},
		httpapi.Control{
			Match: "POST /api/display/power", Summary: "Set display power state", Example: "{\"power\": \"on\"}\n",
			Fields: []httpapi.Field{{Path: pathPower, Name: "power", Kind: httpapi.Choice, Values: powers}},
			Ack:    func(d spec.Device) any { return powerAck{true, spec.String(d, pathPower)} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/display/input", Summary: "Switch display input source", Example: "{\"input\": \"hdmi1\"}\n",
			Fields: []httpapi.Field{{Path: pathInput, Name: "input", Kind: httpapi.Choice, Values: inputs}},
			Ack:    func(d spec.Device) any { return inputAck{true, spec.String(d, pathInput)} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/display/brightness", Summary: "Set display brightness (0-100)", Example: "{\"brightness\": 80}\n",
			Fields: []httpapi.Field{{Path: pathBrightness, Name: "brightness", Kind: httpapi.Number}},
			Ack:    func(d spec.Device) any { return brightnessAck{true, spec.Float(d, pathBrightness)} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/audio/volume", Summary: "Set audio volume (0-100)", Example: "{\"volume\": 30}\n",
			Fields: []httpapi.Field{{Path: pathVolume, Name: "volume", Kind: httpapi.Number}},
			Ack:    func(d spec.Device) any { return volumeAck{true, spec.Float(d, pathVolume)} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/audio/mute", Summary: "Toggle audio mute", Example: "{\"mute\": true}\n",
			Fields: []httpapi.Field{{Path: pathMute, Name: "mute", Kind: httpapi.Flag}},
			Ack:    func(d spec.Device) any { return muteAck{true, spec.Bool(d, pathMute)} },
		}.Route(),
		// A reboot clears the uptime and puts the volatile display state back.
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot display (clears uptime, resets display state)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, pathInput, pathBrightness)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
		// A factory reset puts every volatile status path back.
		httpapi.Action{
			Match: "POST /api/admin/factory-reset", Summary: "Factory reset (resets every status path to defaults)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append(
					append(httpapi.Reset(d, pathPower), surface.Change{Path: pathBootEpoch, Value: d.Now().Unix()}),
					httpapi.Reset(d, pathInput, pathBrightness, pathContrast, pathTemperature, pathResolution, pathVolume, pathMute)...)
			},
			Ack: actionAck{true, "factory-reset"},
		}.Route(),
	}
}

// surfaceOf is the SNMP agent: the MIB-II system group, then the Boreal arc.
// Firmware is deliberately absent from SNMP (and from the status document): it
// is on the dashboard only, so the course's "scrape the firmware string off the
// admin page" datapoint has exactly one way in.
func surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	arc := func(suffix string) string { return fmt.Sprintf("1.3.6.1.4.1.%d.%s", enterprise, suffix) }
	text := func(suffix, path string) surface.OID {
		return surface.Text(arc(suffix), func() string { return spec.String(d, path) }).At(path)
	}
	integer := func(suffix, path string) surface.OID {
		return surface.Integer(arc(suffix), func() int { return spec.Int(d, path) }).At(path)
	}
	oids = append(oids,
		text("1.1.1.0", pathPower),
		text("2.2.0", pathInput),
		integer("2.3.0", pathBrightness),
		integer("2.4.0", pathTemperature),
		integer("3.1.0", pathVolume),
		text("3.2.0", pathMute),
		text("4.1.0", pathAddress),
		text("4.2.0", pathMAC),
		text("5.2.0", pathSerial),
	)
	return spec.Surface{OIDs: oids}
}
