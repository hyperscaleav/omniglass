package driver_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// Records and identity (#1386, ADR-1282): a function may read an array of
// records out of a JSON body, its emits and its identity once per record, and
// name each record's identity (a match key and the value its extract reads).
// Every metric, property and event a record yields carries the identity; a
// record whose identity misses lands nothing and is one fault; a log line
// never carries one.

const roomList = `{"rooms":[{"id":"r1","state":"in-call"},{"id":"r2","state":"idle"}]}`

func roomsFunction() *driver.BakedFunction {
	return &driver.BakedFunction{
		Driver: "ucc-api", Function: "rooms",
		Request:  &driver.Request{Path: "/rooms"},
		Records:  "rooms",
		Identity: &driver.IdentitySpec{Key: "ucc-room-id", Extract: driver.Extract{JSONPath: "id"}},
		Emits:    []driver.BakedEmit{{Name: "call-state", Lane: driver.LaneProperty, Extract: driver.Extract{JSONPath: "state"}}},
	}
}

func jsonPayload(body string) driver.Payload {
	return driver.HTTPPayload([]byte(body), &driver.Request{})
}

func TestRecordsYieldOneIdentifiedSamplePerRecord(t *testing.T) {
	emitted, faults := roomsFunction().Interpret(jsonPayload(roomList), time.Now())
	if len(faults) != 0 {
		t.Fatalf("faults %v", faults)
	}
	if len(emitted) != 2 {
		t.Fatalf("emitted %+v, want one call-state per room", emitted)
	}
	want := map[string]string{"r1": "in-call", "r2": "idle"}
	for _, e := range emitted {
		if e.Identity == nil || e.Identity.Key != "ucc-room-id" {
			t.Fatalf("emitted %+v carries no ucc-room-id identity", e)
		}
		if e.Name != "call-state" || e.Lane != driver.LaneProperty || e.Text != want[e.Identity.Value] {
			t.Fatalf("emitted %+v, want room %s's state %q", e, e.Identity.Value, want[e.Identity.Value])
		}
		delete(want, e.Identity.Value)
	}
	if len(want) != 0 {
		t.Fatalf("rooms %v landed nothing", want)
	}
}

func TestARecordWhoseIdentityMissesLandsNothing(t *testing.T) {
	fn := roomsFunction()
	fn.Emits = append(fn.Emits,
		driver.BakedEmit{Name: "call-started", Lane: driver.LaneEvent, Message: &driver.Field{Value: "state read"}},
		driver.BakedEmit{Lane: driver.LaneLog, Message: &driver.Field{Extract: &driver.Extract{JSONPath: "state"}}},
	)
	body := `{"rooms":[{"state":"in-call"},{"id":"r2","state":"idle"}]}`
	emitted, faults := fn.Interpret(jsonPayload(body), time.Now())
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "record 1") || !strings.Contains(faults[0].Error(), "ucc-room-id") {
		t.Fatalf("faults %v, want one naming record 1 and its identity key", faults)
	}
	var samples, logs int
	for _, e := range emitted {
		if e.Lane == driver.LaneLog {
			logs++
			if e.Identity != nil {
				t.Fatalf("a log line carries an identity: %+v", e)
			}
			continue
		}
		samples++
		if e.Identity == nil || e.Identity.Value != "r2" {
			t.Fatalf("emitted %+v, want only room r2's samples", e)
		}
	}
	if samples != 2 {
		t.Fatalf("samples %d, want r2's property and event only", samples)
	}
	// Log emits never fan out, so the unidentified record's line still lands.
	if logs != 2 {
		t.Fatalf("log lines %d, want one per record, the unidentified one's too", logs)
	}
}

func TestALogEmitInAnIdentifiedFunctionCarriesNoIdentity(t *testing.T) {
	fn := roomsFunction()
	fn.Records = ""
	fn.Emits = []driver.BakedEmit{
		{Name: "call-state", Lane: driver.LaneProperty, Extract: driver.Extract{JSONPath: "state"}},
		{Lane: driver.LaneLog},
	}
	emitted, faults := fn.Interpret(jsonPayload(`{"id":"r9","state":"idle"}`), time.Now())
	if len(faults) != 0 || len(emitted) != 2 {
		t.Fatalf("emitted %+v faults %v", emitted, faults)
	}
	for _, e := range emitted {
		switch e.Lane {
		case driver.LaneLog:
			if e.Identity != nil {
				t.Fatalf("log line %+v carries an identity", e)
			}
		default:
			if e.Identity == nil || *e.Identity != (driver.Identity{Key: "ucc-room-id", Value: "r9"}) {
				t.Fatalf("property %+v, want identity ucc-room-id=r9 (read once per payload with no records)", e)
			}
		}
	}
}

func TestRecordsThatDoNotLocateAreOneFault(t *testing.T) {
	for name, body := range map[string]string{
		"missing path": `{"spaces":[]}`,
		"not an array": `{"rooms":{"id":"r1"}}`,
		"not json":     `rooms: r1`,
	} {
		t.Run(name, func(t *testing.T) {
			emitted, faults := roomsFunction().Interpret(jsonPayload(body), time.Now())
			if len(emitted) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "records") {
				t.Fatalf("emitted %+v faults %v, want nothing and one fault naming the records", emitted, faults)
			}
		})
	}
	emitted, faults := roomsFunction().Interpret(jsonPayload(`{"rooms":[]}`), time.Now())
	if len(emitted) != 0 || len(faults) != 0 {
		t.Fatalf("an empty array: emitted %+v faults %v, want nothing and no fault", emitted, faults)
	}
}

func TestARecordIsItsOwnPayload(t *testing.T) {
	// A regex reads the record's compact JSON, not the whole body.
	fn := roomsFunction()
	fn.Emits = []driver.BakedEmit{{Name: "call-state", Lane: driver.LaneProperty, Extract: driver.Extract{Regex: `"state":"([a-z-]+)"`}}}
	emitted, faults := fn.Interpret(jsonPayload(`{"rooms":[ {"id": "r1", "state": "in-call"}, {"id":"r2","state":"idle"} ]}`), time.Now())
	if len(faults) != 0 || len(emitted) != 2 || emitted[0].Text != "in-call" || emitted[1].Text != "idle" {
		t.Fatalf("emitted %+v faults %v, want each record's own state", emitted, faults)
	}
}

func TestAnIdentityValueIsBounded(t *testing.T) {
	for name, id := range map[string]string{
		"empty":         `""`,
		"blank":         `"   "`,
		"too long":      `"` + strings.Repeat("a", 257) + `"`,
		"control char":  `"r1\u0007"`,
		"non-scalar id": `{"x":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"rooms":[{"id":` + id + `,"state":"idle"}]}`
			emitted, faults := roomsFunction().Interpret(jsonPayload(body), time.Now())
			if len(emitted) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "identity") {
				t.Fatalf("emitted %+v faults %v, want nothing and an identity fault", emitted, faults)
			}
		})
	}
	emitted, faults := roomsFunction().Interpret(jsonPayload(`{"rooms":[{"id":"  r1 ","state":"idle"},{"id":12,"state":"idle"}]}`), time.Now())
	if len(faults) != 0 || len(emitted) != 2 || emitted[0].Identity.Value != "r1" || emitted[1].Identity.Value != "12" {
		t.Fatalf("emitted %+v faults %v, want the trimmed id and a numeric id rendered", emitted, faults)
	}
}

func TestAFunctionWithoutIdentityBakesUnchanged(t *testing.T) {
	sched := driver.Schedule{Every: "30s"}
	fn := driver.BakedFunction{Driver: "d", Version: "1", Function: "f", Schedule: &sched, Request: &driver.Request{Path: "/x"},
		Emits: []driver.BakedEmit{{Name: "uptime", Lane: driver.LaneMetric, Extract: driver.Extract{JSONPath: "u"}}}}
	b, err := json.Marshal(fn)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"driver":"d","version":"1","function":"f","schedule":{"every":"30s"},"request":{"path":"/x"},"emits":[{"name":"uptime","lane":"metric","extract":{"jsonpath":"u"}}]}`
	if string(b) != want {
		t.Fatalf("baked\n %s\nwant\n %s", b, want)
	}
	fn.Records, fn.Identity = "rooms", &driver.IdentitySpec{Key: "ucc-room-id", Extract: driver.Extract{JSONPath: "id"}}
	b, _ = json.Marshal(fn)
	back, err := driver.ParseBaked(b)
	if err != nil || back.Records != "rooms" || back.Identity == nil || back.Identity.Key != "ucc-room-id" || back.Identity.Extract.JSONPath != "id" {
		t.Fatalf("an identified function round-trips to %+v (%v)", back, err)
	}
}

// identityCatalog adds the identity keys the refusals read: a registered
// property (ucc-room-id), a metric, and an unregistered name.
func identityCatalog() fakeCatalog {
	c := httpCatalog()
	c.lanes["ucc-room-id"] = "property"
	c.lanes["call-state"] = "property"
	c.lanes["call-started"] = "event"
	c.lanes["call-ended"] = "event"
	c.secrets["hmac-key"] = true
	return c
}

func identifiedHTTPSpec() *driver.Spec {
	s := httpSpec()
	s.Polls = append(s.Polls, driver.Poll{
		Name: "rooms", Schedule: driver.Schedule{Every: "30s"},
		Request:  driver.Request{Path: "/rooms"},
		Records:  "rooms",
		Identity: &driver.IdentitySpec{Key: "ucc-room-id", Extract: driver.Extract{JSONPath: "id"}},
		Emits:    []driver.Emit{{Name: "call-state", Extract: driver.Extract{JSONPath: "state"}}},
	})
	return s
}

func identifiedWebhookSpec() *driver.Spec {
	s := webhookSpec()
	s.Listeners[0].Records = "rooms"
	s.Listeners[0].Identity = &driver.IdentitySpec{Key: "ucc-room-id", Extract: driver.Extract{JSONPath: "room"}}
	return s
}

func TestAnIdentifiedSpecValidates(t *testing.T) {
	if err := identifiedHTTPSpec().Validate(identityCatalog()); err != nil {
		t.Fatalf("identified http poll refused: %v", err)
	}
	if err := identifiedWebhookSpec().Validate(identityCatalog()); err != nil {
		t.Fatalf("identified webhook listener refused: %v", err)
	}
	// Structure alone without a catalog.
	if err := identifiedHTTPSpec().Validate(nil); err != nil {
		t.Fatalf("identified http poll refused without a catalog: %v", err)
	}
}

func TestIdentityAndRecordsRefusals(t *testing.T) {
	cases := []struct {
		name string
		spec func() *driver.Spec
		want string
	}{
		{"an unregistered key", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Identity.Key = "zoom-room-id"
			return s
		}, "zoom-room-id"},
		{"a metric key", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Identity.Key = "volume"
			return s
		}, "property"},
		{"an event key", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Identity.Key = "call-started"
			return s
		}, "property"},
		{"a reserved key", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Identity.Key = "collection-failed"
			return s
		}, "collection-failed"},
		{"a calc-owned key", func() *driver.Spec {
			s := identifiedHTTPSpec()
			return s
		}, "calc"},
		{"no key", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Identity.Key = ""
			return s
		}, "identity"},
		{"no extract", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Identity.Extract = driver.Extract{}
			return s
		}, "identity"},
		{"an identity read by oid over http", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Identity.Extract = driver.Extract{OID: "1.3.6"}
			return s
		}, "identity"},
		{"an identity read by key off a json webhook", func() *driver.Spec {
			s := identifiedWebhookSpec()
			s.Listeners[0].Identity.Extract = driver.Extract{Key: "room"}
			return s
		}, "identity"},
		{"records over xml", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Request.Format = driver.FormatXML
			s.Polls[3].Identity.Extract = driver.Extract{XPath: "//id"}
			s.Polls[3].Emits[0].Extract = driver.Extract{XPath: "//state"}
			return s
		}, "records"},
		{"records on a line transport", func() *driver.Spec {
			s := validSpec()
			s.Polls[0].Records = "rooms"
			return s
		}, "records"},
		{"records on a form webhook", func() *driver.Spec {
			s := identifiedWebhookSpec()
			s.Session.Webhook.Format = driver.FormatForm
			s.Listeners[0].Match = driver.Match{Header: &driver.HeaderMatch{Name: "X-UCC-Event", Equals: "meeting.started"}}
			s.Listeners[0].Identity.Extract = driver.Extract{Key: "room"}
			s.Listeners[0].Emits = []driver.Emit{{Name: "call-started", Message: &driver.Field{Value: "Meeting started"}}}
			return s
		}, "records"},
		{"records with an empty step", func() *driver.Spec {
			s := identifiedHTTPSpec()
			s.Polls[3].Records = "data..rooms"
			return s
		}, "records"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := identityCatalog()
			if tc.name == "a calc-owned key" {
				cat.calcOwned = map[string]bool{"ucc-room-id": true}
			}
			err := tc.spec().Validate(cat)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}
