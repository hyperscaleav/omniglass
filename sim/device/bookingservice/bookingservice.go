// Package bookingservice is the booking service (#2836), a Go device family of
// one: a room-booking platform's first-party integration. It is an outside actor,
// not a device on the network: a scheduling platform that reports the bookings of
// the places it manages by pushing them to Omniglass, as a first-party integration
// does. It has no interface to poll. Its push face holds a bearer token for a user
// granted telemetry:push alone on the systems it books, reads those systems off its
// own grant (GET /auth/me), and posts each one's batch through POST /telemetry:push
// every 30 seconds. A scenario declares it as a pusher; the lab seeds its user and
// mints its token per run.
//
// Pushed datapoints, per system it is granted:
//
//	booking-count  (metric)    booked meetings left in the day, the one running included
//	booking-state  (property)  booked while a booked meeting holds the room, free between
//
// Every reading is the lab calendar's booked entries for the room the system sits
// in, resolved through the world the simulator holds, on the device's clock. A
// walk-in is no booking, and a no-show is: its slot reads booked. A system the world
// does not place has no calendar and keeps the service's earlier behavior exactly:
// the meeting day in configuration.calendar.bookings, windows in UTC that repeat
// every day.
//
// The family replaced the booking-service YAML profile; its pushed batches matched the profile's
// before the profile was deleted (#2826).
package bookingservice

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Name is the family's id, the profile name its devices carry: the product it is
// a family of one for.
const Name = "booking-service"

// Row is one model of the service.
type Row struct {
	// Product is the catalog product the row simulates.
	Product string
}

// Rows is the model table: the booking service, one model.
var Rows = []Row{{Product: Name}}

// The state paths of the service.
const (
	pathBootEpoch = "status.device.boot_epoch"
	pathUptime    = "status.device.uptime"
	pathDay       = "configuration.calendar.bookings"
)

// source names the integration on every batch it pushes.
const source = "sim-booking-service"

// every is how often the service pushes each system's batch.
const every = 30 * time.Second

// day is the meeting day of a system the world does not place: windows in UTC
// that repeat every day.
var day = []any{"09:00-10:00", "10:30-11:30", "13:00-14:00", "15:00-16:30"}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Shinra Cloud", Model: "Booking Service (integration)", DeviceType: "cloud-service",
			Summary: "Room-booking platform's integration: pushes each booked location's day, read off the lab calendar, through /telemetry:push",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: "Booking Service (integration)"}
		}),
		State: map[string]any{
			"status":        map[string]any{"device": map[string]any{"boot_epoch": 0}},
			"configuration": map[string]any{"calendar": map[string]any{"bookings": day}},
		},
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Push:    &spec.PushFace{Every: every, Body: batch},
	}
}

// reading is what a system's room holds: the meetings left in the day, the one
// running included, and whether a booked meeting holds it now.
type reading struct {
	Remaining int
	State     string
}

// readingOf is the system's booked day. A system the world places reads off the
// lab calendar's booked entries for its room; any other keeps the fixed meeting
// day the device holds, an error when that day is not a list of HH:MM-HH:MM windows.
func readingOf(d spec.Device, owner string) (reading, error) {
	rooms := spec.RoomsOf(d)
	if room, placed := rooms.Place(owner); placed {
		b := rooms.Bookings(room)
		return reading{Remaining: b.Remaining, State: b.State}, nil
	}
	list, ok := d.Read(pathDay).([]any)
	if !ok {
		return reading{}, fmt.Errorf("bookings %v is not a list of HH:MM-HH:MM windows", d.Read(pathDay))
	}
	windows := make([]string, len(list))
	for i, w := range list {
		s, ok := w.(string)
		if !ok {
			return reading{}, fmt.Errorf("booking window %v is not an HH:MM-HH:MM string", w)
		}
		windows[i] = s
	}
	parsed, err := behave.ParseWindows(windows)
	if err != nil {
		return reading{}, err
	}
	state, remaining := behave.FixedDay(d.Now(), parsed)
	return reading{Remaining: remaining, State: state}, nil
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// batch is what the service pushes for one system: its booking count as a metric
// and its booking state as a property, the owner a system by id. The document is
// laid out a line a section, as the profile's body template wrote it.
func batch(d spec.Device, owner string) ([]byte, error) {
	r, err := readingOf(d, owner)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("{\"owner\":{\"kind\":\"system\",\"ref\":%s},\n \"source\":%s,\n \"metrics\":[{\"name\":\"booking-count\",\"value\":%d}],\n \"properties\":[{\"name\":\"booking-state\",\"value\":%s}]}\n",
		quote(owner), quote(source), r.Remaining, quote(r.State))), nil
}
