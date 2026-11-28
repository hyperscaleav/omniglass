package sim

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The room phase (#2797): one derived reading of a room, computed once per
// room per second however many devices read it. A processor, a panel, a
// display, a bar and a PDU in one room read the same RoomState, so the wake
// lead, the cooldown tail and the sharing stretch live once per standard in
// the meeting knobs (calendar.Timing) and a profile carries none of them.

// roomMemoLimit bounds the memo: past it, readings of older seconds go.
const roomMemoLimit = 1 << 14

// roomMemoKey names one reading: the calendar and world it was read through,
// the room and the second. A test that swaps a device's calendar or world
// reads a fresh entry, never a stale one.
type roomMemoKey struct {
	cal  *calendar.Calendar
	w    *world.World
	room calendar.Room
	sec  int64
}

// roomMemoEntry is one room's readings in one second; each is computed on the
// first read that needs it.
type roomMemoEntry struct {
	stateOnce sync.Once
	state     calendar.RoomState
	bookOnce  sync.Once
	book      map[string]any
	// typed is the same reading as behave.Bookings, for a Go family's tick.
	typedOnce sync.Once
	typed     behave.Bookings
}

// roomMemo is the engine-level memo of room readings: the room state and the
// booked-meetings reading (calendar_bookings.go). A device standing alone has
// its own.
type roomMemo struct {
	mu sync.Mutex
	m  map[roomMemoKey]*roomMemoEntry
	// states and books count the readings computed, which a test (or a bench)
	// holds to once per room per second.
	states, books atomic.Int64
}

func (r *roomMemo) entry(k roomMemoKey) *roomMemoEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[roomMemoKey]*roomMemoEntry{}
	}
	if e, ok := r.m[k]; ok {
		return e
	}
	if len(r.m) >= roomMemoLimit {
		for old := range r.m {
			if old.sec < k.sec {
				delete(r.m, old)
			}
		}
		if len(r.m) >= roomMemoLimit {
			clear(r.m)
		}
	}
	e := &roomMemoEntry{}
	r.m[k] = e
	return e
}

// roomMemo is the memo the device reads through: its engine's, or its own.
func (d *Device) roomMemo() *roomMemo {
	if d.memo != nil {
		return d.memo
	}
	d.ownMemoOnce.Do(func() { d.ownMemo = &roomMemo{} })
	return d.ownMemo
}

// roomState is the room's state at the device's clock, from the memo. A room
// that is the zero key, or a device with no calendar, reads an idle room with
// no calendar.
func (d *Device) roomState(room calendar.Room) calendar.RoomState {
	c := d.calendarSrc()
	if c == nil || room == (calendar.Room{}) {
		return calendar.RoomState{Phase: calendar.PhaseIdle, Occupancy: calendar.PhaseIdle}
	}
	at := d.clock().UTC().Truncate(time.Second)
	w := d.world()
	memo := d.roomMemo()
	e := memo.entry(roomMemoKey{cal: c, w: w, room: room, sec: at.Unix()})
	e.stateOnce.Do(func() {
		memo.states.Add(1)
		e.state = c.StateAt(room, at)
		if h, ok := buildingSpan(w, c, room); ok {
			e.state.BuildingOpen = h.Open(at)
		}
	})
	return e.state
}

// bookingsRead is the device's room's booked meetings at its clock as a Go
// family's tick reads them (spec.TickContext.Bookings): the typed form of the
// `bookings` template function, memoized per room per second beside it. A
// device in no room, or with no calendar, reads a free and empty room.
func (d *Device) bookingsRead() behave.Bookings {
	c, room := d.calendarSrc(), d.room
	if c == nil || room == (calendar.Room{}) {
		return behave.Bookings{State: behave.StateFree}
	}
	at := d.clock().UTC().Truncate(time.Second)
	memo := d.roomMemo()
	e := memo.entry(roomMemoKey{cal: c, w: nil, room: room, sec: at.Unix()})
	e.typedOnce.Do(func() {
		memo.books.Add(1)
		e.typed = behave.BookingsAt(c, room, at)
	})
	return e.typed
}
