package storage_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The notification queue and its drain (#618, #1321), against a real
// Postgres: a transition queues in its own transaction (and rolls back with
// it), the drain records attempts with backoff to a final failure, a retry
// re-queues, two drainers never send one row twice, and every read is scoped
// by the alarm's component.

type notifyFixture struct {
	gw  *storage.PG
	dsn string
	ctx context.Context
	all scope.Set
}

func newNotifyFixture(t *testing.T) *notifyFixture {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	f := &notifyFixture{gw: gw, dsn: dsn, ctx: ctx, all: scope.Set{All: true}}
	for _, name := range []string{"display-1", "display-2"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, f.all, f.all, f.all, f.all); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"ops-chat", "pager"} {
		if _, err := gw.CreateNotificationTarget(ctx, "", storage.NotificationTargetSpec{Name: name, Kind: storage.ChannelWebhook,
			URL: "https://hooks.example.com/" + name}, f.all, storage.SecretUse{}); err != nil {
			t.Fatal(err)
		}
	}
	// High and above, both edges, to both targets.
	if _, err := gw.CreateActionRule(ctx, "", storage.ActionRuleSpec{Name: "page-high", On: []string{"raised", "cleared"},
		When: "severity_rank >= 40", Targets: []string{"ops-chat", "pager"}}, f.all); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *notifyFixture) list(t *testing.T, filter storage.NotificationFilter) []storage.Notification {
	t.Helper()
	ns, err := f.gw.ListNotifications(f.ctx, filter, f.all)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

func TestATransitionQueuesItsNotificationsInItsOwnTransaction(t *testing.T) {
	f := newNotifyFixture(t)
	ctx := f.ctx

	// Below the predicate's floor: nothing queues.
	if _, err := f.gw.RaiseAlarm(ctx, "", "display-1", storage.AlarmSpec{Severity: "warning", Message: "fan noise"}); err != nil {
		t.Fatal(err)
	}
	if ns := f.list(t, storage.NotificationFilter{}); len(ns) != 0 {
		t.Fatalf("a warning queued %d notifications, want none", len(ns))
	}

	// A hand raise at high: one queued row per target, rendered.
	a, err := f.gw.RaiseAlarm(ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	ns := f.list(t, storage.NotificationFilter{AlarmID: a.ID})
	if len(ns) != 2 {
		t.Fatalf("a high raise queued %d, want one per target", len(ns))
	}
	for _, n := range ns {
		if n.Status != storage.NotificationQueued || n.Transition != "raised" || n.Type != "alarm-raised" ||
			n.Rule != "page-high" || !strings.Contains(n.Subject, "[High]") || !strings.Contains(n.Subject, "no video") {
			t.Fatalf("notification = %+v", n)
		}
	}
	// A re-raise of the open condition writes nothing.
	if _, err := f.gw.RaiseAlarm(ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"}); err != nil {
		t.Fatal(err)
	}
	// Acknowledging notifies nothing.
	if _, err := f.gw.AcknowledgeAlarm(ctx, "", "display-1", a.ID); err != nil {
		t.Fatal(err)
	}
	if ns := f.list(t, storage.NotificationFilter{AlarmID: a.ID}); len(ns) != 2 {
		t.Fatalf("a re-raise or an acknowledgement queued more: %d", len(ns))
	}
	// A hand clear notifies the cleared edge, with the official cleared type.
	if err := f.gw.ClearAlarm(ctx, "", "display-1", a.ID); err != nil {
		t.Fatal(err)
	}
	cleared := f.list(t, storage.NotificationFilter{AlarmID: a.ID})
	if len(cleared) != 4 || cleared[0].Transition != "cleared" || cleared[0].Type != "alarm-cleared" {
		t.Fatalf("after the clear = %+v, want two cleared on top", cleared)
	}

	// A raise whose transaction rolls back leaves no notification.
	if err := f.gw.ExportRaiseThenFail(ctx, "display-2", storage.AlarmSpec{Severity: "critical", Message: "rolled back"}); err == nil {
		t.Fatal("the raise did not fail")
	}
	if ns := f.list(t, storage.NotificationFilter{}); len(ns) != 4 {
		t.Fatalf("a rolled-back raise left %d notifications, want the 4 from before", len(ns))
	}

	// A rule's raise and clear pass the same seam.
	id, err := f.gw.ExportRuleRaiseAndClear(ctx, "reachability-lost", "display-2", false)
	if err != nil {
		t.Fatal(err)
	}
	if ns := f.list(t, storage.NotificationFilter{AlarmID: id}); len(ns) != 2 || ns[0].Transition != "raised" {
		t.Fatalf("a rule's raise queued %+v", ns)
	}
	if _, err := f.gw.ExportRuleRaiseAndClear(ctx, "reachability-lost", "display-2", true); err != nil {
		t.Fatal(err)
	}
	if ns := f.list(t, storage.NotificationFilter{AlarmID: id}); len(ns) != 4 || ns[0].Transition != "cleared" {
		t.Fatalf("a rule's clear queued %+v", ns)
	}
}

// sender is a fake receiver for the drain: it answers each send from a
// script and counts what each notification was sent, so a double send shows.
type sender struct {
	mu     sync.Mutex
	sent   map[string]int
	answer func(n int) storage.SendResult
	calls  atomic.Int32
	hold   time.Duration
}

func (s *sender) send(_ context.Context, ns storage.NotificationSend) storage.SendResult {
	n := int(s.calls.Add(1))
	if s.hold > 0 {
		time.Sleep(s.hold)
	}
	s.mu.Lock()
	s.sent[ns.ID]++
	s.mu.Unlock()
	return s.answer(n)
}

func drainAll(t *testing.T, f *notifyFixture, s *sender) {
	t.Helper()
	for {
		took, err := f.gw.DeliverNext(f.ctx, s.send)
		if err != nil {
			t.Fatal(err)
		}
		if !took {
			return
		}
	}
}

func TestTheDrainRetriesWithBackoffToAFinalFailureAndARetryRequeues(t *testing.T) {
	f := newNotifyFixture(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	down := &sender{sent: map[string]int{}, answer: func(int) storage.SendResult {
		return storage.SendResult{Status: 503, Reason: "the receiver answered 503"}
	}}
	start := time.Now()
	drainAll(t, f, down)
	ns := f.list(t, storage.NotificationFilter{AlarmID: a.ID})
	for _, n := range ns {
		if n.Status != storage.NotificationQueued || n.Attempts != 1 || n.LastStatus == nil || *n.LastStatus != 503 ||
			n.NextAttemptAt == nil || n.NextAttemptAt.Sub(start) < 9*time.Second || n.NextAttemptAt.Sub(start) > 15*time.Second {
			t.Fatalf("after one failed attempt = %+v, want queued again about 10s on", n)
		}
	}
	// Not due yet: the drain takes nothing.
	if took, _ := f.gw.DeliverNext(f.ctx, down.send); took {
		t.Fatal("the drain took a row still waiting out its backoff")
	}
	for i := 2; i <= storage.NotificationMaxAttempts; i++ {
		if err := f.gw.ExportDueNow(f.ctx); err != nil {
			t.Fatal(err)
		}
		drainAll(t, f, down)
	}
	for _, n := range f.list(t, storage.NotificationFilter{AlarmID: a.ID}) {
		if n.Status != storage.NotificationFailed || n.Attempts != storage.NotificationMaxAttempts || n.LastError != "the receiver answered 503" {
			t.Fatalf("after the cap = %+v, want failed with the last status and error", n)
		}
	}
	if storage.BackoffAfter(1) != 10*time.Second || storage.BackoffAfter(3) != 40*time.Second {
		t.Fatalf("backoff = %s, %s", storage.BackoffAfter(1), storage.BackoffAfter(3))
	}

	// :retry re-queues one, and a later success reads sent.
	n := f.list(t, storage.NotificationFilter{AlarmID: a.ID})[0]
	if _, err := f.gw.RetryNotification(f.ctx, "", n.ID, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.RetryNotification(f.ctx, "", n.ID, f.all, f.all); err != storage.ErrNotificationNotRetryable {
		t.Fatalf("retrying a queued notification = %v, want not retryable", err)
	}
	up := &sender{sent: map[string]int{}, answer: func(int) storage.SendResult { return storage.SendResult{OK: true, Status: 200} }}
	drainAll(t, f, up)
	got, err := f.gw.GetNotification(f.ctx, n.ID, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != storage.NotificationSent || got.Attempts != 1 || got.SentAt == nil || up.sent[n.ID] != 1 {
		t.Fatalf("after the retry = %+v (sends %d), want sent once", got, up.sent[n.ID])
	}
}

func TestTwoDrainersNeverSendOneNotificationTwice(t *testing.T) {
	f := newNotifyFixture(t)
	for i := 0; i < 5; i++ {
		if _, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "fault", DedupKey: "k" + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	// A second gateway on the same database stands in for a second replica.
	other, err := storage.NewPG(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	s := &sender{sent: map[string]int{}, hold: 50 * time.Millisecond, answer: func(int) storage.SendResult { return storage.SendResult{OK: true, Status: 200} }}
	var wg sync.WaitGroup
	for _, gw := range []*storage.PG{f.gw, other} {
		wg.Add(1)
		go func(gw *storage.PG) {
			defer wg.Done()
			for {
				took, err := gw.DeliverNext(f.ctx, s.send)
				if err != nil {
					t.Error(err)
					return
				}
				if !took {
					return
				}
			}
		}(gw)
	}
	wg.Wait()
	if len(s.sent) != 10 {
		t.Fatalf("%d notifications sent, want all 10", len(s.sent))
	}
	for id, n := range s.sent {
		if n != 1 {
			t.Errorf("notification %s sent %d times", id, n)
		}
	}
}

func TestQueuedNotificationsSurviveARestart(t *testing.T) {
	f := newNotifyFixture(t)
	if _, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"}); err != nil {
		t.Fatal(err)
	}
	f.gw.Close()
	again, err := storage.NewPG(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	s := &sender{sent: map[string]int{}, answer: func(int) storage.SendResult { return storage.SendResult{OK: true, Status: 200} }}
	for {
		took, err := again.DeliverNext(f.ctx, s.send)
		if err != nil {
			t.Fatal(err)
		}
		if !took {
			break
		}
	}
	if len(s.sent) != 2 {
		t.Fatalf("after a restart %d were delivered, want both queued before it", len(s.sent))
	}
}

func TestNotificationReadsAreScopedByTheAlarmsComponent(t *testing.T) {
	f := newNotifyFixture(t)
	a1, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "two"}); err != nil {
		t.Fatal(err)
	}
	only1 := scope.Set{IDs: []string{a1.ComponentID}}
	ns, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{}, only1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 2 || ns[0].ComponentName != "display-1" || ns[1].ComponentName != "display-1" {
		t.Fatalf("a display-1 scope listed %+v", ns)
	}
	all := f.list(t, storage.NotificationFilter{})
	var other string
	for _, n := range all {
		if n.ComponentName == "display-2" {
			other = n.ID
		}
	}
	if _, err := f.gw.GetNotification(f.ctx, other, only1); err != storage.ErrNotificationNotFound {
		t.Fatalf("a notification outside the scope = %v, want not found", err)
	}
	if _, err := f.gw.RetryNotification(f.ctx, "", ns[0].ID, only1, scope.Set{}); err != storage.ErrNotificationForbidden {
		t.Fatalf("a retry outside the action scope = %v, want forbidden", err)
	}
	if _, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{}, scope.Set{}); err != nil {
		t.Fatal(err)
	}
}

func TestANotificationJoinsItsAlarmsTimelineAfterTheEdge(t *testing.T) {
	f := newNotifyFixture(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ClearAlarm(f.ctx, "", "display-1", a.ID); err != nil {
		t.Fatal(err)
	}
	tl, err := f.gw.AlarmTimeline(f.ctx, a.ComponentID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range tl {
		kinds = append(kinds, e.Kind+":"+e.What)
	}
	want := []string{"audit:raise", "notification:queued", "notification:queued", "audit:clear", "notification:queued", "notification:queued"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("timeline = %v, want %v", kinds, want)
	}
	if tl[1].Target == "" || tl[1].Rule != "page-high" || tl[1].Transition != "raised" || tl[4].Transition != "cleared" {
		t.Fatalf("a notification line = %+v", tl[1])
	}
}
