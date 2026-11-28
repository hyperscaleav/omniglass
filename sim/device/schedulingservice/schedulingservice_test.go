package schedulingservice

import (
	"encoding/json"
	"strings"
	"testing"

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

// world is the rooms a service reads, each with the bookings it holds.
type world struct {
	spec.Rooms
	served []spec.ServedRoom
	books  map[string]behave.Bookings
}

func (w world) Served() []spec.ServedRoom { return w.served }
func (w world) Bookings(r calendar.Room) behave.Bookings {
	return w.books[r.Room]
}

// The service's day is the sum of its rooms' bookings, and each room is listed
// under the system it belongs to beside the unassigned room nobody bound.
func TestTheTickSumsTheRoomsAndListsThemBySystem(t *testing.T) {
	tick := Spec().Ticks[0]
	f := spectest.NewFake(Spec(), Name)
	f.RoomsRead = world{
		served: []spec.ServedRoom{{System: "sys-a", Site: "hq", Building: "tower", Room: "r1", Label: "Curie"}, {Site: "hq", Building: "tower", Room: "r2"}},
		books: map[string]behave.Bookings{
			"r1": {Today: 4, Remaining: 3, Booked: true, State: "booked", Now: &behave.Booking{Organizer: "Quinn", SlotStart: 100}, Next: &behave.Booking{SlotStart: 900}},
			"r2": {Today: 2, Remaining: 1, State: "free"},
		},
	}
	for _, ch := range tick.Run(f, spec.TickContext{}) {
		_ = f.Write(ch.Path, ch.Value)
	}
	for path, want := range map[string]int64{pathToday: 6, pathRemaining: 4, pathProgress: 1, pathServed: 2} {
		if f.Read(path) != want {
			t.Errorf("%s = %v, want %d", path, f.Read(path), want)
		}
	}
	rooms := f.Read(pathRooms).(map[string]any)
	if len(rooms) != 3 {
		t.Fatalf("rooms = %v, want the two served and the unassigned one", rooms)
	}
	if r := rooms["sys-a"].(map[string]any); r["name"] != "Curie" || r["address"] != "r1" || r["state"] != "booked" || r["current"] != "Quinn" || r["next_start"] != int64(900) {
		t.Errorf("sys-a = %v", r)
	}
	if r := rooms["r2"].(map[string]any); r["name"] != "r2" || r["state"] != "free" {
		t.Errorf("a room with no system is listed by its room: %v", r)
	}
	if r := rooms[unassigned].(map[string]any); r["address"] != "" || r["today"] != int64(0) {
		t.Errorf("unassigned = %v", r)
	}
}

// A service the world covers no room for has no calendar: it keeps today's count
// the fixed 8, reports nothing left or running, and leaves the room list as it was.
func TestAServiceWithNoRoomKeepsItsFixedDay(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	for _, ch := range Spec().Ticks[0].Run(f, spec.TickContext{}) {
		if ch.Path == pathRooms {
			t.Errorf("a service with no room wrote its room list")
		}
		_ = f.Write(ch.Path, ch.Value)
	}
	if f.Read(pathToday) != int64(8) || f.Read(pathRemaining) != int64(0) || f.Read(pathServed) != int64(0) {
		t.Errorf("today %v remaining %v served %v, want 8, 0 and 0", f.Read(pathToday), f.Read(pathRemaining), f.Read(pathServed))
	}
}

func pages(t *testing.T, f *spectest.Fake, query map[string]string) (ids []string, next string, size float64) {
	t.Helper()
	res := route(t, "GET /api/rooms").Handle(f, spec.HTTPRequest{Query: query})
	var doc struct {
		Rooms []struct{ ID string } `json:"rooms"`
		Size  float64               `json:"page_size"`
		Next  string                `json:"next_page_token"`
	}
	if err := json.Unmarshal(res.Body.([]byte), &doc); err != nil {
		t.Fatalf("the room list is not JSON: %v\n%s", err, res.Body)
	}
	for _, r := range doc.Rooms {
		ids = append(ids, r.ID)
	}
	return ids, doc.Next, doc.Size
}

// The room list pages by the cloud's names: a token names the last room of the
// page before, a size caps the page, an unreadable size is the default page.
func TestTheRoomListPagesByTheCloudsNames(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	rooms := map[string]any{}
	for _, id := range []string{"sys-c", "sys-a", "sys-b", "sys-d"} {
		rooms[id] = map[string]any{"name": id, "today": int64(1)}
	}
	_ = f.Write(pathRooms, rooms)
	if ids, next, size := pages(t, f, map[string]string{"page_size": "3"}); strings.Join(ids, ",") != "sys-a,sys-b,sys-c" || next != "sys-c" || size != 3 {
		t.Errorf("first page: %v next %q size %v", ids, next, size)
	}
	if ids, next, _ := pages(t, f, map[string]string{"page_size": "3", "next_page_token": "sys-c"}); strings.Join(ids, ",") != "sys-d" || next != "" {
		t.Errorf("last page: %v next %q", ids, next)
	}
	if ids, _, _ := pages(t, f, map[string]string{"page_size": "many"}); len(ids) != 4 {
		t.Errorf("an unreadable size lists %v, want the default page", ids)
	}
	if ids, _, _ := pages(t, f, map[string]string{"limit": "1"}); len(ids) != 4 {
		t.Errorf("another API's parameter was read: %v", ids)
	}
}

// The dashboard follows the state and escapes what the device says; its sync flag
// reads ok or failed.
func TestTheDashboardShowsTheDay(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	f.Host = "<b>x</b>"
	_ = f.Write(pathToday, int64(11))
	_ = f.Write(pathSyncOK, false)
	page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	for _, want := range []string{`<span class="k">today</span><span class="v">11</span>`, `<span class="v">failed</span>`, "&lt;b&gt;x&lt;/b&gt;", "// This page is served by the simulated device"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(page, "<b>x</b>") {
		t.Error("the hostname reached the page unescaped")
	}
}

// The bookings document carries the day's counts and the sync flag.
func TestTheBookingsDocumentIsJSON(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	var doc struct {
		Today, Remaining, InProgress, RoomsServed int
		SyncOK                                    bool `json:"sync_ok"`
	}
	body := route(t, "GET /api/bookings").Handle(f, spec.HTTPRequest{}).Body.([]byte)
	if err := json.Unmarshal(body, &doc); err != nil || doc.Today != 8 || !doc.SyncOK {
		t.Errorf("bookings = %+v (%v) from %s", doc, err, body)
	}
}
