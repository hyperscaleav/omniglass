// Package polarisdsp is the Polaris DSP 16 (#2835), a rack-mounted 16-channel
// audio processor for conference rooms that mixes the microphones, cancels echo
// and routes the speakers, a Go device family of one model. It answers on five
// faces from one live state: an HTTP JSON API behind the Polaris header token
// (with a public dashboard page), an SNMP agent on the Polaris enterprise arc,
// a CLI over SSH and Telnet, a JSON-RPC control plane over raw TCP, and a UDP
// echo responder. The CLI and the control plane keep a session open and push
// what a subscribed value does, as a real processor's remote control does.
//
// It follows its room's meeting off the lab calendar through the shared
// behavior primitives: its four table and ceiling strips open a gate for every
// two attendees actually in the room and rise with the head count, and sit shut
// at the noise floor in an empty room.
//
// The family replaced the polaris-dsp-16 YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package polarisdsp

import (
	"fmt"
	"html"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
)

// Name is the family's id, the profile name its devices carry.
const Name = "polaris-dsp"

// Row is one model of the line: the typed row the model table is written in,
// and what a handler reads back for the product it answers for.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself, Firmware and HardwareRevision what it ships with.
	Product, Model, Firmware, HardwareRevision string
	// Strips are its channels, indexed from 0 as the API addresses them.
	Strips []Strip
}

// Strip is one channel of the mixer. A Gated strip is one a talker's gate opens
// and shuts, the room's microphones.
type Strip struct {
	Name   string
	Gain   int
	Mute   bool
	PeakDB int
	Gated  bool
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string {
	return fmt.Sprintf("Polaris %s, %d-channel audio DSP", r.Model, len(r.Strips))
}

// Rows is the model table: the Polaris DSP line, one model. Mutes and gains are
// the real source of truth; the peak of a strip with no gate is a static
// placeholder.
var Rows = []Row{{
	Product: "polaris-dsp-16", Model: "DSP 16", Firmware: "1.8.0", HardwareRevision: "A3",
	Strips: []Strip{
		{Name: "Table Mic 1", Gain: -6, PeakDB: -42, Gated: true},
		{Name: "Table Mic 2", Gain: -6, PeakDB: -38, Gated: true},
		{Name: "Ceiling Mic 1", Gain: -3, PeakDB: -45, Gated: true},
		{Name: "Ceiling Mic 2", Gain: -3, PeakDB: -50, Gated: true},
		{Name: "Wireless 1", Mute: true, PeakDB: -90},
		{Name: "Wireless 2", Mute: true, PeakDB: -90},
		{Name: "Program In L", Gain: -12, PeakDB: -20},
		{Name: "Program In R", Gain: -12, PeakDB: -22},
		{Name: "Aux In 1", Mute: true, PeakDB: -90},
		{Name: "Aux In 2", Mute: true, PeakDB: -90},
		{Name: "Aux In 3", Mute: true, PeakDB: -90},
		{Name: "Aux In 4", Mute: true, PeakDB: -90},
		{Name: "Matrix Out A", PeakDB: -30},
		{Name: "Matrix Out B", PeakDB: -32},
		{Name: "Matrix Out C", PeakDB: -36},
		{Name: "Matrix Out D", PeakDB: -35},
	},
}}

// base is the row a device of the family's own name (no product chosen) reads as.
func base() Row { return Rows[0] }

// The state paths of the processor.
const (
	pathUptime      = "status.device.uptime"
	pathBootEpoch   = "status.device.boot_epoch"
	pathCPU         = "status.dsp.cpu_load"
	pathActive      = "status.dsp.active_channels"
	pathTemperature = "status.dsp.temperature"
	pathFan         = "status.dsp.fan_rpm"
	pathOutMute     = "status.output.mute"
	pathOutLevel    = "status.output.level_db"
	pathAddress     = "status.network.ipv4.address"
	pathMAC         = "status.network.ipv4.mac"
	pathModel       = "configuration.device.model"
	pathSerial      = "configuration.device.serial"
	pathHardware    = "configuration.device.hardware_revision"
	pathFirmware    = "configuration.firmware.version"
	pathHostname    = "configuration.network.hostname"
	listChannels    = "status.channels"
)

func channelPath(i int, field string) string { return fmt.Sprintf("%s.%d.%s", listChannels, i, field) }

// Polaris's (fictional) enterprise number.
const enterprise = 55540

// apiToken is the token every /api request carries in X-Polaris-Token, and the
// credentials the CLI asks for.
const (
	apiToken    = "polaris-lab-token"
	cliUser     = "admin"
	cliPassword = "polaris"
)

func stateFor(r Row) map[string]any {
	channels := make([]any, len(r.Strips))
	for i, s := range r.Strips {
		c := map[string]any{"index": i, "name": s.Name, "gain": s.Gain, "mute": s.Mute, "peak_db": s.PeakDB}
		if s.Gated {
			c["gate"] = false
		}
		channels[i] = c
	}
	return map[string]any{
		"status": map[string]any{
			// The anchor of the uptime a read computes; 0 is unset, and the
			// engine seeds it to the device's start.
			"device": map[string]any{"boot_epoch": 0},
			"dsp":    map[string]any{"cpu_load": 22, "active_channels": 8, "temperature": 42, "fan_rpm": 3800},
			// The program output: unmuted, at a level a room is run at.
			"output":   map[string]any{"mute": false, "level_db": -18},
			"channels": channels,
			"network":  map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Polaris", "hardware_revision": r.HardwareRevision},
			"firmware": map[string]any{"version": r.Firmware},
			"network":  map[string]any{"hostname": ""},
		},
	}
}

// session is what a persistent CLI face shares: the framing and the limits.
func session(name, protocol string, port int, summary string) profile.Interface {
	return profile.Interface{
		Name: name, Protocol: protocol, Port: port, APISurface: Name,
		Doc:        profile.Doc{Summary: summary, Tags: []string{"management", "cli", "persistent-session"}},
		Auth:       profile.Auth{Type: "password", DefaultUser: cliUser, DefaultPassword: cliPassword},
		Persistent: true, LineEnding: "\r\n", Commands: listed(protocol == "telnet"),
		Welcome:  "Polaris DSP CLI ready. 'subscribe <path>' for live updates.",
		Limits:   profile.SessionLimits{MaxSubsPerSession: 64, PublishMinIntervalMs: 50, MaxSessionsPerDevice: 16, OutBuffer: 256},
		Subjects: cliSubjects(),
	}
}

// Spec is the family.
func Spec() spec.Spec {
	r := base()
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Polaris", Model: r.Model, DeviceType: "audio-dsp", Summary: r.Summary(),
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, Summary: r.Summary(), State: map[string]any{
				"configuration": map[string]any{
					"device":   map[string]any{"model": r.Model, "hardware_revision": r.HardwareRevision},
					"firmware": map[string]any{"version": r.Firmware},
				},
			}}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, APISurface: Name,
				Doc: profile.Doc{Summary: "Polaris web and JSON API", Tags: []string{"management", "api"}},
				// Header-based API key: every /api request must carry the token,
				// otherwise the listener answers 401 naming the header it
				// expects. The dashboard page is public so an operator can paste
				// their token into it; every /api endpoint stays gated.
				Auth: profile.Auth{Type: "header", Header: "X-Polaris-Token", APIKey: apiToken, Enforce: true, PublicPaths: []string{"/"}},
			},
			session("ssh-console", "ssh", 22, "SSH CLI with persistent-session subscriptions"),
			session("telnet-console", "telnet", 23, "Telnet CLI with persistent-session subscriptions"),
			qrcFace(),
			{
				Name: "snmp", Protocol: "snmp", Port: 161, APISurface: Name,
				Doc: profile.Doc{Summary: "SNMP monitoring", Tags: []string{"monitoring"}},
				// A community the agent knows and refuses answers
				// authorizationError, where any other is dropped silently.
				Auth: profile.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{
				Name: "udp-echo", Protocol: "udp", Port: 9000, APISurface: Name,
				Doc: profile.Doc{Summary: "UDP echo responder for custom protocol probing", Tags: []string{"diagnostic"}},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: stateFor(r),
		HTTP:  routes(),
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   ticks(),
		Surface: surfaceOf,
	}
}

// ---- the HTTP API ----

type (
	dspDoc struct {
		CPULoad        float64 `json:"cpu_load"`
		ActiveChannels float64 `json:"active_channels"`
		Temperature    float64 `json:"temperature"`
		FanRPM         float64 `json:"fan_rpm"`
	}
	outputDoc struct {
		Mute    bool    `json:"mute"`
		LevelDB float64 `json:"level_db"`
	}
	// channelDoc is a strip as the API lists it, the keys in the order the
	// document gives them; Gate is on the strips that have one.
	channelDoc struct {
		Gain   float64 `json:"gain"`
		Gate   *bool   `json:"gate,omitempty"`
		Index  int     `json:"index"`
		Mute   bool    `json:"mute"`
		Name   string  `json:"name"`
		PeakDB float64 `json:"peak_db"`
	}
)

func dsp(d spec.Device) dspDoc {
	return dspDoc{CPULoad: spec.Float(d, pathCPU), ActiveChannels: spec.Float(d, pathActive), Temperature: spec.Float(d, pathTemperature), FanRPM: spec.Float(d, pathFan)}
}

func channels(d spec.Device) []channelDoc {
	list, _ := d.Read(listChannels).([]any)
	out := make([]channelDoc, len(list))
	for i := range list {
		c := channelDoc{
			Gain: spec.Float(d, channelPath(i, "gain")), Index: spec.Int(d, channelPath(i, "index")), Mute: spec.Bool(d, channelPath(i, "mute")),
			Name: spec.String(d, channelPath(i, "name")), PeakDB: spec.Float(d, channelPath(i, "peak_db")),
		}
		if d.Read(channelPath(i, "gate")) != nil {
			gate := spec.Bool(d, channelPath(i, "gate"))
			c.Gate = &gate
		}
		out[i] = c
	}
	return out
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.Identity{
		Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware), Uptime: spec.Int64(d, pathUptime),
	}
	out := outputDoc{Mute: spec.Bool(d, pathOutMute), LevelDB: spec.Float(d, pathOutLevel)}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Status(id, httpapi.UptimeFirst, httpapi.Block("dsp", dsp(d)), httpapi.Block("output", out))}
}

func channelsRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Sections(
		httpapi.Inline("active_channels", spec.Float(d, pathActive)), httpapi.Inline("channels", channels(d)))}
}

// statusXML is the same status as a namespaced document (#902): the DSP's own
// namespace by default and its system facts under a second prefix, the shape an
// XPath extraction declares its prefixes for.
func statusXML(d spec.Device) string {
	esc := func(s string) string { return html.EscapeString(s) }
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<status xmlns=\"urn:polaris:dsp:1\" xmlns:sys=\"urn:polaris:system:1\">\n")
	fmt.Fprintf(&b, "  <sys:device model=\"%s\" serial=\"%s\">\n", esc(spec.String(d, pathModel)), esc(spec.String(d, pathSerial)))
	fmt.Fprintf(&b, "    <sys:firmware>%s</sys:firmware>\n", esc(spec.String(d, pathFirmware)))
	fmt.Fprintf(&b, "    <sys:uptime unit=\"s\">%d</sys:uptime>\n", spec.Int64(d, pathUptime))
	b.WriteString("  </sys:device>\n  <dsp>\n")
	fmt.Fprintf(&b, "    <cpuLoad unit=\"%%\">%s</cpuLoad>\n", esc(spec.String(d, pathCPU)))
	fmt.Fprintf(&b, "    <temperature unit=\"C\">%s</temperature>\n", esc(spec.String(d, pathTemperature)))
	fmt.Fprintf(&b, "    <fan rpm=\"%s\"/>\n", esc(spec.String(d, pathFan)))
	b.WriteString("  </dsp>\n</status>\n")
	return b.String()
}

// An acknowledgment answers ok and what it changed, read back from state.
type (
	channelAck struct {
		OK bool `json:"ok"`
	}
	outputAck struct {
		OK   bool `json:"ok"`
		Mute bool `json:"mute"`
	}
	actionAck struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
	}
)

// rebootPaths are the volatile telemetry a reboot puts back to its defaults,
// beside the uptime it restarts; a factory reset puts back every one of them.
var (
	rebootPaths       = []string{pathCPU, pathTemperature, pathOutMute, pathOutLevel}
	factoryResetPaths = []string{pathCPU, pathActive, pathTemperature, pathFan, pathOutMute, pathOutLevel}
)

// restart is what a reboot or a factory reset writes: the uptime restarts and
// each path in reset goes back to the value the device was built with. The HTTP
// routes and the CLI verbs of the same name are the one list.
func restart(d spec.Device, reset []string) []surface.Change {
	return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, reset...)...)
}

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /", Summary: "DSP dashboard", Handle: dashboardRoute},
		{Match: "GET /api/status", Summary: "DSP status snapshot", Handle: statusRoute},
		{Match: "GET /api/status.xml", Summary: "DSP status snapshot as namespaced XML", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return spec.HTTPResponse{ContentType: "application/xml", Body: statusXML(d)}
		}},
		{Match: "GET /api/channels", Summary: "Per-channel state (gain, mute, peak)", Handle: channelsRoute},
		httpapi.Element{
			Match: "POST /api/channels/mute", Summary: "Mute a channel by index", Example: "{\"index\": 0, \"mute\": true}\n",
			IndexField: "index", List: listChannels,
			Fields: []httpapi.Field{{Path: "mute", Name: "mute", Kind: httpapi.Flag}},
			Ack:    func(spec.Device) any { return channelAck{true} },
		}.Route(),
		httpapi.Element{
			Match: "POST /api/channels/gain", Summary: "Set channel gain (-60 to +12 dB)", Example: "{\"index\": 0, \"gain\": -6}\n",
			IndexField: "index", List: listChannels,
			Fields: []httpapi.Field{{Path: "gain", Name: "gain", Kind: httpapi.Number}},
			Ack:    func(spec.Device) any { return channelAck{true} },
		}.Route(),
		// The program output (#1493): the bus that feeds the room, whose mute and
		// level the DSP reports over SNMP. The per-channel strips above are a
		// separate surface.
		httpapi.Control{
			Match: "POST /api/output/mute", Summary: "Mute or unmute the program output", Example: "{\"mute\": true}\n",
			Fields: []httpapi.Field{{Path: pathOutMute, Name: "mute", Kind: httpapi.Switch}},
			Ack:    func(d spec.Device) any { return outputAck{true, spec.Bool(d, pathOutMute)} },
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot DSP (clears uptime, resets cpu/temp drift)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change { return restart(d, rebootPaths) },
			Ack:    actionAck{true, "reboot"},
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/factory-reset", Summary: "Factory reset (resets every DSP status path to defaults)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change { return restart(d, factoryResetPaths) },
			Ack:    actionAck{true, "factory-reset"},
		}.Route(),
	}
}

// ---- behavior ----

// ticks are the processor's behaviors, each a shared primitive bound to the
// state it reads.
func ticks() []spec.Tick {
	gated := gatedStrips(base())
	writes := []string{pathActive}
	for i := range gated {
		writes = append(writes, channelPath(i, "gate"), channelPath(i, "peak_db"))
	}
	return []spec.Tick{
		bind.Drift("cpu_drift", bind.Seconds(30), pathCPU, behave.Band{Lo: 15, Hi: 65, Step: 3}, behave.SigFigs(3)),
		bind.Drift("temperature_drift", bind.Seconds(60), pathTemperature, behave.Band{Lo: 38, Hi: 58, Step: 1}, behave.SigFigs(3)),
		// Program audio rides speech: the output level moves every few seconds
		// within the band a room is run at, a behavior a scenario can drive.
		bind.Drift("output_level_drift", bind.Seconds(15), pathOutLevel, behave.Band{Lo: -40, Hi: -6, Step: 3}, behave.Places(1)),
		// The room's talkers, off the lab calendar through the device clock: each
		// gated strip opens a gate for every two attendees actually in the room
		// (a no-show opens none), and active_channels is the count of open
		// gates. A strip an operator muted stays shut. A room with no calendar
		// leaves the strips as declared.
		{
			Name: "room_signal", Interval: bind.Seconds(15), OnStart: true, Writes: writes,
			Run: func(d spec.Device, c spec.TickContext) []surface.Change {
				prev := behave.GateState{Active: spec.Int(d, pathActive), Strips: make([]behave.Strip, len(gated))}
				for i := range gated {
					prev.Strips[i] = behave.Strip{Muted: spec.Bool(d, channelPath(i, "mute")), Gate: spec.Bool(d, channelPath(i, "gate")), PeakDB: spec.Float(d, channelPath(i, "peak_db"))}
				}
				next := behave.Gates(prev, c.Room, behave.DefaultGates, c.Rand)
				out := []surface.Change{{Path: pathActive, Value: next.Active}}
				for i, s := range next.Strips {
					out = append(out, surface.Change{Path: channelPath(i, "gate"), Value: s.Gate}, surface.Change{Path: channelPath(i, "peak_db"), Value: s.PeakDB})
				}
				return out
			},
		},
	}
}

// gatedStrips are the indexes of the strips a talker's gate opens.
func gatedStrips(r Row) []int {
	var out []int
	for i, s := range r.Strips {
		if s.Gated {
			out = append(out, i)
		}
	}
	return out
}

// ---- SNMP ----

// surfaceOf is the SNMP agent: the MIB-II system group (and sysName, the
// hostname), then the Polaris arc. The load, active channels and temperature are
// whole numbers, the fan a counter; the rest read as text, as the agent has
// always answered them.
func surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	arc := func(suffix string) string { return fmt.Sprintf("1.3.6.1.4.1.%d.%s", enterprise, suffix) }
	text := func(oid, path string) surface.OID {
		return surface.Text(oid, func() string { return spec.String(d, path) }).At(path)
	}
	integer := func(suffix, path string) surface.OID {
		return surface.Integer(arc(suffix), func() int { return spec.Int(d, path) }).At(path)
	}
	oids = append(oids,
		integer("2.1.0", pathCPU),
		integer("2.2.0", pathActive),
		integer("2.3.0", pathTemperature),
		surface.Counter(arc("2.4.0"), func() uint { return uint(spec.Int64(d, pathFan)) }).At(pathFan),
		text(arc("2.5.0"), pathOutMute),
		text(arc("2.6.0"), pathOutLevel),
		// Identity (#2754): sysName, then the hardware revision, address and MAC
		// beside the serial on the Polaris arc.
		text("1.3.6.1.2.1.1.5.0", pathHostname),
		text(arc("5.3.0"), pathHardware),
		text(arc("5.4.0"), pathAddress),
		text(arc("5.5.0"), pathMAC),
	)
	oids = append(oids, snmp.Identity{
		Enterprise: enterprise,
		Firmware:   func() string { return spec.String(d, pathFirmware) }, FirmwarePath: pathFirmware,
		Serial: func() string { return spec.String(d, pathSerial) }, SerialPath: pathSerial,
	}.OIDs()...)
	return spec.Surface{OIDs: oids, Commanders: []surface.Commander{newShell(d)}}
}
