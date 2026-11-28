//go:build linux

package sim

import (
	"strconv"
	"testing"
	"time"
)

// The Tessellate Census C1's wire script (#2836): the REST and event API behind
// its key, the raw-TCP feed with its push subscriptions, the binary hex poll,
// the drift of its head-count, and its pinned day in a room with and without
// the lab calendar.

const censusProduct = "tessellate-census-c1"

func censusScript() []wireStep {
	return []wireStep{
		wireGet("the dashboard", "/"),
		{Name: "the dashboard needs no key", Method: "GET", Path: "/", NoKey: true},
		wireGet("the status snapshot", "/api/status"),
		{Name: "the status with no key", Method: "GET", Path: "/api/status", NoKey: true},
		wireGet("the occupancy signal", "/api/occupancy"),
		wireGet("the event feed", "/api/events"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "an unknown path with no key", Method: "GET", Path: "/nope", NoKey: true},
		{Name: "the wrong method on the webhook", Method: "GET", Path: "/api/webhook"},
		{Name: "the wrong method on the status", Method: "POST", Path: "/api/status", Body: `{}`},
		{Name: "a head request on the root", Method: "HEAD", Path: "/"},
		wirePost("the webhook target set", "/api/webhook", `{"url": "https://collector.shinra.lan/occupancy"}`),
		wireGet("the dashboard with a target", "/"),
		wireGet("the status with a target", "/api/status"),
		wireGet("the event feed with a target", "/api/events"),
		wirePost("the webhook target cleared", "/api/webhook", `{"url": ""}`),
		wirePost("the webhook target set again", "/api/webhook", `{"url": "https://collector.shinra.lan/census"}`),
		wirePost("reboot", "/api/admin/reboot", `{}`),
		wireGet("the status after a reboot", "/api/status"),
		wirePost("reboot with no body", "/api/admin/reboot", ``),
		wirePost("factory reset", "/api/admin/factory-reset", `{}`),
		wireGet("the status after a factory reset", "/api/status"),
		wireGet("the dashboard after a factory reset", "/"),
		{Name: "reboot with no key", Method: "POST", Path: "/api/admin/reboot", Body: `{}`, NoKey: true},
		// The requests the profile answered with an accident of its templates
		// come last, so the state they left does not carry into the rest.
		{Name: "the webhook target missing", Method: "POST", Path: "/api/webhook", Body: `{}`, Snapshot: true, Quirk: "the template wrote <no value> into the target"},
		{Name: "the webhook target a number", Method: "POST", Path: "/api/webhook", Body: `{"url": 5}`, Snapshot: true, Quirk: "the template wrote the number into the target as text"},
		{Name: "the webhook target a boolean", Method: "POST", Path: "/api/webhook", Body: `{"url": true}`, Snapshot: true, Quirk: "the template wrote the boolean into the target as text"},
	}
}

func censusTicks() []tickPlan {
	return []tickPlan{{"occupancy_drift", []string{
		"status.occupancy.people_count", "status.occupancy.people_count_prev", "status.occupancy.last_event", "status.occupancy.vacant"}}}
}

func censusRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, "status.occupancy.people_count", "status.occupancy.people_count_prev", "status.occupancy.last_event", "status.occupancy.vacant")
	w.sample("start")
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 20*time.Second, func(string) { d.TickBehavior("occupancy_drift") }, func(string) {})
	return w.out
}

// censusExtra records the raw-TCP feed and the binary band: what each says on
// connect, how it answers each command, and what it pushes when the count moves.
func censusExtra(t *testing.T, fam wireFamily, product string) map[string]any {
	d := wireDevice(t, fam, product)
	count := func(n int, event string, vacant bool) map[string]any {
		return map[string]any{"status.occupancy.people_count": n, "status.occupancy.last_event": event, "status.occupancy.vacant": vacant}
	}
	feed := wireConverse(t, d, "control", []wireTalk{
		{Name: "count", Send: "COUNT?\r\n"},
		{Name: "vacant", Send: "VACANT?\r\n"},
		{Name: "a command nobody answers", Send: "BOGUS\r\n"},
		{Name: "a command of the wrong case", Send: "count?\r\n"},
		{Name: "a command with trailing space", Send: "COUNT? \r\n"},
		{Name: "subscribe to the count", Send: "SUBSCRIBE COUNT\r\n"},
		{Name: "the count moves and is pushed", Set: count(4, "increased", false)},
		{Name: "the count moves again", Set: count(2, "decreased", false)},
		{Name: "count after the moves", Send: "COUNT?\r\n"},
		{Name: "subscribe to vacant", Send: "SUBSCRIBE VACANT\r\n"},
		{Name: "subscribe to the event", Send: "SUBSCRIBE EVENT\r\n"},
		// Subscriptions of one session are kept in a map, so the lines a batch of
		// changes pushes arrive in no fixed order: each path moves on its own.
		{Name: "the room empties: the count", Set: map[string]any{"status.occupancy.people_count": 0}},
		{Name: "the room empties: vacant", Set: map[string]any{"status.occupancy.vacant": true}},
		{Name: "the room empties: the event", Set: map[string]any{"status.occupancy.last_event": "decreased"}},
		{Name: "vacant after the room empties", Send: "VACANT?\r\n"},
		{Name: "unsubscribe from the count", Send: "UNSUBSCRIBE COUNT\r\n"},
		{Name: "the count moves unheard", Set: map[string]any{"status.occupancy.people_count": 7}},
		{Name: "vacant moves heard", Set: map[string]any{"status.occupancy.vacant": false}},
		{Name: "the event moves heard", Set: map[string]any{"status.occupancy.last_event": "increased"}},
		{Name: "unsubscribe from vacant", Send: "UNSUBSCRIBE VACANT\r\n"},
		{Name: "unsubscribe from the event", Send: "UNSUBSCRIBE EVENT\r\n"},
		{Name: "subscribe to a subject nobody has", Send: "SUBSCRIBE BOGUS\r\n"},
		{Name: "subscribe to the count again", Send: "SUBSCRIBE COUNT\r\n"},
		{Name: "subscribe to the count a second time", Send: "SUBSCRIBE COUNT\r\n"},
		{Name: "the count moves to a fraction", Set: map[string]any{"status.occupancy.people_count": 3}},
	})
	hexDev := wireDevice(t, fam, product)
	poll := func(n int) wireTalk {
		return wireTalk{Name: "poll with the count at " + strconv.Itoa(n), Set: map[string]any{"status.occupancy.people_count": n}, Send: "\x02\xaa\x01\x03\r"}
	}
	hex := wireConverse(t, hexDev, "control-hex", []wireTalk{
		poll(0), poll(1), poll(3), poll(13), poll(14), poll(255), poll(256),
		{Name: "a poll of another opcode", Send: "\x02\xaa\x02\x03\r"},
		{Name: "a poll of another magic", Send: "\x02\xab\x01\x03\r"},
		{Name: "an unframed poll", Send: "\xaa\x01\r"},
		{Name: "a text command on the binary band", Send: "\x02COUNT?\x03\r"},
	})
	return map[string]any{"feed": feed, "hex": hex}
}

var censusWire = wireFamily{
	Name: "tessellate-census-c1", Products: []string{censusProduct}, Engine: shippedEngineOf, Source: wireYAMLSource, Frozen: wireYAMLFrozen,
	Script: censusScript(), Ticks: censusTicks(), RoomDay: censusRoomDay, Extra: censusExtra,
}

func init() { wireFamilies = append(wireFamilies, censusWire) }
