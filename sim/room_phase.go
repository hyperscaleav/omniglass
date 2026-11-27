package sim

import (
	"sync"
	"sync/atomic"
	"text/template"
	"time"

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

// roomPhaseFuncs are the template functions that read a room's phase through
// the device's clock (the seam a test pins). Each reads a map with every key
// present (calendar.RoomState.Map), so a profile branches on has_calendar and
// never on a missing key:
//
//	roomPhase        -> the device's own room
//	roomPhaseOf s b r -> the room site s, building b, room r (a service that speaks for many)
//	ownerPhase o     -> the room of the system o in the world (a push's .Owner); a system the
//	                    world does not name reads as a room with no calendar
//
// The keys: phase (idle, waking, in-meeting, sharing, cooldown or impaired),
// occupancy (the phase the meetings alone give), has_calendar, scheduled,
// in_use, call_active, attendees, sharing, impaired, building_open, use_until,
// recording_pending and meeting (the meeting running now, nil when none).
func (d *Device) roomPhaseFuncs() template.FuncMap {
	return template.FuncMap{
		"roomPhase": func() map[string]any { return d.roomState(d.room).Map() },
		"roomPhaseOf": func(site, building, room string) map[string]any {
			return d.roomState(calendar.Room{Site: site, Building: building, Room: room}).Map()
		},
		"ownerPhase": func(owner string) map[string]any {
			p, ok := d.world().PlaceSystem(owner)
			if !ok {
				return d.roomState(calendar.Room{}).Map()
			}
			return d.roomState(p.CalRoom()).Map()
		},
	}
}
