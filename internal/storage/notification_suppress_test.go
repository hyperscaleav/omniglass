package storage_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A snoozed condition's notifications are written suppressed (#1318,
// ADR-1318): every row a firing rule would write, on every channel, a raise
// and a clear alike, is written with the terminal status suppressed and the
// reason, so the alarm's history keeps what was held back while nothing is
// sent and nothing reaches an inbox. A row already waiting out a retry is
// marked suppressed by the tick instead of being sent.

// TestASnoozedConditionsRowsAreWrittenSuppressedOnEveryChannel drives the
// seam over every kind of row: a group's inbox people, a webhook, an email
// target and a personal rule, on a re-raise and on a clear.
func TestASnoozedConditionsRowsAreWrittenSuppressedOnEveryChannel(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "ops-chat", Kind: storage.ChannelWebhook,
		URL: "https://hooks.example.com/ops"}, f.all, storage.SecretUse{}, storage.PeopleUse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "ops-mail", Kind: storage.TargetEmail,
		Addresses: []string{"ops@example.com"}}, f.all, storage.SecretUse{}, storage.PeopleUse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateActionRule(f.ctx, "", storage.ActionRuleSpec{Name: "page-ops", On: []string{"raised", "cleared"},
		When: "severity_rank >= 40", Targets: []string{"ops-chat", "ops-mail"}}, f.all); err != nil {
		t.Fatal(err)
	}
	f.personal(t, "alice", "mine", "true")
	// A rule whose when fails whenever it runs still writes failed during a
	// snooze: a broken rule stays visible.
	f.personal(t, "alice", "broken", "severity_rank % (severity_rank - severity_rank) > 0")

	first, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no video", DedupKey: "video"})
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.rows(t, first.ID))
	if before == 0 {
		t.Fatal("the unsnoozed raise queued nothing; the fixture's rules never matched")
	}
	inbox := func(user string) int {
		t.Helper()
		in, err := f.gw.ListInbox(f.ctx, f.ids[user], storage.InboxFilter{}, f.all)
		if err != nil {
			t.Fatal(err)
		}
		return len(in.Notifications)
	}
	aliceBefore := inbox("alice")

	if _, err := f.gw.SnoozeAlarm(f.ctx, f.ids["root"], "display-2", first.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ClearAlarm(f.ctx, "", "display-2", first.ID); err != nil {
		t.Fatal(err)
	}
	again, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no video", DedupKey: "video"})
	if err != nil {
		t.Fatal(err)
	}

	cleared := f.rows(t, first.ID)[:len(f.rows(t, first.ID))-before]
	raised := f.rows(t, again.ID)
	channels := map[string]bool{}
	for _, n := range append(cleared, raised...) {
		if n.Rule == "broken" {
			if n.Status != storage.NotificationFailed {
				t.Errorf("the broken rule wrote %s during the snooze, want failed", n.Status)
			}
			continue
		}
		channels[n.Channel] = true
		if n.Status != storage.NotificationSuppressed || !strings.Contains(n.LastError, "snoozed until") || !strings.Contains(n.LastError, "by root") {
			t.Errorf("during the snooze a %s %s row = %s %q, want suppressed with the snooze's reason", n.Transition, n.Channel, n.Status, n.LastError)
		}
	}
	for _, ch := range []string{storage.DeliveryInbox, storage.DeliveryWebhook, storage.DeliveryEmail} {
		if !channels[ch] {
			t.Errorf("no %s row was written during the snooze; the suppressed rows must still be on the alarm's history", ch)
		}
	}
	if len(cleared) == 0 || cleared[0].Transition != "cleared" {
		t.Errorf("the clear during the snooze wrote %+v, want suppressed cleared rows", cleared)
	}
	if got := inbox("alice"); got != aliceBefore {
		t.Errorf("alice's inbox went from %d to %d during the snooze; a suppressed row is never shown", aliceBefore, got)
	}

	// Another condition on the same component notifies as usual.
	other, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio", DedupKey: "audio"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range f.rows(t, other.ID) {
		if n.Status == storage.NotificationSuppressed {
			t.Errorf("another condition's row was suppressed: %+v", n)
		}
	}
	if got := inbox("alice"); got <= aliceBefore {
		t.Error("another condition's raise never reached alice's inbox")
	}

	// The list filters by the new status, under the caller's component scope.
	all, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{Status: storage.NotificationSuppressed}, f.all, storage.RecipientView{All: true})
	if err != nil || len(all) == 0 {
		t.Fatalf("status=suppressed listed %d (%v), want the suppressed rows", len(all), err)
	}
	elsewhere := scope.Set{IDs: []string{f.comps["display-1"]}}
	none, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{Status: storage.NotificationSuppressed}, elsewhere, storage.RecipientView{})
	if err != nil || len(none) != 0 {
		t.Fatalf("a caller scoped to display-1 listed %d suppressed rows about display-2 (%v), want none", len(none), err)
	}

	// :retry refuses a suppressed row.
	if _, err := f.gw.RetryNotification(f.ctx, "", raised[0].ID, f.all, f.all, storage.RecipientView{All: true}); err != storage.ErrNotificationNotRetryable {
		t.Errorf("retrying a suppressed row = %v, want not retryable", err)
	}
}

// TestTheTickSuppressesARowWaitingOutItsRetry is the re-check where a row
// waits: a row queued before the snooze, in backoff when it starts, is marked
// suppressed when its turn comes, and never sent.
func TestTheTickSuppressesARowWaitingOutItsRetry(t *testing.T) {
	f := newNotifyFixture(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video", DedupKey: "video"})
	if err != nil {
		t.Fatal(err)
	}
	down := &sender{sent: map[string]int{}, answer: func(int) storage.SendResult {
		return storage.SendResult{Status: 503, Reason: "the receiver answered 503"}
	}}
	drainAll(t, f, down)
	if _, err := f.gw.SnoozeAlarm(f.ctx, "", "display-1", a.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ExportDueNow(f.ctx); err != nil {
		t.Fatal(err)
	}
	up := &sender{sent: map[string]int{}, answer: func(int) storage.SendResult { return storage.SendResult{OK: true, Status: 200} }}
	drainAll(t, f, up)
	if len(up.sent) != 0 {
		t.Fatalf("the tick sent %d rows of a snoozed condition, want none", len(up.sent))
	}
	for _, n := range f.list(t, storage.NotificationFilter{AlarmID: a.ID}) {
		if n.Status != storage.NotificationSuppressed || n.Attempts != 1 || !strings.Contains(n.LastError, "snoozed until") {
			t.Errorf("a row in backoff when the snooze began = %s after %d attempts (%q), want suppressed with the reason", n.Status, n.Attempts, n.LastError)
		}
	}
	// A row of another condition still sends.
	if _, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no audio", DedupKey: "audio"}); err != nil {
		t.Fatal(err)
	}
	drainAll(t, f, up)
	if len(up.sent) != 2 {
		t.Fatalf("another condition's rows sent %d, want both", len(up.sent))
	}
}
