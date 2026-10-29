package ingress_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
)

// A batched post (#1386): a listener reading records names each record's
// identity, and the batch the push lane carries puts it on every wire
// sample; the instance stays the endpoint's (the fan-out consumer decides a
// fanned sample's instance). A record whose identity misses lands nothing
// and its fault rides the raw line.

func roomListener() []*driver.BakedFunction {
	return []*driver.BakedFunction{{
		Driver: "ucc-webhook", Function: "rooms-changed",
		Match:    &driver.Match{Field: &driver.FieldMatch{Path: "event", Equals: "rooms.changed"}},
		Records:  "rooms",
		Identity: &driver.IdentitySpec{Key: "ucc-room-id", Extract: driver.Extract{JSONPath: "id"}},
		Emits: []driver.BakedEmit{
			{Name: "call-state", Lane: driver.LaneProperty, Extract: driver.Extract{JSONPath: "state"}},
			{Name: "people-count", Lane: driver.LaneMetric, Extract: driver.Extract{JSONPath: "people"}},
			{Name: "call-started", Lane: driver.LaneEvent, Message: &driver.Field{Value: "state read"}},
		},
	}}
}

func TestABatchedPostCarriesEachRecordsIdentity(t *testing.T) {
	body := `{"event":"rooms.changed","rooms":[{"id":"r1","state":"in-call","people":3},{"id":"r2","state":"idle","people":0}]}`
	r := ingress.Route(roomListener(), driver.WebhookMessage([]byte(body), driver.FormatJSON, nil), now)
	if len(r.Faults) != 0 || len(r.Samples) != 6 {
		t.Fatalf("route = %+v, want three samples per room and no fault", r)
	}
	b := ingress.Batch("comp-1", "webhook", r)
	if len(b.GetProperties()) != 2 || len(b.GetMetrics()) != 2 || len(b.GetEvents()) != 2 {
		t.Fatalf("batch = %v, want two of each lane", b)
	}
	state := map[string]string{}
	for _, p := range b.GetProperties() {
		if p.GetIdentity().GetKey() != "ucc-room-id" || p.GetInstance() != "webhook" {
			t.Fatalf("property %v, want identity ucc-room-id under the endpoint's instance", p)
		}
		state[p.GetIdentity().GetValue()] = p.GetValueJson()
	}
	if state["r1"] != `"in-call"` || state["r2"] != `"idle"` {
		t.Fatalf("states by room = %v", state)
	}
	for _, m := range b.GetMetrics() {
		if m.GetIdentity().GetKey() != "ucc-room-id" || m.GetIdentity().GetValue() == "" {
			t.Fatalf("metric %v carries no identity", m)
		}
	}
	for _, e := range b.GetEvents() {
		if e.GetIdentity().GetKey() != "ucc-room-id" || e.GetIdentity().GetValue() == "" {
			t.Fatalf("event %v carries no identity", e)
		}
	}
	if len(b.GetLogs()) != 1 || b.GetLogs()[0].GetMessage() != body {
		t.Fatalf("logs %v, want the post's raw line", b.GetLogs())
	}
}

func TestARecordFaultRidesTheRawLine(t *testing.T) {
	body := `{"event":"rooms.changed","rooms":[{"state":"in-call","people":3},{"id":"r2","state":"idle","people":0}]}`
	r := ingress.Route(roomListener(), driver.WebhookMessage([]byte(body), driver.FormatJSON, nil), now)
	if len(r.Faults) != 1 || len(r.Samples) != 3 {
		t.Fatalf("route = %+v, want room r2's three samples and one fault", r)
	}
	for _, s := range r.Samples {
		if s.Identity == nil || s.Identity.Value != "r2" {
			t.Fatalf("sample %+v, want only r2's", s)
		}
	}
	b := ingress.Batch("comp-1", "webhook", r)
	if len(b.GetLogs()) != 1 || !strings.Contains(string(b.GetLogs()[0].GetAttributes()), "record 1") {
		t.Fatalf("logs %v, want the raw line carrying the record's fault", b.GetLogs())
	}
	if b.GetLogs()[0].GetMessage() != body {
		t.Fatalf("raw line %q, want the post", b.GetLogs()[0].GetMessage())
	}
}
