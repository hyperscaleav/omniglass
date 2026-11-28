// Package tessellatecensus is the Tessellate Census C1 (#2836), a Go device
// family of one: a ceiling-mounted stereo-vision people counter for meeting rooms.
// It reports a live head-count, an occupancy increased/decreased signal and a
// derived vacant flag.
//
// Unlike the lab's other devices its primary surface is an HTTP REST and event API
// (the data and push band), not SNMP: sensor vendors ship telemetry over REST and
// webhooks, not enterprise MIBs, so it carries no SNMP face. The API sits behind a
// header key (X-Census-Key); the dashboard is public so an operator can paste a
// key, and every /api route is gated. The live head-count (people_count) is the
// course's raw-TCP datapoint and is deliberately absent from HTTP: the only way in
// for it is the raw-TCP control band on port 5000 (COUNT? and SUBSCRIBE COUNT), and
// a binary AV-control band on port 5001 answers a fixed hex poll. What stays on
// HTTP is the derived last_event and vacant signal.
//
// The simulator has no outbound-HTTP mechanism for this device, so the webhook the
// real sensor would post to is modeled as poll: /api/events carries the latest
// event, the webhook target is stored for display, and the TCP band pushes the same
// signals to a subscriber on its own socket.
//
// The head-count is who the lab calendar says is in the device's own room, never a
// walk of its own (behave.PeopleCount); with no calendar it walks.
//
// The family replaced the tessellate-census-c1 YAML profile; its wire matched the profile's before
// the profile was deleted (#2826); the driver contract test now holds it.
package tessellatecensus

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry: the product it is
// a family of one for.
const Name = "tessellate-census-c1"

// Row is one model of the line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
}

// Rows is the model table: the Census C1, one model.
var Rows = []Row{{Product: Name, Model: "Census C1", Firmware: "1.2.0"}}

// The state paths of the sensor.
const (
	pathBootEpoch = "status.device.boot_epoch"
	pathUptime    = "status.device.uptime"
	pathCount     = "status.occupancy.people_count"
	pathPrev      = "status.occupancy.people_count_prev"
	pathEvent     = "status.occupancy.last_event"
	pathVacant    = "status.occupancy.vacant"
	pathWebhook   = "configuration.webhook.url"
	pathModel     = "configuration.device.model"
	pathSerial    = "configuration.device.serial"
	pathFirmware  = "configuration.firmware.version"
)

// The client's key for the REST API, which the dashboard's own poll sends.
const apiKey = "census-lab-key"

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			// The anchor of the uptime a read computes; 0 is unset, and the
			// engine seeds it to the device's start.
			"device": map[string]any{"boot_epoch": 0},
			"occupancy": map[string]any{
				"people_count": 0,
				// The prior tick's count, which the drift derives the
				// increased and decreased signal from.
				"people_count_prev": 0,
				// increased, decreased or steady.
				"last_event": "steady",
				"vacant":     true,
			},
			"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Tessellate"},
			"firmware": map[string]any{"version": r.Firmware},
			// The modeled webhook target: stored for display, never posted to.
			"webhook": map[string]any{"url": ""},
			"network": map[string]any{"hostname": ""},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Tessellate", Model: "Census C1", DeviceType: "people-counter",
			Summary: "Tessellate Census C1 ceiling people counter",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, APISurface: "tessellate-census",
				Doc: wire.Doc{Summary: "Census REST + event API", Tags: []string{"management", "api", "data"}},
				// The dashboard is public so an operator can paste a key into
				// the page; every /api route stays gated behind the header.
				Auth: wire.Auth{Type: "header", Header: "X-Census-Key", APIKey: apiKey, Enforce: true, PublicPaths: []string{"/"}},
			},
			// The raw-TCP control band, the closest supported push transport: a
			// room-control system dials in and subscribes, and the sensor pushes
			// #<token>=<value> lines on the same socket whenever the count or the
			// vacant flag changes.
			{
				Name: "control", Protocol: "tcp", Port: 5000, APISurface: "tessellate-census-control", Persistent: true,
				Doc:        wire.Doc{Summary: "Raw-TCP occupancy feed (CRLF-delimited ASCII, push subscriptions)", Tags: []string{"control", "persistent-session", "raw-tcp", "data"}},
				LineEnding: "\r\n", Welcome: "TESSELLATE CENSUS C1 FEED READY",
				Limits: wire.SessionLimits{MaxSubsPerSession: 64, PublishMinIntervalMs: 50, MaxSessionsPerDevice: 16, OutBuffer: 256},
			},
			// The binary AV-control band, a fixed-frame hex poll for the live
			// head-count: a request frame in, a response frame back, a poll only.
			//
			//	request   02 AA 01 03 0D   STX, magic 0xAA, opcode 0x01 (poll people_count), ETX, CR
			//	response  02 AA 81 CC 03   STX, magic, 0x81 (the opcode with the response bit), CC the count as one raw byte, ETX
			//
			// The response is fixed width, so a client reads exactly five bytes
			// instead of scanning for a delimiter (a count may be 0x03 or 0x0D).
			{
				Name: "control-hex", Protocol: "tcp", Port: 5001, APISurface: "tessellate-census-hex", Persistent: true,
				Doc:        wire.Doc{Summary: "Binary AV-control band, fixed hex poll for live head-count", Tags: []string{"control", "raw-tcp", "binary", "data"}},
				LineEnding: "\r", FrameStart: "\x02", FrameEnd: "\x03",
				// The poll is a binary frame, so the console lists it as its hex
				// bytes here (a listing only); the control Commander answers it.
				Commands: []wire.Command{{Match: "hex AA 01",
					Doc: wire.Doc{Summary: "Hex poll: people_count (request AA 01 → response AA 81 <count>)"}}},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: stateFor(Rows[0]),
		HTTP: []spec.HTTPRoute{
			{Match: "GET /", Summary: "Sensor dashboard", Handle: dashboardRoute},
			{Match: "GET /api/status", Summary: "Sensor status snapshot", Handle: statusRoute},
			{Match: "GET /api/occupancy", Summary: "Occupancy event signal + vacant flag (no head-count; that is raw-TCP only)", Handle: occupancyRoute},
			{Match: "GET /api/events", Summary: "Latest occupancy event (poll this; webhook is poll-modeled)", Handle: eventsRoute},
			httpapi.Control{
				Match: "POST /api/webhook", Summary: "Set webhook target URL (stored only; no outbound POST)", Example: "{\"url\": \"https://collector.shinra.lan/occupancy\"}\n",
				Fields: []httpapi.Field{{Path: pathWebhook, Name: "url", Kind: httpapi.Text}},
				Ack:    func(d spec.Device) any { return webhookAck{true, spec.String(d, pathWebhook), "poll-modeled"} },
			}.Route(),
			// A reboot re-anchors the uptime and puts the volatile occupancy
			// telemetry back to its defaults; a factory reset also clears the
			// webhook target.
			httpapi.Action{
				Match: "POST /api/admin/reboot", Summary: "Reboot sensor (clears uptime, resets occupancy state)", Example: "{}",
				Writes: func(d spec.Device) []surface.Change { return append(anchor(d), occupancyDefaults(d)...) },
				Ack:    actionAck{true, "reboot"},
			}.Route(),
			httpapi.Action{
				Match: "POST /api/admin/factory-reset", Summary: "Factory reset (resets every status path + webhook target)", Example: "{}",
				Writes: func(d spec.Device) []surface.Change {
					return append(append(anchor(d), occupancyDefaults(d)...), httpapi.Reset(d, pathWebhook)...)
				},
				Ack: actionAck{true, "factory-reset"},
			}.Route(),
		},
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{occupancyTick()},
		Surface: surfaceOf,
	}
}

func anchor(d spec.Device) []surface.Change {
	return []surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}
}

func occupancyDefaults(d spec.Device) []surface.Change {
	return httpapi.Reset(d, pathCount, pathPrev, pathEvent, pathVacant)
}

// The documents the API answers.
type (
	statusDoc struct {
		Device struct {
			Uptime   int64  `json:"uptime"`
			Model    string `json:"model"`
			Serial   string `json:"serial"`
			Firmware string `json:"firmware"`
		} `json:"device"`
		Occupancy signal `json:"occupancy"`
		Webhook   struct {
			URL string `json:"url"`
		} `json:"webhook"`
	}
	signal struct {
		LastEvent string `json:"last_event"`
		Vacant    bool   `json:"vacant"`
	}
	eventsDoc struct {
		Latest struct {
			Event  string `json:"event"`
			Vacant bool   `json:"vacant"`
			At     int64  `json:"at"`
		} `json:"latest"`
		Delivery      string `json:"delivery"`
		WebhookTarget string `json:"webhook_target"`
		Note          string `json:"note"`
	}
	webhookAck struct {
		OK         bool   `json:"ok"`
		WebhookURL string `json:"webhook_url"`
		Delivery   string `json:"delivery"`
	}
	actionAck struct {
		OK     bool   `json:"ok"`
		Action string `json:"action"`
	}
)

// eventsNote says where the head-count is, since the event feed carries only the
// signal.
const eventsNote = "the live head-count (people_count) is NOT exposed over HTTP, read it on the raw-TCP control band (COUNT? / SUBSCRIBE COUNT, port 5000). " +
	"This route carries the occupancy event signal + vacant flag only. Webhook target is stored but not POSTed by the simulator."

func jsonRoute(doc any) spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(doc)}
}

func signalOf(d spec.Device) signal {
	return signal{LastEvent: spec.String(d, pathEvent), Vacant: spec.Bool(d, pathVacant)}
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	var doc statusDoc
	doc.Device.Uptime, doc.Device.Model = spec.Int64(d, pathUptime), spec.String(d, pathModel)
	doc.Device.Serial, doc.Device.Firmware = spec.String(d, pathSerial), spec.String(d, pathFirmware)
	doc.Occupancy = signalOf(d)
	doc.Webhook.URL = spec.String(d, pathWebhook)
	return jsonRoute(doc)
}

func occupancyRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	return jsonRoute(signalOf(d))
}

func eventsRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	var doc eventsDoc
	doc.Latest.Event, doc.Latest.Vacant, doc.Latest.At = spec.String(d, pathEvent), spec.Bool(d, pathVacant), spec.Int64(d, pathUptime)
	doc.Delivery, doc.WebhookTarget, doc.Note = "poll", spec.String(d, pathWebhook), eventsNote
	return jsonRoute(doc)
}

// occupancyTick is the head-count: who the calendar says is in the sensor's own
// room (less the one who stepped out on some ticks), the signal derived from it
// against the tick before, and the vacant flag. One tick owns the whole tuple so
// the fields stay internally consistent.
func occupancyTick() spec.Tick {
	return spec.Tick{
		Name: "occupancy_drift", Interval: bind.Seconds(20),
		Writes: []string{pathPrev, pathCount, pathEvent, pathVacant},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			prev := behave.OccupancyState{Count: int(spec.Int64(d, pathCount))}
			next := behave.PeopleCount(prev, c.Room, 0, behave.DefaultOccupancy, c.Rand)
			return []surface.Change{
				{Path: pathPrev, Value: int64(next.Prev)},
				{Path: pathCount, Value: int64(next.Count)},
				{Path: pathEvent, Value: next.LastEvent},
				{Path: pathVacant, Value: next.Vacant},
			}
		},
	}
}

// feed is a state path the control band answers a query for and pushes.
type feed struct {
	// Query is the command that reads it, Subject the word that subscribes.
	Query, Subject, Token, Path string
}

var feeds = []feed{
	{"COUNT?", "COUNT", "COUNT", pathCount},
	{"VACANT?", "VACANT", "VACANT", pathVacant},
	{"", "EVENT", "EVENT", pathEvent},
}

// control is the raw-TCP band's commands and the binary band's poll.
type control struct{ d spec.Device }

// Commands lists the lines the bands answer, in help order.
func (control) Commands() []string {
	out := []string{"COUNT?", "VACANT?", "SUBSCRIBE COUNT", "SUBSCRIBE VACANT", "SUBSCRIBE EVENT", "UNSUBSCRIBE COUNT", "UNSUBSCRIBE VACANT"}
	return append(out, hexPoll)
}

// Summary documents the query lines as the console lists them.
func (control) Summary(line string) string {
	switch line {
	case "COUNT?":
		return "Query live people count"
	case "VACANT?":
		return "Query vacant flag"
	}
	return ""
}

// hexPoll is the binary poll's payload: the magic byte and the opcode.
const hexPoll = "\xaa\x01"

// Handle answers one line.
func (c control) Handle(line string) (surface.Reply, bool) {
	if line == hexPoll {
		// The count as one raw byte, its low eight bits.
		return surface.Reply{Text: "\xaa\x81" + string([]byte{byte(spec.Int64(c.d, pathCount))})}, true
	}
	for _, f := range feeds {
		switch line {
		case f.Query:
			if f.Query != "" {
				return surface.Reply{Text: fmt.Sprintf("#%s=%s\n", f.Token, spec.String(c.d, f.Path))}, true
			}
		case "SUBSCRIBE " + f.Subject:
			return surface.Reply{Text: "#ACK SUBSCRIBE " + f.Subject, Action: surface.ActionSubscribe, Subject: f.Path, Token: f.Token}, true
		case "UNSUBSCRIBE " + f.Subject:
			// The event feed cannot be unsubscribed from, as it has never
			// been: only the count and the vacant flag can.
			if f.Subject != "EVENT" {
				return surface.Reply{Text: "#ACK UNSUBSCRIBE " + f.Subject, Action: surface.ActionUnsubscribe, Subject: f.Path}, true
			}
		}
	}
	return surface.Reply{}, false
}

// surfaceOf is the control band: its commands, and the subjects a session may
// subscribe to, each pushing "#<token>=<value>" with the value as JSON.
func surfaceOf(d spec.Device) spec.Surface {
	subjects := make([]surface.Subject, len(feeds))
	for i, f := range feeds {
		token := f.Token
		subjects[i] = surface.Subject{Match: f.Path, Path: f.Path, Token: token,
			Push: func(v any) string { return "#" + token + "=" + surface.Encoded(v) }}
	}
	return spec.Surface{Commanders: []surface.Commander{control{d}}, Subjects: map[string][]surface.Subject{"control": subjects}, ListedOn: "control"}
}
