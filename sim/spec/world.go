package spec

import (
	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// A service that speaks for many rooms (a booking platform, a meeting platform,
// a recording service) reads the world the simulator holds and the lab calendar
// through the device's clock: which rooms it covers, what each holds now, what is
// booked in it, and which devices it lists (#2836). Rooms is that read, handed to
// a family's handler or tick through RoomsOf; the engine answers it from the one
// memo of room readings every device shares, so a service reading thousands of
// rooms reads each once a second however many devices ask.

// ServedRoom is a room a service speaks for: the system that sits in it (the id
// the lab's installer binds), where it is, and the name a company gives it.
type ServedRoom struct {
	System, Site, Building, Room, Label string
}

// Calendar is the room as a calendar key.
func (r ServedRoom) Calendar() calendar.Room {
	return calendar.Room{Site: r.Site, Building: r.Building, Room: r.Room}
}

// ListedDevice is a device of a covered room that a meeting platform lists: its
// id, name, kind and firmware as the world gives them, the system of the room it
// sits in, and whether the arbiter has it unplugged (and since when, unix seconds).
type ListedDevice struct {
	ID, Name, System, Type, Firmware string
	Offline                          bool
	Since                            int64
}

// Rooms is what a service reads of the rooms it speaks for.
type Rooms interface {
	// Served is the rooms the service speaks for (the world's covers, each with
	// its system), else the room the device sits in when it has one, else none.
	Served() []ServedRoom
	// Covered is the rooms the world says the service covers, none when the
	// world names none (a device's own room is never covered).
	Covered() []ServedRoom
	// Place is the room of a system in the world.
	Place(system string) (calendar.Room, bool)
	// State is a room's phase and the facts every device in it shares.
	State(room calendar.Room) calendar.RoomState
	// MeetingNow is the meeting held in a room now, a no-show included.
	MeetingNow(room calendar.Room) (calendar.Entry, bool)
	// Bookings is a room's booked meetings alone, as a scheduling surface reads
	// them; a room with no calendar reads free and empty.
	Bookings(room calendar.Room) behave.Bookings
	// Offline says whether a system's room reads offline on a meeting platform:
	// a codec or a bar in it that the arbiter has unplugged.
	Offline(system string) bool
	// Listed is the devices of the covered rooms a platform lists, in id order.
	Listed() []ListedDevice
}

// roomsProvider is a device that can say what its rooms hold.
type roomsProvider interface{ Rooms() Rooms }

// RoomsOf is the rooms the device reads: the engine's device answers from the
// world and the lab calendar, a device with neither (a unit test's) reads none.
func RoomsOf(d Device) Rooms {
	if p, ok := d.(roomsProvider); ok {
		return p.Rooms()
	}
	return noRooms{}
}

type noRooms struct{}

func (noRooms) Served() []ServedRoom                            { return nil }
func (noRooms) Covered() []ServedRoom                           { return nil }
func (noRooms) Place(string) (calendar.Room, bool)              { return calendar.Room{}, false }
func (noRooms) MeetingNow(calendar.Room) (calendar.Entry, bool) { return calendar.Entry{}, false }
func (noRooms) Offline(string) bool                             { return false }
func (noRooms) Listed() []ListedDevice                          { return nil }
func (noRooms) State(calendar.Room) calendar.RoomState {
	return calendar.RoomState{Phase: calendar.PhaseIdle, Occupancy: calendar.PhaseIdle}
}
func (noRooms) Bookings(calendar.Room) behave.Bookings {
	return behave.Bookings{State: behave.StateFree}
}

// A device that sends webhooks, or reports a named event on its event stream,
// does so through these. A device that cannot (a unit test's) drops them, so a
// handler is written once.

// Post is one webhook a device sends: where, what and how it is signed. An empty
// URL is a device not yet subscribed, and sends nothing.
type Post struct {
	URL     string
	Body    []byte
	Headers map[string]string
	// Sign is how the post is authenticated, nil for none.
	Sign *Signing
}

// Signing is how a sender authenticates its post (#1024): its scheme (hmac,
// token, basic or none), its secret (the hmac key, the token, or user:password),
// the header the signature or token rides and the prefix before it, the header
// carrying the signed unix timestamp (hmac then signs the timestamp, a dot and
// the body), the header carrying each post's delivery id, and a fault that posts
// as a sender gone wrong: bad-signature, stale (a timestamp ten minutes old) or
// redeliver (the same delivery twice).
type Signing struct {
	Scheme, Secret, Fault               string
	Header, Prefix, Timestamp, Delivery string
}

type poster interface{ Post(Post) }

// Send posts a webhook as the device, when the device can.
func Send(d Device, p Post) {
	if s, ok := d.(poster); ok {
		s.Post(p)
	}
}

type reporter interface{ Report(name, data string) }

// Report names an event on the device's event stream, after the state changes
// that came with it, when the device has a stream.
func Report(d Device, name, data string) {
	if s, ok := d.(reporter); ok {
		s.Report(name, data)
	}
}
