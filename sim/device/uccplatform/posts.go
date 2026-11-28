package uccplatform

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// The account's webhook posts: where each goes, how it is signed, and what each
// event says. A post names its room by id, so each value lands on the system its
// room id is bound to; an id nobody bound lands in the conduit's unbound inbox.

// Event names the platform posts.
const (
	eventStarted = "room.meeting.started"
	eventEnded   = "room.meeting.ended"
	eventJoined  = "room.participant.joined"
	eventLeft    = "room.participant.left"
	eventDevice  = "device.status.changed"
	eventReport  = "rooms.state"
)

// post sends one event to the subscribed receiver, signed as the subscription says,
// and sends nothing before the account is subscribed (its target is empty).
func post(d spec.Device, event, body string) {
	target := strings.TrimSpace(spec.String(d, pathTarget))
	if target == "" {
		return
	}
	spec.Send(d, spec.Post{
		URL: target, Body: []byte(body), Headers: map[string]string{"X-UCC-Event": event},
		Sign: &spec.Signing{
			Scheme: strings.TrimSpace(spec.String(d, pathScheme)), Secret: strings.TrimSpace(spec.String(d, pathSecret)), Fault: strings.TrimSpace(spec.String(d, pathFault)),
			Header: "X-UCC-Signature", Prefix: "sha256=", Timestamp: "X-UCC-Timestamp", Delivery: "X-UCC-Delivery",
		},
	})
}

func quote(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// meetingBody is the post of a meeting starting or ending in a room; the room and
// the meeting are written as they were sent, a start naming its meeting.
func meetingBody(event, room, meeting string, ts int64) string {
	if event == eventStarted {
		return fmt.Sprintf(`{"event":"%s","room_id":"%s","meeting":"%s","ts":%d}`, event, room, meeting, ts)
	}
	return fmt.Sprintf(`{"event":"%s","room_id":"%s","ts":%d}`, event, room, ts)
}

// participantBody is the post of a participant joining or leaving: the room and the
// headcount after.
func participantBody(event, room string, participants int64, ts int64) string {
	return fmt.Sprintf(`{"event":"%s","room_id":%s,"participants":%d,"ts":%d}`, event, quote(room), participants, ts)
}

// deviceBody is the post of a device's status changing.
func deviceBody(room any, device, status string, ts int64) string {
	return fmt.Sprintf(`{"event":"%s","room_id":%s,"device_id":%s,"status":%s,"ts":%d}`, eventDevice, quote(room), quote(device), quote(status), ts)
}

// reportBody is the batched report: one post naming every room on the account and
// its state, as a platform's periodic digest does, in id order.
func reportBody(d spec.Device, ts int64) string {
	rooms, _ := d.Read(pathRooms).(map[string]any)
	ids := make([]string, 0, len(rooms))
	for id := range rooms {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString(`{"event":"rooms.state","rooms":[`)
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		r, _ := rooms[id].(map[string]any)
		state := "<no value>"
		if v, ok := r["state"]; ok && v != nil {
			state = fmt.Sprint(v)
		}
		fmt.Fprintf(&b, `{"room_id":"%s","state":"%s"}`, id, state)
	}
	fmt.Fprintf(&b, `],"ts":%d}`, ts)
	return b.String()
}

// reportTick is the periodic digest.
func reportTick(d spec.Device, c spec.TickContext) []surface.Change {
	post(d, eventReport, reportBody(d, c.Now.Unix()))
	return nil
}
