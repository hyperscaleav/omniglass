//go:build linux

package sim

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The UCC platform's wire script (#2836): the account's admin API behind its
// token, its paged room and device lists, every route that moves a room, the
// signed webhook posts those routes and the calendar send, and the account's
// day over a world of rooms.

const uccProduct = "ucc-platform"

func uccScript() []wireStep {
	return []wireStep{
		wireGet("the dashboard", "/"),
		{Name: "the dashboard needs no token", Method: "GET", Path: "/", NoKey: true},
		wireGet("the room list", "/api/rooms"),
		{Name: "the room list with no token", Method: "GET", Path: "/api/rooms", NoKey: true},
		wireGet("rooms in pages of two", "/api/rooms?page_size=2"),
		wireGet("the second page", "/api/rooms?page_size=2&next_page_token=zr-boardroom-a"),
		wireGet("the last page", "/api/rooms?page_size=2&next_page_token=zr-briefing-av"),
		wireGet("past the last page", "/api/rooms?page_size=2&next_page_token=zr-huddle-north"),
		wireGet("a size that is no number", "/api/rooms?page_size=many"),
		wireGet("a size past the most", "/api/rooms?page_size=9999"),
		wireGet("a token nobody issued", "/api/rooms?next_page_token=zr-bo&page_size=1"),
		wireGet("the device list", "/api/devices"),
		{Name: "the device list with no token", Method: "GET", Path: "/api/devices", NoKey: true},
		wireGet("devices in pages of one", "/api/devices?page_size=1"),
		wireGet("the second device", "/api/devices?page_size=1&next_page_token=zd-annex-codec"),
		wireGet("past the last device", "/api/devices?page_size=1&next_page_token=zd-spare-bar"),
		wireGet("a device size that is no number", "/api/devices?page_size=x"),
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the wrong method on the rooms", Method: "DELETE", Path: "/api/rooms"},
		{Name: "a head request on the rooms", Method: "HEAD", Path: "/api/rooms"},
		wirePost("a room joins the account", "/api/rooms", `{"id": "hq-conf-1"}`),
		wirePost("a room the account knows keeps its call", "/api/rooms", `{"id": "zr-huddle-north"}`),
		wirePost("a meeting starts", "/api/rooms/meetings/start", `{"room": "zr-boardroom-a", "id": "m-2001", "participants": 4, "topic": "Weekly sync"}`),
		wirePost("a meeting starts with the defaults", "/api/rooms/meetings/start", `{"room": "zr-briefing-av", "id": "m-2002"}`),
		wirePost("a meeting starts in a room nobody has seen", "/api/rooms/meetings/start", `{"room": "zr-annex-2", "id": "m-3001", "participants": 5}`),
		wirePost("a meeting starts with the largest count", "/api/rooms/meetings/start", `{"room": "zr-auditorium-av", "id": "m-2003", "participants": 999, "topic": "Town hall, all hands."}`),
		wirePost("a meeting starts with a count as text", "/api/rooms/meetings/start", `{"room": "hq-conf-1", "id": "m-2004", "participants": "12"}`),
		wirePost("a participant joins a room nobody has seen", "/api/rooms/participants/join", `{"room": "zr-joined-only", "participants": 2}`),
		wireGet("the room list in meetings", "/api/rooms"),
		wireGet("the dashboard in meetings", "/"),
		wirePost("a participant joins", "/api/rooms/participants/join", `{"room": "zr-boardroom-a", "participants": 6}`),
		wirePost("a participant joins by default", "/api/rooms/participants/join", `{"room": "zr-briefing-av"}`),
		wirePost("a participant leaves", "/api/rooms/participants/leave", `{"room": "zr-boardroom-a", "participants": 2}`),
		wirePost("a participant leaves by default", "/api/rooms/participants/leave", `{"room": "zr-briefing-av"}`),
		wirePost("a meeting ends", "/api/rooms/meetings/end", `{"room": "zr-huddle-north"}`),
		wirePost("a meeting ends in a room nobody has seen", "/api/rooms/meetings/end", `{"room": "zr-nowhere"}`),
		wireGet("the room list after the ends", "/api/rooms?page_size=300"),
		wirePost("a device goes offline", "/api/devices/status", `{"device": "zd-spare-bar", "room": "zr-boardroom-b", "status": "offline"}`),
		wirePost("a device comes online", "/api/devices/status", `{"device": "zd-spare-bar", "room": "zr-boardroom-b", "status": "online"}`),
		wirePost("a device nobody has seen reports", "/api/devices/status", `{"device": "zd-new-cam", "room": "zr-annex-2", "status": "online"}`),
		wireGet("the device list after the changes", "/api/devices"),
		wirePost("the batched report", "/api/rooms/report", `{}`),
		wirePost("the batched report with no body", "/api/rooms/report", ``),
		wirePost("a subscription", "/api/webhooks", `{"url": "http://127.0.0.1:1/ucc", "scheme": "hmac", "secret": "ucc-lab-signing-key"}`),
		wirePost("a subscription with the defaults", "/api/webhooks", `{"url": "http://127.0.0.1:1/ucc-b"}`),
		wirePost("a token subscription", "/api/webhooks", `{"url": "https://hooks.example/ucc?x=1", "scheme": "token", "secret": "abc"}`),
		wirePost("a basic subscription", "/api/webhooks", `{"url": "https://hooks.example/ucc", "scheme": "basic", "secret": "user:pass"}`),
		wirePost("an unsigned subscription", "/api/webhooks", `{"url": "https://hooks.example/ucc", "scheme": "none"}`),
		wirePost("a bad signature fault", "/api/webhooks", `{"url": "http://127.0.0.1:1/ucc", "scheme": "hmac", "secret": "k", "fault": "bad-signature"}`),
		wirePost("a stale fault", "/api/webhooks", `{"url": "http://127.0.0.1:1/ucc", "scheme": "hmac", "secret": "k", "fault": "stale"}`),
		wirePost("a redeliver fault", "/api/webhooks", `{"url": "http://127.0.0.1:1/ucc", "scheme": "hmac", "secret": "k", "fault": "redeliver"}`),
		wirePost("a subscription cleared", "/api/webhooks", `{"url": ""}`),
		wirePost("a subscription with no body", "/api/webhooks", ``),
		wirePost("a subscription with an empty object", "/api/webhooks", `{}`),
		wirePost("a subscription with every field wrong", "/api/webhooks", `{"url": "ftp://x/y", "scheme": "bogus", "fault": "wrong"}`),
		wirePost("a subscription with a url that is a number", "/api/webhooks", `{"url": 5}`),
		wirePost("a subscription with a scheme that is a number", "/api/webhooks", `{"url": "http://127.0.0.1:1/x", "scheme": 5}`),
		wirePost("a subscription with a fault that is null", "/api/webhooks", `{"url": "http://127.0.0.1:1/x", "fault": null}`),
		// The requests the profile answered with an accident of its templates
		// come last, so the state they left does not carry into the rest.
		{Name: "an unknown scheme", Method: "POST", Path: "/api/webhooks", Body: `{"url": "http://127.0.0.1:1/x", "scheme": "bogus"}`, Quirk: "the mutation pipeline wrote the url and the secret and refused the scheme"},
		{Name: "an unknown fault", Method: "POST", Path: "/api/webhooks", Body: `{"url": "http://127.0.0.1:1/x", "fault": "wrong"}`, Quirk: "the mutation pipeline wrote the url and refused the fault"},
		{Name: "a url that is not http", Method: "POST", Path: "/api/webhooks", Body: `{"url": "ftp://x/y"}`, Quirk: "the mutation pipeline refused the url and wrote the rest"},
		{Name: "a url with a space", Method: "POST", Path: "/api/webhooks", Body: `{"url": "http://x/ y"}`, Quirk: "the mutation pipeline refused the url and wrote the rest"},
		{Name: "a room id with a dot", Method: "POST", Path: "/api/rooms", Body: `{"id": "a.b"}`, Quirk: "the template engine refused the path segment"},
		{Name: "a room id that is empty", Method: "POST", Path: "/api/rooms", Body: `{"id": ""}`, Quirk: "the template engine refused the path segment"},
		{Name: "a room with no id", Method: "POST", Path: "/api/rooms", Body: `{}`, Quirk: "the template engine refused the path segment"},
		{Name: "a meeting with an id that has a space", Method: "POST", Path: "/api/rooms/meetings/start", Body: `{"room": "zr-boardroom-a", "id": "m 1", "participants": 2}`, Quirk: "the mutation pipeline wrote the fields that passed and left the room idle"},
		{Name: "a meeting with a count of four digits", Method: "POST", Path: "/api/rooms/meetings/start", Body: `{"room": "zr-boardroom-a", "id": "m-9", "participants": 1000}`, Quirk: "the mutation pipeline wrote the fields that passed and left the room idle"},
		{Name: "a meeting with a negative count", Method: "POST", Path: "/api/rooms/meetings/start", Body: `{"room": "zr-boardroom-a", "id": "m-9", "participants": -1}`, Quirk: "the mutation pipeline wrote the fields that passed and left the room idle"},
		{Name: "a meeting with a topic of markup", Method: "POST", Path: "/api/rooms/meetings/start", Body: `{"room": "zr-boardroom-a", "id": "m-9", "topic": "<b>x</b>"}`, Quirk: "the mutation pipeline wrote the fields that passed and left the room idle"},
		{Name: "a meeting with no room", Method: "POST", Path: "/api/rooms/meetings/start", Body: `{"id": "m-9"}`, Quirk: "the template engine refused the path segment"},
		{Name: "a meeting with a room of two segments", Method: "POST", Path: "/api/rooms/meetings/start", Body: `{"room": "a.b", "id": "m-9"}`, Quirk: "the template engine refused the path segment"},
		{Name: "an end with no room", Method: "POST", Path: "/api/rooms/meetings/end", Body: `{}`, Quirk: "the template engine refused the path segment"},
		{Name: "a join with a count of four digits", Method: "POST", Path: "/api/rooms/participants/join", Body: `{"room": "zr-boardroom-a", "participants": 1000}`, Quirk: "the mutation pipeline refused the count"},
		{Name: "a leave with a count that is a word", Method: "POST", Path: "/api/rooms/participants/leave", Body: `{"room": "zr-boardroom-a", "participants": "many"}`, Quirk: "the mutation pipeline refused the count"},
		{Name: "a device with an unknown status", Method: "POST", Path: "/api/devices/status", Body: `{"device": "zd-spare-bar", "room": "zr-boardroom-b", "status": "asleep"}`, Quirk: "the mutation pipeline stamped the device and refused the status"},
		{Name: "a device with no id", Method: "POST", Path: "/api/devices/status", Body: `{"status": "online"}`, Quirk: "the template engine refused the path segment"},
	}
}

func uccTicks() []tickPlan {
	return []tickPlan{{"rooms_calendar", []string{"status.rooms", "status.sim.told", "status.sim.announce"}}}
}

var uccWatch = []string{"status.rooms", "status.sim.told", "status.sim.announce"}

// uccWorld is the account's world: three rooms the account covers, each with a
// codec, a bar and a controller the platform lists and a display it does not,
// and a codec in the second room that a scripted problem unplugs each morning.
func uccWireWorld() (*world.World, *calendar.Calendar) {
	w := &world.World{Version: world.Version, CalendarSeed: 24, Meetings: map[string]calendar.Knobs{"conf": parityKnobs()}}
	site := world.Site{Name: "hq"}
	svc := world.Service{Name: "d-1", Profile: "ucc-platform", Webhook: true}
	for _, n := range []string{"r1", "r2", "r3"} {
		sys := "hq-" + n
		label := map[string]string{"r1": "Curie", "r2": "Hopper", "r3": "Huddle 3"}[n]
		r := world.Room{Name: n, Label: label, Building: "tower", Standard: "conf", System: sys}
		for _, dv := range []struct{ slot, platform, fw string }{
			{"video-bar", "bar", "2.7.3"}, {"codec", "codec", "2.7.3"}, {"touch-control", "controller", "4.1.2"}, {"main-display", "", "9.9"},
		} {
			name := sys + "-" + dv.slot + "-1"
			r.Devices = append(r.Devices, world.Device{Name: name, Slot: dv.slot, ID: "dev-" + name, Product: dv.slot, Platform: dv.platform, Firmware: dv.fw})
		}
		site.Rooms = append(site.Rooms, r)
		svc.Covers = append(svc.Covers, world.Cover{System: sys, Site: "hq", Building: "tower", Room: n, Label: label})
	}
	w.Sites, w.Services = []world.Site{site}, []world.Service{svc}
	w.Problems = []calendar.ProblemSpec{{Name: "codec-unplugged", Cadence: "daily", At: "09:30", For: 40 * time.Minute,
		Scope: calendar.Scope{Rooms: []string{"hq-r2"}, Role: "codec"}, Impairment: calendar.Impairment{Unplugged: true}}}
	return w, w.Calendar()
}

func uccRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, uccWatch...)
	w.sample("start")
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 15*time.Second, func(string) { d.TickBehavior("rooms_calendar") }, func(string) {})
	return w.out
}

// uccWireRecv is a receiver as the platform signs for: the headers its posts
// carry the signature, timestamp and delivery id under.
func uccWireRecv(t *testing.T) *wireRecv {
	r := newWireRecv(t)
	r.Secret, r.Signature, r.Prefix, r.Timestamp, r.Delivery = "ucc-wire-key", "X-UCC-Signature", "sha256=", "X-UCC-Timestamp", "X-UCC-Delivery"
	return r
}

// uccPosts is every post the account makes while its routes run: each step names
// how many posts it answers with, and the posts are recorded with what they were
// signed as.
func uccPosts(t *testing.T, fam wireFamily, product string) []map[string]any {
	d := wireDevice(t, fam, product)
	recv := uccWireRecv(t)
	var out []map[string]any
	run := func(name, path, body string, posts int) {
		res := wireRequest(t, d, wireStep{Name: name, Method: "POST", Path: path, Body: body})
		got := recv.take(t, posts, true)
		// A refusal's words are the HTTP script's to hold; here it is only that it
		// was refused and posted nothing.
		response := res.Response
		if res.Status >= 400 {
			response = ""
		}
		out = append(out, map[string]any{"step": name, "status": res.Status, "response": response, "posts": got})
	}
	subscribe := func(name, scheme, secret, fault string) {
		b, _ := json.Marshal(map[string]string{"url": recv.srv.URL + "/hook?token=t0k", "scheme": scheme, "secret": secret, "fault": fault})
		run(name, "/api/webhooks", string(b), 0)
	}
	subscribe("an hmac subscription", "hmac", "ucc-wire-key", "")
	run("a meeting starts", "/api/rooms/meetings/start", `{"room": "zr-annex-2", "id": "m-3001", "participants": 5, "topic": "Planning"}`, 1)
	run("a meeting starts in a known room", "/api/rooms/meetings/start", `{"room": "zr-boardroom-a", "id": "m-3002"}`, 1)
	run("a meeting is refused", "/api/rooms/meetings/start", `{"room": "zr-boardroom-a", "id": "bad id", "participants": 2}`, 0)
	run("a participant joins", "/api/rooms/participants/join", `{"room": "zr-annex-2", "participants": 6}`, 1)
	run("a participant leaves", "/api/rooms/participants/leave", `{"room": "zr-annex-2"}`, 1)
	run("a meeting ends", "/api/rooms/meetings/end", `{"room": "zr-annex-2"}`, 1)
	run("a device changes", "/api/devices/status", `{"device": "zd-spare-bar", "room": "zr-boardroom-b", "status": "offline"}`, 1)
	run("a device change is refused", "/api/devices/status", `{"device": "zd-spare-bar", "room": "zr-boardroom-b", "status": "asleep"}`, 0)
	run("the batched report", "/api/rooms/report", `{}`, 1)
	run("a room joins and is silent", "/api/rooms", `{"id": "hq-conf-9"}`, 0)
	run("the report names the new room", "/api/rooms/report", `{}`, 1)
	recv.Signature = "" // a token and a basic login are read as they were sent
	subscribe("a token subscription", "token", "tok-secret", "")
	run("a post signed with a token", "/api/rooms/meetings/end", `{"room": "zr-boardroom-a"}`, 1)
	subscribe("a basic subscription", "basic", "user:pass", "")
	run("a post signed with a login", "/api/rooms/meetings/end", `{"room": "zr-boardroom-a"}`, 1)
	subscribe("an unsigned subscription", "none", "", "")
	run("an unsigned post", "/api/rooms/meetings/end", `{"room": "zr-boardroom-a"}`, 1)
	recv.Signature = "X-UCC-Signature"
	subscribe("a bad signature subscription", "hmac", "ucc-wire-key", "bad-signature")
	run("a post signed wrongly", "/api/rooms/meetings/end", `{"room": "zr-boardroom-a"}`, 1)
	subscribe("a stale subscription", "hmac", "ucc-wire-key", "stale")
	run("a post signed long ago", "/api/rooms/meetings/end", `{"room": "zr-boardroom-a"}`, 1)
	subscribe("a redelivering subscription", "hmac", "ucc-wire-key", "redeliver")
	run("a post sent twice", "/api/rooms/meetings/end", `{"room": "zr-boardroom-a"}`, 2)
	subscribe("a subscription with no secret", "hmac", "", "")
	run("a post signed with an empty key", "/api/rooms/meetings/end", `{"room": "zr-boardroom-a"}`, 1)
	return out
}

// uccWorldDay walks the account over the world's rooms through the parity day:
// what its rooms read each tick, and each meeting boundary it posts, in order.
func uccWorldDay(t *testing.T, fam wireFamily, product string) []wireChange {
	w, wcal := uccWireWorld()
	d, at := serviceDevice(t, fam, product, w, wcal)
	recv := uccWireRecv(t)
	s := d.State()
	_ = SetPath(s, "configuration.webhook.target_url", recv.srv.URL+"/hook")
	_ = SetPath(s, "configuration.webhook.secret", "ucc-wire-key")
	d.SetState(s, d.Version+1)
	ch := newWatch(d, uccWatch...)
	ch.sample("start")
	roomDayWalk(d, at, ch, "07:00:00", "15:00:00", 15*time.Second, func(hms string) {
		d.TickBehavior("rooms_calendar")
		if a, _ := d.ReadPath("status.sim.announce.kind").(string); a != "" {
			for _, p := range recv.take(t, 1, false) {
				ch.out = append(ch.out, wireChange{At: hms, Path: "post", Value: p})
			}
		}
	}, func(string) {})
	if stray := recv.take(t, 0, false); len(stray) > 0 {
		t.Errorf("the day left %d posts unaccounted for", len(stray))
	}
	return ch.out
}

func uccExtra(t *testing.T, fam wireFamily, product string) map[string]any {
	w, wcal := uccWireWorld()
	d, at := serviceDevice(t, fam, product, w, wcal)
	*at = parityAt("10:05:00")
	d.TickBehavior("rooms_calendar")
	rooms := runWireHTTP(t, d, []wireStep{
		wireGet("the room list", "/api/rooms"),
		wireGet("rooms in pages of two", "/api/rooms?page_size=2"),
		wireGet("the second page", "/api/rooms?page_size=2&next_page_token=hq-r2"),
		wireGet("the device list", "/api/devices"),
		wireGet("devices in pages of four", "/api/devices?page_size=4"),
		wireGet("the second page of devices", "/api/devices?page_size=4&next_page_token=dev-hq-r1-video-bar-1"),
		wireGet("a device page past the end", "/api/devices?page_size=4&next_page_token=zzz"),
		wireGet("the dashboard", "/"),
	})
	*at = parityAt("09:45:00")
	d.TickBehavior("rooms_calendar")
	during := runWireHTTP(t, d, []wireStep{
		wireGet("the device list with a codec unplugged", "/api/devices?page_size=300"),
		wireGet("the room list with a codec unplugged", "/api/rooms"),
	})
	rep := wireDevice(t, fam, product)
	recv := uccWireRecv(t)
	s := rep.State()
	_ = SetPath(s, "configuration.webhook.target_url", recv.srv.URL+"/hook")
	_ = SetPath(s, "configuration.webhook.secret", "ucc-wire-key")
	rep.SetState(s, rep.Version+1)
	rep.TickBehavior("rooms_report")
	report := recv.take(t, 1, false)
	return map[string]any{
		"rooms": rooms, "unplugged": during, "posts": uccPosts(t, fam, product), "report": report,
		"room_day_world": uccWorldDay(t, fam, product),
	}
}

var uccWire = wireFamily{
	Name: "ucc-platform", Products: []string{uccProduct}, Engine: shippedEngineOf, Source: wireYAMLSource, Frozen: wireYAMLFrozen,
	Script: uccScript(), Ticks: uccTicks(), RoomDay: uccRoomDay, Extra: uccExtra,
}

func init() { wireFamilies = append(wireFamilies, uccWire) }
