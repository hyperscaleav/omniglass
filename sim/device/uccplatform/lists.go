package uccplatform

import (
	"fmt"
	"sort"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
)

// roomRecord is one room as the account lists it. Every field is read from the room
// the platform holds, a room the calendar does not place keeping what its routes gave
// it; a room with no booking or no next meeting reads empty ones.
func roomRecord(id string, room map[string]any) string {
	state := orText(room["state"], "idle")
	status := "available"
	switch state {
	case "in-call":
		status = "in-meeting"
	case "offline":
		status = "offline"
	}
	booking, next := `{"topic": "", "start": 0, "end": 0}`, `{"topic": "", "start": 0}`
	if m, _ := room["booking"].(map[string]any); len(m) > 0 {
		booking = quote(m)
	}
	if m, _ := room["next"].(map[string]any); len(m) > 0 {
		next = quote(m)
	}
	return fmt.Sprintf("{\"id\": %s, \"name\": %s, \"address\": %s, \"status\": %s,\n     \"meeting\": {\"id\": %s, \"topic\": %s, \"start\": %d, \"participants\": %d},\n     \"booking\": %s,\n     \"next\": %s}",
		quote(id), quote(orText(room["name"], id)), quote(orText(room["address"], "")), quote(status),
		quote(orText(room["meeting"], "")), quote(orText(room["topic"], "")), spec.Whole(room["start"]), spec.Whole(room["participants"]), booking, next)
}

// roomsRoute lists the account's rooms a page at a time: a token names the last room
// of the page before, a size caps the page. A room is available (nobody in a meeting),
// in-meeting or offline; its meeting is always present, empty when there is none, and
// its booking is the booked slot that holds it now, which a no-show leaves in place
// while the room reads available.
func roomsRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	rooms, _ := d.Read(pathRooms).(map[string]any)
	ids := make([]string, 0, len(rooms))
	for id := range rooms {
		ids = append(ids, id)
	}
	page, next := httpapi.Cut(ids, httpapi.Cloud.After(r.Query), httpapi.Cloud.Size(r.Query))
	records := make([]string, len(page))
	for i, id := range page {
		room, _ := rooms[id].(map[string]any)
		records[i] = roomRecord(id, room)
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.PagedDocument("rooms", records, 1, next)}
}

// listed is a device the account lists: its record and the id it is paged by.
type listed struct {
	id, name, room, kind, status, firmware string
	lastSeen                               int64
}

func (l listed) record() string {
	return fmt.Sprintf(`{"id": %s, "name": %s, "room_id": %s, "type": %s, "status": %s, "firmware": %s, "last_seen": %d}`,
		quote(l.id), quote(l.name), quote(l.room), quote(l.kind), quote(l.status), quote(l.firmware), l.lastSeen)
}

// devices is every device on the account in id order: the devices of the rooms the
// world covers (online and seen now, or offline and seen since the arbiter had them
// unplugged) and the ones the platform holds that no room places (as it holds them,
// seen now when it holds none).
func devices(d spec.Device) []listed {
	now := d.Now().Unix()
	var out []listed
	known := map[string]bool{}
	for _, l := range spec.RoomsOf(d).Listed() {
		known[l.ID] = true
		item := listed{id: l.ID, name: l.Name, room: l.System, kind: l.Type, status: "online", firmware: l.Firmware, lastSeen: now}
		if l.Offline {
			item.status, item.lastSeen = "offline", l.Since
		}
		out = append(out, item)
	}
	held, _ := d.Read(pathDevices).(map[string]any)
	for id, v := range held {
		if known[id] {
			continue
		}
		m, _ := v.(map[string]any)
		seen := spec.Whole(m["last_seen"])
		if seen <= 0 {
			seen = now
		}
		out = append(out, listed{id: id, name: orText(m["name"], id), room: orText(m["room_id"], ""), kind: orText(m["type"], ""),
			status: orText(m["status"], "online"), firmware: orText(m["firmware"], ""), lastSeen: seen})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// devicesRoute lists the account's devices, token-paged like its rooms, in id order.
func devicesRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	all := devices(d)
	ids := make([]string, len(all))
	byID := map[string]listed{}
	for i, l := range all {
		ids[i] = l.id
		byID[l.id] = l
	}
	page, next := httpapi.Cut(ids, httpapi.Cloud.After(r.Query), httpapi.Cloud.Size(r.Query))
	records := make([]string, len(page))
	for i, id := range page {
		records[i] = byID[id].record()
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.PagedDocument("devices", records, 1, next)}
}
