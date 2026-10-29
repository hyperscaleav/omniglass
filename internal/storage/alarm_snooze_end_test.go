package storage_test

import (
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The end of a snooze tells people again about a condition that is still
// raised (#1318, ADR-1318): at the deadline, or by :unsnooze, the snooze is
// deleted and, if an alarm of the condition is still open, it is announced
// again as raised, judged against the rules as they stand then. The end is
// audited (snooze-end from the platform, unsnooze naming the actor) just
// before the new rows. Replicas sweeping one expired snooze announce it once.

func (f *notifyFixture) snoozeAndExpire(t *testing.T, component, alarmID string) {
	t.Helper()
	if _, err := f.gw.SnoozeAlarm(f.ctx, "", component, alarmID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ExportExpireSnoozes(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func raisedRows(ns []storage.Notification) int {
	n := 0
	for _, x := range ns {
		if x.Transition == "raised" {
			n++
		}
	}
	return n
}

// TestTheDeadlineAnnouncesAStillRaisedConditionAgain is the core behavior:
// the sweep ends the expired snooze, the timeline reads snooze-end then the
// new raised rows, and the alarm no longer reads snoozed.
func TestTheDeadlineAnnouncesAStillRaisedConditionAgain(t *testing.T) {
	f := newNotifyFixture(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video", DedupKey: "video"})
	if err != nil {
		t.Fatal(err)
	}
	f.snoozeAndExpire(t, "display-1", a.ID)
	// Nothing ends before the sweep, and a sweep with nothing due ends nothing.
	if n, err := f.gw.EndExpiredSnoozes(f.ctx, 10); err != nil || n != 1 {
		t.Fatalf("the sweep ended %d (%v), want the one expired snooze", n, err)
	}
	if n, err := f.gw.EndExpiredSnoozes(f.ctx, 10); err != nil || n != 0 {
		t.Fatalf("a second sweep ended %d (%v), want none", n, err)
	}
	ns := f.list(t, storage.NotificationFilter{AlarmID: a.ID})
	if len(ns) != 4 || raisedRows(ns) != 4 {
		t.Fatalf("after the deadline the alarm has %d rows (%d raised), want its 2 first raises and 2 announced again", len(ns), raisedRows(ns))
	}
	for _, n := range ns[:2] {
		if n.Status != storage.NotificationQueued {
			t.Errorf("an announcement at the end of a snooze = %s, want queued (a plain raised notification)", n.Status)
		}
	}
	timeline, err := f.gw.AlarmTimeline(f.ctx, a.ComponentID, a.ID, storage.RecipientView{All: true})
	if err != nil {
		t.Fatal(err)
	}
	var tail []string
	for _, e := range timeline[len(timeline)-3:] {
		tail = append(tail, e.Kind+":"+e.What)
	}
	if tail[0] != "audit:snooze-end" || tail[1] != "notification:queued" || tail[2] != "notification:queued" {
		t.Errorf("the timeline ends %v, want snooze-end then the two announcements", tail)
	}
	if got, err := f.gw.ListAlarms(f.ctx, "display-1", storage.AlarmFilter{}); err != nil || got[0].Snoozed() {
		t.Errorf("after the deadline the alarm reads %+v (%v), want it unsnoozed", got, err)
	}
}

// TestAConditionThatClearedDuringTheSnoozeGetsNoAnnouncement: the end tells
// people about a condition still raised, and nothing about one that is not.
func TestAConditionThatClearedDuringTheSnoozeGetsNoAnnouncement(t *testing.T) {
	f := newNotifyFixture(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video", DedupKey: "video"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.SnoozeAlarm(f.ctx, "", "display-1", a.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ClearAlarm(f.ctx, "", "display-1", a.ID); err != nil {
		t.Fatal(err)
	}
	before := len(f.list(t, storage.NotificationFilter{AlarmID: a.ID}))
	if err := f.gw.ExportExpireSnoozes(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := f.gw.EndExpiredSnoozes(f.ctx, 10); err != nil || n != 1 {
		t.Fatalf("the sweep ended %d (%v), want 1", n, err)
	}
	if after := len(f.list(t, storage.NotificationFilter{AlarmID: a.ID})); after != before {
		t.Errorf("a cleared condition's snooze ending queued %d rows, want none", after-before)
	}
	timeline, err := f.gw.AlarmTimeline(f.ctx, a.ComponentID, a.ID, storage.RecipientView{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if last := timeline[len(timeline)-1]; last.What != "snooze-end" || last.Actor != "" {
		t.Errorf("the timeline ends %+v, want the platform's snooze-end", last)
	}
}

// TestUnsnoozingEarlyAnnouncesLikeTheDeadline: :unsnooze takes the same
// path, audited as unsnooze naming the operator; unsnoozing a condition that
// is not snoozed announces nothing.
func TestUnsnoozingEarlyAnnouncesLikeTheDeadline(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no video", DedupKey: "video"})
	if err != nil {
		t.Fatal(err)
	}
	first := len(f.rows(t, a.ID))
	if _, err := f.gw.UnsnoozeAlarm(f.ctx, f.ids["root"], "display-2", a.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(f.rows(t, a.ID)); got != first {
		t.Fatalf("unsnoozing an unsnoozed condition queued %d rows, want none", got-first)
	}
	if _, err := f.gw.SnoozeAlarm(f.ctx, f.ids["root"], "display-2", a.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.UnsnoozeAlarm(f.ctx, f.ids["root"], "display-2", a.ID); err != nil {
		t.Fatal(err)
	}
	// The group's people who read display-2 (alice and bob) are told again,
	// in their inboxes, judged by ADR-1341's scope check as at any raise.
	again := f.rows(t, a.ID)[:len(f.rows(t, a.ID))-first]
	if got := recipients(again); len(got) != 2 {
		t.Fatalf("the early end announced %v, want alice and bob told again", got)
	}
	timeline, err := f.gw.AlarmTimeline(f.ctx, a.ComponentID, a.ID, storage.RecipientView{All: true})
	if err != nil {
		t.Fatal(err)
	}
	var unsnooze *storage.AlarmTimelineEntry
	for i := range timeline {
		if timeline[i].What == "unsnooze" {
			unsnooze = &timeline[i]
		}
	}
	if unsnooze == nil || unsnooze.Actor != "root" {
		t.Fatalf("the timeline's unsnooze = %+v, want one naming root", unsnooze)
	}
}

// TestARuleDisabledDuringTheSnoozeStaysQuiet: the end judges the rules as
// they stand, so a rule paused while the condition was snoozed tells nobody.
func TestARuleDisabledDuringTheSnoozeStaysQuiet(t *testing.T) {
	f := newNotifyFixture(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video", DedupKey: "video"})
	if err != nil {
		t.Fatal(err)
	}
	f.snoozeAndExpire(t, "display-1", a.ID)
	off := false
	if _, err := f.gw.UpdateActionRule(f.ctx, "", "page-high", storage.ActionRulePatch{Enabled: &off}, f.all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.EndExpiredSnoozes(f.ctx, 10); err != nil {
		t.Fatal(err)
	}
	if ns := f.list(t, storage.NotificationFilter{AlarmID: a.ID}); len(ns) != 2 {
		t.Errorf("a disabled rule announced at the end: %d rows, want the 2 from the raise", len(ns))
	}
}

// TestTwoReplicasAnnounceOneExpiredSnoozeOnce: two gateways on one database
// sweep at once, and the snooze is ended and announced exactly once.
func TestTwoReplicasAnnounceOneExpiredSnoozeOnce(t *testing.T) {
	f := newNotifyFixture(t)
	var ids []string
	for _, key := range []string{"a", "b", "c"} {
		a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "fault " + key, DedupKey: key})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.gw.SnoozeAlarm(f.ctx, "", "display-1", a.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	if err := f.gw.ExportExpireSnoozes(f.ctx); err != nil {
		t.Fatal(err)
	}
	other, err := storage.NewPG(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ended := 0
	for _, gw := range []*storage.PG{f.gw, other, f.gw, other} {
		wg.Add(1)
		go func(gw *storage.PG) {
			defer wg.Done()
			n, err := gw.EndExpiredSnoozes(f.ctx, 10)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			ended += n
			mu.Unlock()
		}(gw)
	}
	wg.Wait()
	if ended != 3 {
		t.Fatalf("the sweeps ended %d snoozes between them, want each of the 3 once", ended)
	}
	for _, id := range ids {
		if ns := f.list(t, storage.NotificationFilter{AlarmID: id}); len(ns) != 4 {
			t.Errorf("alarm %s has %d rows, want its 2 raises and 2 announced once", id, len(ns))
		}
	}
}
