package uccplatform

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
)

// What the routes take of a request, each as the platform has always checked it.
var (
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
	countPattern   = regexp.MustCompile(`^[0-9]{1,3}$`)
	topicPattern   = regexp.MustCompile(`^[A-Za-z0-9 .,'-]{0,64}$`)
	urlPattern     = regexp.MustCompile(`^(https?://\S{1,1000})?$`)
)

// The sources of those patterns and the values a field takes, as a refusal names them.
const (
	idSource    = "^[A-Za-z0-9-]{1,32}$"
	countSource = "^[0-9]{1,3}$"
	topicSource = "^[A-Za-z0-9 .,'-]{0,64}$"
	urlSource   = "^(https?://\\S{1,1000})?$"
)

var (
	schemes = []string{"hmac", "token", "basic", "none"}
	faults  = []string{"", "bad-signature", "stale", "redeliver"}
	states  = []string{"idle", "in-call"}
	online  = []string{"online", "offline"}
)

// ack is the one-line acknowledgment every route answers.
func ack() spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Spaced(struct {
		OK bool `json:"ok"`
	}{true})}
}

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /", Summary: "UCC platform dashboard: every room on the account", Handle: dashboardRoute},
		{Match: "GET /api/rooms", Summary: "Every room on the account with its meeting state, a page at a time", Handle: roomsRoute},
		{Match: "GET /api/devices", Summary: "Every device on the account with its status and firmware, a page at a time", Handle: devicesRoute},
		// The webhook subscription (#1024): where the platform posts its room events
		// and how each post is signed.
		{Match: "POST /api/webhooks", Summary: "Subscribe a webhook receiver to every room's meeting events", Handle: webhooksRoute,
			Example: "{\"url\": \"https://omniglass.example/api/v1/webhooks/TOKEN\", \"scheme\": \"hmac\", \"secret\": \"ucc-lab-signing-key\"}\n"},
		// A room joins the account, as the platform's admin adds one (#1747): the
		// simulator calls this for each room a scale lab binds on the account's
		// webhook endpoint, so the batched report names it. A room the account
		// already knows keeps its call; a new one reads idle.
		{Match: "POST /api/rooms", Summary: "Add a room to the account (idle until a meeting starts)", Handle: addRoomRoute, Example: "{\"id\": \"hq-conf-1\"}\n"},
		// A meeting starts in one room: the room's state flips and the platform posts
		// room.meeting.started naming the room by id. A room id the account has not
		// seen yet joins it (a room added on the platform), which is how an unbound
		// room is heard.
		{Match: "POST /api/rooms/meetings/start", Summary: "Start a meeting in a room (posts room.meeting.started)", Handle: startRoute,
			Example: "{\"room\": \"zr-huddle-north\", \"id\": \"m-2001\", \"participants\": 4}\n"},
		{Match: "POST /api/rooms/meetings/end", Summary: "End the meeting in a room (posts room.meeting.ended)", Handle: endRoute, Example: "{\"room\": \"zr-huddle-north\"}\n"},
		// A participant joins or leaves a room's meeting: the headcount after is the
		// room's, and the platform posts room.participant.joined or
		// room.participant.left naming the room.
		{Match: "POST /api/rooms/participants/join", Summary: "A participant joins a room's meeting (posts room.participant.joined)", Example: "{\"room\": \"zr-huddle-north\", \"participants\": 4}\n",
			Handle: participantRoute(eventJoined, 1)},
		{Match: "POST /api/rooms/participants/leave", Summary: "A participant leaves a room's meeting (posts room.participant.left)", Example: "{\"room\": \"zr-huddle-north\", \"participants\": 2}\n",
			Handle: participantRoute(eventLeft, 0)},
		// A device's status on the platform changes: the platform keeps what it last
		// heard for a device the world does not name, and posts device.status.changed
		// naming the room and the device by id.
		{Match: "POST /api/devices/status", Summary: "A device's status changes (posts device.status.changed)", Handle: deviceStatusRoute,
			Example: "{\"device\": \"zd-spare-bar\", \"room\": \"zr-boardroom-b\", \"status\": \"offline\"}\n"},
		// The batched report: one post naming every room on the account and its
		// state, as a platform's periodic digest does (read as records).
		{Match: "POST /api/rooms/report", Summary: "Post one batched state report naming every room (posts rooms.state)", Example: "{}\n",
			Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
				post(d, eventReport, reportBody(d, d.Now().Unix()))
				return ack()
			}},
	}
}

// text is a request field as the platform's templates wrote it: a string as itself,
// a number as Go prints it, a field the request lacks as <no value>.
func text(v any) string {
	if v == nil {
		return "<no value>"
	}
	return fmt.Sprint(v)
}

// truthy is whether a request field holds something (a template's `or` keeps the
// first value that does).
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	}
	return true
}

// orText is a field's text, or the default when it holds nothing.
func orText(v any, def string) string {
	if truthy(v) {
		return text(v)
	}
	return def
}

// unreadable says the body was sent and is not a JSON object.
func unreadable(r spec.HTTPRequest) bool {
	return r.Body == nil && len(bytes.TrimSpace(r.Raw)) > 0
}

// roomOf is the room a request names: a string that is a state path segment (letters,
// digits, - and _, at most 64), so it can name a key and never reach into another path.
func roomOf(body map[string]any, field string) (string, bool) {
	s, ok := body[field].(string)
	return s, ok && segmentPattern.MatchString(s)
}

// badSegment is the refusal of a name that is no state path segment.
func badSegment(field string) spec.HTTPResponse {
	return httpapi.Refuse(field + " must be a state path segment (letters, digits, - and _, at most 64)")
}

// textField is a body field that must be a string when it is present; ok is false
// when it holds another kind of value.
func textField(body map[string]any, field string) (string, bool) {
	v, present := body[field]
	if !present || v == nil {
		return "", true
	}
	s, ok := v.(string)
	return s, ok
}

// webhooksRoute subscribes a receiver: the url it posts to, how each post is signed,
// the secret, and a fault that makes the sender go wrong. Every field is checked
// before any is written.
func webhooksRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if unreadable(r) {
		return httpapi.Unreadable()
	}
	url, scheme, fault := text(r.Body["url"]), orText(r.Body["scheme"], "hmac"), orText(r.Body["fault"], "")
	var refused []httpapi.Invalid
	if !urlPattern.MatchString(url) {
		refused = append(refused, httpapi.Invalid{Path: pathTarget, Value: url, Pattern: urlSource})
	}
	if !contains(schemes, scheme) {
		refused = append(refused, httpapi.Invalid{Path: pathScheme, Value: scheme, Enum: schemes})
	}
	if !contains(faults, fault) {
		refused = append(refused, httpapi.Invalid{Path: pathFault, Value: fault, Enum: faults})
	}
	if len(refused) > 0 {
		return httpapi.Reject(refused...)
	}
	secret, ok := r.Body["secret"].(string)
	if truthy(r.Body["secret"]) && !ok {
		return httpapi.Refuse("secret must be " + "text")
	}
	if err := d.WriteMany(
		surface.Change{Path: pathTarget, Value: url}, surface.Change{Path: pathScheme, Value: scheme},
		surface.Change{Path: pathSecret, Value: secret}, surface.Change{Path: pathFault, Value: fault},
	); err != nil {
		return spec.Failure(500, err.Error())
	}
	return ack()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// addRoomRoute adds a room to the account. A room the account already knows keeps
// its state, which must be one a room is added in (a room that reads offline is not).
func addRoomRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if unreadable(r) {
		return httpapi.Unreadable()
	}
	id, ok := roomOf(r.Body, "id")
	if !ok {
		return badSegment("id")
	}
	state := "idle"
	if rooms, _ := d.Read(pathRooms).(map[string]any); rooms != nil {
		if room, _ := rooms[id].(map[string]any); room != nil {
			state = orText(room["state"], "idle")
		}
	}
	if !contains(states, state) {
		return httpapi.Reject(httpapi.Invalid{Path: roomPath(id, "state"), Value: state, Enum: states})
	}
	if err := d.Write(roomPath(id, "state"), state); err != nil {
		return spec.Failure(500, err.Error())
	}
	return ack()
}

// count reads a participants field as the platform's pattern does: a whole number up
// to three digits, in a number or in text, the default when the field holds nothing.
func count(v any, def int64) (int64, bool) {
	s := orText(v, strconv.FormatInt(def, 10))
	if !countPattern.MatchString(s) {
		return 0, false
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n, true
}

// startRoute starts a meeting in a room: its id, its headcount, its topic and its
// start, and the room reads in a call. Each field is checked before any is written, so
// a refused id or count never leaves a room reading in a call.
func startRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if unreadable(r) {
		return httpapi.Unreadable()
	}
	room, ok := roomOf(r.Body, "room")
	if !ok {
		return badSegment("room")
	}
	id, idOK := textField(r.Body, "id")
	if !idOK {
		return httpapi.Refuse("id must be " + "text")
	}
	topic, topicOK := textField(r.Body, "topic")
	if !topicOK {
		return httpapi.Refuse("topic must be " + "text")
	}
	participants, countOK := count(r.Body["participants"], 1)
	var refused []httpapi.Invalid
	if !idPattern.MatchString(id) {
		refused = append(refused, httpapi.Invalid{Path: roomPath(room, "meeting"), Value: text(r.Body["id"]), Pattern: idSource})
	}
	if !countOK {
		refused = append(refused, httpapi.Invalid{Path: roomPath(room, "participants"), Value: orText(r.Body["participants"], "1"), Pattern: countSource})
	}
	if !topicPattern.MatchString(topic) {
		refused = append(refused, httpapi.Invalid{Path: roomPath(room, "topic"), Value: orText(r.Body["topic"], ""), Pattern: topicSource})
	}
	if len(refused) > 0 {
		return httpapi.Reject(refused...)
	}
	now := d.Now().Unix()
	if err := d.WriteMany(
		surface.Change{Path: roomPath(room, "meeting"), Value: id}, surface.Change{Path: roomPath(room, "participants"), Value: participants},
		surface.Change{Path: roomPath(room, "topic"), Value: topic}, surface.Change{Path: roomPath(room, "start"), Value: now},
		surface.Change{Path: roomPath(room, "state"), Value: "in-call"},
	); err != nil {
		return spec.Failure(500, err.Error())
	}
	post(d, eventStarted, meetingBody(eventStarted, room, id, now))
	return ack()
}

// endRoute ends the meeting in a room and clears what it held.
func endRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if unreadable(r) {
		return httpapi.Unreadable()
	}
	room, ok := roomOf(r.Body, "room")
	if !ok {
		return badSegment("room")
	}
	if err := d.WriteMany(
		surface.Change{Path: roomPath(room, "state"), Value: "idle"}, surface.Change{Path: roomPath(room, "meeting"), Value: ""},
		surface.Change{Path: roomPath(room, "participants"), Value: int64(0)}, surface.Change{Path: roomPath(room, "topic"), Value: ""},
		surface.Change{Path: roomPath(room, "start"), Value: int64(0)},
	); err != nil {
		return spec.Failure(500, err.Error())
	}
	post(d, eventEnded, meetingBody(eventEnded, room, "", d.Now().Unix()))
	return ack()
}

// participantRoute is a participant joining (the headcount after defaulting to one)
// or leaving (to none): the headcount is the room's, and the platform posts it.
func participantRoute(event string, def int64) spec.HTTPHandler {
	return func(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
		if unreadable(r) {
			return httpapi.Unreadable()
		}
		room, ok := roomOf(r.Body, "room")
		if !ok {
			return badSegment("room")
		}
		n, ok := count(r.Body["participants"], def)
		if !ok {
			return httpapi.Reject(httpapi.Invalid{Path: roomPath(room, "participants"), Value: orText(r.Body["participants"], strconv.FormatInt(def, 10)), Pattern: countSource})
		}
		if err := d.Write(roomPath(room, "participants"), n); err != nil {
			return spec.Failure(500, err.Error())
		}
		post(d, event, participantBody(event, room, n, d.Now().Unix()))
		return ack()
	}
}

// deviceStatusRoute changes a device's status on the platform: the status and the
// time it was last seen, and the platform posts it naming the room and the device.
func deviceStatusRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if unreadable(r) {
		return httpapi.Unreadable()
	}
	device, ok := roomOf(r.Body, "device")
	if !ok {
		return badSegment("device")
	}
	status := text(r.Body["status"])
	if !contains(online, status) {
		return httpapi.Reject(httpapi.Invalid{Path: devicePath(device, "status"), Value: status, Enum: online})
	}
	now := d.Now().Unix()
	if err := d.WriteMany(surface.Change{Path: devicePath(device, "status"), Value: status}, surface.Change{Path: devicePath(device, "last_seen"), Value: now}); err != nil {
		return spec.Failure(500, err.Error())
	}
	post(d, eventDevice, deviceBody(r.Body["room"], device, status, now))
	return ack()
}
