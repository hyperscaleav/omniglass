// Package kestrelrelay is the Kestrel Relay C1 conferencing codec (#2834), a Go
// device family of one model. The Relay C1 is Kestrel's rack codec: the box a
// room's cameras, microphones and touch panel plug into, with no camera or
// speakers of its own (the VRoom is the all-in-one bar). What an operator
// watches it for is whether it is in a call, whether the room is sharing
// content, and whether its peripherals are still plugged in. Its one face is
// the HTTP JSON API over https behind basic auth the VRoom speaks (admin /
// kestrel), with the routes a codec has: no SNMP, no command line.
//
// The codec's call, far-end participants and shared content follow the lab
// calendar: it is in a call exactly when its room's meeting has somebody in it,
// as the video bar beside it is, and keeps no schedule of its own. A call placed
// over the API holds against the calendar until it is disconnected, and a
// disconnect on a meeting is not rejoined. With no calendar, or in no room, it
// never calls by itself.
//
// The family replaced the kestrel-relay-c1 YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package kestrelrelay

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
)

// Name is the family's id, the profile name its devices carry.
const Name = "kestrel-relay"

// Row is one model of the codec line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
}

// Rows is the model table: the Relay line, one model.
var Rows = []Row{
	{Product: "kestrel-relay-c1", Model: "Relay C1", Firmware: "2.7.3"},
}

// The state paths of the codec.
const (
	pathUptime       = "status.device.uptime"
	pathBootEpoch    = "status.device.boot_epoch"
	pathActive       = "status.call.active"
	pathParticipants = "status.call.participants"
	pathSharing      = "status.content.sharing"
	pathSource       = "status.content.source"
	pathConnected    = "status.peripherals.connected"
	pathEvent        = "status.sim.event"
	pathWant         = "status.sim.want"
	pathWantN        = "status.sim.want_n"
	pathCall         = "status.sim.call"
	pathSkip         = "status.sim.skip"
	pathHungup       = "status.sim.hungup"
	pathDialed       = "status.sim.dialed"
	pathSharePrev    = "status.sim.share_prev"
	pathModel        = "configuration.device.model"
	pathMaker        = "configuration.device.manufacturer"
	pathSerial       = "configuration.device.serial"
	pathFirmware     = "configuration.firmware.version"
)

// sources are the inputs the codec shares from.
var sources = []string{"hdmi1", "hdmi2", "usbc", "wireless"}

// callParams are the codec's call rules: it reads the calendar alone, and an
// ended call leaves no identity behind.
func callParams() behave.CallParams {
	p := behave.DefaultCall
	p.ClearIDOnEnd = true
	return p
}

// shareParams hold sharing as it was outside a call, ending it only with the
// call that carried it.
var shareParams = behave.ShareParams{ToggleChance: behave.DefaultShare.ToggleChance, HoldIdle: true}

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device":      map[string]any{"boot_epoch": 0},
			"call":        map[string]any{"active": false, "participants": 0},
			"content":     map[string]any{"sharing": false, "source": ""},
			"peripherals": map[string]any{"connected": 3},
			// The call cycle's bookkeeping: what this tick did, the call the
			// room's meeting says it should be in and how many came, the call
			// it is in, the start of a meeting a dial overtook or a hang-up
			// ended (skipped), a hang-up waiting for the next tick and whether
			// the call was dialed. The simulator's, not the codec's: no route
			// serves it.
			"sim": map[string]any{
				"event": "", "want": "", "want_n": 0, "call": "", "skip": 0,
				"hungup": false, "dialed": false, "share_prev": false,
			},
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
			Manufacturer: "Kestrel", Model: "Relay", DeviceType: "video-codec",
			Summary: "Kestrel Relay C1 conferencing codec",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, Summary: fmt.Sprintf("Kestrel %s conferencing codec", r.Model)}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 443, TLS: true, Doc: profile.Doc{Summary: "Kestrel Relay JSON API"},
				Auth: profile.Auth{Type: "basic", DefaultUser: "admin", DefaultPassword: "kestrel", Realm: "Kestrel Relay C1", Enforce: true},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability"}},
		},
		State:   stateFor(Rows[0]),
		HTTP:    routes(),
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{callCycle()},
	}
}

// callOf is the call the codec holds, as the primitives read it. The codec
// keeps the call's id in status.sim.call.
func callOf(d spec.Device) behave.CallState {
	return behave.CallState{
		Active: spec.Bool(d, pathActive), ID: spec.String(d, pathCall), Participants: spec.Int(d, pathParticipants),
		Event: behave.CallEvent(spec.String(d, pathEvent)), Skip: spec.Int64(d, pathSkip), Want: spec.String(d, pathWant), WantN: spec.Int(d, pathWantN),
		Hungup: spec.Bool(d, pathHungup), Dialed: spec.Bool(d, pathDialed),
	}
}

// callCycle is the codec's call following its room's meetings, a tick a
// minute. One tick reads the calendar once into the call the room should be in
// and who came, and the rest of the tick reads those. A meeting starting as the
// last ends moves the call on in one tick. Far-end participants open at half to
// all of who came and move inside it from the device's own draws, and content is
// shared now and then in a call, from one of the codec's sources.
func callCycle() spec.Tick {
	return spec.Tick{
		Name: "call_cycle", Interval: bind.Seconds(60), OnStart: true,
		Writes: []string{pathSkip, pathHungup, pathWant, pathWantN, pathEvent, pathCall, pathActive, pathParticipants, pathSharing, pathSharePrev, pathSource},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			call := behave.FollowsCall(callOf(d), c.Room, c.Now, callParams(), c.Rand)
			share := behave.ContentSharing(behave.ShareState{Sharing: spec.Bool(d, pathSharing), Prev: spec.Bool(d, pathSharePrev)}, c.Room,
				behave.CallSignals{Active: call.Active, Dialed: call.Dialed, Ended: call.Event == behave.EventEnd}, shareParams, c.Rand)
			source := behave.ContentSource(spec.String(d, pathSource), share.Sharing, sources, c.Rand)
			return []surface.Change{
				{Path: pathSkip, Value: call.Skip},
				{Path: pathHungup, Value: call.Hungup},
				{Path: pathWant, Value: call.Want},
				{Path: pathWantN, Value: int64(call.WantN)},
				{Path: pathEvent, Value: string(call.Event)},
				{Path: pathCall, Value: call.ID},
				{Path: pathActive, Value: call.Active},
				{Path: pathParticipants, Value: int64(call.Participants)},
				{Path: pathSharing, Value: share.Sharing},
				{Path: pathSharePrev, Value: share.Prev},
				{Path: pathSource, Value: source},
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

type callDoc struct {
	Active       bool  `json:"active"`
	Participants int64 `json:"participants"`
}

type contentDoc struct {
	Sharing bool   `json:"sharing"`
	Source  string `json:"source"`
}

type peripheralsDoc struct {
	Connected int64 `json:"connected"`
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
		{Match: "GET /api/call", Summary: "Current call state", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(callDoc{spec.Bool(d, pathActive), spec.Int64(d, pathParticipants)})
		}},
		{Match: "GET /api/content", Summary: "Content sharing state and its source", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(contentDoc{spec.Bool(d, pathSharing), spec.String(d, pathSource)})
		}},
		{Match: "GET /api/peripherals", Summary: "Peripherals connected to the codec", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
			return document(peripheralsDoc{spec.Int64(d, pathConnected)})
		}},
		{Match: "POST /api/call/dial", Summary: "Place a call", Example: "{\"participants\": 3}\n", Handle: dial},
		httpapi.Action{
			Match: "POST /api/call/disconnect", Summary: "Disconnect the active call (and stop sharing)", Example: "{}",
			// The next tick marks the meeting it ended as skipped.
			Writes: func(spec.Device) []surface.Change {
				return []surface.Change{
					{Path: pathActive, Value: false}, {Path: pathParticipants, Value: 0}, {Path: pathSharing, Value: false}, {Path: pathSource, Value: ""},
					{Path: pathDialed, Value: false}, {Path: pathHungup, Value: true},
				}
			},
			Ack: ack{true},
		}.Route(),
		httpapi.Control{
			Match: "POST /api/content/share", Summary: "Start sharing content from a source", Example: "{\"source\": \"hdmi1\"}\n",
			Fields: []httpapi.Field{{Path: pathSource, Name: "source", Kind: httpapi.Choice, Values: sources}},
			Also: func(spec.Device, map[string]any) []surface.Change {
				return []surface.Change{{Path: pathSharing, Value: true}}
			},
			Ack: func(spec.Device) any { return ack{true} },
		}.Route(),
		httpapi.Action{
			Match: "POST /api/content/stop", Summary: "Stop sharing content", Example: "{}",
			Writes: func(spec.Device) []surface.Change {
				return []surface.Change{{Path: pathSharing, Value: false}, {Path: pathSource, Value: ""}}
			},
			Ack: ack{true},
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot codec (clears the call and any shared content, and uptime)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return []surface.Change{
					{Path: pathBootEpoch, Value: d.Now().Unix()}, {Path: pathActive, Value: false}, {Path: pathParticipants, Value: 0},
					{Path: pathSharing, Value: false}, {Path: pathSource, Value: ""}, {Path: pathDialed, Value: false},
				}
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
	}
}

// defaultPeers is the head count a call placed with none holds.
const defaultPeers = 2

// dial places a call now: it holds against the lab calendar until it is
// disconnected, and a meeting running under it is not joined. The count is the
// body's participants, two when it names none (or names zero, as the profile's
// template did); a count that is not a number is refused.
func dial(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	peers, ok := peersOf(r.Body["participants"])
	if !ok {
		return httpapi.Refuse("participants must be a number")
	}
	err := d.WriteMany(
		surface.Change{Path: pathActive, Value: true}, surface.Change{Path: pathParticipants, Value: peers},
		surface.Change{Path: pathDialed, Value: true}, surface.Change{Path: pathCall, Value: ""},
	)
	if err != nil {
		return spec.Failure(500, err.Error())
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Spaced(ack{true})}
}

// peersOf reads a head count the way the template that took it did: a value
// that is missing, empty or zero is the default, a number or the text of one
// is itself, whole when it is whole. Anything else is not a count.
func peersOf(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return int64(defaultPeers), true
	case float64:
		if x == 0 {
			return int64(defaultPeers), true
		}
		return whole(x), true
	case string:
		text := strings.TrimSpace(x)
		if text == "" {
			return int64(defaultPeers), true
		}
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, false
		}
		return whole(f), true
	case bool:
		if !x {
			return int64(defaultPeers), true
		}
	}
	return nil, false
}

func whole(f float64) any {
	if f == float64(int64(f)) && f > -1e15 && f < 1e15 {
		return int64(f)
	}
	return f
}
