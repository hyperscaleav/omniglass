package uccplatform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func route(t *testing.T, match string) spec.HTTPRoute {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r
		}
	}
	t.Fatalf("no route %s", match)
	return spec.HTTPRoute{}
}

func call(t *testing.T, f *spectest.Fake, match, body string) spec.HTTPResponse {
	t.Helper()
	var parsed map[string]any
	_ = json.Unmarshal([]byte(body), &parsed)
	return route(t, match).Handle(f, spec.HTTPRequest{Body: parsed, Raw: []byte(body)})
}

func subscribed(t *testing.T) *spectest.Fake {
	t.Helper()
	f := spectest.NewFake(Spec(), Name)
	if res := call(t, f, "POST /api/webhooks", `{"url": "https://hooks.example/ucc", "scheme": "hmac", "secret": "k"}`); res.Status != 0 {
		t.Fatalf("subscribe: %d %s", res.Status, res.Body)
	}
	return f
}

// The account posts nothing before it is subscribed; once it is, each post carries
// the event in a header and is signed as the subscription says.
func TestPostsWaitForASubscriptionAndAreSignedAsItSays(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	call(t, f, "POST /api/rooms/meetings/start", `{"room": "r1", "id": "m-1"}`)
	if len(f.Posts) != 0 {
		t.Fatalf("an unsubscribed account posted %+v", f.Posts)
	}
	f = subscribed(t)
	call(t, f, "POST /api/rooms/meetings/start", `{"room": "r1", "id": "m-1", "participants": 4, "topic": "Sync"}`)
	if len(f.Posts) != 1 {
		t.Fatalf("posts = %+v", f.Posts)
	}
	p := f.Posts[0]
	want := `{"event":"room.meeting.started","room_id":"r1","meeting":"m-1","ts":` + "1793707200" + `}`
	if string(p.Body) != want || p.URL != "https://hooks.example/ucc" || p.Headers["X-UCC-Event"] != "room.meeting.started" {
		t.Errorf("post = %s %s %v", p.URL, p.Body, p.Headers)
	}
	if s := p.Sign; s == nil || s.Scheme != "hmac" || s.Secret != "k" || s.Header != "X-UCC-Signature" || s.Prefix != "sha256=" || s.Timestamp != "X-UCC-Timestamp" || s.Delivery != "X-UCC-Delivery" {
		t.Errorf("signing = %+v", s)
	}
}

// A subscription is checked whole before any of it is written, and a refusal names
// every field wrong, in the engine's own words.
func TestASubscriptionIsCheckedWholeBeforeItIsWritten(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	res := call(t, f, "POST /api/webhooks", `{"url": "ftp://x/y", "scheme": "bogus", "fault": "wrong"}`)
	body := string(res.Body.([]byte))
	for _, want := range []string{`"path":"configuration.webhook.target_url"`, `"path":"configuration.webhook.scheme"`, `"path":"configuration.webhook.fault"`, `"enum":["hmac","token","basic","none"]`} {
		if res.Status != 400 || !strings.Contains(body, want) {
			t.Errorf("status %d body %s lacks %s", res.Status, body, want)
		}
	}
	if len(f.Writes) != 0 {
		t.Errorf("a refused subscription wrote %v", f.Writes)
	}
	for _, ok := range []string{`{"url": ""}`, `{"url": "http://x/y", "scheme": "none"}`, `{"url": "https://h/x", "fault": "redeliver", "secret": "s"}`} {
		if res := call(t, f, "POST /api/webhooks", ok); res.Status != 0 {
			t.Errorf("%s refused: %s", ok, res.Body)
		}
	}
	if res := call(t, f, "POST /api/webhooks", `{}`); res.Status != 400 {
		t.Errorf("a subscription with no url: status %d, want a refusal", res.Status)
	}
}

// A start is checked whole: a refused topic, id or count writes no field and leaves
// the room reading as it did, and a topic alone is refused in the engine's validation
// words.
func TestAStartIsCheckedWholeAndLeavesTheRoomAsItWas(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"a topic of markup", `{"room": "r1", "id": "m-1", "topic": "<b>x</b>"}`, `{"error":"validation failed","fields":[{"path":"status.rooms.r1.topic","value":"\u003cb\u003ex\u003c/b\u003e","pattern":"^[A-Za-z0-9 .,'-]{0,64}$"}]}`},
		{"an id with a space", `{"room": "r1", "id": "m 1"}`, `status.rooms.r1.meeting`},
		{"a count of four digits", `{"room": "r1", "id": "m-1", "participants": 1000}`, `status.rooms.r1.participants`},
		{"a negative count", `{"room": "r1", "id": "m-1", "participants": -1}`, `"value":"-1"`},
		{"no id", `{"room": "r1"}`, `\u003cno value\u003e`},
		{"a room with a dot", `{"room": "a.b", "id": "m-1"}`, `state path segment`},
		{"no room", `{"id": "m-1"}`, `state path segment`},
		{"an id that is a number", `{"room": "r1", "id": 5}`, `id must be text`},
	} {
		f := subscribed(t)
		before := len(f.Writes)
		res := call(t, f, "POST /api/rooms/meetings/start", tc.body)
		if res.Status != 400 || !strings.Contains(string(res.Body.([]byte)), tc.want) || len(f.Writes) != before || len(f.Posts) != 0 {
			t.Errorf("%s: status %d, body %s, %d writes, %d posts; want a 400 naming %s that changes and posts nothing", tc.name, res.Status, res.Body, len(f.Writes)-before, len(f.Posts), tc.want)
		}
	}
	f := subscribed(t)
	call(t, f, "POST /api/rooms/meetings/start", `{"room": "r1", "id": "m-1"}`)
	if f.Read("status.rooms.r1.participants") != int64(1) || f.Read("status.rooms.r1.state") != "in-call" || f.Read("status.rooms.r1.topic") != "" {
		t.Errorf("a start with the defaults left %v %v %q", f.Read("status.rooms.r1.participants"), f.Read("status.rooms.r1.state"), f.Read("status.rooms.r1.topic"))
	}
}

// Ending clears what the meeting held; joining and leaving set the headcount after,
// a join defaulting to one and a leave to none; a device's status is stamped and posted.
func TestEndJoinLeaveAndDeviceStatus(t *testing.T) {
	f := subscribed(t)
	call(t, f, "POST /api/rooms/meetings/start", `{"room": "r1", "id": "m-1", "participants": 4, "topic": "Sync"}`)
	call(t, f, "POST /api/rooms/participants/join", `{"room": "r1"}`)
	if f.Read("status.rooms.r1.participants") != int64(1) {
		t.Errorf("a join with no count left %v, want one", f.Read("status.rooms.r1.participants"))
	}
	call(t, f, "POST /api/rooms/participants/leave", `{"room": "r1"}`)
	if f.Read("status.rooms.r1.participants") != int64(0) {
		t.Errorf("a leave with no count left %v, want none", f.Read("status.rooms.r1.participants"))
	}
	call(t, f, "POST /api/rooms/meetings/end", `{"room": "r1"}`)
	for path, want := range map[string]any{"state": "idle", "meeting": "", "participants": int64(0), "topic": "", "start": int64(0)} {
		if got := f.Read("status.rooms.r1." + path); got != want {
			t.Errorf("after an end %s = %v, want %v", path, got, want)
		}
	}
	call(t, f, "POST /api/devices/status", `{"device": "zd-spare-bar", "room": "zr-boardroom-b", "status": "offline"}`)
	if f.Read("status.devices.zd-spare-bar.status") != "offline" || f.Read("status.devices.zd-spare-bar.last_seen") != f.Clock.Unix() {
		t.Errorf("device = %v seen %v", f.Read("status.devices.zd-spare-bar.status"), f.Read("status.devices.zd-spare-bar.last_seen"))
	}
	bodies := []string{}
	for _, p := range f.Posts {
		bodies = append(bodies, string(p.Body))
	}
	got := strings.Join(bodies, "\n")
	for _, want := range []string{`"event":"room.participant.joined","room_id":"r1","participants":1`, `"event":"room.participant.left","room_id":"r1","participants":0`, `"event":"room.meeting.ended","room_id":"r1","ts"`, `"event":"device.status.changed","room_id":"zr-boardroom-b","device_id":"zd-spare-bar","status":"offline"`} {
		if !strings.Contains(got, want) {
			t.Errorf("no post carries %s in\n%s", want, got)
		}
	}
	if res := call(t, f, "POST /api/devices/status", `{"device": "zd-spare-bar", "status": "asleep"}`); res.Status != 400 {
		t.Errorf("an unknown device status: %d", res.Status)
	}
}

// A room is added idle, a room the account knows keeps its call, and a room that
// reads offline cannot be added over: the state a room is added in is idle or in-call.
func TestAddingARoomKeepsItsCall(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	call(t, f, "POST /api/rooms", `{"id": "hq-conf-1"}`)
	call(t, f, "POST /api/rooms", `{"id": "zr-huddle-north"}`)
	if f.Read("status.rooms.hq-conf-1.state") != "idle" || f.Read("status.rooms.zr-huddle-north.state") != "in-call" {
		t.Errorf("added %v, kept %v", f.Read("status.rooms.hq-conf-1.state"), f.Read("status.rooms.zr-huddle-north.state"))
	}
	_ = f.Write("status.rooms.hq-conf-1.state", "offline")
	if res := call(t, f, "POST /api/rooms", `{"id": "hq-conf-1"}`); res.Status != 400 || !strings.Contains(string(res.Body.([]byte)), `"value":"offline"`) {
		t.Errorf("adding over an offline room: %d %s", res.Status, res.Body)
	}
	if res := call(t, f, "POST /api/rooms", `{"id": "a.b"}`); res.Status != 400 {
		t.Errorf("a room id with a dot: %d", res.Status)
	}
}

// rooms is a world of two placed rooms the calendar holds a meeting in.
type rooms struct {
	spec.Rooms
	meeting map[string]calendar.Entry
	offline map[string]bool
	books   map[string]behave.Bookings
}

func (rooms) Served() []spec.ServedRoom {
	return []spec.ServedRoom{{System: "sys-a", Site: "hq", Building: "tower", Room: "r1", Label: "Curie"}, {System: "sys-b", Site: "hq", Building: "tower", Room: "r2"}}
}
func (rooms) Place(system string) (calendar.Room, bool) {
	switch system {
	case "sys-a":
		return calendar.Room{Site: "hq", Building: "tower", Room: "r1"}, true
	case "sys-b":
		return calendar.Room{Site: "hq", Building: "tower", Room: "r2"}, true
	}
	return calendar.Room{}, false
}
func (r rooms) MeetingNow(c calendar.Room) (calendar.Entry, bool) {
	e, ok := r.meeting[c.Room]
	return e, ok
}
func (r rooms) Bookings(c calendar.Room) behave.Bookings { return r.books[c.Room] }
func (r rooms) Offline(system string) bool               { return r.offline[system] }

func runCalendar(f *spectest.Fake) {
	for _, ch := range Spec().Ticks[0].Run(f, spec.TickContext{Now: f.Clock}) {
		_ = f.Write(ch.Path, ch.Value)
	}
}

// The calendar tick reads each placed room off the calendar and announces a boundary
// once subscribed: nothing for a room it has just learned, a started when a meeting
// begins, an ended when it ends, and a room it does not place keeps its state.
func TestTheCalendarTickReadsRoomsAndAnnouncesBoundaries(t *testing.T) {
	start := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	held := calendar.MeetingEntry(start, start.Add(time.Hour), calendar.Room{Site: "hq", Building: "tower", Room: "r1"}, calendar.Meeting{Actual: 3, Organizer: "Quinn", Outcome: calendar.OutcomeHeld})
	noShow := calendar.MeetingEntry(start, start.Add(time.Hour), calendar.Room{Site: "hq", Building: "tower", Room: "r2"}, calendar.Meeting{Outcome: calendar.OutcomeNoShow, Organizer: "Gray"})
	w := rooms{meeting: map[string]calendar.Entry{"r1": held, "r2": noShow}, offline: map[string]bool{}, books: map[string]behave.Bookings{
		"r1": {Booked: true, State: "booked", Now: &behave.Booking{Organizer: "Quinn", SlotStart: 100, SlotEnd: 200}, Next: &behave.Booking{Organizer: "Gray", SlotStart: 300}},
	}}
	f := subscribed(t)
	f.RoomsRead = w
	runCalendar(f)
	if len(f.Posts) != 0 {
		t.Fatalf("a room just learned was announced: %s", f.Posts[0].Body)
	}
	if f.Read("status.rooms.sys-a.state") != "in-call" || f.Read("status.rooms.sys-a.meeting") != "m-"+itoa(start.Unix())+"-sys-a" || f.Read("status.rooms.sys-a.participants") != int64(3) || f.Read("status.rooms.sys-a.topic") != "Meeting hosted by Quinn" {
		t.Errorf("sys-a reads %v", f.Read("status.rooms.sys-a"))
	}
	if f.Read("status.rooms.sys-a.name") != "Curie" || f.Read("status.rooms.sys-b.name") != "r2" || f.Read("status.rooms.sys-b.state") != "idle" {
		t.Errorf("names %v %v, a no-show reads %v", f.Read("status.rooms.sys-a.name"), f.Read("status.rooms.sys-b.name"), f.Read("status.rooms.sys-b.state"))
	}
	if b, _ := json.Marshal(f.Read("status.rooms.sys-a.booking")); string(b) != `{"end":200,"start":100,"topic":"Meeting hosted by Quinn"}` {
		t.Errorf("booking = %s", b)
	}
	if f.Read("status.rooms.zr-boardroom-b.state") != "in-call" {
		t.Errorf("a room the world does not place lost its state: %v", f.Read("status.rooms.zr-boardroom-b"))
	}
	// The meeting ends: the tick announces it, once.
	w.meeting = map[string]calendar.Entry{}
	f.RoomsRead = w
	runCalendar(f)
	if len(f.Posts) != 1 || !strings.Contains(string(f.Posts[0].Body), `"event":"room.meeting.ended","room_id":"sys-a"`) || f.Posts[0].Headers["X-UCC-Event"] != "room.meeting.ended" {
		t.Fatalf("the end was announced as %+v", f.Posts)
	}
	runCalendar(f)
	if len(f.Posts) != 1 {
		t.Errorf("the end was announced again: %d posts", len(f.Posts))
	}
	// A new meeting: a started post names the meeting.
	w.meeting = map[string]calendar.Entry{"r1": held}
	f.RoomsRead = w
	runCalendar(f)
	if len(f.Posts) != 2 || !strings.Contains(string(f.Posts[1].Body), `"event":"room.meeting.started","room_id":"sys-a","meeting":"m-`) {
		t.Errorf("the start was announced as %+v", f.Posts)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// A room whose codec or bar is unplugged reads offline, and keeps its meeting.
func TestAnUnpluggedRoomReadsOffline(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	f.RoomsRead = rooms{offline: map[string]bool{"sys-a": true}}
	runCalendar(f)
	if f.Read("status.rooms.sys-a.state") != "offline" || f.Read("status.rooms.sys-b.state") != "idle" {
		t.Errorf("states %v %v", f.Read("status.rooms.sys-a.state"), f.Read("status.rooms.sys-b.state"))
	}
}

// The room list maps the state to the platform's three statuses, reads a room's
// booking as it is held (a map in key order, or the empty one), and a room with
// no name is named by its id.
func TestTheRoomListReadsEachRoomAsThePlatformDoes(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write("status.rooms.hq-new.state", "offline")
	_ = f.Write("status.rooms.hq-new.booking", map[string]any{"topic": "T", "start": int64(5), "end": int64(9)})
	res := route(t, "GET /api/rooms").Handle(f, spec.HTTPRequest{Query: map[string]string{"page_size": "300"}})
	var doc struct {
		Rooms []struct {
			ID, Name, Status string
			Booking          map[string]any
		}
	}
	if err := json.Unmarshal(res.Body.([]byte), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, res.Body)
	}
	got := map[string]string{}
	for _, r := range doc.Rooms {
		got[r.ID] = r.Status + "/" + r.Name
	}
	for id, want := range map[string]string{"hq-new": "offline/hq-new", "zr-huddle-north": "in-meeting/Huddle North", "zr-boardroom-a": "available/Boardroom A"} {
		if got[id] != want {
			t.Errorf("%s reads %q, want %q", id, got[id], want)
		}
	}
	if !strings.Contains(string(res.Body.([]byte)), `"booking": {"end":9,"start":5,"topic":"T"}`) || !strings.Contains(string(res.Body.([]byte)), `"booking": {"topic": "", "start": 0, "end": 0}`) {
		t.Errorf("bookings are not laid out as held:\n%s", res.Body)
	}
}

// devices merge the world's listing (online, or offline since the arbiter had the
// device unplugged) with the devices the platform holds that no room places, in id
// order, a held device seen now when it holds no time.
type listing struct{ spec.Rooms }

func (listing) Listed() []spec.ListedDevice {
	return []spec.ListedDevice{
		{ID: "dev-a", Name: "a-codec", System: "sys-a", Type: "codec", Firmware: "2.7"},
		{ID: "dev-b", Name: "b-bar", System: "sys-a", Type: "bar", Firmware: "2.7", Offline: true, Since: 1234},
	}
}

func TestTheDeviceListMergesTheWorldAndWhatThePlatformHolds(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	f.RoomsRead = listing{}
	res := route(t, "GET /api/devices").Handle(f, spec.HTTPRequest{Query: map[string]string{"page_size": "3"}})
	var doc struct {
		Devices []struct {
			ID, Status string
			LastSeen   int64 `json:"last_seen"`
		}
		Next string `json:"next_page_token"`
	}
	if err := json.Unmarshal(res.Body.([]byte), &doc); err != nil || len(doc.Devices) != 3 || doc.Next != "zd-annex-codec" {
		t.Fatalf("devices = %+v (%v) from %s", doc, err, res.Body)
	}
	for i, want := range []struct {
		id, status string
		seen       int64
	}{{"dev-a", "online", f.Clock.Unix()}, {"dev-b", "offline", 1234}, {"zd-annex-codec", "offline", f.Clock.Unix()}} {
		if g := doc.Devices[i]; g.ID != want.id || g.Status != want.status || g.LastSeen != want.seen {
			t.Errorf("device %d = %+v, want %+v", i, g, want)
		}
	}
}

// The dashboard lists every room by name and id, a room with no name by its id and
// one with no state as idle, and escapes what the account holds.
func TestTheDashboardListsTheRoomsAndEscapesThem(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write("status.rooms.hq-bare.meeting", "m")
	_ = f.Write("status.rooms.hq-evil.name", "<script>x</script>")
	page := string(route(t, "GET /").Handle(f, spec.HTTPRequest{}).Body.([]byte))
	for _, want := range []string{`<span class="k">Boardroom A (zr-boardroom-a)</span><span>idle</span>`, `<span class="k">hq-bare (hq-bare)</span><span>idle</span>`, `&lt;script&gt;x&lt;/script&gt; (hq-evil)`, "// apiURL() resolves"} {
		if !strings.Contains(page, want) {
			t.Errorf("the dashboard lacks %q", want)
		}
	}
	if strings.Contains(page, "<script>x</script>") {
		t.Error("a room name reached the page unescaped")
	}
}

// The family declares the webhook face the simulator subscribes it through.
func TestTheFamilyDeclaresItsWebhookFace(t *testing.T) {
	cat, err := spec.NewCatalog(Spec())
	if err != nil {
		t.Fatal(err)
	}
	r, _ := cat.ForProduct(Name)
	w := r.Definition.WebhookFace()
	if w == nil || w.Subscribe != "/api/webhooks" || w.Driver != "ucc-webhook" || w.Provision != "/api/rooms" || r.Definition.PushFace() != nil {
		t.Errorf("webhook face = %+v", w)
	}
}
