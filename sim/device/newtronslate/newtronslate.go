// Package newtronslate is the Newtron scheduling panel (#2833), a Go device
// family of one model: the panel mounted at a room's door that shows whether
// the room is booked and what is next. It is not a touch panel and controls
// nothing in the room, so its one face is the NVP line protocol with its room's
// booking alone: GET BOOKING, BOOK and RELEASE, GET NEXT and GET CURRENT, and
// the BOOKING subject. A different API from the touch panel's, so a family of
// its own.
//
// The booking it shows is the lab calendar's: the panel has no schedule of its
// own. BOOK and RELEASE are its own buttons, a momentary override that holds
// until the calendar's reading of the room next changes.
//
// The family replaced the newtron-slate-5 YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package newtronslate

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// Name is the family's id, the profile name its devices carry.
const Name = "newtron-slate"

// Row is one model of the scheduling panel line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	// Firmware is the version it ships with.
	Firmware string
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string { return fmt.Sprintf("Newtron %s, room scheduling panel", r.Model) }

// Rows is the model table: the Slate line, one model.
var Rows = []Row{
	{Product: "newtron-slate-5", Model: "Slate 5", Firmware: "2.3.0"},
}

// The state paths of the panel.
const (
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
	pathBooking   = "status.room.booking"
	pathCalendar  = "status.room.cal_booking"
	pathCurrent   = "status.room.current_meeting"
	pathNext      = "status.room.next_meeting"
)

const nvpFace = "nvp"

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device": map[string]any{"boot_epoch": 0},
			"room": map[string]any{
				// Whether the room is booked now: booked or free; the calendar's last
				// reading of it, which the booking follows on a change; and the booked
				// meeting holding the room and the next one, "none" when there is none.
				"booking": behave.StateFree, "cal_booking": behave.StateFree, "current_meeting": "none", "next_meeting": "none",
			},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Newtron"},
			"firmware": map[string]any{"version": r.Firmware},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Newtron", Model: "Slate", DeviceType: "scheduling-panel",
			Summary: "Newtron Slate scheduling panel",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{
				Product: r.Product, Model: r.Model, Summary: r.Summary(),
				State: map[string]any{"configuration": map[string]any{"device": map[string]any{"model": r.Model}, "firmware": map[string]any{"version": r.Firmware}}},
			}
		}),
		Interfaces: []profile.Interface{
			{
				Name: nvpFace, Protocol: "tcp", Port: 51325, Persistent: true, LineEnding: "\r\n",
				Doc: profile.Doc{Summary: "NVP line protocol: the room's booking and its next meeting"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability"}},
		},
		State:   stateFor(Rows[0]),
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{roomBookings()},
		Surface: surfaceOf,
	}
}

// roomBookings reads the room's booked meetings off the lab calendar: booked
// entries alone (a no-show holds its room booked, a walk-in is on no booking).
// The panel's own booking follows the calendar's reading only when it changes,
// so a button press holds until then (behave.RoomBooking).
func roomBookings() spec.Tick {
	return spec.Tick{
		Name: "room_bookings", Interval: bind.Seconds(10), OnStart: true,
		Writes: []string{pathBooking, pathCalendar, pathCurrent, pathNext},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			next := behave.RoomBooking(behave.RoomBookingState{
				Booking: spec.String(d, pathBooking), CalBooking: spec.String(d, pathCalendar),
			}, c.ReadBookings())
			return []surface.Change{
				{Path: pathBooking, Value: next.Booking},
				{Path: pathCalendar, Value: next.CalBooking},
				{Path: pathCurrent, Value: next.Current},
				{Path: pathNext, Value: next.Next},
			}
		},
	}
}

// lines is the panel's line protocol.
type lines struct{ d spec.Device }

var _ surface.Commander = lines{}

func (lines) Commands() []string {
	return []string{"GET BOOKING", "BOOK", "RELEASE", "GET NEXT", "GET CURRENT", "SUBSCRIBE BOOKING", "UNSUBSCRIBE BOOKING"}
}

func (l lines) Handle(line string) (surface.Reply, bool) {
	switch line {
	case "GET BOOKING":
		return surface.Reply{Text: "BOOKING " + spec.String(l.d, pathBooking)}, true
	case "BOOK":
		return setBooking(behave.StateBooked), true
	case "RELEASE":
		return setBooking(behave.StateFree), true
	// The room's next booked meeting and the one holding it now, each
	// "<start> <end> <organizer>" in unix seconds of the booked slot, or "none".
	case "GET NEXT":
		return surface.Reply{Text: "NEXT " + spec.String(l.d, pathNext)}, true
	case "GET CURRENT":
		return surface.Reply{Text: "CURRENT " + spec.String(l.d, pathCurrent)}, true
	case "SUBSCRIBE BOOKING":
		return surface.Reply{Text: "OK", Action: surface.ActionSubscribe, Subject: pathBooking, Token: "BOOKING"}, true
	case "UNSUBSCRIBE BOOKING":
		return surface.Reply{Text: "OK", Action: surface.ActionUnsubscribe, Subject: pathBooking}, true
	}
	return surface.Reply{}, false
}

func setBooking(state string) surface.Reply {
	return surface.Reply{Text: "BOOKING " + state, Changes: []surface.Change{{Path: pathBooking, Value: state}}}
}

// surfaceOf is the line protocol, and the booking a session may subscribe to:
// "EVT BOOKING <state>" on change.
func surfaceOf(d spec.Device) spec.Surface {
	return spec.Surface{
		Commanders: []surface.Commander{lines{d}},
		Subjects: map[string][]surface.Subject{nvpFace: {{
			Match: pathBooking, Path: pathBooking, Token: "BOOKING",
			Push: func(v any) string { return "EVT BOOKING " + surface.Raw(v) },
		}}},
		ListedOn: nvpFace,
	}
}
