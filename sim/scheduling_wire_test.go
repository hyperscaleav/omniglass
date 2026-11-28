//go:build linux

package sim

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The scheduling service's wire script (#2836): the admin API behind its token,
// its dashboard, the bookings it reads off the lab calendar for the rooms it
// speaks for, and the paged room list those become.

const schedulingProduct = "scheduling-service"

func schedulingScript() []wireStep {
	return []wireStep{
		wireGet("the dashboard", "/"),
		{Name: "the dashboard needs no token", Method: "GET", Path: "/", NoKey: true},
		wireGet("the status", "/api/status"),
		{Name: "the status with no token", Method: "GET", Path: "/api/status", NoKey: true},
		wireGet("the room list", "/api/rooms"),
		wireGet("the bookings", "/api/bookings"),
		{Name: "the bookings with no token", Method: "GET", Path: "/api/bookings", NoKey: true},
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the wrong method on the status", Method: "POST", Path: "/api/status", Body: `{}`},
		{Name: "a head request on the status", Method: "HEAD", Path: "/api/status"},
	}
}

func schedulingTicks() []tickPlan {
	return []tickPlan{{"bookings_sync", []string{"status.bookings.today", "status.bookings.remaining", "status.bookings.in_progress", "configuration.rooms_served", "status.rooms"}}}
}

var schedulingWatch = []string{"status.bookings.today", "status.bookings.remaining", "status.bookings.in_progress", "configuration.rooms_served", "status.rooms"}

func schedulingRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, schedulingWatch...)
	w.sample("start")
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 30*time.Second, func(string) { d.TickBehavior("bookings_sync") }, func(string) {})
	return w.out
}

// serviceDevice is a service in the parity room on an engine that holds the
// world and its calendar, as a lab's does: the rooms it covers, the problems
// the arbiter reads, and the clock the parity day is walked on.
func serviceDevice(t *testing.T, fam wireFamily, product string, w *world.World, cal *calendar.Calendar) (*Device, *time.Time) {
	t.Helper()
	e := fam.Engine(t)
	e.SetWorld(w)
	d, at := roomDayEngine(t, e, product, false)
	e.SetCalendar(cal)
	d.calSrc = func() *calendar.Calendar { return cal }
	return d, at
}

// serviceWorldDay walks a service that speaks for the parity world's rooms
// through the parity day, sampling its watched paths.
func serviceWorldDay(t *testing.T, fam wireFamily, product, profile string, watch []string, tick func(*Device), step time.Duration) []wireChange {
	w, wcal := parityWorld("d-1", profile, parityCovers())
	d, at := serviceDevice(t, fam, product, w, wcal)
	ch := newWatch(d, watch...)
	ch.sample("start")
	roomDayWalk(d, at, ch, "07:00:00", "15:00:00", step, func(string) { tick(d) }, func(string) {})
	return ch.out
}

// schedulingExtra records the service in a world: the room list in pages, and
// the day its three rooms hold.
func schedulingExtra(t *testing.T, fam wireFamily, product string) map[string]any {
	w, wcal := parityWorld("d-1", "scheduling-service", parityCovers())
	d, at := serviceDevice(t, fam, product, w, wcal)
	*at = parityAt("10:05:00")
	d.TickBehavior("bookings_sync")
	pages := runWireHTTP(t, d, []wireStep{
		wireGet("the room list", "/api/rooms"),
		wireGet("pages of one", "/api/rooms?page_size=1"),
		wireGet("the second page", "/api/rooms?page_size=1&next_page_token=sys-a"),
		wireGet("the third page", "/api/rooms?page_size=1&next_page_token=sys-b"),
		wireGet("the last page", "/api/rooms?page_size=1&next_page_token=sys-c"),
		wireGet("past the last page", "/api/rooms?page_size=1&next_page_token=unassigned-room"),
		wireGet("pages of two", "/api/rooms?page_size=2"),
		wireGet("the page after two", "/api/rooms?page_size=2&next_page_token=sys-b"),
		wireGet("a token with no size", "/api/rooms?next_page_token=sys-a"),
		wireGet("a size of zero", "/api/rooms?page_size=0"),
		wireGet("a negative size", "/api/rooms?page_size=-3"),
		wireGet("a size that is no number", "/api/rooms?page_size=many"),
		wireGet("a size past the most", "/api/rooms?page_size=9999"),
		wireGet("a token nobody issued", "/api/rooms?next_page_token=sys-bb&page_size=1"),
		wireGet("a token before every room", "/api/rooms?next_page_token=a&page_size=1"),
		wireGet("an empty token", "/api/rooms?next_page_token=&page_size=1"),
		wireGet("the status", "/api/status"),
		wireGet("the bookings", "/api/bookings"),
		wireGet("the dashboard", "/"),
	})
	return map[string]any{
		"pages": pages,
		"room_day_world": serviceWorldDay(t, fam, product, "scheduling-service", schedulingWatch,
			func(d *Device) { d.TickBehavior("bookings_sync") }, 30*time.Second),
	}
}

var schedulingWire = wireFamily{
	Name: "scheduling-service", Products: []string{schedulingProduct}, Engine: shippedEngineOf, Source: wireYAMLSource, Frozen: wireYAMLFrozen,
	Script: schedulingScript(), Ticks: schedulingTicks(), RoomDay: schedulingRoomDay, Extra: schedulingExtra,
}

func init() { wireFamilies = append(wireFamilies, schedulingWire) }
