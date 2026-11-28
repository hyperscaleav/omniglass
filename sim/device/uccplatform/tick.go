package uccplatform

import (
	"fmt"
	"sort"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// topicOf is the topic a meeting reads as: the lab calendar carries no topic of its
// own, so the platform names it for the person who booked it, and a walk-in, which
// nobody booked, plainly.
func topicOf(organizer string) string {
	if organizer != "" {
		return "Meeting hosted by " + organizer
	}
	return "Walk-in meeting"
}

// placedRoom is a room the world places, as the calendar reads it now: in a call
// exactly when its meeting has somebody in it, the meeting named by its start and the
// room, who came, the booking that holds the room and the next booked one. A booked
// meeting nobody came to leaves the room available but still booked, and a room whose
// codec or bar the arbiter has unplugged reads offline.
func placedRoom(world spec.Rooms, s spec.ServedRoom) map[string]any {
	room := s.Calendar()
	var in bool
	var meeting, topic string
	var participants, start int64
	if e, ok := world.MeetingNow(room); ok {
		if m, isMeeting := e.AsMeeting(); isMeeting && m.Outcome != calendar.OutcomeNoShow {
			in, meeting, participants, start = true, fmt.Sprintf("m-%d-%s", e.Start.Unix(), s.System), int64(m.Actual), e.Start.Unix()
			topic = topicOf(m.Organizer)
		}
	}
	books := world.Bookings(room)
	booking := map[string]any{"topic": "", "start": int64(0), "end": int64(0)}
	if books.Now != nil {
		booking = map[string]any{"topic": topicOf(books.Now.Organizer), "start": books.Now.SlotStart, "end": books.Now.SlotEnd}
	}
	next := map[string]any{"topic": "", "start": int64(0)}
	if books.Next != nil {
		next = map[string]any{"topic": topicOf(books.Next.Organizer), "start": books.Next.SlotStart}
	}
	state := "idle"
	switch {
	case world.Offline(s.System):
		state = "offline"
	case in:
		state = "in-call"
	}
	return map[string]any{
		"name": orText(s.Label, s.Room), "address": s.Room, "state": state, "meeting": meeting, "participants": participants,
		"topic": topic, "start": start, "booking": booking, "next": next,
	}
}

// keptRoom is a room the world does not place, as its routes left it, with the
// fields a placed room has: it keeps the state its routes gave it, and no booking.
func keptRoom(id string, held map[string]any) map[string]any {
	return map[string]any{
		"name": orText(held["name"], id), "address": orText(held["address"], ""), "state": orText(held["state"], "idle"),
		"meeting": orText(held["meeting"], ""), "participants": spec.Whole(held["participants"]), "topic": orText(held["topic"], ""),
		"start":   spec.Whole(held["start"]),
		"booking": map[string]any{"topic": "", "start": int64(0), "end": int64(0)}, "next": map[string]any{"topic": "", "start": int64(0)},
	}
}

// announcement is the one post a tick makes: a meeting that began or ended.
type announcement struct{ kind, room, meeting string }

func (a announcement) value() map[string]any {
	return map[string]any{"kind": a.kind, "room": a.room, "meeting": a.meeting}
}

// calendarTick reads each room the world places off the lab calendar into
// status.rooms and tells the server when a meeting begins or ends: one post a tick,
// an ended before a started when a meeting follows another, in room-id order when
// several fall due together. Before the account is subscribed, and for a room it has
// just learned, it posts nothing and takes the room's state as it stands, so a lab
// does not announce every meeting already running; the batched report tells the whole
// state. status.sim is the simulator's: told is the meeting each room was last told of
// ("-" for none) and announce is what this tick posts.
func calendarTick() spec.Tick {
	return spec.Tick{
		Name: "rooms_calendar", Interval: bind.Seconds(15), OnStart: true,
		Writes: []string{pathRooms, pathAnnounce, pathTold},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			world := spec.RoomsOf(d)
			rooms := map[string]any{}
			for _, s := range world.Served() {
				if s.System != "" {
					rooms[s.System] = placedRoom(world, s)
				}
			}
			held, _ := d.Read(pathRooms).(map[string]any)
			for id, v := range held {
				if _, placed := world.Place(id); !placed {
					m, _ := v.(map[string]any)
					rooms[id] = keptRoom(id, m)
				}
			}
			ids := make([]string, 0, len(rooms))
			for id := range rooms {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			told, _ := d.Read(pathTold).(map[string]any)
			subscribed := spec.String(d, pathTarget) != ""

			var out announcement
			for _, id := range ids {
				if _, placed := world.Place(id); !placed || out.kind != "" {
					continue
				}
				r := rooms[id].(map[string]any)
				switch t, _ := told[id].(string); {
				case !subscribed || t == "":
				case t != "-" && (r["state"] == "idle" || r["meeting"] != t):
					out = announcement{"ended", id, t}
				case t == "-" && r["state"] == "in-call":
					out = announcement{"started", id, r["meeting"].(string)}
				}
			}
			newTold := map[string]any{}
			for _, id := range ids {
				if _, placed := world.Place(id); !placed {
					continue
				}
				r := rooms[id].(map[string]any)
				v, _ := told[id].(string)
				switch {
				case subscribed && v != "":
					newTold[id] = v
					if out.room == id {
						newTold[id] = "-"
						if out.kind == "started" {
							newTold[id] = r["meeting"]
						}
					}
				case r["state"] == "in-call":
					newTold[id] = r["meeting"]
				default:
					newTold[id] = "-"
				}
			}
			if out.kind != "" {
				post(d, "room.meeting."+out.kind, fmt.Sprintf(`{"event":"room.meeting.%s","room_id":"%s","meeting":"%s","ts":%d}`, out.kind, out.room, out.meeting, c.Now.Unix()))
			}
			return []surface.Change{{Path: pathRooms, Value: rooms}, {Path: pathAnnounce, Value: out.value()}, {Path: pathTold, Value: newTold}}
		},
	}
}
