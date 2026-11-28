package sim

import (
	"log/slog"
	"sort"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// What a Go family reads of the rooms it speaks for, and the webhooks and events
// it sends (#2836): the device as a handler sees it answers spec.Rooms,
// spec.Post and spec.Report beside spec.Device, from the same world, calendar,
// memo and sender a YAML profile's template functions read.

// Rooms is the rooms the device speaks for, as spec.RoomsOf reads them.
func (g goDevice) Rooms() spec.Rooms { return goRooms{g.d} }

type goRooms struct{ d *Device }

func (r goRooms) Served() []spec.ServedRoom {
	var out []spec.ServedRoom
	if w := r.d.world(); w != nil {
		for _, s := range w.Services {
			if s.Name != r.d.Hostname {
				continue
			}
			for _, c := range s.Covers {
				out = append(out, spec.ServedRoom{System: c.System, Site: c.Site, Building: c.Building, Room: c.Room, Label: c.Label})
			}
		}
	}
	if len(out) == 0 && r.d.room != (calendar.Room{}) {
		out = append(out, spec.ServedRoom{Site: r.d.room.Site, Building: r.d.room.Building, Room: r.d.room.Room})
	}
	return out
}

func (r goRooms) Covered() []spec.ServedRoom {
	var out []spec.ServedRoom
	for _, p := range r.d.world().Covered(r.d.Hostname) {
		out = append(out, spec.ServedRoom{Site: p.Site, Building: p.Building, Room: p.Room})
	}
	return out
}

func (r goRooms) Place(system string) (calendar.Room, bool) {
	p, ok := r.d.world().PlaceSystem(system)
	if !ok {
		return calendar.Room{}, false
	}
	return p.CalRoom(), true
}

func (r goRooms) State(room calendar.Room) calendar.RoomState { return r.d.roomState(room) }

func (r goRooms) MeetingNow(room calendar.Room) (calendar.Entry, bool) {
	c := r.d.calendarSrc()
	if c == nil {
		return calendar.Entry{}, false
	}
	return c.Now(room, r.d.clock(), calendar.MeetingKinds)
}

// Bookings is the booked meetings of a room, read once a second however many
// devices ask (the memo the template functions share).
func (r goRooms) Bookings(room calendar.Room) behave.Bookings {
	c, at := r.d.calendarSrc(), r.d.clock().UTC().Truncate(time.Second)
	if room == (calendar.Room{}) {
		c = nil // no room: nothing is booked in it
	}
	memo := r.d.roomMemo()
	e := memo.entry(roomMemoKey{cal: c, w: nil, room: room, sec: at.Unix()})
	e.typedOnce.Do(func() {
		memo.books.Add(1)
		e.typed = behave.BookingsAt(c, room, at)
	})
	return e.typed
}

func (r goRooms) Offline(system string) bool { return r.d.roomOffline(system) }

func (r goRooms) Listed() []spec.ListedDevice {
	listed := r.d.world().Listed(r.d.Hostname)
	out := make([]spec.ListedDevice, 0, len(listed))
	for _, l := range listed {
		item := spec.ListedDevice{ID: l.ID, Name: l.Name, System: l.System, Type: l.Type, Firmware: l.Firmware}
		item.Since, item.Offline = r.d.unpluggedSince(l)
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Post sends a webhook as the device: signed as its Signing says, from its own
// goroutine, best effort, as a YAML behavior's emit is.
func (g goDevice) Post(p spec.Post) {
	if p.URL == "" {
		return
	}
	r := renderedEmit{url: p.URL, body: p.Body, headers: p.Headers}
	if s := p.Sign; s != nil {
		r.scheme, r.secret, r.fault = s.Scheme, s.Secret, s.Fault
		r.sign = &emitSign{header: s.Header, prefix: s.Prefix, timestamp: s.Timestamp, delivery: s.Delivery}
	}
	go func() {
		if err := dispatchEmit(r); err != nil {
			slog.Warn("webhook post failed", "device_id", g.d.DeviceID, "error", err)
		}
	}()
}

// Report names an event on the device's event stream, if it has one.
func (g goDevice) Report(name, data string) {
	if g.d.events != nil {
		g.d.events.publish(name, data)
	}
}
