// Package newtronpanel is the Newtron Panel 7 touch panel (#2833), a Go device
// family of one model: the wall-mounted room interface that starts meetings,
// selects sources and shows what the room is doing. Its faces are the web admin
// behind a form sign-in (an HTML dashboard and a JSON API), an SSH shell, the
// NVP line protocol on a raw TCP port, over TLS, over Telnet behind a login and
// as UDP datagrams, and a deliberately dead SNMP agent that answers only the
// system group: every operational signal an integrator wants lives behind the
// web API and the shell. One room surface (the shared sim/surface/nvp) serves
// all five line faces.
//
// It follows the lab calendar: it wakes two minutes before a booked meeting
// someone attends (at the start of a walk-in) and sleeps five minutes after,
// and its active page is the call while the meeting is held, sharing through
// the stretch content is shared, and idle otherwise.
//
// The family replaced the newtron-panel-7 YAML profile; its wire matched the profile's before the
// profile was deleted (#2826); the driver contract test now holds it.
package newtronpanel

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/nvp"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry.
const Name = "newtron-panel"

// Row is one model of the touch panel line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	// Inches is the screen's diagonal.
	Inches int
	// Firmware is the version it ships with, ConfigVersion the room
	// configuration the panel carries (the one datapoint only a browser reads).
	Firmware, ConfigVersion string
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string {
	return fmt.Sprintf("Newtron %s, %d-inch room touchpanel", r.Model, r.Inches)
}

// Rows is the model table: the Panel line, one model.
var Rows = []Row{
	{Product: "newtron-panel-7", Model: "Panel 7", Inches: 7, Firmware: "4.1.2", ConfigVersion: "rcfg-2026.05-7f3a"},
}

// base is the row a device of the family's own name (no product chosen) reads as.
func base() Row { return Rows[0] }

// The state paths of the panel.
const (
	pathUptime        = "status.device.uptime"
	pathBootEpoch     = "status.device.boot_epoch"
	pathBrightness    = "status.panel.brightness"
	pathOccupancy     = "status.panel.occupancy"
	pathAwake         = "status.panel.awake"
	pathPage          = "status.panel.page"
	pathScene         = nvp.PathScene
	pathSource        = "status.room.source"
	pathLog           = nvp.PathLog
	pathInput         = nvp.PathInput
	pathModel         = "configuration.device.model"
	pathSerial        = "configuration.device.serial"
	pathFirmware      = "configuration.firmware.version"
	pathConfigVersion = "configuration.room.config_version"
)

// The interfaces the panel serves, by name.
const (
	faceWeb    = "http-api"
	faceShell  = "ssh-console"
	faceNVP    = "nvp"
	faceTLS    = "nvp-tls"
	faceTelnet = "nvp-telnet"
	faceUDP    = "nvp-udp"
)

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			// The anchor of the uptime a read computes; 0 is unset, and the engine
			// seeds it to the device's start.
			"device": map[string]any{"boot_epoch": 0},
			"panel": map[string]any{
				"brightness": 85, "occupancy": false,
				// Whether the panel is awake, and its active page: idle, in-call or
				// sharing.
				"awake": false, "page": "idle",
			},
			"room": map[string]any{"scene": "idle", "source": "none"},
			// The last line the panel logged, pushed as LOG on its NVP faces.
			"diagnostics": map[string]any{"log": ""},
			// The selected video input, as the NVP line protocol reports and sets it.
			"video":   map[string]any{"input": "hdmi1"},
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Newtron"},
			"firmware": map[string]any{"version": r.Firmware},
			"network":  map[string]any{"hostname": ""},
			"room":     map[string]any{"config_version": r.ConfigVersion},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	password := wire.Auth{Type: "password", DefaultUser: "admin", DefaultPassword: "newtron"}
	b := base()
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Newtron", Model: "Panel", DeviceType: "touchpanel",
			Summary: "Newtron Panel touchpanel",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{
				Product: r.Product, Model: r.Model, Summary: r.Summary(),
				State: map[string]any{"configuration": map[string]any{
					"device": map[string]any{"model": r.Model}, "firmware": map[string]any{"version": r.Firmware},
					"room": map[string]any{"config_version": r.ConfigVersion},
				}},
			}
		}),
		Interfaces: []wire.Interface{
			{
				// A form sign-in: POST /login opens a session cookie, a request with
				// no live session is sent to /login, a wrong password back to
				// /login?error=1, and the console expires every session at once.
				Name: faceWeb, Protocol: "http", Port: 443, TLS: true, Doc: wire.Doc{Summary: "Newtron HTTP API"},
				Auth: wire.Auth{
					Type: "form", DefaultUser: "admin", DefaultPassword: "newtron", Enforce: true,
					LoginPath: "/login", ExpirePath: "/api/admin/sessions/expire",
				},
			},
			{
				Name: faceShell, Protocol: "ssh", Port: 22, Doc: wire.Doc{Summary: "SSH diagnostic CLI (persistent session)"}, Auth: password,
				Persistent: true, LineEnding: "\r\n", Welcome: fmt.Sprintf("Newtron %s CLI ready.", b.Model),
				Limits: wire.SessionLimits{MaxSessionsPerDevice: 16, OutBuffer: 256},
			},
			{
				Name: faceNVP, Protocol: "tcp", Port: 51325, Persistent: true, LineEnding: "\r\n",
				Doc: wire.Doc{Summary: "NVP line protocol: video input status, selection and events"},
			},
			{
				// The same line protocol behind a real TLS handshake. The panel
				// self-signs: its key is derived from the device's id, so a pinned
				// endpoint stays pinned across a simulator restart.
				Name: faceTLS, Protocol: "tcp", Port: 51326, TLS: true, TLSValidityDays: 397, Persistent: true, LineEnding: "\r\n",
				Doc: wire.Doc{Summary: "NVP line protocol over TLS, the panel's own self-signed certificate"},
			},
			{
				// The same line protocol behind a login: "login: " and "Password: " with
				// no line ending, a wrong credential answered "auth rejected" with a
				// hang-up, a good one greeted with the welcome line.
				Name: faceTelnet, Protocol: "telnet", Port: 23, Auth: password, Persistent: true, LineEnding: "\r\n",
				Welcome: fmt.Sprintf("Newtron %s NVP ready.", b.Model),
				Doc:     wire.Doc{Summary: "NVP line protocol over Telnet, behind a login"},
			},
			{
				// The same line protocol as datagrams, one line each; every source
				// address is its own session, and a seeded share is lost each way so a
				// driver's retries are exercised on a real wire.
				Name: faceUDP, Protocol: "udp", Port: 51327, LossPct: 5, Persistent: true, LineEnding: "\r\n",
				Doc: wire.Doc{Summary: "NVP line protocol as UDP datagrams, with a seeded share lost"},
			},
			{
				// A community the agent knows and refuses answers authorizationError,
				// where any other is dropped silently.
				Name: "snmp", Protocol: "snmp", Port: 161, Doc: wire.Doc{Summary: "SNMP monitoring (MIB-2 only, teaching case)"},
				Auth: wire.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability"}},
		},
		State:    stateFor(b),
		HTTP:     routes(),
		Commands: commands(),
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{roomMeeting()},
		Surface: surfaceOf,
	}
}

// roomMeeting wakes the panel for its room's meetings and changes its page with
// the call (behave.PanelFollowsRoom). A panel whose room has no calendar is
// left as it was.
func roomMeeting() spec.Tick {
	return spec.Tick{
		Name: "room_meeting", Interval: bind.Seconds(30), OnStart: true,
		Writes: []string{pathAwake, pathPage},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			next := behave.PanelFollowsRoom(behave.PanelState{Awake: spec.Bool(d, pathAwake), Page: spec.String(d, pathPage)}, c.Room, behave.DefaultPanel)
			return []surface.Change{{Path: pathAwake, Value: next.Awake}, {Path: pathPage, Value: next.Page}}
		},
	}
}

// statusDoc is GET /api/status: the panel and its identity. The room's scene
// and source are deliberately not here: the shell's `show room` is the one way
// in for the course's two room datapoints.
type statusDoc struct {
	Brightness float64 `json:"brightness"`
	Occupancy  bool    `json:"occupancy"`
	Awake      bool    `json:"awake"`
	Page       string  `json:"page"`
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.Identity{Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware), Uptime: spec.Int64(d, pathUptime)}
	panel := statusDoc{spec.Float(d, pathBrightness), spec.Bool(d, pathOccupancy), spec.Bool(d, pathAwake), spec.String(d, pathPage)}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Status(id, httpapi.UptimeFirst, httpapi.Block("panel", panel))}
}

// The acknowledgments of the routes. A route that sets the room, the backlight
// or the log answers the room's scene (or nothing but ok): the panel has always
// answered its acknowledgments this way, whichever the route.
type (
	sceneAck struct {
		OK    bool   `json:"ok"`
		Scene string `json:"scene"`
	}
	okAck struct {
		OK bool `json:"ok"`
	}
	actionAck struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
	}
)

func sceneOf(d spec.Device) any { return sceneAck{true, spec.String(d, pathScene)} }

// reboot clears the uptime and puts the panel state back; a factory reset
// extends to every volatile status path with a default. The shell's two verbs
// and the web API's two routes share these lists, so they never drift.
func rebootChanges(d spec.Device) []surface.Change {
	return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, pathBrightness, pathOccupancy)...)
}

func factoryResetChanges(d spec.Device) []surface.Change {
	return append(rebootChanges(d), httpapi.Reset(d, pathScene, pathSource)...)
}

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /", Summary: "Touchpanel dashboard", Handle: dashboardRoute},
		{Match: "GET /api/status", Summary: "Touchpanel status (panel backlight, occupancy, identity)", Handle: statusRoute},
		httpapi.Control{
			Match: "POST /api/room/scene", Summary: "Switch room scene (e.g. presentation, vc)", Example: "{\"scene\": \"presentation\"}\n",
			Fields: []httpapi.Field{{Path: pathScene, Name: "scene", Kind: httpapi.Text}}, Ack: sceneOf,
		}.Route(),
		httpapi.Control{
			Match: "POST /api/room/source", Summary: "Select active room source", Example: "{\"source\": \"laptop\"}\n",
			Fields: []httpapi.Field{{Path: pathSource, Name: "source", Kind: httpapi.Text}}, Ack: sceneOf,
		}.Route(),
		// A line written here is what the panel logs next, pushed on its NVP faces as
		// "LOG <severity> <text>" to a subscriber: the way to make the panel say
		// something a driver maps to a log line.
		httpapi.Control{
			Match: "POST /api/diagnostics/log", Summary: "Write a line to the panel's diagnostics log", Example: "{\"line\": \"WARN backlight driver reset\"}\n",
			Fields: []httpapi.Field{{Path: pathLog, Name: "line", Kind: httpapi.Text, Pattern: `^[^\r\n]{1,200}$`}},
			Ack:    func(spec.Device) any { return okAck{true} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/panel/brightness", Summary: "Set panel backlight brightness (0-100)", Example: "{\"brightness\": 85}\n",
			Fields: []httpapi.Field{{Path: pathBrightness, Name: "brightness", Kind: httpapi.Number}}, Ack: sceneOf,
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot panel (clears uptime, resets panel state)", Example: "{}",
			Writes: rebootChanges, Ack: actionAck{true, "reboot"},
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/factory-reset", Summary: "Factory reset (resets every status path to defaults)", Example: "{}",
			Writes: factoryResetChanges, Ack: actionAck{true, "factory-reset"},
		}.Route(),
	}
}

// commands are the shell's own verbs; the NVP lines it answers too are the
// shared room surface's.
func commands() []spec.Command {
	return []spec.Command{
		{Match: "show status", Summary: "Panel status overview", Face: faceShell, Handle: showStatus},
		{Match: "show room", Summary: "Current room scene and source", Face: faceShell, Handle: showRoom},
		{Match: "reboot", Summary: "Reboot panel", Face: faceShell, Handle: func(d spec.Device) string { return run(d, rebootChanges(d)) }},
		{Match: "factory-reset", Summary: "Factory reset panel", Face: faceShell, Handle: func(d spec.Device) string { return run(d, factoryResetChanges(d)) }},
	}
}

// run applies a verb's changes as one and answers OK.
func run(d spec.Device, changes []surface.Change) string {
	if err := d.WriteMany(changes...); err != nil {
		return "ERR " + err.Error() + "\n"
	}
	return "OK\n"
}

func showStatus(d spec.Device) string {
	return fmt.Sprintf("hostname      : %s\nmodel         : %s\nfirmware      : %s\nserial        : %s\nuptime (s)    : %d\n"+
		"brightness    : %s%%\noccupancy     : %s\nawake         : %s\npage          : %s\n",
		d.Hostname(), spec.String(d, pathModel), spec.String(d, pathFirmware), spec.String(d, pathSerial), spec.Int64(d, pathUptime),
		spec.String(d, pathBrightness), spec.String(d, pathOccupancy), spec.String(d, pathAwake), spec.String(d, pathPage))
}

func showRoom(d spec.Device) string {
	return fmt.Sprintf("scene     : %s\nsource    : %s\noccupancy : %s\n", spec.String(d, pathScene), spec.String(d, pathSource), spec.String(d, pathOccupancy))
}

// surfaceOf is what the panel answers beyond its web API: the NVP room on every
// line face, with the subjects each face pushes (the raw port the whole set, the
// others the input alone), and the SNMP system group. The agent is deliberately
// dead beside it: ICMP and the system group answer, nothing of the enterprise
// arc does.
func surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	input := []surface.Subject{nvp.InputSubject()}
	return spec.Surface{
		Commanders: []surface.Commander{bind.NVPRoom(d)},
		Subjects: map[string][]surface.Subject{
			faceNVP: nvp.Subjects(), faceShell: input, faceTLS: input, faceTelnet: input, faceUDP: input,
		},
		OIDs:     oids,
		ListedOn: faceNVP,
	}
}
