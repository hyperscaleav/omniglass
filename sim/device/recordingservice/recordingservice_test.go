package recordingservice

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

func fail(t *testing.T, f *spectest.Fake, body string) spec.HTTPResponse {
	t.Helper()
	var parsed map[string]any
	_ = json.Unmarshal([]byte(body), &parsed)
	return route(t, "POST /api/recordings/fail").Handle(f, spec.HTTPRequest{Body: parsed, Raw: []byte(body)})
}

// A failure is recorded as the last one and named on the live feed with the
// recording's id and why; one change carries both, so the stream never reads an id
// beside the reason before it.
func TestAFailureIsRecordedAndNamedOnTheFeed(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	res := fail(t, f, `{"id": "r-9001", "reason": "disk full"}`)
	if res.Status != 0 || string(res.Body.([]byte)) != "{\"ok\": true}\n" {
		t.Fatalf("answer = %d %s", res.Status, res.Body)
	}
	if f.Read(pathFailID) != "r-9001" || f.Read(pathFailWhy) != "disk full" || len(f.Writes) != 1 || len(f.Writes[0]) != 2 {
		t.Errorf("last failure = %v %v in %d writes", f.Read(pathFailID), f.Read(pathFailWhy), len(f.Writes))
	}
	if len(f.Events) != 1 || f.Events[0] != (spec.Event{Name: "recording-failed", Data: `{"id":"r-9001","reason":"disk full"}`}) {
		t.Errorf("reports = %v", f.Events)
	}
}

// A refused failure changes nothing and names nothing: an id or a reason outside
// its pattern, missing, or not text.
func TestARefusedFailureChangesAndNamesNothing(t *testing.T) {
	for _, body := range []string{
		`{"id": "bad id!", "reason": "x"}`, `{"id": "r-1", "reason": "say \"no\""}`, `{"id": "r-1"}`, `{"reason": "x"}`, `{}`,
		`{"id": 77, "reason": "numeric"}`, `{"id": "r-1", "reason": 5}`, `{"id": "", "reason": "x"}`, `[1]`,
		`{"id": "r-123456789012345678901234567890123", "reason": "x"}`,
	} {
		f := spectest.NewFake(Spec(), Name)
		if res := fail(t, f, body); res.Status != 400 || len(f.Writes) != 0 || len(f.Events) != 0 {
			t.Errorf("%s: status %d, %d writes, %d reports; want a 400 that changes and names nothing", body, res.Status, len(f.Writes), len(f.Events))
		}
	}
	f := spectest.NewFake(Spec(), Name)
	res := fail(t, f, `{"id": "bad id!", "reason": ""}`)
	if got := string(res.Body.([]byte)); !strings.Contains(got, `"path":"status.recordings.last_failure.id"`) || !strings.Contains(got, `"path":"status.recordings.last_failure.reason"`) || !strings.Contains(got, `"pattern":"^[A-Za-z0-9-]{1,32}$"`) {
		t.Errorf("the refusal names %s, want both fields and their patterns", got)
	}
}

// With a world the service lists each room it speaks for under its system, and
// its counters are those of the rooms it covers; with none it drifts.
type rooms struct {
	spec.Rooms
	covered, served []spec.ServedRoom
	state           map[string]calendar.RoomState
}

func (r rooms) Covered() []spec.ServedRoom { return r.covered }
func (r rooms) Served() []spec.ServedRoom  { return r.served }
func (r rooms) State(c calendar.Room) calendar.RoomState {
	return r.state[c.Room]
}

func TestTheWorkIsTheCoveredRoomsAndTheListIsTheServedOnes(t *testing.T) {
	tick := Spec().Ticks[0]
	start := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	recorded := calendar.RoomState{HasCalendar: true, Scheduled: true, HasMeeting: true,
		Meeting: calendar.MeetingEntry(start, start.Add(time.Hour), calendar.Room{}, calendar.Meeting{Recorded: true, Actual: 3})}
	pending := calendar.RoomState{HasCalendar: true, Scheduled: true, RecordingPending: true}
	r := []spec.ServedRoom{{System: "sys-a", Room: "r1"}, {System: "sys-b", Room: "r2"}, {System: "sys-c", Room: "r3"}}
	f := spectest.NewFake(Spec(), Name)
	f.RoomsRead = rooms{covered: r, served: r, state: map[string]calendar.RoomState{"r1": recorded, "r2": pending, "r3": {HasCalendar: true, Scheduled: true}}}
	for _, ch := range tick.Run(f, spec.TickContext{Rand: behave.NewSource("d")}) {
		_ = f.Write(ch.Path, ch.Value)
	}
	if f.Read(pathInProg) != int64(1) || f.Read(pathQueue) != int64(1) {
		t.Errorf("in progress %v queue %v, want one each", f.Read(pathInProg), f.Read(pathQueue))
	}
	got, _ := json.Marshal(f.Read(pathRooms))
	if string(got) != `{"sys-a":{"in_progress":1,"queue_depth":0},"sys-b":{"in_progress":0,"queue_depth":1},"sys-c":{"in_progress":0,"queue_depth":0}}` {
		t.Errorf("rooms = %s", got)
	}

	// With no room to read the counters drift within their bounds and the list stays.
	alone := spectest.NewFake(Spec(), Name)
	_ = alone.Write(pathRooms, map[string]any{"keep": map[string]any{}})
	src := behave.NewSource("d")
	for i := 0; i < 40; i++ {
		for _, ch := range tick.Run(alone, spec.TickContext{Rand: src}) {
			if ch.Path == pathRooms {
				t.Fatal("a service with no room rewrote its room list")
			}
			_ = alone.Write(ch.Path, ch.Value)
		}
		if n, q := alone.Read(pathInProg).(int64), alone.Read(pathQueue).(int64); n < 0 || n > 12 || q < 0 || q > 40 {
			t.Fatalf("tick %d: in progress %d queue %d outside 0..12 and 0..40", i, n, q)
		}
	}
}

// The room list pages by the cloud's names and lists each room's own work.
func TestTheRecordingListPagesAndNamesEachRoom(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write(pathRooms, map[string]any{"b": map[string]any{"in_progress": int64(1), "queue_depth": int64(0)}, "a": map[string]any{"in_progress": int64(0), "queue_depth": int64(2)}})
	res := route(t, "GET /api/recordings").Handle(f, spec.HTTPRequest{Query: map[string]string{"page_size": "1"}})
	var doc struct {
		Recordings []struct {
			ID         string
			QueueDepth int `json:"queue_depth"`
		}
		Next string `json:"next_page_token"`
	}
	if err := json.Unmarshal(res.Body.([]byte), &doc); err != nil || len(doc.Recordings) != 1 || doc.Recordings[0].ID != "a" || doc.Recordings[0].QueueDepth != 2 || doc.Next != "a" {
		t.Errorf("first page = %+v (%v)", doc, err)
	}
}

func TestHealthAndWorkReportTheState(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write(pathStorage, 41.5)
	if got := string(route(t, "GET /api/health").Handle(f, spec.HTTPRequest{}).Body.([]byte)); !strings.Contains(got, `"status": "ok"`) || !strings.Contains(got, `"version": "2026.10.2"`) {
		t.Errorf("health = %s", got)
	}
	if got := string(route(t, "GET /api/work").Handle(f, spec.HTTPRequest{}).Body.([]byte)); !strings.Contains(got, `"storage_used_percent": 41.5`) || !strings.Contains(got, `"recordings_in_progress": 4`) {
		t.Errorf("work = %s", got)
	}
}
