package collection_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A poll run on demand can see what its fetch answered (#1163): the capture
// hook receives the payload before any emit reads it, so a preview shows the
// raw answer beside what was extracted from it, and a scheduled poll, which
// sets no hook, is unchanged.
func TestCollectDriverPollCapturesThePayload(t *testing.T) {
	getter := &fakeSNMPGetter{res: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal AirWall 3", "1.3.6.1.2.1.1.3.0": "8123456"}}
	r := &collection.Runner{SNMP: getter}
	task := snmpPollTask()
	var seen driver.Payload
	task.Capture = func(p driver.Payload) { seen = p }
	if _, faults := r.CollectDriverPoll(context.Background(), task); len(faults) != 0 {
		t.Fatalf("faults: %v", faults)
	}
	if seen.Values["1.3.6.1.2.1.1.1.0"] != "Boreal AirWall 3" {
		t.Fatalf("captured %+v, want the fetched values", seen)
	}

	line := &fakeLineExchanger{answer: "INPUT HDMI2"}
	lr := &collection.Runner{Line: line}
	lt := collection.DriverPollTask{Transport: "tcp", Target: "10.0.0.1:4998", Fn: &driver.BakedFunction{
		Driver: "d", Function: "status", Request: &driver.Request{Line: "GET INPUT"},
		Emits: []driver.BakedEmit{{Name: "video-input", Lane: "property", Extract: driver.Extract{Regex: `^INPUT (\S+)$`}}},
	}}
	var text string
	lt.Capture = func(p driver.Payload) { text = p.Text }
	lr.CollectDriverPoll(context.Background(), lt)
	if text != "INPUT HDMI2" {
		t.Fatalf("captured line %q", text)
	}
}

// The excerpt a preview shows is bounded and readable: an snmp answer as one
// line per oid in order, a text answer as itself, cut at the limit and saying
// so.
func TestPayloadExcerpt(t *testing.T) {
	got, cut := collection.PayloadExcerpt(driver.Payload{Values: map[string]string{"1.3.6.1.2.1.1.3.0": "812", "1.3.6.1.2.1.1.1.0": "Boreal"}}, 1024)
	if cut || got != "1.3.6.1.2.1.1.1.0 = Boreal\n1.3.6.1.2.1.1.3.0 = 812" {
		t.Errorf("snmp excerpt = %q (cut %v)", got, cut)
	}
	got, cut = collection.PayloadExcerpt(driver.Payload{Text: strings.Repeat("a", 50)}, 10)
	if !cut || got != strings.Repeat("a", 10) {
		t.Errorf("long text excerpt = %q (cut %v)", got, cut)
	}
	got, _ = collection.PayloadExcerpt(driver.Payload{Text: "héllo wörld"}, 3)
	if !json.Valid([]byte(`"`+got+`"`)) || strings.ContainsRune(got, '�') {
		t.Errorf("an excerpt cut mid-rune = %q, want it cut on a rune boundary", got)
	}
}

// A sample crosses the run wire as what it is: a metric's number, a
// property's text, an event's lane and message.
func TestRunSampleOf(t *testing.T) {
	ts := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	m := collection.RunSampleOf(collection.Sample{Name: "uptime", Value: 81.5, TS: ts})
	if m.Name != "uptime" || m.Value == nil || *m.Value != 81.5 || m.Text != "" || !m.TS.Equal(ts) {
		t.Errorf("metric = %+v", m)
	}
	p := collection.RunSampleOf(collection.Sample{Name: "model-number", Text: "VRoom", IsText: true})
	if p.Value != nil || p.Text != "VRoom" {
		t.Errorf("property = %+v", p)
	}
	e := collection.RunSampleOf(collection.Sample{Name: "button-pressed", Lane: "event", Text: "pressed"})
	if e.Lane != "event" || e.Text != "pressed" || e.Value != nil {
		t.Errorf("event = %+v", e)
	}
	// A record's identity (#1386) crosses too, so a preview shows which
	// owner each sample is about; an unidentified sample carries none.
	id := collection.RunSampleOf(collection.Sample{Name: "call-state", Text: "idle", IsText: true, Identity: &driver.Identity{Key: "ucc-room-id", Value: "r2"}})
	if id.Identity == nil || id.Identity.Key != "ucc-room-id" || id.Identity.Value != "r2" || p.Identity != nil {
		t.Errorf("identified = %+v, unidentified = %+v", id, p)
	}
	if got := collection.RunSubject("edge-hq"); got != "og.v1.run.edge-hq" {
		t.Errorf("RunSubject = %q", got)
	}
}
