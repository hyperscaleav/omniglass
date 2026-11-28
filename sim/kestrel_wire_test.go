//go:build linux

package sim

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/wire"
)

// The Kestrel products' wire script (#2834): the requests, drifting behaviors
// and room days every golden transcript of the VRoom bar, the Relay C1 codec and
// the Gaze P2 camera holds. The recorder ran it against the YAML profiles; the
// parity test runs it against the Go families.

const (
	kestrelVRoomProduct = "kestrel-vroom"
	kestrelRelayProduct = "kestrel-relay-c1"
	kestrelGazeProduct  = "kestrel-gaze-p2"
)

// vroomScript is every route of the bar's web admin and JSON API, valid and
// refused, and the feed events its routes publish.
func vroomScript() []wireStep {
	return []wireStep{
		wireGet("the dashboard at rest", "/"),
		wireGet("the status snapshot", "/api/status"),
		wireGet("the app band", "/api/app"),
		wireGet("the private log route", "/api/logs"),
		wireGet("the call", "/api/call"),
		wireGet("the audio", "/api/audio"),
		wireGet("the system unit", "/api/system"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/audio/volume"},
		{Name: "a head request on the root", Method: "HEAD", Path: "/"},
		{Name: "the status with no login", Method: "GET", Path: "/api/status", NoKey: true},
		{Name: "the dashboard with no login", Method: "GET", Path: "/", NoKey: true},
		{Name: "a hang-up with no login", Method: "POST", Path: "/api/call/disconnect", Body: `{}`, NoKey: true},
		wirePost("volume 30", "/api/audio/volume", `{"volume": 30}`),
		wirePost("volume 0", "/api/audio/volume", `{"volume": 0}`),
		wirePost("volume 100", "/api/audio/volume", `{"volume": 100}`),
		wirePost("volume a fraction", "/api/audio/volume", `{"volume": 42.5}`),
		wirePost("volume as text", "/api/audio/volume", `{"volume": "64"}`),
		wirePost("mute", "/api/audio/mute", `{"mute": true}`),
		wirePost("unmute", "/api/audio/mute", `{"mute": false}`),
		wirePost("mute as a word", "/api/audio/mute", `{"mute": "true"}`),
		wirePost("camera hidden", "/api/video/camera", `{"camera_on": false}`),
		wirePost("camera shown", "/api/video/camera", `{"camera_on": true}`),
		wirePost("standby", "/api/system/standby", `{"power": "standby"}`),
		wireGet("the dashboard in standby", "/"),
		wirePost("power on", "/api/system/standby", `{"power": "on"}`),
		wirePost("power off", "/api/system/standby", `{"power": "off"}`),
		wirePost("power refused", "/api/system/standby", `{"power": "bogus"}`),
		wirePost("power with no field", "/api/system/standby", `{}`),
		wirePost("power on again", "/api/system/standby", `{"power": "on"}`),
		wirePost("dial a call", "/api/call/dial", `{"id": "c9001", "participants": 3}`),
		wireGet("the call while dialed", "/api/call"),
		wireGet("the dashboard in a call", "/"),
		wirePost("dial over a call", "/api/call/dial", `{"id": "c9002"}`),
		wirePost("dial with a participant count as text", "/api/call/dial", `{"id": "c9003", "participants": "12"}`),
		wirePost("dial with an id that is refused", "/api/call/dial", `{"id": "bad id", "participants": 3}`),
		wirePost("dial with too many participants", "/api/call/dial", `{"id": "c9004", "participants": 65}`),
		wirePost("dial with a fractional count", "/api/call/dial", `{"id": "c9005", "participants": 3.5}`),
		wirePost("dial with both refused", "/api/call/dial", `{"id": "", "participants": "x"}`),
		wirePost("dial with no body", "/api/call/dial", ``),
		wirePost("dial with a zero count", "/api/call/dial", `{"id": "c9006", "participants": 0}`),
		wireGet("the status in a call", "/api/status"),
		wirePost("hang up", "/api/call/disconnect", `{}`),
		wirePost("hang up with no call", "/api/call/disconnect", `{}`),
		wirePost("dial again", "/api/call/dial", `{"id": "c9007", "participants": 5}`),
		wirePost("set it all away from default", "/api/audio/volume", `{"volume": 77}`),
		wirePost("mute again", "/api/audio/mute", `{"mute": true}`),
		wirePost("standby again", "/api/system/standby", `{"power": "standby"}`),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the system unit after a reboot", "/api/system"),
		wirePost("set it away again", "/api/audio/volume", `{"volume": 12}`),
		wirePost("mute once more", "/api/audio/mute", `{"mute": true}`),
		wirePost("camera hidden once more", "/api/video/camera", `{"camera_on": false}`),
		wirePost("dial once more", "/api/call/dial", `{"id": "c9008", "participants": 2}`),
		wirePost("factory reset", "/api/admin/factory-reset", `{}`),
		wireGet("the dashboard after a factory reset", "/"),
		wireGet("the status after a factory reset", "/api/status"),
		// The requests the profile answered with an accident of its templates
		// come last, so the state they left does not carry into the rest.
		{Name: "volume a word", Method: "POST", Path: "/api/audio/volume", Body: `{"volume": "loud"}`, Snapshot: true, Quirk: "the template wrote the word into state"},
		{Name: "volume missing", Method: "POST", Path: "/api/audio/volume", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
		{Name: "mute a number", Method: "POST", Path: "/api/audio/mute", Body: `{"mute": 7}`, Snapshot: true, Quirk: "the template wrote the number into state"},
		{Name: "mute missing", Method: "POST", Path: "/api/audio/mute", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
		{Name: "camera a word", Method: "POST", Path: "/api/video/camera", Body: `{"camera_on": "maybe"}`, Snapshot: true, Quirk: "the template wrote the word into state"},
		{Name: "camera missing", Method: "POST", Path: "/api/video/camera", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
	}
}

func vroomTicks() []tickPlan {
	return []tickPlan{
		{"call_cycle", []string{"status.call.active", "status.call.participants", "status.call.id", "status.sim.event", "status.sim.plan", "status.sim.slot",
			"status.sim.boundary", "status.sim.booked", "status.sim.start_at", "status.sim.end_at", "status.sim.plan_at"}},
		{"people_count", []string{"status.video.people_count"}},
		{"media_stats", []string{"status.media.packet_loss", "status.media.jitter_ms", "status.media.bitrate_kbps", "status.media.frame_rate", "status.audio.speaker_level",
			"status.audio.mic_level_db", "status.device.cpu_load", "status.device.temperature", "status.device.fan_rpm", "status.device.memory_used", "status.sim.net_event"}},
		{"status_log", []string{"status.device.power"}},
	}
}

// setState is an operator's write to a path.
func setState(d *Device, path string, v any) {
	s := d.State()
	if err := SetPath(s, path, v); err != nil {
		panic(err)
	}
	d.SetState(s, d.Version+1)
}

// feedChanges appends the named events the device's feed numbered since seq as
// changes at the step, "event:<name>" with the data.
func feedChanges(d *Device, w *watchChanges, at string, seq uint64) {
	for _, e := range feedSince(d, seq) {
		name, data := splitFirst(e)
		w.out = append(w.out, wireChange{At: at, Path: "event:" + name, Value: data})
	}
}

func splitFirst(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

var vroomWatch = []string{
	"status.call.active", "status.call.participants", "status.call.id", "status.presentation.local_active", "status.video.people_count",
	"status.media.packet_loss", "status.media.jitter_ms", "status.media.bitrate_kbps", "status.media.frame_rate", "status.audio.speaker_level", "status.audio.mic_level_db",
	"status.device.cpu_load", "status.device.temperature", "status.device.fan_rpm", "status.device.memory_used",
	"status.sim.event", "status.sim.cal", "status.sim.skip", "status.sim.want", "status.sim.want_n", "status.sim.hungup", "status.sim.dialed", "status.sim.dial_ticks",
	"status.sim.share_prev", "status.sim.call_seq", "status.sim.plan", "status.sim.slot", "status.sim.boundary", "status.sim.booked", "status.sim.start_at",
	"status.sim.end_at", "status.sim.plan_at", "status.sim.net_event", "status.sim.uplink_degraded", "status.sim.id_prev", "status.sim.participants_prev",
}

// vroomRoomDay walks the bar through the parity room day: its call follows the
// meetings (a minute a tick), the camera counts the room and the call stats
// drift (every half minute), an operator stops a share, hangs up and dials in
// the middle of it, and the bar's own schedule runs in a room with no calendar.
// The named events the feed numbered are recorded beside the state.
func vroomRoomDay(d *Device, at *time.Time) []wireChange {
	// The feed the bar's web admin updates from, as its interface declares it.
	d.SetEvents(wire.Events{Path: "/api/events", RetryMS: 2000, DropPath: "/api/admin/streams/drop",
		Paths: []string{"status.audio.volume", "status.audio.mic_mute", "status.call.active", "status.call.participants", "status.device.power"}})
	w := newWatch(d, vroomWatch...)
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "08:42:00":
			setState(d, "status.presentation.local_active", false)
		case "10:20:00":
			setState(d, "status.call.active", false)
			setState(d, "status.call.participants", 0)
			setState(d, "status.sim.hungup", true)
		case "12:00:00":
			setState(d, "status.call.active", true)
			setState(d, "status.call.id", "c-dial")
			setState(d, "status.call.participants", 3)
			setState(d, "status.sim.dialed", true)
			setState(d, "status.sim.dial_length", 4)
		}
	}
	tick := func(hms string) {
		seq := feedSeq(d)
		if hms[6:] == "00" {
			d.TickBehavior("call_cycle")
		}
		d.TickBehavior("people_count")
		d.TickBehavior("media_stats")
		if hms[3:] == "00:00" || hms[3:] == "15:00" || hms[3:] == "30:00" || hms[3:] == "45:00" {
			d.TickBehavior("status_log")
		}
		feedChanges(d, w, hms, seq)
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, tick, operator)
	return w.out
}

// relayScript is the codec's JSON API.
func relayScript() []wireStep {
	return []wireStep{
		wireGet("the system unit", "/api/system"),
		wireGet("the call", "/api/call"),
		wireGet("the content", "/api/content"),
		wireGet("the peripherals", "/api/peripherals"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/call/dial"},
		{Name: "the call with no login", Method: "GET", Path: "/api/call", NoKey: true},
		wirePost("dial", "/api/call/dial", `{"participants": 3}`),
		wireGet("the call dialed", "/api/call"),
		wirePost("dial with no count", "/api/call/dial", `{}`),
		wirePost("dial with no body", "/api/call/dial", ``),
		wirePost("dial with a zero count", "/api/call/dial", `{"participants": 0}`),
		wirePost("dial with a count as text", "/api/call/dial", `{"participants": "9"}`),
		wirePost("share hdmi1", "/api/content/share", `{"source": "hdmi1"}`),
		wirePost("share hdmi2", "/api/content/share", `{"source": "hdmi2"}`),
		wirePost("share usbc", "/api/content/share", `{"source": "usbc"}`),
		wirePost("share wireless", "/api/content/share", `{"source": "wireless"}`),
		wirePost("share refused", "/api/content/share", `{"source": "vga"}`),
		wirePost("share with no source", "/api/content/share", `{}`),
		wireGet("the content shared", "/api/content"),
		wirePost("stop sharing", "/api/content/stop", `{}`),
		wirePost("stop sharing again", "/api/content/stop", `{}`),
		wirePost("share to hang up with", "/api/content/share", `{"source": "usbc"}`),
		wirePost("disconnect", "/api/call/disconnect", `{}`),
		wireGet("the call after a hang-up", "/api/call"),
		wireGet("the content after a hang-up", "/api/content"),
		wirePost("dial again", "/api/call/dial", `{"participants": 6}`),
		wirePost("share again", "/api/content/share", `{"source": "hdmi1"}`),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the system unit after a reboot", "/api/system"),
		wireGet("the call after a reboot", "/api/call"),
		{Name: "factory reset is not on a codec", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`},
		wirePost("a body that is not JSON", "/api/call/dial", `dial please`),
	}
}

func relayTicks() []tickPlan {
	return []tickPlan{{"call_cycle", []string{"status.call.active", "status.call.participants", "status.content.sharing", "status.content.source", "status.sim.event",
		"status.sim.want", "status.sim.want_n", "status.sim.call", "status.sim.skip", "status.sim.share_prev"}}}
}

var relayWatch = []string{
	"status.call.active", "status.call.participants", "status.content.sharing", "status.content.source",
	"status.sim.event", "status.sim.want", "status.sim.want_n", "status.sim.call", "status.sim.skip", "status.sim.hungup", "status.sim.dialed", "status.sim.share_prev",
}

// relayRoomDay walks the codec through the parity room day: its call follows
// the room's meetings a minute a tick, an operator stops a share, hangs up and
// dials.
func relayRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, relayWatch...)
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "08:42:00":
			setState(d, "status.content.sharing", false)
		case "10:20:00":
			setState(d, "status.call.active", false)
			setState(d, "status.call.participants", 0)
			setState(d, "status.sim.hungup", true)
		case "12:00:00":
			setState(d, "status.call.active", true)
			setState(d, "status.sim.call", "c-dial")
			setState(d, "status.call.participants", 3)
			setState(d, "status.sim.dialed", true)
		case "12:30:00":
			setState(d, "status.sim.dialed", false)
			setState(d, "status.call.active", false)
		}
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", time.Minute, func(string) { d.TickBehavior("call_cycle") }, operator)
	return w.out
}

// gazeScript is the camera's JSON API.
func gazeScript() []wireStep {
	return []wireStep{
		wireGet("the system unit", "/api/system"),
		wireGet("the camera", "/api/camera"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the wrong method on a route", Method: "GET", Path: "/api/camera/preset"},
		{Name: "the camera with no login", Method: "GET", Path: "/api/camera", NoKey: true},
		wirePost("preset 1", "/api/camera/preset", `{"preset": 1}`),
		wireGet("the camera on preset 1", "/api/camera"),
		wirePost("preset 2", "/api/camera/preset", `{"preset": 2}`),
		wirePost("preset 3", "/api/camera/preset", `{"preset": 3}`),
		wirePost("preset as text", "/api/camera/preset", `{"preset": "2"}`),
		wirePost("preset refused", "/api/camera/preset", `{"preset": 4}`),
		wirePost("preset zero is refused", "/api/camera/preset", `{"preset": 0}`),
		wirePost("preset with no field", "/api/camera/preset", `{}`),
		wirePost("preset with no body", "/api/camera/preset", ``),
		wirePost("position", "/api/camera/position", `{"pan": -12.5, "tilt": 4, "magnification": 1.5}`),
		wireGet("the camera pointed", "/api/camera"),
		wirePost("position whole numbers", "/api/camera/position", `{"pan": 20, "tilt": -3, "magnification": 2}`),
		wirePost("position at the limits", "/api/camera/position", `{"pan": 60, "tilt": 20, "magnification": 4}`),
		wirePost("position out of the lens range", "/api/camera/position", `{"pan": 90, "tilt": 45, "magnification": 9}`),
		wirePost("position as text", "/api/camera/position", `{"pan": "5", "tilt": "1", "magnification": "3"}`),
		wirePost("tracking on", "/api/camera/tracking", `{"tracking": "tracking"}`),
		wirePost("tracking off", "/api/camera/tracking", `{"tracking": "manual"}`),
		wirePost("tracking refused", "/api/camera/tracking", `{"tracking": "auto"}`),
		wirePost("tracking with no field", "/api/camera/tracking", `{}`),
		wirePost("tracking on again", "/api/camera/tracking", `{"tracking": "tracking"}`),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the camera after a reboot", "/api/camera"),
		wireGet("the system unit after a reboot", "/api/system"),
		wirePost("point it away again", "/api/camera/position", `{"pan": 11, "tilt": 2, "magnification": 2.5}`),
		wirePost("reboot again", "/api/admin/reboot", `{}`),
		{Name: "factory reset is not on a camera", Method: "POST", Path: "/api/admin/factory-reset", Body: `{}`},
		// The requests the profile answered with an accident of its templates
		// come last, so the state they left does not carry into the rest.
		{Name: "position a word", Method: "POST", Path: "/api/camera/position", Body: `{"pan": "left", "tilt": 4, "magnification": 1.5}`, Snapshot: true, Quirk: "the template wrote the word into state"},
		{Name: "position missing", Method: "POST", Path: "/api/camera/position", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
		{Name: "position partly missing", Method: "POST", Path: "/api/camera/position", Body: `{"pan": 3}`, Snapshot: true, Quirk: "the template wrote <no value> into state"},
	}
}

func gazeTicks() []tickPlan {
	return []tickPlan{
		{"room_meeting", []string{"status.sim.known", "status.camera.people", "status.camera.presence", "status.camera.magnification", "status.camera.pan", "status.camera.tilt",
			"status.camera.preset", "status.camera.tracking", "status.sim.meeting"}},
		{"tracking_drift", []string{"status.camera.pan", "status.camera.tilt"}},
	}
}

var gazeWatch = []string{
	"status.sim.known", "status.sim.meeting", "status.camera.people", "status.camera.presence", "status.camera.magnification", "status.camera.pan", "status.camera.tilt",
	"status.camera.preset", "status.camera.tracking",
}

// gazeRoomDay walks the camera through the parity room day: it frames the
// room's group when a meeting starts and returns home when it ends, tracking
// drifts inside a meeting, and an operator points it by hand in the middle of
// one.
func gazeRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, gazeWatch...)
	w.sample("start")
	operator := func(hms string) {
		switch hms {
		case "08:50:00":
			setState(d, "status.camera.preset", 2)
			setState(d, "status.camera.pan", 30)
			setState(d, "status.camera.tracking", "manual")
		case "10:15:00":
			setState(d, "status.camera.tracking", "tracking")
		}
	}
	tick := func(hms string) {
		d.TickBehavior("room_meeting")
		d.TickBehavior("tracking_drift")
	}
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 20*time.Second, tick, operator)
	return w.out
}

const wireKestrelSource = "recorded from the YAML profile before its deletion (#2834); see sim/testdata/wire/README.md"

const kestrelFrozen = "recorded from the YAML profile before its deletion (#2834); re-recording from the Go family would hold it to itself"

var (
	vroomWire = wireFamily{Name: "kestrel-bar", Products: []string{kestrelVRoomProduct}, Engine: shippedEngineOf, Source: wireKestrelSource, Frozen: kestrelFrozen,
		Script: vroomScript(), Ticks: vroomTicks(), RoomDay: vroomRoomDay, Diverges: vroomDiverges}
	relayWire = wireFamily{Name: "kestrel-relay", Products: []string{kestrelRelayProduct}, Engine: shippedEngineOf, Source: wireKestrelSource, Frozen: kestrelFrozen,
		Script: relayScript(), Ticks: relayTicks(), RoomDay: relayRoomDay}
	gazeWire = wireFamily{Name: "kestrel-gaze", Products: []string{kestrelGazeProduct}, Engine: shippedEngineOf, Source: wireKestrelSource, Frozen: kestrelFrozen,
		Script: gazeScript(), Ticks: gazeTicks(), RoomDay: gazeRoomDay}
)

func init() { wireFamilies = append(wireFamilies, vroomWire, relayWire, gazeWire) }
