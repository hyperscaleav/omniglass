// Package polarisamp is the Polaris Amp 4-100 (#2835), a rack-mounted
// four-channel power amplifier, 100 W a channel, the stage after the DSP in a
// room's audio rack: each channel drives a passive loudspeaker zone. It is a Go
// device family of one model.
//
// Its monitoring face is SNMP under the Polaris enterprise arc, the
// amplifier's branch at .3: the chassis power state, the protect state (an
// amplifier mutes its outputs and latches protect on a fault), the heatsink
// temperature and fan, and a channel table of output level, clip and load. Its
// control face is the HTTP JSON API the DSP has, behind the same header token:
// standby and wake, and the protect reset an operator reaches for once the
// fault is cleared.
//
// It follows its room's meeting off the lab calendar through the shared
// behavior primitives: its outputs carry the program and its heatsink warms
// while the room is in use, and it sits at its floor with signal absent
// otherwise.
//
// The family replaced the polaris-amp-4100 YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package polarisamp

import (
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
)

// Name is the family's id, the profile name its devices carry.
const Name = "polaris-amp"

// Row is one model of the line: the typed row the model table is written in.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself, Firmware the version it ships with.
	Product, Model, Firmware string
	// Zones are the channels, each driving a loudspeaker zone.
	Zones []Zone
}

// Zone is one channel: the loudspeaker zone it drives, the level it starts at,
// and the level it wakes to from silence when its room fills.
type Zone struct {
	Name     string
	OutputDB int
	WakeDB   float64
	LoadOhms int
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string {
	return fmt.Sprintf("Polaris %s, %d-channel power amplifier", r.Model, len(r.Zones))
}

// Rows is the model table: the Polaris amplifier line, one model. Each zone is
// a ceiling-speaker zone; 8 ohms is a healthy zone's load.
var Rows = []Row{{
	Product: "polaris-amp-4100", Model: "Amp 4-100", Firmware: "2.3.1",
	Zones: []Zone{
		{Name: "Zone 1", OutputDB: -18, WakeDB: -24, LoadOhms: 8},
		{Name: "Zone 2", OutputDB: -18, WakeDB: -24, LoadOhms: 8},
		{Name: "Zone 3", OutputDB: -21, WakeDB: -27, LoadOhms: 8},
		{Name: "Zone 4", OutputDB: -21, WakeDB: -27, LoadOhms: 8},
	},
}}

// base is the row a device of the family's own name (no product chosen) reads as.
func base() Row { return Rows[0] }

// The state paths of the amplifier.
const (
	pathUptime      = "status.device.uptime"
	pathBootEpoch   = "status.device.boot_epoch"
	pathPower       = "status.amp.power"
	pathProtect     = "status.amp.protect"
	pathSignal      = "status.amp.signal_present"
	pathTemperature = "status.amp.temperature"
	pathFan         = "status.amp.fan_rpm"
	pathModel       = "configuration.device.model"
	pathSerial      = "configuration.device.serial"
	pathFirmware    = "configuration.firmware.version"
)

// Protect states, and the power states the API takes.
const (
	protectNormal = "normal"
	powerOn       = "on"
)

var powers = []string{powerOn, "standby"}

// Polaris's (fictional) enterprise number. The amplifier's branch is .3: scalars
// at .3.1 to .3.4, and a channel table at .3.10.1.<column>.<channel>.
const enterprise = 55540

// The columns of the channel table.
const (
	colOutput = 2
	colClip   = 3
	colLoad   = 4
)

func channelPath(i int, field string) string { return fmt.Sprintf("status.channels.%d.%s", i, field) }

func stateFor(r Row) map[string]any {
	channels := make([]any, len(r.Zones))
	for i, z := range r.Zones {
		channels[i] = map[string]any{"index": i, "name": z.Name, "output_db": z.OutputDB, "clip": false, "load_ohms": z.LoadOhms}
	}
	return map[string]any{
		"status": map[string]any{
			"device":   map[string]any{"boot_epoch": 0},
			"amp":      map[string]any{"power": powerOn, "protect": protectNormal, "signal_present": false, "temperature": 41, "fan_rpm": 2800},
			"channels": channels,
			"network":  map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Polaris"},
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
			Manufacturer: "Polaris", Model: base().Model, DeviceType: "amplifier", Summary: base().Summary(),
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, Summary: r.Summary(), State: map[string]any{
				"configuration": map[string]any{"device": map[string]any{"model": r.Model}, "firmware": map[string]any{"version": r.Firmware}},
			}}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, APISurface: Name,
				Doc:  profile.Doc{Summary: "Polaris JSON control API", Tags: []string{"management", "api"}},
				Auth: profile.Auth{Type: "header", Header: "X-Polaris-Token", APIKey: "polaris-lab-token", Enforce: true},
			},
			{
				Name: "snmp", Protocol: "snmp", Port: 161, APISurface: Name,
				Doc: profile.Doc{Summary: "SNMP monitoring", Tags: []string{"monitoring"}},
				// A community the agent knows and refuses answers
				// authorizationError, where any other is dropped silently.
				Auth: profile.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: stateFor(base()),
		HTTP:  routes(),
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   ticks(),
		Surface: surfaceOf,
	}
}

// channelDoc is a channel as the status document lists it, the keys in the
// order the document gives them.
type channelDoc struct {
	Clip     bool    `json:"clip"`
	Index    int     `json:"index"`
	LoadOhms float64 `json:"load_ohms"`
	Name     string  `json:"name"`
	OutputDB float64 `json:"output_db"`
}

type ampDoc struct {
	Power         string  `json:"power"`
	Protect       string  `json:"protect"`
	SignalPresent bool    `json:"signal_present"`
	Temperature   float64 `json:"temperature"`
	FanRPM        float64 `json:"fan_rpm"`
}

func channels(d spec.Device) []channelDoc {
	list, _ := d.Read("status.channels").([]any)
	out := make([]channelDoc, len(list))
	for i := range list {
		out[i] = channelDoc{
			Clip: spec.Bool(d, channelPath(i, "clip")), Index: spec.Int(d, channelPath(i, "index")), LoadOhms: spec.Float(d, channelPath(i, "load_ohms")),
			Name: spec.String(d, channelPath(i, "name")), OutputDB: spec.Float(d, channelPath(i, "output_db")),
		}
	}
	return out
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.Identity{
		Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware), Uptime: spec.Int64(d, pathUptime),
	}
	amp := ampDoc{
		Power: spec.String(d, pathPower), Protect: spec.String(d, pathProtect), SignalPresent: spec.Bool(d, pathSignal),
		Temperature: spec.Float(d, pathTemperature), FanRPM: spec.Float(d, pathFan),
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Status(id, httpapi.UptimeFirst,
		httpapi.Block("amp", amp), httpapi.Inline("channels", channels(d)))}
}

// An acknowledgment answers ok and the one thing it changed, read back from state.
type (
	powerAck struct {
		OK    bool   `json:"ok"`
		Power string `json:"power"`
	}
	protectAck struct {
		OK      bool   `json:"ok"`
		Protect string `json:"protect"`
	}
	actionAck struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
	}
)

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /api/status", Summary: "Amplifier status snapshot (power, protect, thermal, channels)", Handle: statusRoute},
		httpapi.Control{
			Match: "POST /api/power", Summary: "Enter standby or wake (on, standby)", Example: "{\"power\": \"standby\"}\n",
			Fields: []httpapi.Field{{Path: pathPower, Name: "power", Kind: httpapi.Choice, Values: powers}},
			Ack:    func(d spec.Device) any { return powerAck{true, spec.String(d, pathPower)} },
		}.Route(),
		// Protect latches: an amplifier stays muted after the fault clears until
		// an operator resets it. A scenario trips it (status.amp.protect to
		// "protect"); this is the way back.
		httpapi.Action{
			Match: "POST /api/protect/reset", Summary: "Clear a latched protect state", Example: "{}",
			Writes: func(spec.Device) []surface.Change { return []surface.Change{{Path: pathProtect, Value: protectNormal}} },
			Ack:    protectAck{true, protectNormal},
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot amplifier (clears uptime and a latched protect)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, pathProtect, pathTemperature)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
	}
}

// poweredOn says the amplifier is on, as the primitives read it.
func poweredOn(d spec.Device) bool { return spec.String(d, pathPower) == powerOn }

// ticks are the amplifier's behaviors, each a shared primitive bound to the
// state it reads.
func ticks() []spec.Tick {
	zones := base().Zones
	outputs := []string{pathSignal}
	for i := range zones {
		outputs = append(outputs, channelPath(i, "output_db"))
	}
	return []spec.Tick{
		{
			Name: "temperature_drift", Interval: time.Minute, Writes: []string{pathTemperature},
			Run: func(d spec.Device, c spec.TickContext) []surface.Change {
				t := behave.AmpTemperature(spec.Float(d, pathTemperature), c.Room, poweredOn(d), behave.DefaultAmp, c.Rand)
				return []surface.Change{{Path: pathTemperature, Value: t}}
			},
		},
		bind.Drift("fan_drift", bind.Seconds(60), pathFan, behave.Band{Lo: 2200, Hi: 3600, Step: 80}, behave.Places(0)),
		{
			Name: "output_drift", Interval: 30 * time.Second, Writes: outputs,
			Run: func(d spec.Device, c spec.TickContext) []surface.Change {
				on := poweredOn(d)
				out := []surface.Change{{Path: pathSignal, Value: behave.AmpSignalPresent(spec.Bool(d, pathSignal), c.Room, on)}}
				for i, z := range zones {
					p := channelPath(i, "output_db")
					out = append(out, surface.Change{Path: p, Value: behave.AmpOutputLevel(spec.Float(d, p), c.Room, on, z.WakeDB, behave.DefaultAmp, c.Rand)})
				}
				return out
			},
		},
	}
}

// surfaceOf is the SNMP agent: the MIB-II system group, then the Polaris arc.
// Temperature is a whole number of degrees and the fan a counter; the rest read
// as text, as the agent has always answered them.
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
		text("3.1.0", pathPower),
		text("3.2.0", pathProtect),
		surface.Integer(arc("3.3.0"), func() int { return spec.Int(d, pathTemperature) }).At(pathTemperature),
		surface.Counter(arc("3.4.0"), func() uint { return uint(spec.Int64(d, pathFan)) }).At(pathFan),
	)
	for _, col := range []struct {
		n     int
		field string
	}{{colOutput, "output_db"}, {colClip, "clip"}, {colLoad, "load_ohms"}} {
		for i := range base().Zones {
			oids = append(oids, text(fmt.Sprintf("3.10.1.%d.%d", col.n, i+1), channelPath(i, col.field)))
		}
	}
	oids = append(oids, snmp.Identity{
		Enterprise: enterprise,
		Firmware:   func() string { return spec.String(d, pathFirmware) }, FirmwarePath: pathFirmware,
		Serial: func() string { return spec.String(d, pathSerial) }, SerialPath: pathSerial,
	}.OIDs()...)
	return spec.Surface{OIDs: oids}
}
