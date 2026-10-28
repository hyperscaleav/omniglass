package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The email channel at the drain (#1317, #1341), against a real Postgres and
// a recording sender: a fixed-address target sends to its addresses, a
// person's email row to their directory email resolved at send, and a
// person's row is judged again at send (ADR-1341): one who lost the alarm, left
// the group, or has no email fails with a plain reason instead of sending.

type mailbox struct {
	mu   sync.Mutex
	sent []storage.NotificationSend
}

func (m *mailbox) send(_ context.Context, ns storage.NotificationSend) storage.SendResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, ns)
	return storage.SendResult{OK: true, Status: 250}
}

func (m *mailbox) to() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for _, s := range m.sent {
		if s.Email != nil {
			out = append(out, strings.Join(s.Email.To, "+"))
		}
	}
	return out
}

func (f *peopleFixture) relay(t *testing.T) {
	t.Helper()
	if _, err := f.gw.MergePatchSettingOverride(f.ctx, "", "platform", "notification", map[string]any{
		"mail_relay": "relay.example.com:587", "mail_from": "Omniglass <alerts@example.com>"}); err != nil {
		t.Fatal(err)
	}
}

func (f *peopleFixture) drain(t *testing.T, m *mailbox) {
	t.Helper()
	for {
		took, err := f.gw.DeliverNext(f.ctx, m.send)
		if err != nil {
			t.Fatal(err)
		}
		if !took {
			return
		}
	}
}

func (f *peopleFixture) emailRule(t *testing.T) {
	t.Helper()
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "on-call-page", Kind: storage.TargetPrincipalGroup,
		PrincipalGroup: "on-call", Channels: []string{storage.DeliveryInbox, storage.DeliveryEmail}}, f.all, storage.SecretUse{}, bindAll); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateActionRule(f.ctx, "", storage.ActionRuleSpec{Name: "page-on-call", On: []string{"raised"},
		Targets: []string{"on-call-page"}}, f.all); err != nil {
		t.Fatal(err)
	}
}

func (f *peopleFixture) failures(t *testing.T, alarmID string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, n := range f.rows(t, alarmID) {
		if n.Channel == storage.DeliveryEmail && n.Status == storage.NotificationFailed {
			out[n.Recipient] = n.LastError
		}
	}
	return out
}

func TestAnEmailTargetSendsToItsAddressesAndAPersonToTheirDirectoryEmail(t *testing.T) {
	f := newPeopleFixture(t)
	f.relay(t)
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "ops-mailbox", Kind: storage.TargetEmail,
		Addresses: []string{"Ops <ops@example.com>", "ops@example.com", "noc@example.com"}}, f.all, storage.SecretUse{}, storage.PeopleUse{}); err != nil {
		t.Fatal(err)
	}
	f.emailRule(t)
	if _, err := f.gw.UpdateActionRule(f.ctx, "", "page-on-call", storage.ActionRulePatch{Targets: &[]string{"on-call-page", "ops-mailbox"}}, f.all); err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatal(err)
	}
	m := &mailbox{}
	f.drain(t, m)
	got := m.to()
	want := []string{"alice@example.com", "bob@example.com", "ops@example.com+noc@example.com"}
	if fmt.Sprint(sortedCopy(got)) != fmt.Sprint(want) {
		t.Fatalf("the drain sent to %v, want %v", got, want)
	}
	for _, s := range m.sent {
		if s.Email.Relay.Addr != "relay.example.com:587" || s.Email.Relay.From != "Omniglass <alerts@example.com>" || s.Email.Date.IsZero() {
			t.Fatalf("an email send = %+v", s.Email)
		}
	}
	for _, n := range f.rows(t, a.ID) {
		if n.Status != storage.NotificationSent {
			t.Fatalf("a row was not sent: %+v", n)
		}
	}
}

func TestAPersonsEmailIsJudgedAgainAtSend(t *testing.T) {
	f := newPeopleFixture(t)
	f.relay(t)
	f.human(t, "carol", storage.GrantSpec{Role: "viewer", ScopeKind: "all"})
	f.human(t, "dave", storage.GrantSpec{Role: "viewer", ScopeKind: "all"})
	pr, err := f.gw.CreateHumanPrincipal(f.ctx, "", storage.HumanSpec{Username: "erin"}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateGrant(f.ctx, "", pr.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, f.all); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"carol", "dave", "erin"} {
		if err := f.gw.AddGroupMember(f.ctx, "", "on-call", u, f.all); err != nil {
			t.Fatal(err)
		}
	}
	f.emailRule(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	// Between the queue and the send: alice loses the fleet (her grant is
	// narrowed to display-2), carol leaves the group, dave is archived; erin
	// never had an email.
	alice, err := f.gw.GetPrincipal(f.ctx, f.ids["alice"], f.all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateGrant(f.ctx, "", alice.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "component", ScopeID: f.comps["display-2"]}, f.all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.RevokeGrant(f.ctx, "", alice.ID, alice.Grants[0].ID, f.all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.RemoveGroupMember(f.ctx, "", "on-call", "carol", f.all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ArchivePrincipal(f.ctx, "", f.ids["dave"], f.all); err != nil {
		t.Fatal(err)
	}
	m := &mailbox{}
	f.drain(t, m)
	if got := m.to(); len(got) != 0 {
		t.Fatalf("the drain emailed %v; every one of them should have been refused", got)
	}
	fails := f.failures(t, a.ID)
	for who, want := range map[string]string{
		"alice": "no longer reads",
		"carol": "no longer a member",
		"dave":  "no longer active",
		"erin":  "no directory email",
	} {
		if !strings.Contains(fails[who], want) {
			t.Errorf("%s's email failed with %q, want a reason saying %q", who, fails[who], want)
		}
	}
	// Each failed at once, without retrying.
	for _, n := range f.rows(t, a.ID) {
		if n.Channel == storage.DeliveryEmail && n.Attempts != 1 {
			t.Fatalf("a refused email took %d attempts, want 1: %+v", n.Attempts, n)
		}
	}
	_ = scope.Set{}
}

func TestNoRelayFailsTheEmailWithTheReason(t *testing.T) {
	f := newPeopleFixture(t)
	f.emailRule(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t, &mailbox{})
	for who, reason := range f.failures(t, a.ID) {
		if !strings.Contains(reason, "no mail relay is configured") {
			t.Fatalf("%s's email failed with %q", who, reason)
		}
	}
	if len(f.failures(t, a.ID)) != 2 {
		t.Fatalf("failures = %v", f.failures(t, a.ID))
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// The relay's secret binding is judged on the settings a patch RESULTS in,
// every layer resolved: a caller who may not reveal the operator file's relay
// secret cannot repoint the relay at it in two steps (unbind and move, then
// let the file's binding show through).
func TestTheRelaysSecretIsJudgedOnTheResultingSettings(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t),
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x9}, 32))),
		storage.WithSettingsFile(settings.Doc{"notification": map[string]any{"mail_relay": "smtp.corp.example:587", "mail_secret": "relay-login", "mail_from": "alerts@example.com"}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "relay-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "u", "password": "p"}}, all, true); err != nil {
		t.Fatal(err)
	}
	noReveal := storage.SecretUse{Read: all}
	reveal := storage.SecretUse{Read: all, Reveal: all, CanAdmin: true}
	if _, err := gw.MergePatchNotificationSettings(ctx, "", map[string]any{"mail_relay": "evil.example.com:587"}, noReveal); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Fatalf("moving the relay with the file's secret bound = %v, want forbidden", err)
	}
	if _, err := gw.MergePatchNotificationSettings(ctx, "", map[string]any{"mail_secret": "", "mail_relay": "evil.example.com:587"}, noReveal); err != nil {
		t.Fatalf("moving the relay with no secret bound = %v", err)
	}
	if _, err := gw.MergePatchNotificationSettings(ctx, "", map[string]any{"mail_secret": nil}, noReveal); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Fatalf("letting the file's secret show through onto the moved relay = %v, want forbidden", err)
	}
	if _, err := gw.MergePatchNotificationSettings(ctx, "", map[string]any{"mail_secret": nil}, reveal); err != nil {
		t.Fatalf("the same patch by a caller who may reveal it = %v", err)
	}
	relay, refused, err := gw.MailRelay(ctx)
	if err != nil || refused != "" || relay.Addr != "evil.example.com:587" || relay.Username != "u" {
		t.Fatalf("the relay = %+v, %q, %v", relay, refused, err)
	}
}
