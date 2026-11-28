//go:build linux

package sim

import (
	"testing"
	"time"
)

// The recording service's wire script (#2836): health, work and the paged
// recordings list behind its key, the failure route and the live feed it names
// the failure on, the drifting storage, and the recordings of the meetings the
// lab calendar says are recorded.

const recordingProduct = "recording-service"

func recordingScript() []wireStep {
	return []wireStep{
		wireGet("the health", "/api/health"),
		{Name: "the health with no key", Method: "GET", Path: "/api/health", NoKey: true},
		wireGet("the work", "/api/work"),
		wireGet("the recordings", "/api/recordings"),
		{Name: "the recordings with no key", Method: "GET", Path: "/api/recordings", NoKey: true},
		{Name: "an unknown path", Method: "GET", Path: "/nope"},
		{Name: "the root", Method: "GET", Path: "/"},
		{Name: "the wrong method on the health", Method: "POST", Path: "/api/health", Body: `{}`},
		{Name: "a head request on the health", Method: "HEAD", Path: "/api/health"},
		{Name: "the wrong method on the failure route", Method: "GET", Path: "/api/recordings/fail"},
		wirePost("a recording fails", "/api/recordings/fail", `{"id": "r-9001", "reason": "disk full"}`),
		wireGet("the work after a failure", "/api/work"),
		wirePost("another recording fails", "/api/recordings/fail", `{"id": "r-9002", "reason": "Lost the stream, retrying."}`),
		wirePost("an id of the longest", "/api/recordings/fail", `{"id": "r-12345678901234567890123456789", "reason": "edge of the id"}`),
		// The template stored an id that reads as a number or a flag as that
		// number or flag; the family keeps the text, so these steps do not hold
		// the state they leave to the recording (ADR-2836).
		{Name: "an id of digits as text", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "12345", "reason": "digits"}`},
		{Name: "an id that reads as a float", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "1e5", "reason": "exponent"}`},
		{Name: "an id that reads as a flag", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "true", "reason": "flag"}`},
		wirePost("a reason of the longest", "/api/recordings/fail", `{"id": "r-9005", "reason": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
		wireGet("the work at the end", "/api/work"),
		{Name: "a failure with no key", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "r-1", "reason": "x"}`, NoKey: true},
		// The refusals the profile answered with the template engine's error
		// text come last: the family refuses each with a 400 that changes nothing.
		{Name: "an id with a space", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "bad id!", "reason": "x"}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "an id too long", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "r-123456789012345678901234567890123", "reason": "x"}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "a reason with a quote", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "r-9003", "reason": "say \"no\""}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "a reason too long", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "r-9004", "reason": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "an empty id", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "", "reason": "no id"}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "an empty reason", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "r-9006", "reason": ""}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "no reason", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": "r-9007"}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "no id", Method: "POST", Path: "/api/recordings/fail", Body: `{"reason": "no id"}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "an empty object", Method: "POST", Path: "/api/recordings/fail", Body: `{}`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "no body", Method: "POST", Path: "/api/recordings/fail", Body: ``, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "an array body", Method: "POST", Path: "/api/recordings/fail", Body: `[1]`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "a text body", Method: "POST", Path: "/api/recordings/fail", Body: `r-1 failed`, Snapshot: true, Quirk: "the mutation pipeline refused it with the template engine's own error text"},
		{Name: "an id that is a number", Method: "POST", Path: "/api/recordings/fail", Body: `{"id": 77, "reason": "numeric id"}`, Snapshot: true, Quirk: "the template wrote the number into the id"},
	}
}

func recordingTicks() []tickPlan {
	return []tickPlan{
		{"room_recordings", []string{"status.recordings.in_progress", "status.recordings.queue_depth", "status.recordings.rooms"}},
		{"storage_drift", []string{"status.storage.used_percent"}},
	}
}

var recordingWatch = []string{"status.recordings.in_progress", "status.recordings.queue_depth", "status.recordings.rooms", "status.storage.used_percent"}

func recordingRoomDay(d *Device, at *time.Time) []wireChange {
	w := newWatch(d, recordingWatch...)
	w.sample("start")
	n := 0
	roomDayWalk(d, at, w, "07:00:00", "15:00:00", 60*time.Second, func(string) {
		d.TickBehavior("room_recordings")
		if n++; n%5 == 0 {
			d.TickBehavior("storage_drift")
		}
	}, func(string) {})
	return w.out
}

func recordingExtra(t *testing.T, fam wireFamily, product string) map[string]any {
	w, wcal := parityWorld("d-1", "recording-service", parityCovers())
	d, at := serviceDevice(t, fam, product, w, wcal)
	*at = parityAt("10:05:00")
	d.TickBehavior("room_recordings")
	pages := runWireHTTP(t, d, []wireStep{
		wireGet("the recordings", "/api/recordings"),
		wireGet("pages of one", "/api/recordings?page_size=1"),
		wireGet("the second page", "/api/recordings?page_size=1&next_page_token=sys-a"),
		wireGet("the last page", "/api/recordings?page_size=1&next_page_token=sys-b"),
		wireGet("past the last page", "/api/recordings?page_size=1&next_page_token=sys-c"),
		wireGet("pages of two", "/api/recordings?page_size=2"),
		wireGet("a size that is no number", "/api/recordings?page_size=many"),
		wireGet("a size past the most", "/api/recordings?page_size=9999"),
		wireGet("a token nobody issued", "/api/recordings?next_page_token=sys-aa&page_size=1"),
		wireGet("the work", "/api/work"),
	})
	// The live feed: a failure is named the moment it happens, and a reconnect
	// replays from the event it names.
	feed := wireDevice(t, fam, product)
	failed := wireStream(t, feed, "/api/events", func() {
		runWireHTTP(t, feed, []wireStep{
			wirePost("a recording fails", "/api/recordings/fail", `{"id": "r-9001", "reason": "disk full"}`),
			wirePost("a refused failure is not named", "/api/recordings/fail", `{"id": "bad id!", "reason": "x"}`),
			wirePost("another fails", "/api/recordings/fail", `{"id": "r-9002", "reason": "lost the stream"}`),
		})
	})
	return map[string]any{
		"pages":  pages,
		"stream": failed,
		"room_day_world": serviceWorldDay(t, fam, product, "recording-service", recordingWatch,
			func(d *Device) { d.TickBehavior("room_recordings") }, time.Minute),
	}
}

var recordingWire = wireFamily{
	Name: "recording-service", Products: []string{recordingProduct}, Engine: shippedEngineOf, Source: wireYAMLSource, Frozen: wireYAMLFrozen,
	Script: recordingScript(), Ticks: recordingTicks(), RoomDay: recordingRoomDay, Extra: recordingExtra,
}

func init() { wireFamilies = append(wireFamilies, recordingWire) }
