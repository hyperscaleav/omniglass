package storage_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Personal action rules (#1353), against a real Postgres: a rule a user owns
// tells its owner alone, never matches an alarm outside the owner's component
// read scope (its when is not even run there), and is the owner's alone to
// read, change, test or delete.

func (f *peopleFixture) personal(t *testing.T, owner, name, when string) {
	t.Helper()
	if _, err := f.gw.CreatePersonalRule(f.ctx, f.ids[owner], f.ids[owner], storage.PersonalRuleSpec{Name: name, On: []string{"raised"},
		When: when, Channels: []string{storage.DeliveryInbox}}); err != nil {
		t.Fatal(err)
	}
}

func TestAPersonalRuleNeverMatchesOutsideItsOwnersScope(t *testing.T) {
	f := newPeopleFixture(t)
	// bob reads display-2 alone. One rule always holds; the other's when
	// fails whenever it runs (a division by zero), which would itself write a
	// failed row about the alarm.
	f.personal(t, "bob", "everything", "true")
	f.personal(t, "bob", "broken", "severity_rank % (severity_rank - severity_rank) > 0")

	out, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.rows(t, out.ID); len(got) != 0 {
		t.Fatalf("an alarm outside bob's scope queued %v; his rules must not match it, nor run their when", recipients(got))
	}
	in, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatal(err)
	}
	got := f.rows(t, in.ID)
	statuses := map[string]string{}
	for _, n := range got {
		if n.Recipient != "bob" || n.Channel != storage.DeliveryInbox || n.Target != "" {
			t.Fatalf("a personal rule's row = %+v, want bob's inbox and no target", n)
		}
		statuses[n.Rule] = n.Status
	}
	if statuses["everything"] != storage.NotificationSent || statuses["broken"] != storage.NotificationFailed {
		t.Fatalf("inside bob's scope his rules wrote %v, want everything sent and broken failed", statuses)
	}
	// The rule's name, which can say whose it is, is withheld with the name.
	others, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{AlarmID: in.ID}, f.all, storage.RecipientView{Self: f.ids["alice"]})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range others {
		if n.Rule != "" || n.Recipient != "" {
			t.Fatalf("someone else read bob's personal row as %+v", n)
		}
	}
}

func TestAPersonalRuleIsItsOwnersAlone(t *testing.T) {
	f := newPeopleFixture(t)
	f.personal(t, "bob", "my-displays", "true")
	alice := f.ids["alice"]
	// alice may have her own rule of the same name.
	f.personal(t, "alice", "my-displays", "severity_rank >= 50")
	if _, err := f.gw.GetPersonalRule(f.ctx, alice, "my-displays"); err != nil {
		t.Fatal(err)
	}
	bobs, err := f.gw.GetPersonalRule(f.ctx, f.ids["bob"], "my-displays")
	if err != nil {
		t.Fatal(err)
	}
	// By uuid, bob's rule does not exist for alice: read, change, test, delete.
	if _, err := f.gw.GetPersonalRule(f.ctx, alice, bobs.ID); !errors.Is(err, storage.ErrActionRuleNotFound) {
		t.Fatalf("alice reading bob's rule = %v", err)
	}
	off := false
	if _, err := f.gw.UpdatePersonalRule(f.ctx, alice, alice, bobs.ID, storage.PersonalRulePatch{Enabled: &off}); !errors.Is(err, storage.ErrActionRuleNotFound) {
		t.Fatalf("alice pausing bob's rule = %v", err)
	}
	if _, err := f.gw.TestPersonalRule(f.ctx, alice, bobs.ID, storage.RuleDraft{}, f.all, 0); !errors.Is(err, storage.ErrActionRuleNotFound) {
		t.Fatalf("alice testing bob's rule = %v", err)
	}
	if err := f.gw.DeletePersonalRule(f.ctx, alice, alice, bobs.ID); !errors.Is(err, storage.ErrActionRuleNotFound) {
		t.Fatalf("alice deleting bob's rule = %v", err)
	}
	// The platform surface never sees a personal rule.
	rs, err := f.gw.ListActionRules(f.ctx)
	if err != nil || len(rs) != 0 {
		t.Fatalf("the platform list = %v, %v; want no personal rule", rs, err)
	}
	if _, err := f.gw.GetActionRule(f.ctx, bobs.ID); !errors.Is(err, storage.ErrActionRuleNotFound) {
		t.Fatalf("the platform read of bob's rule = %v", err)
	}
	if err := f.gw.DeleteActionRule(f.ctx, "", bobs.ID, f.all); !errors.Is(err, storage.ErrActionRuleNotFound) {
		t.Fatalf("the platform delete of bob's rule = %v", err)
	}
	// A personal rule names no target.
	if _, err := f.gw.CreatePersonalRule(f.ctx, alice, alice, storage.PersonalRuleSpec{Name: "x", On: []string{"raised"},
		Channels: []string{"inbox"}, Targets: []string{"ops-chat"}}); !errors.Is(err, storage.ErrActionRuleInvalid) {
		t.Fatalf("a personal rule naming a target = %v", err)
	}
	// Purging the owner takes their rules with them.
	if err := f.gw.ArchivePrincipal(f.ctx, "", f.ids["bob"], f.all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.PurgePrincipal(f.ctx, "", f.ids["bob"], f.all); err != nil {
		t.Fatal(err)
	}
	if rs, _ := f.gw.ListPersonalRules(f.ctx, f.ids["bob"]); len(rs) != 0 {
		t.Fatalf("bob's rules outlived him: %v", rs)
	}
}

func TestATestJudgesTheOwnersRecentAlarmsInScope(t *testing.T) {
	f := newPeopleFixture(t)
	f.personal(t, "bob", "loud", "severity_rank >= 50")
	for _, a := range []struct{ comp, sev string }{{"display-1", "disaster"}, {"display-2", "high"}, {"display-2", "disaster"}} {
		if _, err := f.gw.RaiseAlarm(f.ctx, "", a.comp, storage.AlarmSpec{Severity: a.sev, Message: a.sev + " on " + a.comp}); err != nil {
			t.Fatal(err)
		}
	}
	bobRead := scope.Set{IDs: []string{f.comps["display-2"]}}
	ms, err := f.gw.TestPersonalRule(f.ctx, f.ids["bob"], "loud", storage.RuleDraft{}, bobRead, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, m := range ms {
		got = append(got, fmt.Sprintf("%s/%s/%v", m.ComponentName, m.Severity, m.Matched))
	}
	if fmt.Sprint(got) != "[display-2/disaster/true display-2/high/false]" {
		t.Fatalf("bob's :test judged %v; want his display-2 alarms alone", got)
	}
	// A draft when is judged before it is saved.
	w := "true"
	ms, err = f.gw.TestPersonalRule(f.ctx, f.ids["bob"], "loud", storage.RuleDraft{When: &w}, bobRead, 0)
	if err != nil || len(ms) != 2 || !ms[0].Matched || !ms[1].Matched {
		t.Fatalf("a draft :test = %+v, %v", ms, err)
	}
}

// The bounds and the reach of a person's rules (#1353 review): a person owns
// at most PersonalRuleCap; a type an admin deletes releases a personal rule
// rather than being held by it; an owner who loses notification:subscribe is
// told nothing more; a paused rule's queued email does not send.
func TestAPersonsRulesAreBoundedAndReleaseWhatAnAdminDeletes(t *testing.T) {
	f := newPeopleFixture(t)
	bob := f.ids["bob"]
	for i := range storage.PersonalRuleCap {
		f.personal(t, "bob", fmt.Sprintf("r-%d", i), "false")
	}
	if _, err := f.gw.CreatePersonalRule(f.ctx, bob, bob, storage.PersonalRuleSpec{Name: "one-more", On: []string{"raised"},
		Channels: []string{"inbox"}}); !errors.Is(err, storage.ErrPersonalRuleCap) {
		t.Fatalf("a rule past the cap = %v", err)
	}
	alice := f.ids["alice"]
	if _, err := f.gw.CreateNotificationType(f.ctx, "", storage.NotificationTypeSpec{Name: "terse", Subject: "{{.Severity}}", Text: "{{.Message}}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreatePersonalRule(f.ctx, alice, alice, storage.PersonalRuleSpec{Name: "terse-mine", On: []string{"raised"},
		NotificationType: "terse", Channels: []string{"inbox"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.DeleteNotificationType(f.ctx, "", "terse"); err != nil {
		t.Fatalf("a personal rule held a type the admin deleted: %v", err)
	}
	if r, err := f.gw.GetPersonalRule(f.ctx, alice, "terse-mine"); err != nil || r.NotificationType != "" {
		t.Fatalf("alice's rule after the type went = %+v, %v; want the official type", r, err)
	}
}

func TestAnOwnerWithoutSubscribeIsToldNothing(t *testing.T) {
	f := newPeopleFixture(t)
	f.personal(t, "alice", "everything", "true")
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	// alice's role loses notification:subscribe (a custom role without it).
	if _, err := conn.Exec(f.ctx, `insert into role (name, permissions, inherits) values ('reader', '{"*:read"}', '{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `update principal_grant set role_id = (select id from role where name = 'reader') where principal_id = $1`, f.ids["alice"]); err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.rows(t, a.ID); len(got) != 0 {
		t.Fatalf("an owner without notification:subscribe was told %v", recipients(got))
	}
}
