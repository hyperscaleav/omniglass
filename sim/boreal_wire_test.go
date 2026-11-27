//go:build linux

package sim

import (
	"testing"
	"time"
)

// The Boreal products' wire script (#2832): the requests, SNMP reads and
// behavior walks every golden transcript of the Edge displays, the Beam
// projector and the Cast player holds. The recorder ran it against the YAML
// profiles; the parity test runs it against the Go families.

// borealEdgeProducts are the five sizes the Edge family serves.
var borealEdgeProducts = []string{"boreal-edge-55", "boreal-edge-65", "boreal-edge-75", "boreal-edge-86", "boreal-edge-98"}

const (
	borealBeamProduct = "boreal-beam-340"
	borealCastProduct = "boreal-cast-m1"
)

func wirePost(name, path, body string) wireStep {
	return wireStep{Name: name, Method: "POST", Path: path, Body: body, Snapshot: true}
}

func wireGet(name, path string) wireStep { return wireStep{Name: name, Method: "GET", Path: path} }

// edgeScript is every route of the web admin, valid and refused.
func edgeScript() []wireStep {
	return []wireStep{
		wireGet("the dashboard at rest", "/"),
		wireGet("the identity document", "/api/status"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/display/power"},
		{Name: "a head request on the root", Method: "HEAD", Path: "/"},
		wirePost("power standby", "/api/display/power", `{"power": "standby"}`),
		wireGet("the dashboard asleep", "/"),
		wirePost("power on", "/api/display/power", `{"power": "on"}`),
		wirePost("power off", "/api/display/power", `{"power": "off"}`),
		wirePost("power refused", "/api/display/power", `{"power": "bogus"}`),
		wirePost("power with no field", "/api/display/power", `{}`),
		wirePost("power with no body", "/api/display/power", ``),
		wirePost("power with a boolean", "/api/display/power", `{"power": true}`),
		wirePost("power on again", "/api/display/power", `{"power":"on"}`),
		wirePost("input usbc", "/api/display/input", `{"input": "usbc"}`),
		wirePost("input dp1", "/api/display/input", `{"input": "dp1"}`),
		wirePost("input hdmi3", "/api/display/input", `{"input": "hdmi3"}`),
		wirePost("input refused", "/api/display/input", `{"input": "vga"}`),
		wirePost("input none is not an input", "/api/display/input", `{"input": "none"}`),
		wirePost("brightness 55", "/api/display/brightness", `{"brightness": 55}`),
		wirePost("brightness 0", "/api/display/brightness", `{"brightness": 0}`),
		wirePost("brightness a fraction", "/api/display/brightness", `{"brightness": 72.5}`),
		wirePost("brightness 100", "/api/display/brightness", `{"brightness": 100}`),
		wirePost("volume 33", "/api/audio/volume", `{"volume": 33}`),
		wirePost("volume 0", "/api/audio/volume", `{"volume": 0}`),
		wirePost("mute", "/api/audio/mute", `{"mute": true}`),
		wirePost("unmute", "/api/audio/mute", `{"mute": false}`),
		wirePost("mute as a word", "/api/audio/mute", `{"mute": "true"}`),
		wirePost("set it all away from default", "/api/display/input", `{"input": "usbc"}`),
		wireGet("the dashboard changed", "/"),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the identity after a reboot", "/api/status"),
		wirePost("set it away again", "/api/display/power", `{"power": "standby"}`),
		wirePost("brightness again", "/api/display/brightness", `{"brightness": 12}`),
		wirePost("volume again", "/api/audio/volume", `{"volume": 77}`),
		wirePost("mute again", "/api/audio/mute", `{"mute": true}`),
		wirePost("factory reset", "/api/admin/factory-reset", `{}`),
		wireGet("the dashboard after a factory reset", "/"),
		// The requests the profile answered with an accident of its templates
		// come last, so the state they left does not carry into the rest.
		{Name: "brightness a word", Method: "POST", Path: "/api/display/brightness", Body: `{"brightness": "abc"}`, Snapshot: true, Quirk: "the template wrote the word into state and answered a document that is not JSON"},
		{Name: "brightness missing", Method: "POST", Path: "/api/display/brightness", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
		{Name: "volume a word", Method: "POST", Path: "/api/audio/volume", Body: `{"volume": "loud"}`, Snapshot: true, Quirk: "the template wrote the word into state and answered a document that is not JSON"},
		{Name: "mute a number", Method: "POST", Path: "/api/audio/mute", Body: `{"mute": 7}`, Snapshot: true, Quirk: "the template wrote the number into state"},
	}
}

// edgeWrites move the SNMP-visible state away from rest before the second
// SNMP read.
func edgeWrites() []wireStep {
	return []wireStep{
		wirePost("power standby", "/api/display/power", `{"power": "standby"}`),
		wirePost("input usbc", "/api/display/input", `{"input": "usbc"}`),
		wirePost("brightness 55", "/api/display/brightness", `{"brightness": 55}`),
		wirePost("volume 33", "/api/audio/volume", `{"volume": 33}`),
		wirePost("mute", "/api/audio/mute", `{"mute": true}`),
	}
}

var edgeOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
	"1.3.6.1.4.1.55520.1.1.1.0", "1.3.6.1.4.1.55520.2.2.0", "1.3.6.1.4.1.55520.2.3.0", "1.3.6.1.4.1.55520.2.4.0",
	"1.3.6.1.4.1.55520.3.1.0", "1.3.6.1.4.1.55520.3.2.0", "1.3.6.1.4.1.55520.4.1.0", "1.3.6.1.4.1.55520.4.2.0",
	"1.3.6.1.4.1.55520.5.1.0", "1.3.6.1.4.1.55520.5.2.0", "1.3.6.1.4.1.55520.9.9.0",
}

// beamScript is the projector's web admin.
func beamScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/api/status"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		wirePost("power standby", "/api/projector/power", `{"power": "standby"}`),
		wirePost("power on", "/api/projector/power", `{"power": "on"}`),
		wirePost("power off is not a projector power", "/api/projector/power", `{"power": "off"}`),
		wirePost("power with no field", "/api/projector/power", `{}`),
		wirePost("shutter closed", "/api/projector/shutter", `{"shutter": "closed"}`),
		wirePost("shutter open", "/api/projector/shutter", `{"shutter": "open"}`),
		wirePost("shutter refused", "/api/projector/shutter", `{"shutter": "ajar"}`),
		wirePost("input hdmi2", "/api/projector/input", `{"input": "hdmi2"}`),
		wirePost("input hdmi1", "/api/projector/input", `{"input": "hdmi1"}`),
		wirePost("input refused", "/api/projector/input", `{"input": "dp1"}`),
		wirePost("shutter closed again", "/api/projector/shutter", `{"shutter": "closed"}`),
		wireGet("the status document changed", "/api/status"),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the status document after a reboot", "/api/status"),
		{Name: "factory reset is not on a projector", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`},
	}
}

func beamWrites() []wireStep {
	return []wireStep{
		wirePost("power standby", "/api/projector/power", `{"power": "standby"}`),
		wirePost("shutter closed", "/api/projector/shutter", `{"shutter": "closed"}`),
		wirePost("input hdmi2", "/api/projector/input", `{"input": "hdmi2"}`),
	}
}

var beamOIDs = []string{
	"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0",
	"1.3.6.1.4.1.55520.1.1.1.0", "1.3.6.1.4.1.55520.6.1.0", "1.3.6.1.4.1.55520.6.2.0", "1.3.6.1.4.1.55520.6.3.0", "1.3.6.1.4.1.55520.6.4.0",
	"1.3.6.1.4.1.55520.5.1.0", "1.3.6.1.4.1.55520.5.2.0", "1.3.6.1.4.1.55520.9.9.0",
}

// castScript is the player's JSON API behind its key.
func castScript() []wireStep {
	return []wireStep{
		wireGet("the status document", "/api/status"),
		wireGet("playback", "/api/playback"),
		{Name: "playback with no key", Method: "GET", Path: "/api/playback", NoKey: true},
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		wirePost("pause", "/api/playback", `{"state": "paused"}`),
		wirePost("play", "/api/playback", `{"state": "playing"}`),
		wirePost("stop", "/api/playback", `{"state": "stopped"}`),
		wirePost("playback refused", "/api/playback", `{"state": "rewinding"}`),
		wirePost("playback with no field", "/api/playback", `{}`),
		wireGet("playback stopped", "/api/playback"),
		wirePost("play again", "/api/playback", `{"state": "playing"}`),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the status document after a reboot", "/api/status"),
		wireGet("the root", "/"),
	}
}

func edgeTicks() []tickPlan {
	return []tickPlan{{"temperature_drift", []string{"status.display.temperature"}}}
}

func beamTicks() []tickPlan {
	return []tickPlan{
		{"temperature_drift", []string{"status.projector.temperature"}},
		{"light_source_aging", []string{"status.projector.light_source_hours"}},
	}
}

func castTicks() []tickPlan {
	return []tickPlan{
		{"storage_drift", []string{"status.storage.used_percent"}},
		{"playlist_advance", []string{"status.playback.index", "status.playback.content"}},
	}
}

// displayOperator is the parity day's operator: a hand on the power and the
// input in the middle of a meeting, and nothing in a no-show.
func displayOperator(d *Device, inputPath string) func(string) {
	return func(hms string) {
		set := func(path string, v any) {
			s := d.State()
			cur := s
			parts := splitPath(path)
			for _, p := range parts[:len(parts)-1] {
				cur = cur[p].(map[string]any)
			}
			cur[parts[len(parts)-1]] = v
			d.SetState(s, d.Version+1)
		}
		switch hms {
		case "08:31:00":
			set("status.device.power", "off")
			set(inputPath, "usbc")
		case "09:50:00":
			set(inputPath, "hdmi2")
		case "13:46:00":
			set("status.device.power", "standby")
		}
	}
}

func displayWatch(input string) []string {
	return []string{"status.device.power", input, "status.sim.busy", "status.sim.known", "status.sim.awake", "status.sim.until"}
}

// displayRoomDay walks a display or projector through the parity room day: it
// follows the room's meetings, and an operator's hand lands in the middle of
// one.
func displayRoomDay(input string) func(d *Device, at *time.Time) []wireChange {
	return func(d *Device, at *time.Time) []wireChange {
		w := newWatch(d, displayWatch(input)...)
		w.sample("start")
		roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) { d.TickBehavior("room_meeting") }, displayOperator(d, input))
		return w.out
	}
}

// castRoomDay walks the player through the building's day, not the room's
// meetings: the building opens and closes it, and an operator pauses it.
func castRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.playback.state", "status.playback.index", "status.playback.content", "status.sim.open")
	step := 0
	operator := func(hms string) {
		step++
		if step%3 == 0 {
			d.TickBehavior("playlist_advance")
		}
		s := d.State()
		switch hms {
		case "03:00:00":
			s["status"].(map[string]any)["playback"].(map[string]any)["state"] = "playing"
			d.SetState(s, d.Version+1)
		case "10:00:00":
			s["status"].(map[string]any)["playback"].(map[string]any)["state"] = "paused"
			d.SetState(s, d.Version+1)
		}
	}
	w.sample("start")
	roomDayWalk(d, at, w, "00:00:00", "23:59:00", time.Minute, func(string) { d.TickBehavior("building_day") }, operator)
	return w.out
}

// The Boreal families, as the harness records and replays them. Their goldens
// were recorded from the YAML profiles the families replaced, so they are
// frozen: recording one from the family would make the hold on the profile
// vacuous.
const borealFrozen = "recorded from the YAML profile before its deletion (#2832); re-recording from the Go family would hold it to itself"

var borealWalks = []string{"1.3.6.1.4.1.55520", "1.3.6.1.2.1.1"}

func borealFamily(name string, products []string, script, writes []wireStep, oids, walks []string, ticks []tickPlan, roomDay func(*Device, *time.Time) []wireChange, body func(*testing.T, string, string) string) wireFamily {
	return wireFamily{
		Name: name, Products: products, Engine: shippedEngineOf, Source: wireBorealSource, Frozen: borealFrozen,
		Script: script, Writes: writes, OIDs: oids, Walks: walks, Ticks: ticks, RoomDay: roomDay, Body: body,
	}
}

// wireBorealSource is the provenance note the Boreal transcripts carry.
const wireBorealSource = "recorded from the YAML profile before its deletion (#2832); see sim/testdata/wire/README.md"

func shippedEngineOf(t testing.TB) *Engine { return shippedEngine(t) }

var (
	borealEdgeWire = borealFamily("boreal-edge", borealEdgeProducts, edgeScript(), edgeWrites(), edgeOIDs, borealWalks, edgeTicks(), displayRoomDay("status.display.input"), asOwnModel)
	borealBeamWire = borealFamily("boreal-beam", []string{borealBeamProduct}, beamScript(), beamWrites(), beamOIDs, borealWalks, beamTicks(), displayRoomDay("status.projector.input"), nil)
	borealCastWire = borealFamily("boreal-cast", []string{borealCastProduct}, castScript(), nil, nil, nil, castTicks(), castRoomDay, nil)
)

func init() { wireFamilies = append(wireFamilies, borealEdgeWire, borealBeamWire, borealCastWire) }
