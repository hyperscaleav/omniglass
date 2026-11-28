// Package recordingservice is the Shinra recording service (#2836), a Go device
// family of one: the central service behind every room's recordings. It takes a
// stream in, writes it to storage and queues what is waiting to be written. It is
// not a box in a room; an operator watches it for whether it is up and healthy by
// its own account, how much it is carrying, whether its storage is filling, and
// when a recording fails.
//
// Its one face is an HTTPS JSON API behind an api key header (X-Recording-Key), the
// way a software service is watched: a health route and a work route to poll, a
// paged list of the rooms it covers, and a live event feed that names a recording
// the moment it fails.
//
// In a lab with a calendar its work is the lab's (behave.Recordings): it records the
// meetings the calendar says are recorded in the rooms it speaks for, or in its own
// room when it sits in one, and the recordings in progress and the queue of those
// just finished are read off the calendar. Without one it drifts on its own. It is
// one account-wide service, the way a real recording platform is: GET
// /api/recordings lists the rooms it covers, one record each (ADR-1282), its id the
// system the room belongs to; a driver walks the pages once and each record's values
// land on the system its room id is bound to.
//
// The family replaced the recording-service YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package recordingservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
)

// Name is the family's id, the profile name its devices carry: the product it is
// a family of one for.
const Name = "recording-service"

// Row is one model of the service.
type Row struct {
	// Product is the catalog product the row simulates, Version the software
	// it reports.
	Product, Version string
}

// Rows is the model table: the recording service, one model.
var Rows = []Row{{Product: Name, Version: "2026.10.2"}}

// The state paths of the service.
const (
	pathBootEpoch = "status.device.boot_epoch"
	pathUptime    = "status.device.uptime"
	pathHealth    = "status.service.state"
	pathInProg    = "status.recordings.in_progress"
	pathQueue     = "status.recordings.queue_depth"
	pathRooms     = "status.recordings.rooms"
	pathFailID    = "status.recordings.last_failure.id"
	pathFailWhy   = "status.recordings.last_failure.reason"
	pathStorage   = "status.storage.used_percent"
	pathVersion   = "configuration.software.version"
)

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device":  map[string]any{"boot_epoch": 0},
			"service": map[string]any{"state": "ok"},
			"recordings": map[string]any{
				"in_progress": 4, "queue_depth": 3, "rooms": map[string]any{},
				"last_failure": map[string]any{"id": "", "reason": ""},
			},
			"storage": map[string]any{"used_percent": 38.5},
		},
		"configuration": map[string]any{"software": map[string]any{"version": r.Version}},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Shinra Cloud", Model: "Recording Service", DeviceType: "cloud-service",
			Summary: "Shinra recording service: health, work counters and a failure feed over https",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model { return spec.Model{Product: r.Product, Model: "Recording Service"} }),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 443, TLS: true, APISurface: "recording-service",
				Doc:  profile.Doc{Summary: "Recording service JSON API", Tags: []string{"management", "api", "cloud"}},
				Auth: profile.Auth{Type: "header", Header: "X-Recording-Key", APIKey: "recording-lab-key", Enforce: true},
				// The live feed a recording's failure arrives on, behind the same key.
				Events: profile.Events{Path: "/api/events", RetryMS: 2000, Paths: []string{"status.recordings.last_failure"}, DropPath: "/api/admin/streams/drop"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: stateFor(Rows[0]),
		HTTP: []spec.HTTPRoute{
			{Match: "GET /api/health", Summary: "Service status, version and uptime", Handle: healthRoute},
			{Match: "GET /api/work", Summary: "Recordings in progress, storage used, queue depth and the last failure", Handle: workRoute},
			{Match: "GET /api/recordings", Summary: "Every room the service covers with its recordings, one record each, paged", Handle: recordingsRoute},
			{Match: "POST /api/recordings/fail", Summary: "Fail a recording (records it and names it on the event feed)", Example: "{\"id\": \"r-9001\", \"reason\": \"disk full\"}\n", Handle: failRoute},
		},
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks: []spec.Tick{
			recordingsTick(),
			// Storage fills slowly between bounds on its own.
			bind.Drift("storage_drift", bind.Seconds(300), pathStorage, behave.Band{Lo: 20, Hi: 90, Step: 0.5}, behave.Places(1)),
		},
	}
}

func jsonRoute(doc any) spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(doc)}
}

func healthRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	return jsonRoute(struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Uptime  int64  `json:"uptime"`
	}{spec.String(d, pathHealth), spec.String(d, pathVersion), spec.Int64(d, pathUptime)})
}

func workRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	type failure struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	return jsonRoute(struct {
		InProgress  int64   `json:"recordings_in_progress"`
		QueueDepth  int64   `json:"queue_depth"`
		StoragePct  float64 `json:"storage_used_percent"`
		LastFailure failure `json:"last_failure"`
	}{spec.Int64(d, pathInProg), spec.Int64(d, pathQueue), spec.Float(d, pathStorage), failure{spec.String(d, pathFailID), spec.String(d, pathFailWhy)}})
}

// recordingsRoute lists the covered rooms as a paged list, one record each: the
// room's id (the system's name) with its own recordings in progress and queue depth.
func recordingsRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	rooms, _ := d.Read(pathRooms).(map[string]any)
	ids := make([]string, 0, len(rooms))
	for id := range rooms {
		ids = append(ids, id)
	}
	page, next := httpapi.Cut(ids, httpapi.Cloud.After(r.Query), httpapi.Cloud.Size(r.Query))
	records := make([]string, len(page))
	for i, id := range page {
		room, _ := rooms[id].(map[string]any)
		records[i] = fmt.Sprintf(`{"id": %s, "in_progress": %d, "queue_depth": %d}`, quote(id), spec.Whole(room["in_progress"]), spec.Whole(room["queue_depth"]))
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.PagedDocument("recordings", records, 2, next)}
}

// quote is a string as the document writes it, markup escaped.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// What a failure's id and reason may be.
var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
	reasonPattern = regexp.MustCompile(`^[A-Za-z0-9 ,.-]{1,64}$`)
)

const (
	idSource     = "^[A-Za-z0-9-]{1,32}$"
	reasonSource = "^[A-Za-z0-9 ,.-]{1,64}$"
)

// failRoute fails a recording: the failure is recorded as the last one and named
// on the live feed with the recording's id and why. The id and the reason are the
// caller's to choose, so a test or a demo names the recording it fails. Each is
// checked against its pattern first and a refused request changes nothing, so a
// refused reason never leaves a failure's id written beside the one before it.
func failRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	if r.Body == nil && len(bytes.TrimSpace(r.Raw)) > 0 {
		return httpapi.Unreadable()
	}
	id, idText := r.Body["id"].(string)
	reason, reasonText := r.Body["reason"].(string)
	var refused []httpapi.Invalid
	if (idText || r.Body["id"] == nil) && !idPattern.MatchString(id) {
		refused = append(refused, httpapi.Invalid{Path: pathFailID, Value: httpapi.FieldText(r.Body["id"]), Pattern: idSource})
	}
	if (reasonText || r.Body["reason"] == nil) && !reasonPattern.MatchString(reason) {
		refused = append(refused, httpapi.Invalid{Path: pathFailWhy, Value: httpapi.FieldText(r.Body["reason"]), Pattern: reasonSource})
	}
	if len(refused) > 0 {
		return httpapi.Reject(refused...)
	}
	if !idText {
		return httpapi.Refuse("id must be " + "text")
	}
	if !reasonText {
		return httpapi.Refuse("reason must be " + "text")
	}
	if err := d.WriteMany(surface.Change{Path: pathFailID, Value: id}, surface.Change{Path: pathFailWhy, Value: reason}); err != nil {
		return spec.Failure(500, err.Error())
	}
	d.Publish(spec.Event{Name: "recording-failed", Data: fmt.Sprintf(`{"id":"%s","reason":"%s"}`, id, reason)})
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Spaced(struct {
		OK bool `json:"ok"`
	}{true})}
}

// recordingsTick is the work: a recording is in progress while a recorded meeting
// runs in a room the service reads, and queued after it ends, waiting to be
// written. The service reads the rooms it covers, else the room it sits in; with no
// calendar it drifts. The rooms it lists, one record each, are the rooms it speaks
// for, each with its own recording and queue.
func recordingsTick() spec.Tick {
	return spec.Tick{
		Name: "room_recordings", Interval: bind.Seconds(60), OnStart: true,
		Writes: []string{pathInProg, pathQueue, pathRooms},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			world := spec.RoomsOf(d)
			var covered []behave.RecordingRoom
			for _, r := range world.Covered() {
				covered = append(covered, behave.RecordingRoom{State: world.State(r.Calendar())})
			}
			prev := behave.RecordingCounts{InProgress: int(spec.Int64(d, pathInProg)), QueueDepth: int(spec.Int64(d, pathQueue))}
			counts := behave.Recordings(prev, covered, c.Room, behave.DefaultRecordings, c.Rand)
			out := []surface.Change{{Path: pathInProg, Value: int64(counts.InProgress)}, {Path: pathQueue, Value: int64(counts.QueueDepth)}}
			if served := world.Served(); len(served) > 0 {
				var read []behave.RecordingRoom
				for _, r := range served {
					id := r.System
					if id == "" {
						id = r.Room
					}
					read = append(read, behave.RecordingRoom{Name: id, State: world.State(r.Calendar())})
				}
				records := map[string]any{}
				for _, r := range behave.PerRoom(read) {
					records[r.Name] = map[string]any{"in_progress": int64(r.InProgress), "queue_depth": int64(r.QueueDepth)}
				}
				out = append(out, surface.Change{Path: pathRooms, Value: records})
			}
			return out
		},
	}
}
