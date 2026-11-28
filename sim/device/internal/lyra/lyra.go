// Package lyra is what the Lyra microphone families share (#2835): the status
// document, the mute control, the SNMP agent on the Lyra enterprise arc and the
// behaviors that make a microphone follow its room. A Lyra microphone answers one
// small API: an HTTP JSON status and mute behind the Lyra header key, and mute
// and the input level (and a wireless pack's battery) over SNMP. The ceiling array
// and the wireless system are different kinds of device with their own collector,
// so each is its own family (sim/device/lyraarc, sim/device/lyrahx) over the one
// body written here, its models the rows of its own table.
//
// It sits under sim/device/internal, so only families import it and the
// registry scan skips it.
package lyra

import (
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
)

// Row is one model of a line: the typed row a model table is written in, and
// what a handler reads back for the product it answers for.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself, Firmware the version it ships with.
	Product, Model, Firmware string
	// DeviceType and Kind describe it where the console lists it.
	DeviceType, Kind string
	// Wireless is a handheld pack and its networked receiver: it transmits
	// where an array is active, and reports its battery. Every row of a line
	// is the same kind.
	Wireless bool
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string { return fmt.Sprintf("Lyra %s, %s", r.Model, r.Kind) }

// Line is a family of Lyra microphones: its id, the profile name its devices
// carry, and the rows of its model table.
type Line struct {
	Name string
	Rows []Row
}

// The state paths of a microphone.
const (
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
	pathMute      = "status.mic.mute"
	pathLevel     = "status.mic.level_db"
	pathActive    = "status.mic.active"
	pathTransmit  = "status.mic.transmitting"
	pathBattery   = "status.mic.battery_percent"
	pathModel     = "configuration.device.model"
	pathSerial    = "configuration.device.serial"
	pathFirmware  = "configuration.firmware.version"
)

// The enterprise number of Lyra's (fictional) arc, the key a request carries and
// the one the lab seeds the collector with, and the name of the wire API both
// families speak.
const (
	enterprise = 55530
	apiKey     = "lyra-lab-key"
	apiSurface = "lyra-mic"
)

// activePath is where a row keeps whether the microphone hears the room.
func (r Row) activePath() string {
	if r.Wireless {
		return pathTransmit
	}
	return pathActive
}

// stateFor is the part of a row's state that is its own: what it is, what it
// ships with, and the mic keys its kind reports.
func stateFor(r Row) map[string]any {
	mic := map[string]any{"active": false}
	if r.Wireless {
		mic = map[string]any{"transmitting": false, "battery_percent": 78.0}
	}
	return map[string]any{
		"configuration": map[string]any{"device": map[string]any{"model": r.Model}, "firmware": map[string]any{"version": r.Firmware}},
		"status":        map[string]any{"mic": mic},
	}
}

// baseState is a device of the line's own name (no product chosen): the first
// row, whole.
func baseState(r Row) map[string]any {
	s := map[string]any{
		"status": map[string]any{
			"device":  map[string]any{"boot_epoch": 0},
			"mic":     map[string]any{"mute": false, "level_db": -42},
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Lyra"},
			"firmware": map[string]any{"version": r.Firmware},
			"network":  map[string]any{"hostname": ""},
		},
	}
	mic := s["status"].(map[string]any)["mic"].(map[string]any)
	for k, v := range stateFor(r)["status"].(map[string]any)["mic"].(map[string]any) {
		mic[k] = v
	}
	return s
}

// rowOf is the row a device is a microphone of: its product's, the line's first
// for a device of the line's own name.
func (l Line) rowOf(d spec.Device) Row {
	if r, ok := spec.Row[Row](d.Model()); ok {
		return r
	}
	return l.Rows[0]
}

// Spec is the family.
func (l Line) Spec() spec.Spec {
	first := l.Rows[0]
	return spec.Spec{
		Name: l.Name,
		Identity: spec.Identity{
			Manufacturer: "Lyra", Model: first.Model, DeviceType: first.DeviceType, Summary: first.Summary(),
		},
		Models: spec.Table(l.Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, DeviceType: r.DeviceType, Summary: r.Summary(), State: stateFor(r)}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, APISurface: apiSurface,
				Doc:  profile.Doc{Summary: "Lyra control API", Tags: []string{"management", "api"}},
				Auth: profile.Auth{Type: "header", Header: "X-Lyra-Key", APIKey: apiKey, Enforce: true},
			},
			{
				Name: "snmp", Protocol: "snmp", Port: 161, APISurface: apiSurface,
				Doc: profile.Doc{Summary: "SNMP monitoring", Tags: []string{"monitoring"}},
				// A community the agent knows and refuses answers
				// authorizationError, where any other is dropped silently.
				Auth: profile.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: baseState(first),
		HTTP:  l.routes(),
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   l.ticks(),
		Surface: l.surfaceOf,
	}
}

// micArc and micPack are the "mic" block of the status document: the keys a
// microphone reports, in the order the document lists them.
type (
	micArc struct {
		Active  bool    `json:"active"`
		LevelDB float64 `json:"level_db"`
		Mute    bool    `json:"mute"`
	}
	micPack struct {
		BatteryPercent float64 `json:"battery_percent"`
		LevelDB        float64 `json:"level_db"`
		Mute           bool    `json:"mute"`
		Transmitting   bool    `json:"transmitting"`
	}
)

func (l Line) statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.Identity{
		Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware), Uptime: spec.Int64(d, pathUptime),
	}
	mute, level := spec.Bool(d, pathMute), spec.Float(d, pathLevel)
	var mic any = micArc{Active: spec.Bool(d, pathActive), LevelDB: level, Mute: mute}
	if l.Rows[0].Wireless {
		mic = micPack{BatteryPercent: spec.Float(d, pathBattery), LevelDB: level, Mute: mute, Transmitting: spec.Bool(d, pathTransmit)}
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Status(id, httpapi.ModelFirst, httpapi.Inline("mic", mic))}
}

type muteAck struct {
	OK   bool `json:"ok"`
	Mute bool `json:"mute"`
}

func (l Line) routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /api/status", Summary: "Microphone identity and state", Handle: l.statusRoute},
		httpapi.Control{
			Match: "POST /api/mute", Summary: "Mute or unmute the microphone", Example: "{\"mute\": true}\n",
			Fields: []httpapi.Field{{Path: pathMute, Name: "mute", Kind: httpapi.Switch}},
			Ack:    func(d spec.Device) any { return muteAck{true, spec.Bool(d, pathMute)} },
		}.Route(),
	}
}

// ticks are the microphone's behaviors, each a shared primitive bound to the
// state it reads; a wireless pack adds its battery.
func (l Line) ticks() []spec.Tick {
	active := l.Rows[0].activePath()
	out := []spec.Tick{
		{
			Name: "call_state", Interval: 15 * time.Second, OnStart: true, Writes: []string{active, pathMute},
			Run: func(d spec.Device, c spec.TickContext) []surface.Change {
				next := behave.MicActivity(micOf(d, active), c.Room, behave.DefaultMic, c.Rand)
				return []surface.Change{{Path: active, Value: next.Active}, {Path: pathMute, Value: next.Mute}}
			},
		},
		{
			Name: "level_drift", Interval: 15 * time.Second, Writes: []string{pathLevel},
			Run: func(d spec.Device, c spec.TickContext) []surface.Change {
				return []surface.Change{{Path: pathLevel, Value: behave.MicLevel(micOf(d, active), c.Room, behave.DefaultMic, c.Rand)}}
			},
		},
	}
	if l.Rows[0].Wireless {
		out = append(out, spec.Tick{
			Name: "battery_drain", Interval: time.Minute, Writes: []string{pathBattery},
			Run: func(d spec.Device, c spec.TickContext) []surface.Change {
				var spans []behave.Span
				if c.Spans != nil {
					spans = c.Spans(c.Now.Add(-behave.DefaultBattery.Window), c.Now)
				}
				return []surface.Change{{Path: pathBattery, Value: behave.Battery(spec.Float(d, pathBattery), c.Room.HasCalendar, spans, c.Now, behave.DefaultBattery)}}
			},
		})
	}
	return out
}

// micOf is the microphone as the primitives read it.
func micOf(d spec.Device, active string) behave.MicState {
	return behave.MicState{Active: spec.Bool(d, active), Mute: spec.Bool(d, pathMute), LevelDB: spec.Float(d, pathLevel)}
}

// surfaceOf is the SNMP agent: the MIB-II system group, then the Lyra arc, the
// microphone at .1 (mute, the input level in dBFS), the wireless link's pack at
// .2.1 (its battery, which an array does not answer) and identity at .5.
func (l Line) surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	arc := func(suffix string) string { return fmt.Sprintf("1.3.6.1.4.1.%d.%s", enterprise, suffix) }
	text := func(suffix, path string) surface.OID {
		return surface.Text(arc(suffix), func() string { return spec.String(d, path) }).At(path)
	}
	oids = append(oids, text("1.1.0", pathMute), text("1.2.0", pathLevel))
	if l.Rows[0].Wireless {
		oids = append(oids, text("2.1.0", pathBattery))
	}
	oids = append(oids, snmp.Identity{
		Enterprise: enterprise,
		Firmware:   func() string { return spec.String(d, pathFirmware) }, FirmwarePath: pathFirmware,
		Serial: func() string { return spec.String(d, pathSerial) }, SerialPath: pathSerial,
	}.OIDs()...)
	return spec.Surface{OIDs: oids}
}
