package node

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A driver emit lands on any lane (#1145), node side: an event rides the
// batch's event lane and a component's log line its task_logs lane (never
// the self-log lane), an unclaimed message lands as a log line under the
// endpoint's listen task when the spec says unmatched: log, and a plain
// poll's events and log lines fire only when they change.

func TestBuildBatchCarriesEventsAndTaskLogs(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	dps := []collection.Sample{
		{Name: "video-input", Text: "hdmi2", IsText: true, TS: at},
		{Name: "input-changed", Lane: driver.LaneEvent, Text: "Input changed", IsText: true, Attributes: json.RawMessage(`{"input":"hdmi2"}`), TS: at},
		{Lane: driver.LaneLog, Text: "backlight driver reset", IsText: true, Severity: "warning", Facility: "panel", Source: "nvp", Attributes: json.RawMessage(`{"zone":"front"}`), TS: at},
	}
	ev := buildBatch("t1", "node-a", dps)
	if len(ev.GetProperties()) != 1 || len(ev.GetEvents()) != 1 || len(ev.GetTaskLogs()) != 1 {
		t.Fatalf("lanes = %d properties / %d events / %d task logs, want 1/1/1", len(ev.GetProperties()), len(ev.GetEvents()), len(ev.GetTaskLogs()))
	}
	if len(ev.GetLogs()) != 0 {
		t.Fatalf("a component's line rode the self-log lane: %+v", ev.GetLogs())
	}
	e := ev.GetEvents()[0]
	if e.GetName() != "input-changed" || e.GetMessage() != "Input changed" || string(e.GetPayload()) != `{"input":"hdmi2"}` || !e.GetTs().AsTime().Equal(at) {
		t.Fatalf("event = %+v", e)
	}
	l := ev.GetTaskLogs()[0]
	if l.GetMessage() != "backlight driver reset" || l.GetSeverity() != "warning" || l.GetFacility() != "panel" || l.GetSource() != "nvp" ||
		string(l.GetAttributes()) != `{"zone":"front"}` || !l.GetTs().AsTime().Equal(at) {
		t.Fatalf("task log = %+v", l)
	}
}

func TestSampleFromEmittedKeepsEveryLane(t *testing.T) {
	at := time.Now().UTC()
	for _, e := range []driver.Emitted{
		{Name: "uptime", Lane: driver.LaneMetric, Value: 7, TS: at},
		{Name: "video-input", Lane: driver.LaneProperty, Text: "hdmi1", IsText: true, TS: at},
		{Name: "input-changed", Lane: driver.LaneEvent, Text: "m", IsText: true, Attributes: json.RawMessage(`{}`), TS: at},
		{Lane: driver.LaneLog, Text: "m", IsText: true, Severity: "info", TS: at},
	} {
		s := collection.FromEmitted(e)
		wantLane := ""
		if e.Lane == driver.LaneEvent || e.Lane == driver.LaneLog {
			wantLane = e.Lane
		}
		if s.Lane != wantLane || s.Name != e.Name || s.Value != e.Value || s.Text != e.Text || s.IsText != e.IsText || s.Severity != e.Severity || !s.TS.Equal(at) {
			t.Fatalf("FromEmitted(%+v) = %+v", e, s)
		}
	}
}

func unmatchedLogTask(id string) collection.TaskSpec {
	task := listenTask(id, "ep-1", "10.0.0.5:51325")
	task.Spec = json.RawMessage(`{"driver":"newtron-nvp","function":"events","arm":["SUBSCRIBE INPUT"],
		"match":{"prefix":"EVT INPUT "}, "unmatched":"log",
		"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^EVT INPUT (\\S+)$"}}]}`)
	return task
}

// TestUnmatchedLogLandsEachUnclaimedMessage: under unmatched: log, every
// message no listener claims lands at once as a log line (the whole message,
// raw arrival) under the endpoint's first listen task, and is still counted
// for the tick's self-log.
func TestUnmatchedLogLandsEachUnclaimedMessage(t *testing.T) {
	c := newCapture()
	want, _ := desiredSessions([]collection.TaskSpec{unmatchedLogTask("l1")}, map[string]collection.SessionDialer{"tcp": &heldFakeDialer{}})
	w := want["ep-1"]
	if w.policy != driver.UnmatchedLog {
		t.Fatalf("policy = %q, want log", w.policy)
	}
	r := newListenerRouter(c.hooks(&transitionLog{}))
	r.set(w.listeners, w.policy)

	r.Unclaimed(driver.LineMessage("EVT SCENE presentation"))
	r.Unclaimed(driver.Message{Data: `{"a":1}`, Event: "status"})
	got := c.samplesOf("l1")
	if len(got) != 2 {
		t.Fatalf("landed %d log lines, want 2: %+v", len(got), got)
	}
	if got[0].Lane != driver.LaneLog || got[0].Text != "EVT SCENE presentation" || got[0].Name != "" {
		t.Fatalf("first log line = %+v", got[0])
	}
	if got[1].Text != `{"a":1}` || !strings.Contains(string(got[1].Attributes), `"event":"status"`) {
		t.Fatalf("an event's log line = %+v %s, want its data with the event name as an attribute", got[1], got[1].Attributes)
	}
	if n, _, policy := r.drain(); n != 2 || policy != driver.UnmatchedLog {
		t.Fatalf("drain = %d %q, want both still counted", n, policy)
	}
}

// TestUnmatchedDiscardLandsNothing: the default stays discard.
func TestUnmatchedDiscardLandsNothing(t *testing.T) {
	c := newCapture()
	want, _ := desiredSessions([]collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")}, map[string]collection.SessionDialer{"tcp": &heldFakeDialer{}})
	r := newListenerRouter(c.hooks(&transitionLog{}))
	r.set(want["ep-1"].listeners, want["ep-1"].policy)
	r.Unclaimed(driver.LineMessage("NOISE"))
	if got := c.samplesOf("l1"); len(got) != 0 {
		t.Fatalf("discard landed %+v", got)
	}
}

// TestAPlainPollFiresOccurrencesOnChange: a poll's event and log line land
// when their content differs from what that task last landed, not on the
// ticks between; metrics and properties pass through untouched (the
// property lane has its own transition guard); a task that leaves the
// worklist is forgotten.
func TestAPlainPollFiresOccurrencesOnChange(t *testing.T) {
	s := newPollSchedule(time.Second)
	t0 := time.Now().UTC()
	tick := func(msg string, at time.Time) []collection.Sample {
		return []collection.Sample{
			{Name: "uptime", Value: 1, TS: at},
			{Lane: driver.LaneLog, Text: msg, IsText: true, Severity: "error", TS: at},
			{Name: "fault-raised", Lane: driver.LaneEvent, Text: msg, IsText: true, TS: at},
		}
	}
	first := s.changed("p1", tick("lamp failed", t0))
	if len(first) != 3 {
		t.Fatalf("first run landed %d, want all 3", len(first))
	}
	s.commitChanged("p1", first)
	again := s.changed("p1", tick("lamp failed", t0.Add(time.Minute)))
	if len(again) != 1 || again[0].Name != "uptime" {
		t.Fatalf("an unchanged run landed %+v, want the metric alone", again)
	}
	s.commitChanged("p1", again)
	changed := s.changed("p1", tick("fan stalled", t0.Add(2*time.Minute)))
	if len(changed) != 3 {
		t.Fatalf("a changed run landed %d, want all 3", len(changed))
	}
	s.commitChanged("p1", changed)
	s.retain(map[string]bool{})
	if back := s.changed("p1", tick("fan stalled", t0.Add(3*time.Minute))); len(back) != 3 {
		t.Fatalf("a task the worklist dropped and returned landed %d, want all 3", len(back))
	}
}

// TestTwoNamelessOccurrencesKeepTheirOwnMemory (adversarial review, finding
// 3): two log lines from one poll share a name ("") and two events may share
// a type; each is remembered by its emit, so neither repeats on an unchanged
// run.
func TestTwoNamelessOccurrencesKeepTheirOwnMemory(t *testing.T) {
	s := newPollSchedule(time.Second)
	tick := func() []collection.Sample {
		return []collection.Sample{
			{Lane: driver.LaneLog, Text: "last error: lamp", IsText: true, Emit: 0},
			{Lane: driver.LaneLog, Text: "last warning: fan", IsText: true, Emit: 1},
		}
	}
	first := s.changed("p1", tick())
	s.commitChanged("p1", first)
	for run := 2; run <= 4; run++ {
		if again := s.changed("p1", tick()); len(again) != 0 {
			t.Fatalf("run %d of an unchanged poll landed %+v, want nothing", run, again)
		}
	}
}
