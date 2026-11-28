// Package kestrelgaze is the Kestrel Gaze P2 PTZ camera (#2834), a Go device
// family of one model. The Gaze P2 is Kestrel's pan-tilt camera with a motorised
// lens for rooms too big for a bar's fixed lens: an auditorium's or lecture
// hall's presenter camera. It either follows the presenter on its own (tracking)
// or sits where an operator put it, by hand or on a stored preset. What an
// operator watches it for is where it points and whether it is still tracking.
//
// In a room with a lab calendar it follows the room's actual attendees:
// presence and the number of people framed come from the meeting in progress
// (nobody in a no-show), it frames the group when a meeting starts (tracking on,
// a magnification wide enough for the room) and returns to its home preset when
// one ends. A camera whose room has no calendar keeps its old self: it tracks
// and drifts on its own. Its one face is the HTTP JSON API over https behind
// basic auth the VRoom speaks (admin / kestrel), with the routes a camera has:
// no SNMP, no command line.
//
// The family replaced the kestrel-gaze-p2 YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package kestrelgaze

import (
	"fmt"
	"strconv"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
)

// Name is the family's id, the profile name its devices carry.
const Name = "kestrel-gaze"

// Row is one model of the camera line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
}

// Rows is the model table: the Gaze line, one model.
var Rows = []Row{
	{Product: "kestrel-gaze-p2", Model: "Gaze P2", Firmware: "1.4.2"},
}

// The state paths of the camera.
const (
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
	pathPan       = "status.camera.pan"
	pathTilt      = "status.camera.tilt"
	pathMag       = "status.camera.magnification"
	pathPreset    = "status.camera.preset"
	pathTracking  = "status.camera.tracking"
	pathPresence  = "status.camera.presence"
	pathPeople    = "status.camera.people"
	pathKnown     = "status.sim.known"
	pathMeeting   = "status.sim.meeting"
	pathModel     = "configuration.device.model"
	pathMaker     = "configuration.device.manufacturer"
	pathSerial    = "configuration.device.serial"
	pathFirmware  = "configuration.firmware.version"
)

// The tracking modes: following the presenter, or holding where it is.
const (
	tracking = "tracking"
	manual   = "manual"
)

// presets are the stored positions, by number: the lectern, the audience and
// the screen. A preset recall moves the camera there and takes it off tracking,
// as a real camera leaves auto-framing when told where to point.
var presets = map[string]struct {
	Pan, Tilt int64
	Mag       float64
}{
	"1": {-30, -5, 1},
	"2": {30, 5, 2.5},
	"3": {0, 10, 4},
}

// The lens covers a range, and tracking wanders inside it.
var (
	panBand  = behave.Band{Lo: -60, Hi: 60, Step: 3}
	tiltBand = behave.Band{Lo: -20, Hi: 20, Step: 1}
)

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device": map[string]any{"boot_epoch": 0},
			"camera": map[string]any{
				"pan": 0, "tilt": 0, "magnification": 1, "preset": 0, "tracking": tracking, "presence": false, "people": 0,
			},
			"sim":     map[string]any{"known": false, "meeting": "none"},
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Kestrel"},
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
			Manufacturer: "Kestrel", Model: "Gaze", DeviceType: "ptz-camera",
			Summary: "Kestrel Gaze P2 PTZ camera",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, Summary: fmt.Sprintf("Kestrel %s PTZ camera", r.Model)}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 443, TLS: true, Doc: profile.Doc{Summary: "Kestrel Gaze JSON API"},
				Auth: profile.Auth{Type: "basic", DefaultUser: "admin", DefaultPassword: "kestrel", Realm: "Kestrel Gaze P2", Enforce: true},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability"}},
		},
		State:   stateFor(Rows[0]),
		HTTP:    routes(),
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{roomMeeting(), trackingDrift()},
	}
}

// meetingKey names the meeting the camera is framed for: none outside one and in
// a no-show, else the meeting by its start.
func meetingKey(c spec.TickContext) string {
	if !c.Room.HasMeeting {
		return "none"
	}
	if m, ok := c.Room.Meeting.AsMeeting(); ok && m.Outcome == "no-show" {
		return "none"
	}
	return "m" + strconv.FormatInt(c.Room.Meeting.Start.Unix(), 10)
}

// roomMeeting is the camera reading its room's meeting off the lab calendar
// through the device's own clock. People and presence are readings, set every
// tick: the meeting's actual attendees, nobody in a no-show or between meetings.
// Framing is the edge: when a meeting starts the camera is told to track and
// takes a magnification that frames the group (tighter for fewer people, 1 to
// 4); when it ends it returns to its home preset (the defaults a reboot
// restores). An operator's change in the middle of a meeting holds until the
// next edge. A room with no calendar is left alone.
func roomMeeting() spec.Tick {
	return spec.Tick{
		Name: "room_meeting", Interval: bind.Seconds(20), OnStart: true,
		Writes: []string{pathKnown, pathPeople, pathPresence, pathMag, pathPan, pathTilt, pathPreset, pathTracking, pathMeeting},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			known := c.Room.HasCalendar
			people := spec.Int64(d, pathPeople)
			if known {
				people = 0
				if c.Room.CallActive {
					people = int64(c.Room.Attendees)
				}
			}
			key, was := meetingKey(c), spec.String(d, pathMeeting)
			edge := known && key != was
			out := []surface.Change{
				{Path: pathKnown, Value: known},
				{Path: pathPeople, Value: people},
				{Path: pathPresence, Value: people != 0},
			}
			switch {
			case !edge:
			case key == "none":
				out = append(out, surface.Change{Path: pathMag, Value: d.Default(pathMag)})
			default:
				out = append(out, surface.Change{Path: pathMag, Value: behave.Round(1, behave.Clamp(1.0, 4.0, 4.5-0.3*float64(people)))})
			}
			if edge {
				for _, p := range []string{pathPan, pathTilt, pathPreset, pathTracking} {
					out = append(out, surface.Change{Path: p, Value: d.Default(p)})
				}
			}
			if known {
				out = append(out, surface.Change{Path: pathMeeting, Value: key})
			}
			return out
		},
	}
}

// trackingDrift is the camera following the presenter: while tracking, pan and
// tilt wander a few degrees a tick within the range the lens covers. Set to
// manual it holds where it is. In a room with a calendar it tracks only while
// someone is in the room: an empty room's camera holds its home position.
func trackingDrift() spec.Tick {
	return spec.Tick{
		Name: "tracking_drift", Interval: bind.Seconds(20), Writes: []string{pathPan, pathTilt},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			if spec.String(d, pathTracking) != tracking || !(spec.Bool(d, pathPresence) || !spec.Bool(d, pathKnown)) {
				return nil
			}
			return []surface.Change{
				{Path: pathPan, Value: behave.Drift(spec.Float(d, pathPan), panBand, c.Rand, behave.Places(1))},
				{Path: pathTilt, Value: behave.Drift(spec.Float(d, pathTilt), tiltBand, c.Rand, behave.Places(1))},
			}
		},
	}
}

type systemDoc struct {
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer"`
	Serial       string `json:"serial"`
	Firmware     string `json:"firmware"`
	Uptime       int64  `json:"uptime"`
}

type cameraDoc struct {
	Pan           float64 `json:"pan"`
	Tilt          float64 `json:"tilt"`
	Magnification float64 `json:"magnification"`
	Preset        int64   `json:"preset"`
	Tracking      string  `json:"tracking"`
	Presence      bool    `json:"presence"`
	People        int64   `json:"people"`
}

type ack struct {
	OK bool `json:"ok"`
}

type actionAck struct {
	OK     bool   `json:"ok"`
	Action string `json:"action"`
}

func document(v any) spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(v)}
}

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /api/system", Summary: "System unit info (model, serial, firmware, uptime)", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(systemDoc{spec.String(d, pathModel), spec.String(d, pathMaker), spec.String(d, pathSerial), spec.String(d, pathFirmware), spec.Int64(d, pathUptime)})
		}},
		{Match: "GET /api/camera", Summary: "Pan, tilt, lens magnification, preset and tracking", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(cameraDoc{
				spec.Float(d, pathPan), spec.Float(d, pathTilt), spec.Float(d, pathMag), spec.Int64(d, pathPreset),
				spec.String(d, pathTracking), spec.Bool(d, pathPresence), spec.Int64(d, pathPeople),
			})
		}},
		httpapi.Control{
			Match: "POST /api/camera/preset", Summary: "Recall a stored preset", Example: "{\"preset\": 2}\n",
			Fields: []httpapi.Field{{Path: pathPreset, Name: "preset", Kind: httpapi.Choice, Values: []string{"1", "2", "3"}}},
			Also: func(written map[string]any) []surface.Change {
				name, _ := written[pathPreset].(string)
				n, _ := strconv.ParseInt(name, 10, 64)
				p := presets[name]
				return []surface.Change{
					{Path: pathPreset, Value: n}, {Path: pathPan, Value: p.Pan}, {Path: pathTilt, Value: p.Tilt}, {Path: pathMag, Value: whole(p.Mag)},
					{Path: pathTracking, Value: manual},
				}
			},
			Ack: func(spec.Device) any { return ack{true} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/camera/position", Summary: "Point the camera by hand", Example: "{\"pan\": -12.5, \"tilt\": 4, \"magnification\": 1.5}\n",
			Fields: []httpapi.Field{
				{Path: pathPan, Name: "pan", Kind: httpapi.Number},
				{Path: pathTilt, Name: "tilt", Kind: httpapi.Number},
				{Path: pathMag, Name: "magnification", Kind: httpapi.Number},
			},
			Also: func(map[string]any) []surface.Change {
				return []surface.Change{{Path: pathPreset, Value: 0}, {Path: pathTracking, Value: manual}}
			},
			Ack: func(spec.Device) any { return ack{true} },
		}.Route(),
		httpapi.Control{
			Match: "POST /api/camera/tracking", Summary: "Turn presenter tracking on or off", Example: "{\"tracking\": \"tracking\"}\n",
			Fields: []httpapi.Field{{Path: pathTracking, Name: "tracking", Kind: httpapi.Choice, Values: []string{tracking, manual}}},
			Ack:    func(spec.Device) any { return ack{true} },
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot camera (clears uptime, returns to its home position)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}},
					httpapi.Reset(d, pathPan, pathTilt, pathMag, pathPreset, pathTracking)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
	}
}

// whole is a number as the profile's template wrote it: a whole one as an
// integer.
func whole(f float64) any {
	if f == float64(int64(f)) {
		return int64(f)
	}
	return f
}
