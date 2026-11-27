package sim

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// A cloud service that speaks for many rooms reads the lab calendar from the
// first request it answers (#2757, central by derivation): the room it
// covers is in a recorded meeting when the service starts, and the service
// says so with no behavior tick between its start and the read. A service
// that waited for its first timer would answer idle for up to a minute while
// the room's own devices were already in the meeting, and a collector polling
// at startup would carry that stale reading as the room's.
func TestACloudServiceReadsTheCalendarFromItsFirstRequest(t *testing.T) {
	k := calTestKnobs()
	k.RecordingRate = 1
	cal := calendar.New(7, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r101"}
	// A held meeting with margin to its end, so the reading holds through the test.
	var at time.Time
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, e := range cal.Entries(room, day, day.Add(24*time.Hour), calendar.MeetingKinds) {
		if caltest.M(e).Outcome != calendar.OutcomeNoShow && e.End.Sub(e.Start) > 20*time.Minute && at.IsZero() {
			at = e.Start.Add(5 * time.Minute)
		}
	}
	if at.IsZero() {
		t.Fatal("the day holds no meeting that ran")
	}

	for _, tc := range []struct {
		profile, header, key, path string
		want                       func(body string) bool
	}{
		{"ucc-platform", "X-UCC-Token", "ucc-lab-token", "/api/rooms", func(b string) bool {
			var doc struct{ Rooms []struct{ ID, Status string } }
			if json.Unmarshal([]byte(b), &doc) != nil {
				return false
			}
			for _, r := range doc.Rooms {
				if r.ID == "sys-101" {
					return r.Status == "in-meeting"
				}
			}
			return false
		}},
		{"scheduling-service", "X-Sched-Token", "sched-lab-token", "/api/rooms", func(b string) bool {
			var doc struct{ Rooms []struct{ ID, State string } }
			if json.Unmarshal([]byte(b), &doc) != nil {
				return false
			}
			for _, r := range doc.Rooms {
				if r.ID == "sys-101" {
					return r.State == "booked"
				}
			}
			return false
		}},
		{"recording-service", "X-Recording-Key", "recording-lab-key", "/api/recordings", func(b string) bool {
			var doc struct {
				Recordings []struct {
					ID         string
					InProgress int `json:"in_progress"`
				}
			}
			if json.Unmarshal([]byte(b), &doc) != nil {
				return false
			}
			for _, r := range doc.Recordings {
				if r.ID == "sys-101" {
					return r.InProgress == 1
				}
			}
			return false
		}},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			store, err := profile.LoadStoreFS(profiles.FS)
			if err != nil {
				t.Fatal(err)
			}
			e := NewEngine(store, profiles.Templates())
			t.Cleanup(e.StopAll)
			e.SetCalendar(cal)
			e.SetClock(Clock{Offset: time.Until(at)})
			e.SetWorld(&world.World{Version: world.Version,
				Sites: []world.Site{{Name: "hq", Rooms: []world.Room{{Name: "r101", Building: "tower", System: "sys-101"}}}},
				Services: []world.Service{{Name: "svc-1", Profile: tc.profile, Covers: []world.Cover{
					{System: "sys-101", Site: "hq", Building: "tower", Room: "r101"}}}}})
			if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "svc-1", Hostname: "svc-1", LoopbackIP: "127.0.0.1", ProfileName: tc.profile, Version: 1}}); err != nil {
				t.Fatal(err)
			}
			d := e.Devices()[0]
			d.StartBehaviors()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, strings.NewReader(""))
			req.Header.Set(tc.header, tc.key)
			d.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || !tc.want(rec.Body.String()) {
				t.Fatalf("GET %s on start = %d %s, want the room in its meeting", tc.path, rec.Code, rec.Body)
			}
		})
	}
}
