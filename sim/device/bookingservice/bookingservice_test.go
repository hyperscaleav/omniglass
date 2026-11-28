package bookingservice

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

// The shipped booking service is a push face: it pushes on a cadence and declares
// no webhook, which is a different face, and it is a cloud service with nothing to
// poll.
func TestTheBookingServiceHasAPushFaceAndNoOther(t *testing.T) {
	s := Spec()
	cat, err := spec.NewCatalog(nil, s)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := cat.ForProduct(Name)
	if !ok {
		t.Fatal("the catalog lacks the booking service")
	}
	p := r.Definition.PushFace()
	if p == nil || p.Every != "30s" || r.Definition.WebhookFace() != nil || r.Identity.DeviceType != "cloud-service" {
		t.Errorf("push %+v webhook %+v type %s, want a cloud service with a push face and no webhook", p, r.Definition.WebhookFace(), r.Identity.DeviceType)
	}
	if len(s.Interfaces) != 0 || len(s.HTTP) != 0 || len(s.Ticks) != 0 {
		t.Errorf("the service has %d interfaces, %d routes and %d ticks: nothing polls it, it reports", len(s.Interfaces), len(s.HTTP), len(s.Ticks))
	}
}

func render(t *testing.T, f *spectest.Fake, owner string) (map[string]any, string) {
	t.Helper()
	b, err := Spec().Push.Body(f, owner)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("the batch is not JSON: %v\n%s", err, b)
	}
	return doc, string(b)
}

// A system the world does not place keeps the fixed meeting day, read on the
// device's clock: booked inside a window, free between, and the windows left
// counted.
func TestASystemNobodyPlacesKeepsTheFixedDay(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		at        string
		remaining float64
		state     string
	}{{"08:00", 4, "free"}, {"09:30", 4, "booked"}, {"10:00", 3, "free"}, {"13:59", 2, "booked"}, {"16:30", 0, "free"}} {
		tm, _ := time.Parse("15:04", tc.at)
		f.Clock = day.Add(time.Duration(tm.Hour())*time.Hour + time.Duration(tm.Minute())*time.Minute)
		doc, _ := render(t, f, "sys-x")
		metric := doc["metrics"].([]any)[0].(map[string]any)
		prop := doc["properties"].([]any)[0].(map[string]any)
		if metric["name"] != "booking-count" || metric["value"] != tc.remaining || prop["name"] != "booking-state" || prop["value"] != tc.state {
			t.Errorf("at %s: count %v state %v, want %v %s", tc.at, metric["value"], prop["value"], tc.remaining, tc.state)
		}
	}
}

// A malformed day is an error, not a made-up push.
func TestAMalformedDayIsRefused(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	for _, bad := range []any{"09:00-10:00", []any{"9-10"}, []any{5}, []any{"10:00-09:00"}} {
		_ = f.Write(pathDay, bad)
		if _, err := Spec().Push.Body(f, "sys"); err == nil {
			t.Errorf("a day of %v was pushed", bad)
		}
	}
}

// rooms is a world that places one system and reads its bookings.
type rooms struct {
	spec.Rooms
	books behave.Bookings
}

func (rooms) Place(system string) (calendar.Room, bool) {
	return calendar.Room{Site: "hq", Room: "r1"}, system == "sys-a"
}
func (r rooms) Bookings(calendar.Room) behave.Bookings { return r.books }

// A system the world places reads off the calendar's booked entries for its room,
// in the layout the integration has always pushed: a line a section, the owner a
// system by id.
func TestAPlacedSystemReadsItsRoomsBookings(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	f.RoomsRead = rooms{books: behave.Bookings{Remaining: 3, Booked: true, State: "booked"}}
	_, raw := render(t, f, "sys-a")
	want := "{\"owner\":{\"kind\":\"system\",\"ref\":\"sys-a\"},\n \"source\":\"sim-booking-service\",\n \"metrics\":[{\"name\":\"booking-count\",\"value\":3}],\n \"properties\":[{\"name\":\"booking-state\",\"value\":\"booked\"}]}\n"
	if raw != want {
		t.Errorf("batch = %q, want %q", raw, want)
	}
	// A system the world does not place falls back to the fixed day (two windows
	// are left at noon), not the room's.
	doc, _ := render(t, f, "sys-b")
	if metric := doc["metrics"].([]any)[0].(map[string]any); metric["value"] != float64(2) {
		t.Errorf("an unplaced system read %v, want the fixed day's 2", metric["value"])
	}
}
