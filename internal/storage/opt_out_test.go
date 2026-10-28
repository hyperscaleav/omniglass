package storage_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Group opt-out (#1354): a member opted out of a rule that allows it is told
// nothing by it; a rule that does not allow it refuses the opt-out; opting
// back in, or the admin taking the permission back, restores the page.
func TestAMemberOptsOutOfARuleTheAdminAllows(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	alice := f.ids["alice"]
	if _, err := f.gw.SetRuleOptOut(f.ctx, alice, alice, "page-on-call", true); !errors.Is(err, storage.ErrOptOutNotAllowed) {
		t.Fatalf("opting out of a rule that does not allow it = %v", err)
	}
	yes := true
	if _, err := f.gw.UpdateActionRule(f.ctx, "", "page-on-call", storage.ActionRulePatch{OptOutAllowed: &yes}, f.all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.SetRuleOptOut(f.ctx, alice, alice, "page-on-call", true); err != nil {
		t.Fatal(err)
	}
	raise := func() string {
		t.Helper()
		a, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
		if err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprint(recipients(f.rows(t, a.ID)))
		if err := f.gw.ClearAlarm(f.ctx, "", "display-2", a.ID); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := raise(); got != "[bob/inbox/sent]" {
		t.Fatalf("with alice opted out the raise queued %v, want bob alone", got)
	}
	// The admin takes the permission back: the opt-out no longer holds.
	no := false
	if _, err := f.gw.UpdateActionRule(f.ctx, "", "page-on-call", storage.ActionRulePatch{OptOutAllowed: &no}, f.all); err != nil {
		t.Fatal(err)
	}
	if got := raise(); got != "[alice/inbox/sent bob/inbox/sent]" && got != "[bob/inbox/sent alice/inbox/sent]" {
		t.Fatalf("with opt-out withdrawn the raise queued %v, want both", got)
	}
	// Opting back in is always allowed.
	// Allowed again, the withdrawn opt-out does not silently return.
	if _, err := f.gw.UpdateActionRule(f.ctx, "", "page-on-call", storage.ActionRulePatch{OptOutAllowed: &yes}, f.all); err != nil {
		t.Fatal(err)
	}
	if got := raise(); got != "[alice/inbox/sent bob/inbox/sent]" && got != "[bob/inbox/sent alice/inbox/sent]" {
		t.Fatalf("an opt-out withdrawn came back when re-allowed: %v", got)
	}
	// Opting out and back in.
	if _, err := f.gw.SetRuleOptOut(f.ctx, alice, alice, "page-on-call", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.SetRuleOptOut(f.ctx, alice, alice, "page-on-call", false); err != nil {
		t.Fatal(err)
	}
	if got := raise(); got != "[alice/inbox/sent bob/inbox/sent]" && got != "[bob/inbox/sent alice/inbox/sent]" {
		t.Fatalf("after opting back in the raise queued %v, want both", got)
	}
	// A target naming alice directly tells her whatever her group opt-out.
	if _, err := f.gw.SetRuleOptOut(f.ctx, alice, alice, "page-on-call", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "z-alice", Kind: storage.TargetPrincipal,
		Principal: "alice", Channels: []string{storage.DeliveryInbox}}, f.all, storage.SecretUse{}, bindAll); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.UpdateActionRule(f.ctx, "", "page-on-call", storage.ActionRulePatch{Targets: &[]string{"on-call-page", "z-alice"}}, f.all); err != nil {
		t.Fatal(err)
	}
	if got := raise(); got != "[alice/inbox/sent bob/inbox/sent]" && got != "[bob/inbox/sent alice/inbox/sent]" {
		t.Fatalf("a direct target with alice opted out of the group queued %v, want both", got)
	}
}
