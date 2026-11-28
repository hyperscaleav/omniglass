// Package kestrelvroom is the Kestrel VRoom video conferencing bar (#2834), a Go
// device family of one model. The VRoom is Kestrel's all-in-one meeting-room
// video bar: codec, camera, microphone array and speakers in a single
// horizontal bar under a display, and in the course the HTTP-JSON teaching
// device: every datapoint a trainee monitors comes in over the HTTP JSON API
// (app mode, UCC sign-in, touch-panel pairing, local presentation, recent
// logs), and nowhere else. It also serves a web admin page, a live feed the page
// updates from (server-sent events, with the named events its calls report) and
// the control socket a room's touch panel drives it over. There is no SNMP,
// SSH, Telnet or raw-TCP surface on this device.
//
// The private-API teaching point is the log route: GET /api/logs is not
// advertised, but the web admin page's "Recent logs" card fetches it from its
// script, so the call shows up in a browser's network tab.
//
// The bar talks on its own, off the lab calendar: its calls are the meetings its
// room holds, so the scheduling panel, the booking service, the meeting
// platform and the codec beside it all say the same thing of one room. With no
// calendar (a bar in no room, the dev fleet with no world) it keeps its own
// schedule. A dial makes it talk now and a hang-up leaves the meeting it ended:
// the calendar yields to both.
//
// The family replaced the kestrel-vroom YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package kestrelvroom

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
)

// Name is the family's id, the profile name its devices carry.
const Name = "kestrel-bar"

// Row is one model of the bar line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
	// Revision is the hardware revision the system document reports.
	Revision string
}

// Rows is the model table: the VRoom line, one model.
var Rows = []Row{
	{Product: "kestrel-vroom", Model: "VRoom", Firmware: "2.7.3", Revision: "B2"},
}

// The state paths of the bar.
const (
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
	pathPower     = "status.device.power"
	pathCPU       = "status.device.cpu_load"
	pathTemp      = "status.device.temperature"
	pathFan       = "status.device.fan_rpm"
	pathMemory    = "status.device.memory_used"

	pathAppMode    = "status.app.mode"
	pathSignin     = "status.app.ucc_signin"
	pathTouch      = "status.peripherals.touch_pair"
	pathPresenting = "status.presentation.local_active"
	pathLogs       = "status.logs.recent"

	pathActive       = "status.call.active"
	pathParticipants = "status.call.participants"
	pathCallID       = "status.call.id"

	pathVolume  = "status.audio.volume"
	pathMute    = "status.audio.mic_mute"
	pathSpeaker = "status.audio.speaker_level"
	pathMic     = "status.audio.mic_level_db"

	pathCamera     = "status.video.camera_on"
	pathResolution = "status.video.resolution"
	pathPeople     = "status.video.people_count"

	pathLoss    = "status.media.packet_loss"
	pathJitter  = "status.media.jitter_ms"
	pathBitrate = "status.media.bitrate_kbps"
	pathFrames  = "status.media.frame_rate"

	pathEvent      = "status.sim.event"
	pathCal        = "status.sim.cal"
	pathSharePrev  = "status.sim.share_prev"
	pathWant       = "status.sim.want"
	pathWantN      = "status.sim.want_n"
	pathSkip       = "status.sim.skip"
	pathHungup     = "status.sim.hungup"
	pathPlan       = "status.sim.plan"
	pathSlot       = "status.sim.slot"
	pathBoundary   = "status.sim.boundary"
	pathBooked     = "status.sim.booked"
	pathStartAt    = "status.sim.start_at"
	pathEndAt      = "status.sim.end_at"
	pathPlanAt     = "status.sim.plan_at"
	pathCallSeq    = "status.sim.call_seq"
	pathDialed     = "status.sim.dialed"
	pathDialTicks  = "status.sim.dial_ticks"
	pathDialLength = "status.sim.dial_length"
	pathIDPrev     = "status.sim.id_prev"
	pathPeersPrev  = "status.sim.participants_prev"
	pathNetEvent   = "status.sim.net_event"
	pathDegraded   = "status.sim.uplink_degraded"

	pathModel    = "configuration.device.model"
	pathMaker    = "configuration.device.manufacturer"
	pathSerial   = "configuration.device.serial"
	pathRevision = "configuration.device.hardware_revision"
	pathFirmware = "configuration.firmware.version"
	pathHostname = "configuration.network.hostname"
	pathAddress  = "status.network.ipv4.address"
	pathMAC      = "status.network.ipv4.mac"
)

// vroomLogs are the recent log lines the private route serves: a mix of INFO and
// WARN, a generic room id and no real hostnames.
var vroomLogs = []any{
	"2026-06-13T08:41:02Z INFO  kvroom.app      UCC client signed in (account=room-vc@example.lan, tenant=teams)",
	"2026-06-13T08:41:05Z INFO  kvroom.pairing  Touch controller paired (model=newtron-panel-7, link=usb-c)",
	"2026-06-13T09:02:17Z INFO  kvroom.call     Call started (id=c8821, protocol=sip, peers=3)",
	"2026-06-13T09:14:53Z WARN  kvroom.net      Uplink degraded: packet loss 4.1%, jitter 38ms, enabling rate adaptation",
	"2026-06-13T09:15:41Z INFO  kvroom.net      Uplink recovered: loss 0.0%, jitter 6ms, resumed 1080p send",
	"2026-06-13T09:31:08Z WARN  kvroom.audio    Microphone array muted by far-end host during active call",
	"2026-06-13T09:58:22Z INFO  kvroom.call     Call ended (id=c8821, duration=3365s, reason=local-hangup)",
	"2026-06-13T10:03:49Z INFO  kvroom.system   Returned to standby after idle timeout (600s)",
}

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			// boot_epoch anchors the read-derived uptime; 0 means unset, seeded
			// to the device's start. The codec's own load (percent),
			// temperature (C), fan (rpm) and memory in use (a fraction) are
			// drifted by media_stats.
			"device": map[string]any{"power": "on", "boot_epoch": 0, "cpu_load": 14, "temperature": 44.0, "fan_rpm": 2400, "memory_used": 0.42},
			// The course datapoints. The sign-in is templated per room, a
			// shared definition must not bake one room's account onto every
			// bar; the touch controller is the panel in this room.
			"app":          map[string]any{"mode": "ucc", "ucc_signin": "room-{site}-{building}-{room}@shinra.lan"},
			"peripherals":  map[string]any{"touch_pair": "{room-device:newtron-panel-7}"},
			"presentation": map[string]any{"local_active": false},
			"logs":         map[string]any{"recent": vroomLogs},
			"call":         map[string]any{"active": false, "participants": 0, "id": ""},
			"audio":        map[string]any{"volume": 50, "mic_mute": false, "speaker_level": 50, "mic_level_db": -60},
			"video":        map[string]any{"camera_on": true, "resolution": "3840x2160", "people_count": 0},
			// The media a call carries, drifted by media_stats while a call is
			// up and zero when idle: packet loss a ratio (0 to 1), jitter in
			// ms, the send bitrate in kbps and the frame rate.
			"media": map[string]any{"packet_loss": 0, "jitter_ms": 0, "bitrate_kbps": 0, "frame_rate": 0},
			// The call cycle's bookkeeping: what this tick did, the call the
			// room's meeting says it should be in and how many came, the
			// start of a meeting a dial overtook or a hang-up ended (skipped),
			// a hang-up waiting for the next tick, a dialed call and its
			// length, and the id and count before this tick. The simulator's,
			// not the codec's: no route serves it.
			"sim": map[string]any{
				"event": "", "cal": false, "share_prev": false, "want": "", "want_n": 0, "skip": 0, "hungup": false,
				"plan": false, "slot": 30, "boundary": 0, "booked": false, "start_at": 0, "end_at": 0, "plan_at": 0,
				"call_seq": 0, "dialed": false, "dial_ticks": 0, "dial_length": 30, "id_prev": "", "participants_prev": 0,
				"net_event": "", "uplink_degraded": false,
			},
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Kestrel", "hardware_revision": r.Revision},
			"firmware": map[string]any{"version": r.Firmware},
			"network":  map[string]any{"hostname": ""},
		},
	}
}

// The paths the live feed streams, beside the named events the routes and the
// call cycle report.
var feedPaths = []string{pathVolume, pathMute, pathActive, pathParticipants, pathPower}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Kestrel", Model: "VRoom", DeviceType: "video-codec",
			Summary: "Kestrel VRoom video conferencing bar",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, Summary: fmt.Sprintf("Kestrel %s video conferencing bar", r.Model)}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 443, TLS: true, Doc: profile.Doc{Summary: "Kestrel HTTP JSON API + web admin UI"},
				Auth: auth(),
				// The live feed the web admin page updates from: every change
				// to the call's state and participant count, the volume, the
				// mute and the power state, one numbered event each, behind
				// the same basic auth. A reconnect's Last-Event-ID replays from
				// that event, and the console drops every stream by firing POST
				// at the drop path.
				Events: profile.Events{Path: "/api/events", RetryMS: 2000, Paths: feedPaths, DropPath: "/api/admin/streams/drop"},
			},
			{
				// The control socket the room's touch panel drives the bar over:
				// a JSON message per text frame, behind the same basic auth as
				// the web API but its own port.
				Name: "ws-api", Protocol: "websocket", Port: 9443, TLS: true,
				Doc:  profile.Doc{Summary: "Kestrel control socket (JSON over a websocket): subscribe, get, set"},
				Auth: auth(),
				WebSocket: profile.WebSocket{
					Path: "/api/ws", Paths: []string{pathVolume, pathMute, "status.call", pathPower},
					Settable: []string{pathVolume, pathMute}, DropPath: "/api/admin/sockets/drop",
				},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability"}},
		},
		State:   stateFor(Rows[0]),
		HTTP:    routes(),
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{callCycle(), peopleCount(), statusLog(), mediaStats()},
	}
}

func auth() profile.Auth {
	return profile.Auth{Type: "basic", DefaultUser: "admin", DefaultPassword: "kestrel", Realm: "Kestrel VRoom", Enforce: true}
}

// ---- behavior ----

// barCall is the bar's call rules: the calendar's meetings in a room that has
// one, its own schedule in a room with none, and a dialed call that ends after
// its length.
func barCall() behave.CallParams {
	p := behave.DefaultCall
	p.Standalone, p.DialEnds = &behave.DefaultStandalone, true
	return p
}

// callOf is the call the bar holds, as the primitives read it. The bar keeps
// the call's id in status.call.id and its bookkeeping in status.sim.
func callOf(d spec.Device) behave.CallState {
	return behave.CallState{
		Active: spec.Bool(d, pathActive), ID: spec.String(d, pathCallID), Participants: spec.Int(d, pathParticipants),
		Event: behave.CallEvent(spec.String(d, pathEvent)), IDPrev: spec.String(d, pathIDPrev), ParticipantsPrev: spec.Int(d, pathPeersPrev),
		Cal: spec.Bool(d, pathCal), Skip: spec.Int64(d, pathSkip), Want: spec.String(d, pathWant), WantN: spec.Int(d, pathWantN),
		Hungup: spec.Bool(d, pathHungup), Dialed: spec.Bool(d, pathDialed),
		DialTicks: spec.Int(d, pathDialTicks), DialLength: spec.Int(d, pathDialLength), CallSeq: spec.Int(d, pathCallSeq),
		Plan: spec.Bool(d, pathPlan), Slot: spec.Int(d, pathSlot), Boundary: spec.Int64(d, pathBoundary), Booked: spec.Bool(d, pathBooked),
		StartAt: spec.Int64(d, pathStartAt), EndAt: spec.Int64(d, pathEndAt), PlanAt: spec.Int64(d, pathPlanAt),
	}
}

// callWrites is the call as the state paths hold it, in the order the profile
// wrote them.
var callWrites = []string{
	pathPeersPrev, pathIDPrev, pathCal, pathSkip, pathHungup, pathWant, pathWantN, pathEvent, pathDialTicks, pathDialed, pathActive,
	pathCallSeq, pathCallID, pathParticipants, pathPresenting, pathSharePrev, pathStartAt, pathPlan, pathSlot, pathBoundary, pathBooked,
	pathEndAt, pathPlanAt,
}

func callChanges(c behave.CallState, share behave.ShareState) []surface.Change {
	return []surface.Change{
		{Path: pathPeersPrev, Value: int64(c.ParticipantsPrev)},
		{Path: pathIDPrev, Value: c.IDPrev},
		{Path: pathCal, Value: c.Cal},
		{Path: pathSkip, Value: c.Skip},
		{Path: pathHungup, Value: c.Hungup},
		{Path: pathWant, Value: c.Want},
		{Path: pathWantN, Value: int64(c.WantN)},
		{Path: pathEvent, Value: string(c.Event)},
		{Path: pathDialTicks, Value: int64(c.DialTicks)},
		{Path: pathDialed, Value: c.Dialed},
		{Path: pathActive, Value: c.Active},
		{Path: pathCallSeq, Value: int64(c.CallSeq)},
		{Path: pathCallID, Value: c.ID},
		{Path: pathParticipants, Value: int64(c.Participants)},
		{Path: pathPresenting, Value: share.Sharing},
		{Path: pathSharePrev, Value: share.Prev},
		{Path: pathStartAt, Value: c.StartAt},
		{Path: pathPlan, Value: c.Plan},
		{Path: pathSlot, Value: int64(c.Slot)},
		{Path: pathBoundary, Value: c.Boundary},
		{Path: pathBooked, Value: c.Booked},
		{Path: pathEndAt, Value: c.EndAt},
		{Path: pathPlanAt, Value: c.PlanAt},
	}
}

// callCycle is the bar's call following its room's meetings, a tick a minute: one
// call per held meeting (none for a no-show), starting and ending at the first
// tick on or after the meeting does, participants joining and leaving inside it
// (never more than the meeting's actual, from the device's own draws), content
// shared now and then. A meeting that starts as the last one ends moves the call
// on in one tick (turn). With no calendar the bar keeps its own schedule, its
// plan and slot bookkeeping. No tick is faster than 15 s, and a tick streams only
// what it changed, so the live feed carries a call's transitions, not a
// heartbeat. What the tick did is reported as named events on the feed.
func callCycle() spec.Tick {
	return spec.Tick{
		Name: "call_cycle", Interval: bind.Seconds(60), OnStart: true, Writes: callWrites,
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			call := behave.FollowsCall(callOf(d), c.Room, c.Now, barCall(), c.Rand)
			share := behave.ContentSharing(behave.ShareState{Sharing: spec.Bool(d, pathPresenting), Prev: spec.Bool(d, pathSharePrev)}, c.Room,
				behave.CallSignals{Active: call.Active, Dialed: call.Dialed, Ended: call.Event == behave.EventEnd}, behave.DefaultShare, c.Rand)
			call = behave.PlanOwnSchedule(call, c.Now, behave.DefaultStandalone, c.Rand)
			return callChanges(call, share)
		},
		Emit: func(d spec.Device, _ spec.TickContext) []spec.Event {
			call := callOf(d)
			id, n := call.ID, call.Participants
			var out []spec.Event
			switch call.Event {
			case behave.EventEnd, behave.EventTurn:
				out = append(out,
					spec.Event{Name: "call-ended", Data: fmt.Sprintf(`{"id":"%s","participants":%d}`, call.IDPrev, call.ParticipantsPrev)},
					logLine("kvroom.call", "INFO", fmt.Sprintf("Call ended (id=%s, peers=%d, reason=remote-hangup)", call.IDPrev, call.ParticipantsPrev)))
			}
			switch call.Event {
			case behave.EventStart, behave.EventTurn:
				out = append(out, spec.Event{Name: "call-started", Data: fmt.Sprintf(`{"id":"%s","participants":%d}`, id, n)},
					logLine("kvroom.call", "INFO", fmt.Sprintf("Call started (id=%s, protocol=sip, peers=%d)", id, n)))
			case behave.EventJoin:
				out = append(out, spec.Event{Name: "participant-joined", Data: fmt.Sprintf(`{"call_id":"%s","participant":"guest-%d","participants":%d}`, id, n, n)},
					logLine("kvroom.call", "INFO", fmt.Sprintf("Participant joined (call=%s, peers=%d)", id, n)))
			case behave.EventLeave:
				out = append(out, spec.Event{Name: "participant-left", Data: fmt.Sprintf(`{"call_id":"%s","participant":"guest-%d","participants":%d}`, id, call.ParticipantsPrev, n)},
					logLine("kvroom.call", "INFO", fmt.Sprintf("Participant left (call=%s, peers=%d)", id, n)))
			}
			return out
		},
	}
}

// logLine is one line of the codec's log stream (event: log): its severity,
// facility and text as the bar's own log writes them.
func logLine(facility, severity, text string) spec.Event {
	return spec.Event{Name: "log", Data: fmt.Sprintf(`{"severity":"%s","facility":"%s","text":"%s"}`, severity, facility, text)}
}

// peopleCount is the camera counting the room: the lab calendar's attendees in
// a held meeting or a walk-in, less the one it misses 15% of ticks, and 0 in a
// no-show or outside any meeting. With no calendar it counts only the call it is
// in, a steady 0 when idle.
func peopleCount() spec.Tick {
	return spec.Tick{
		Name: "people_count", Interval: bind.Seconds(30), OnStart: true, Writes: []string{pathPeople},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			peers := 0
			if spec.Bool(d, pathActive) {
				peers = spec.Int(d, pathParticipants)
			}
			next := behave.PeopleCount(behave.OccupancyState{Count: spec.Int(d, pathPeople)}, c.Room, peers, behave.CallCount, c.Rand)
			return []surface.Change{{Path: pathPeople, Value: int64(next.Count)}}
		},
	}
}

// statusLog is a status line every 15 minutes, call or none, so the log stream
// is not only a call's.
func statusLog() spec.Tick {
	return spec.Tick{
		Name: "status_log", Interval: 15 * time.Minute,
		Run: func(spec.Device, spec.TickContext) []surface.Change { return nil },
		Emit: func(d spec.Device, _ spec.TickContext) []spec.Event {
			call := "idle"
			if spec.Bool(d, pathActive) {
				call = "active"
			}
			return []spec.Event{logLine("kvroom.system", "INFO", fmt.Sprintf("Status: power=%v, call=%s, cpu=%v%%, temp=%vC, fan=%vrpm",
				d.Read(pathPower), call, d.Read(pathCPU), d.Read(pathTemp), d.Read(pathFan)))}
		},
	}
}

// The bands the call stats drift in.
var (
	lossBand    = behave.Band{Lo: 0, Hi: 0.06, Step: 0.012}
	jitterBand  = behave.Band{Lo: 2, Hi: 45, Step: 6}
	bitrateBand = behave.Band{Lo: 1200, Hi: 4000, Step: 400}
	frameBand   = behave.Band{Lo: 24, Hi: 30, Step: 3}
	speakerBand = behave.Band{Lo: 10, Hi: 85, Step: 15}
	micBand     = behave.Band{Lo: -50, Hi: -6, Step: 6}
	busyBand    = behave.Band{Lo: 35, Hi: 80, Step: 8}
	idleBand    = behave.Band{Lo: 8, Hi: 25, Step: 4}
	tempBand    = behave.Band{Lo: 38, Hi: 62, Step: 1.5}
	fanBand     = behave.Band{Lo: 1800, Hi: 3600, Step: 150}
	memoryBand  = behave.Band{Lo: 0.35, Hi: 0.7, Step: 0.03}
)

// mediaStats is the call stats, every 30 s: the media a call carries drifts
// while one is up and rests at zero when the room is idle; the codec's own load,
// temperature, fan and memory drift either way (busier in a call). A lossy
// stretch writes the WARN line a real bar's network log does, and its recovery
// an INFO line.
func mediaStats() spec.Tick {
	return spec.Tick{
		Name: "media_stats", Interval: bind.Seconds(30),
		Writes: []string{pathLoss, pathNetEvent, pathDegraded, pathJitter, pathBitrate, pathFrames, pathSpeaker, pathMic, pathCPU, pathTemp, pathFan, pathMemory},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			active, rng := spec.Bool(d, pathActive), c.Rand
			drift := func(path string, band behave.Band, places int) float64 {
				return behave.Drift(spec.Float(d, path), band, rng, behave.Places(places))
			}
			loss, jitter, bitrate, frames, speaker, mic := 0.0, 0.0, 0.0, 0.0, 0.0, -60.0
			cpuBand := idleBand
			if active {
				loss = drift(pathLoss, lossBand, 4)
				jitter = drift(pathJitter, jitterBand, 1)
				bitrate = drift(pathBitrate, bitrateBand, 0)
				frames = drift(pathFrames, frameBand, 0)
				speaker = drift(pathSpeaker, speakerBand, 0)
				mic = drift(pathMic, micBand, 1)
				cpuBand = busyBand
			}
			// The uplink's state, with hysteresis: degraded as loss crosses 3%,
			// recovered once it is back under 2%, so its log lines are its
			// transitions, not a line a tick.
			was, edge := spec.Bool(d, pathDegraded), ""
			switch l := behave.Clamp(0, 1, loss); {
			case l > 0.03 && !was:
				edge = "degraded"
			case l <= 0.02 && was:
				edge = "recovered"
			}
			degraded := was
			switch edge {
			case "degraded":
				degraded = true
			case "recovered":
				degraded = false
			}
			cpu := drift(pathCPU, cpuBand, 0)
			temp := drift(pathTemp, tempBand, 1)
			fan := drift(pathFan, fanBand, -1)
			memory := drift(pathMemory, memoryBand, 3)
			return []surface.Change{
				{Path: pathLoss, Value: loss}, {Path: pathNetEvent, Value: edge}, {Path: pathDegraded, Value: degraded},
				{Path: pathJitter, Value: jitter}, {Path: pathBitrate, Value: bitrate}, {Path: pathFrames, Value: frames},
				{Path: pathSpeaker, Value: speaker}, {Path: pathMic, Value: mic},
				{Path: pathCPU, Value: cpu}, {Path: pathTemp, Value: temp}, {Path: pathFan, Value: fan}, {Path: pathMemory, Value: memory},
			}
		},
		Emit: func(d spec.Device, _ spec.TickContext) []spec.Event {
			loss := fmt.Sprintf("%.1f", 100*behave.Clamp(0, 1, spec.Float(d, pathLoss)))
			switch spec.String(d, pathNetEvent) {
			case "degraded":
				return []spec.Event{logLine("kvroom.net", "WARN", fmt.Sprintf("Uplink degraded: packet loss %s%%, jitter %vms, enabling rate adaptation", loss, d.Read(pathJitter)))}
			case "recovered":
				return []spec.Event{logLine("kvroom.net", "INFO", fmt.Sprintf("Uplink recovered: loss %s%%, jitter %vms, resumed full-rate send", loss, d.Read(pathJitter)))}
			}
			return nil
		},
	}
}

// ---- documents ----

type systemDoc struct {
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer"`
	Serial       string `json:"serial"`
	Firmware     string `json:"firmware"`
	Revision     string `json:"hardware_revision"`
	Hostname     string `json:"hostname"`
	Address      string `json:"ip_address"`
	MAC          string `json:"mac_address"`
	Uptime       int64  `json:"uptime"`
	Power        string `json:"power"`
}

type callDoc struct {
	Active       bool  `json:"active"`
	Participants int64 `json:"participants"`
}

type audioDoc struct {
	Volume       float64 `json:"volume"`
	Mute         bool    `json:"mic_mute"`
	SpeakerLevel float64 `json:"speaker_level"`
}

type ack struct {
	OK bool `json:"ok"`
}

type powerAck struct {
	OK    bool   `json:"ok"`
	Power string `json:"power"`
}

type actionAck struct {
	OK     bool   `json:"ok"`
	Action string `json:"action"`
}

func document(v any) spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(v)}
}

// appObject is the band the course datapoints are read from, broken out; the
// status snapshot carries the same four.
func appObject(d spec.Device) httpapi.Object {
	return httpapi.Object{
		{Key: "mode", Value: spec.String(d, pathAppMode)},
		{Key: "ucc_signin", Value: spec.String(d, pathSignin)},
		{Key: "touch_pair", Value: spec.String(d, pathTouch)},
		{Key: "local_presentation", Value: spec.Bool(d, pathPresenting)},
	}
}

func noted(o httpapi.Object) httpapi.Object {
	for i := range o {
		o[i].Noted = true
	}
	return o
}

func callObject(d spec.Device) httpapi.Object {
	return httpapi.Object{{Key: "active", Value: spec.Bool(d, pathActive)}, {Key: "participants", Value: spec.Int64(d, pathParticipants)}}
}

func statusDocument(d spec.Device) []byte {
	return httpapi.Annotated(httpapi.Object{
		{Key: "device", Value: httpapi.Object{
			{Key: "power", Value: spec.String(d, pathPower)},
			{Key: "uptime", Value: spec.Int64(d, pathUptime)},
			{Key: "model", Value: spec.String(d, pathModel)},
			{Key: "serial", Value: spec.String(d, pathSerial)},
			{Key: "firmware", Value: spec.String(d, pathFirmware)},
			{Key: "cpu_load", Value: spec.Float(d, pathCPU), Noted: true},
			{Key: "temperature", Value: spec.Float(d, pathTemp), Noted: true},
			{Key: "fan_rpm", Value: spec.Float(d, pathFan), Noted: true},
			{Key: "memory_used", Value: spec.Float(d, pathMemory), Noted: true},
		}},
		{Key: "app", Value: appObject(d)},
		{Key: "call", Value: callObject(d)},
		{Key: "audio", Value: httpapi.Object{
			{Key: "volume", Value: spec.Float(d, pathVolume)},
			{Key: "mic_mute", Value: spec.Bool(d, pathMute)},
			{Key: "speaker_level", Value: spec.Float(d, pathSpeaker)},
			{Key: "mic_level_db", Value: spec.Float(d, pathMic), Noted: true},
		}},
		{Key: "video", Value: httpapi.Object{
			{Key: "camera_on", Value: spec.Bool(d, pathCamera)},
			{Key: "resolution", Value: spec.String(d, pathResolution)},
			{Key: "people_count", Value: spec.Int64(d, pathPeople), Noted: true},
		}},
		{Key: "media", Value: noted(httpapi.Object{
			{Key: "packet_loss", Value: spec.Float(d, pathLoss)},
			{Key: "jitter_ms", Value: spec.Float(d, pathJitter)},
			{Key: "bitrate_kbps", Value: spec.Float(d, pathBitrate)},
			{Key: "frame_rate", Value: spec.Float(d, pathFrames)},
		})},
	})
}

// recent is the log lines the private route serves.
func recent(d spec.Device) []string {
	var out []string
	switch lines := d.Read(pathLogs).(type) {
	case []any:
		for _, l := range lines {
			out = append(out, fmt.Sprint(l))
		}
	case []string:
		out = lines
	}
	return out
}

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /", Summary: "Codec dashboard", Handle: dashboardRoute},
		{Match: "GET /api/status", Summary: "Full status snapshot (app, call, audio, video)", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return spec.HTTPResponse{ContentType: "application/json", Body: statusDocument(d)}
		}},
		{Match: "GET /api/app", Summary: "App mode, UCC sign-in, touch pairing, local presentation", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Annotated(noted(appObject(d)))}
		}},
		// The private route: recent device logs. Not advertised as a public API,
		// but the web admin page's log card fetches it from its script, so the
		// request is visible in the browser's network tab.
		{Match: "GET /api/logs", Summary: "Recent device logs (private; undocumented direct-call route)", Private: true, Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Annotated(httpapi.Object{{Key: "recent", Value: recent(d), Noted: true}})}
		}},
		{Match: "GET /api/call", Summary: "Current call state", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(callDoc{spec.Bool(d, pathActive), spec.Int64(d, pathParticipants)})
		}},
		{Match: "GET /api/audio", Summary: "Audio state (volume, mute)", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(audioDoc{spec.Float(d, pathVolume), spec.Bool(d, pathMute), spec.Float(d, pathSpeaker)})
		}},
		{Match: "GET /api/system", Summary: "System unit info (serial, firmware, uptime)", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(systemDoc{
				Model: spec.String(d, pathModel), Manufacturer: spec.String(d, pathMaker), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware),
				Revision: spec.String(d, pathRevision), Hostname: spec.String(d, pathHostname), Address: spec.String(d, pathAddress), MAC: spec.String(d, pathMAC),
				Uptime: spec.Int64(d, pathUptime), Power: spec.String(d, pathPower),
			})
		}},
		httpapi.Control{
			Match: "POST /api/audio/volume", Summary: "Set speaker volume", Example: "{\"volume\": 50}\n",
			Fields: []httpapi.Field{{Path: pathVolume, Name: "volume", Kind: httpapi.Number}, {Path: pathSpeaker, Name: "volume", Kind: httpapi.Number}},
			Ack:    func(spec.Device) any { return ack{true} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/audio/mute", Summary: "Set microphone mute", Example: "{\"mute\": true}\n",
			Fields: []httpapi.Field{{Path: pathMute, Name: "mute", Kind: httpapi.Flag}},
			Ack:    func(spec.Device) any { return ack{true} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/video/camera", Summary: "Toggle camera privacy shutter", Example: "{\"camera_on\": true}\n",
			Fields: []httpapi.Field{{Path: pathCamera, Name: "camera_on", Kind: httpapi.Flag}},
			Ack:    func(spec.Device) any { return ack{true} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/system/standby", Summary: "Set codec standby state", Example: "{\"power\": \"standby\"}\n",
			Fields: []httpapi.Field{{Path: pathPower, Name: "power", Kind: httpapi.Choice, Values: []string{"on", "off", "standby"}}},
			Ack:    func(d spec.Device) any { return powerAck{true, spec.String(d, pathPower)} },
		}.Route(),
		{Match: "POST /api/call/dial", Summary: "Place a call (the room joins a meeting)", Example: "{\"id\": \"c9001\", \"participants\": 3}\n", Handle: dial},
		{Match: "POST /api/call/disconnect", Summary: "Disconnect active call", Example: "{}", Handle: hangUp},
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot codec (clears call + uptime)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{
					{Path: pathBootEpoch, Value: d.Now().Unix()}, {Path: pathActive, Value: false}, {Path: pathParticipants, Value: 0}, {Path: pathDialed, Value: false},
				}, httpapi.Reset(d, pathMute, pathVolume, pathPresenting)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/factory-reset", Summary: "Factory reset (resets every status path to defaults)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				out := []surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}, {Path: pathDialed, Value: false}}
				return append(out, httpapi.Reset(d, pathPower, pathActive, pathParticipants, pathVolume, pathMute, pathSpeaker, pathCamera, pathResolution,
					pathAppMode, pathSignin, pathTouch, pathPresenting)...)
			},
			Ack: actionAck{true, "factory-reset"},
		}.Route(),
	}
}

// ---- the call, placed and ended over the API ----

var (
	idPattern    = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
	peersPattern = regexp.MustCompile(`^([1-9]|[1-5][0-9]|6[0-4])$`)
)

// defaultPeers is the head count a dialed call holds when the body names none.
const defaultPeers = 2

// truthy is whether a body value counts as given, as the template's `or` took
// it: not missing, false, zero or empty.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// looked is the text a template wrote as the value it reads as when it reaches
// state: a whole number as a number, text that is JSON as that, anything else
// as the text.
func looked(text string) any {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if !strings.ContainsAny(trimmed, ".eE") {
		if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return n
		}
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return text
}

// dial places a call now, whatever the lab calendar holds: a meeting running
// under it is skipped, and the call runs until it is hung up or for the default
// length (status.sim.dial_length minutes of call_cycle ticks). The call starts
// and ends as named occurrences on the live feed, as a codec's xAPI reports
// CallSuccessful and CallDisconnect with the call's id, beside the state changes
// the feed streams. The id is the caller's to choose, so a test or a demo names
// the call it places.
func dial(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.FieldText(r.Body["id"])
	peers := r.Body["participants"]
	if !truthy(peers) {
		peers = int64(defaultPeers)
	}
	peersText := httpapi.FieldText(peers)
	var invalid []httpapi.Invalid
	if !idPattern.MatchString(id) {
		invalid = append(invalid, httpapi.Invalid{Path: pathCallID, Value: id, Pattern: idPattern.String()})
	}
	if !peersPattern.MatchString(peersText) {
		invalid = append(invalid, httpapi.Invalid{Path: pathParticipants, Value: peersText, Pattern: peersPattern.String()})
	}
	if len(invalid) > 0 {
		return httpapi.Reject(invalid...)
	}
	err := d.WriteMany(
		surface.Change{Path: pathCallID, Value: looked(id)}, surface.Change{Path: pathActive, Value: true},
		surface.Change{Path: pathParticipants, Value: looked(peersText)}, surface.Change{Path: pathDialed, Value: true},
		surface.Change{Path: pathDialTicks, Value: 0},
	)
	if err != nil {
		return spec.Failure(500, err.Error())
	}
	d.Publish(spec.Event{Name: "call-started", Data: fmt.Sprintf(`{"id":"%s","participants":%s}`, id, peersText)})
	d.Publish(logLine("kvroom.call", "INFO", fmt.Sprintf("Call started (id=%s, protocol=sip, peers=%s, dialed)", id, peersText)))
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Spaced(powerAck{true, spec.String(d, pathPower)})}
}

// hangUp ends the call now, dialed or on the calendar, and the next tick marks
// the meeting it ended as skipped, so the bar does not join it again; the room's
// next meeting is joined as it comes.
func hangUp(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	err := d.WriteMany(
		surface.Change{Path: pathActive, Value: false}, surface.Change{Path: pathParticipants, Value: 0}, surface.Change{Path: pathDialed, Value: false},
		surface.Change{Path: pathDialTicks, Value: 0}, surface.Change{Path: pathHungup, Value: true},
		surface.Change{Path: pathStartAt, Value: 0}, surface.Change{Path: pathEndAt, Value: 0}, surface.Change{Path: pathPlanAt, Value: 0},
	)
	if err != nil {
		return spec.Failure(500, err.Error())
	}
	id := spec.String(d, pathCallID)
	d.Publish(spec.Event{Name: "call-ended", Data: fmt.Sprintf(`{"id":"%s"}`, id)})
	d.Publish(logLine("kvroom.call", "INFO", fmt.Sprintf("Call ended (id=%s, reason=local-hangup)", id)))
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Spaced(powerAck{true, spec.String(d, pathPower)})}
}
