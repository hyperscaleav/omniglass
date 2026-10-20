package storage_test

import (
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestAlarmTimeline proves an alarm's timeline merges its events and its
// audit rows by time (#950): a rule alarm reads its fire event, the raise,
// the acknowledgement, its clear event and the clear, in that order (an
// event before the audit row it caused at the same instant), each naming
// the rule, or the person who acted. An alarm on another component, or an
// id that names no alarm, is not found.
func TestAlarmTimeline(t *testing.T) {
	r := newEdgeRig(t)
	editor := r.editor()
	r.rule(storage.EventRuleSpec{Name: "timed", Severity: "high", Message: "bar unreachable", ClearEventType: "condition-ended"})
	r.observe("tcp", "down")
	alarm := r.openAlarm("tcp")
	if _, err := r.f.gw.AcknowledgeAlarm(r.ctx, editor, "bar-1", alarm); err != nil {
		t.Fatal(err)
	}
	r.observe("tcp", "up")

	got, err := r.f.gw.AlarmTimeline(r.ctx, "bar-1", alarm)
	if err != nil {
		t.Fatal(err)
	}
	type step struct{ kind, what, actor, rule string }
	want := []step{
		{"event", "condition-started", "", "timed"},
		{"audit", "raise", "", "timed"},
		{"audit", "acknowledge", "root", ""},
		{"event", "condition-ended", "", "timed"},
		{"audit", "clear", "", "timed"},
	}
	if len(got) != len(want) {
		t.Fatalf("timeline = %+v, want %d entries", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.kind || g.What != w.what || g.Actor != w.actor || g.Rule != w.rule {
			t.Errorf("entry %d = %+v, want %+v", i, g, w)
		}
		if i > 0 && g.At.Before(got[i-1].At) {
			t.Errorf("entry %d at %s is before entry %d at %s", i, g.At, i-1, got[i-1].At)
		}
	}
	if got[0].Message != "bar unreachable" || got[0].Instance != "tcp" || got[1].Message != "bar unreachable" {
		t.Errorf("the fire event and the raise read %+v and %+v, want the rule's message on tcp", got[0], got[1])
	}

	if _, err := r.f.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "elsewhere"}, r.f.all, r.f.all, r.f.all, r.f.all); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ component, id string }{{"elsewhere", alarm}, {"bar-1", "not-a-uuid"}, {"bar-1", "01a15700-0000-7000-8000-000000000000"}} {
		if _, err := r.f.gw.AlarmTimeline(r.ctx, c.component, c.id); !errors.Is(err, storage.ErrAlarmNotFound) {
			t.Errorf("timeline of %s on %s: err %v, want ErrAlarmNotFound", c.id, c.component, err)
		}
	}
}
