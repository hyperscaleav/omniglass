package ingress_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
)

// meetingListeners is the proving driver's pair, baked: a started meeting
// lands call-started and the call-state property, an ended one call-ended
// without its raw line (log: false).
func meetingListeners() []*driver.BakedFunction {
	no := false
	return []*driver.BakedFunction{
		{
			Driver: "ucc-webhook", Function: "meeting-started",
			Match: &driver.Match{Field: &driver.FieldMatch{Path: "event", Equals: "meeting.started"}},
			Emits: []driver.BakedEmit{
				{Name: "call-started", Lane: driver.LaneEvent, Message: &driver.Field{Value: "Meeting started"},
					Attributes: map[string]driver.Field{"meeting": {Extract: &driver.Extract{JSONPath: "meeting"}}}},
				{Name: "call-state", Lane: "property", Extract: driver.Extract{JSONPath: "event"},
					Transform: &driver.Transform{Map: map[string]string{"meeting.started": "active"}}},
			},
		},
		{
			Driver: "ucc-webhook", Function: "meeting-ended",
			Match: &driver.Match{Header: &driver.HeaderMatch{Name: "X-UCC-Event", Equals: "meeting.ended"}},
			Emits: []driver.BakedEmit{{Name: "call-ended", Lane: driver.LaneEvent, Message: &driver.Field{Value: "Meeting ended"}}},
			Log:   &no,
		},
	}
}

func TestAClaimedPostLandsItsEmitsAndItsRawLine(t *testing.T) {
	body := `{"event":"meeting.started","meeting":"m-7"}`
	msg := driver.WebhookMessage([]byte(body), driver.FormatJSON, nil)
	r := ingress.Route(meetingListeners(), msg, now)
	if r.Listener != "meeting-started" || len(r.Faults) != 0 {
		t.Fatalf("route = %+v, want claimed by meeting-started, no fault", r)
	}
	b := ingress.Batch("comp-1", "webhook", r)
	if b.GetOwner().GetRef() != "comp-1" || b.GetOwner().GetKind() != "component" || b.GetSource() != "webhook" {
		t.Fatalf("batch owner/source = %v/%q, want the endpoint's component on the push lane", b.GetOwner(), b.GetSource())
	}
	if len(b.GetEvents()) != 1 || b.GetEvents()[0].GetName() != "call-started" || b.GetEvents()[0].GetInstance() != "webhook" {
		t.Fatalf("events = %v, want call-started under the endpoint", b.GetEvents())
	}
	var attrs map[string]string
	if err := json.Unmarshal(b.GetEvents()[0].GetPayload(), &attrs); err != nil || attrs["meeting"] != "m-7" {
		t.Fatalf("call-started payload = %s, want the meeting id", b.GetEvents()[0].GetPayload())
	}
	if len(b.GetProperties()) != 1 || b.GetProperties()[0].GetValueJson() != `"active"` || b.GetProperties()[0].GetInstance() != "webhook" {
		t.Fatalf("properties = %v, want call-state active", b.GetProperties())
	}
	// Decision 5: a claimed message also lands its raw log line, naming the
	// listener that claimed it.
	if len(b.GetLogs()) != 1 || b.GetLogs()[0].GetMessage() != body || b.GetLogs()[0].GetInstance() != "webhook" {
		t.Fatalf("logs = %v, want the raw body once", b.GetLogs())
	}
	if !strings.Contains(string(b.GetLogs()[0].GetAttributes()), `"listener":"meeting-started"`) {
		t.Fatalf("raw line attributes = %s, want the claiming listener", b.GetLogs()[0].GetAttributes())
	}
}

func TestALogFalseListenerLandsOnlyItsEmits(t *testing.T) {
	msg := driver.WebhookMessage([]byte(`{"meeting":"m-7"}`), driver.FormatJSON, map[string][]string{"X-Ucc-Event": {"meeting.ended"}})
	b := ingress.Batch("comp-1", "webhook", ingress.Route(meetingListeners(), msg, now))
	if len(b.GetEvents()) != 1 || b.GetEvents()[0].GetName() != "call-ended" || len(b.GetLogs()) != 0 {
		t.Fatalf("batch = %v, want call-ended alone (log: false)", b)
	}
}

// What no listener claims lands as a log line on a push transport, whole;
// a bare endpoint (no listeners) lands every post that way.
func TestAnUnclaimedPostLandsAsALogLine(t *testing.T) {
	msg := driver.WebhookMessage([]byte(`{"event":"room.cleaned"}`), driver.FormatJSON, nil)
	for name, fns := range map[string][]*driver.BakedFunction{"unclaimed": meetingListeners(), "bare": nil} {
		r := ingress.Route(fns, msg, now)
		b := ingress.Batch("comp-1", "webhook", r)
		if r.Listener != "" || len(b.GetLogs()) != 1 || b.GetLogs()[0].GetMessage() != `{"event":"room.cleaned"}` || len(b.GetEvents()) != 0 {
			t.Fatalf("%s: batch = %v, want the post as one log line", name, b)
		}
	}
}

// A claimed post whose emit cannot extract still lands what did, and its raw
// line carries the fault, even under log: false: an operator sees why the
// post mapped to less than the driver says.
func TestAFaultRidesTheRawLine(t *testing.T) {
	no := false
	fns := []*driver.BakedFunction{{
		Driver: "d", Function: "f", Match: &driver.Match{Prefix: "{"}, Log: &no,
		Emits: []driver.BakedEmit{{Name: "call-state", Lane: "property", Extract: driver.Extract{JSONPath: "missing"}}},
	}}
	r := ingress.Route(fns, driver.WebhookMessage([]byte(`{"a":1}`), driver.FormatJSON, nil), now)
	b := ingress.Batch("comp-1", "webhook", r)
	if len(r.Faults) != 1 || len(b.GetLogs()) != 1 || !strings.Contains(string(b.GetLogs()[0].GetAttributes()), "fault") {
		t.Fatalf("route = %+v, batch logs = %v; want the fault on the raw line", r, b.GetLogs())
	}
}
