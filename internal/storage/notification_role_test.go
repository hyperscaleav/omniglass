package storage_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A role as a notification target, and the routing that ships on it (#1987),
// against a real Postgres: a role target reaches every person holding the
// role, directly or through a group grant, read at each transition and
// scope-filtered per person as a group target is; the boot seed ships
// platform-alarms-to-admins, which sends an alarm on the platform system to
// the admin role on the inbox and email, and an operator's disable survives
// the next boot.

// shippedRule is the action rule the boot seed ships.
const shippedRule = "platform-alarms-to-admins"

// platformAlarm raises an alarm on the server's platform component.
func (f *peopleFixture) platformAlarm(t *testing.T, message string) string {
	t.Helper()
	id, err := f.gw.EnsureServerComponent(f.ctx, "og-server-1")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", id, storage.AlarmSpec{Severity: "high", Message: message})
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

// told is the rows about an alarm, each person/channel/status, in order.
func (f *peopleFixture) told(t *testing.T, alarmID string) []string {
	t.Helper()
	got := recipients(f.rows(t, alarmID))
	slices.Sort(got)
	return got
}

func TestTheShippedRuleTellsWhoeverHoldsAdminAboutAPlatformAlarm(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})

	// ada holds admin: she is told on the inbox and by email. root holds
	// owner, and alice and bob viewer: none of them holds admin.
	a := f.platformAlarm(t, "the server's queue is backing up")
	if got := fmt.Sprint(f.told(t, a)); got != "[ada/email/queued ada/inbox/sent]" {
		t.Fatalf("a platform alarm told %v, want ada on the inbox and by email", got)
	}

	// An alarm on a fleet component is not the platform's: the shipped rule
	// does not route it.
	b, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.told(t, b.ID); len(got) != 0 {
		t.Fatalf("a fleet alarm told %v, want no one", got)
	}

	// The role is read at the transition: with the grant revoked, the next
	// platform alarm tells no one.
	ada, err := f.gw.GetPrincipal(f.ctx, f.ids["ada"], f.all)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.gw.RevokeGrant(f.ctx, "", ada.ID, ada.Grants[0].ID, f.all); err != nil {
		t.Fatal(err)
	}
	c := f.platformAlarm(t, "the server's disk is filling")
	if got := f.told(t, c); len(got) != 0 {
		t.Fatalf("after the revoke a platform alarm told %v, want no one", got)
	}
}

func TestAPersonHoldingTheRoleTwiceIsToldOnce(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	if _, err := f.gw.CreateGroup(f.ctx, "", storage.GroupSpec{Name: "admins"}, f.all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateGroupGrant(f.ctx, "", "admins", storage.GrantSpec{Role: "admin", ScopeKind: "all"}, f.all); err != nil {
		t.Fatal(err)
	}
	// ada holds admin directly and through the group; grace through the
	// group alone.
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})
	f.human(t, "grace")
	for _, u := range []string{"ada", "grace"} {
		if err := f.gw.AddGroupMember(f.ctx, "", "admins", u, f.all); err != nil {
			t.Fatal(err)
		}
	}
	a := f.platformAlarm(t, "the server's queue is backing up")
	want := "[ada/email/queued ada/inbox/sent grace/email/queued grace/inbox/sent]"
	if got := fmt.Sprint(f.told(t, a)); got != want {
		t.Fatalf("a platform alarm told %v, want %v", got, want)
	}
}

func TestARoleTargetIsScopeFilteredPerPerson(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	// carol holds operator over display-2 alone; dave over the fleet.
	f.human(t, "carol", storage.GrantSpec{Role: "operator", ScopeKind: "component", ScopeID: f.comps["display-2"]})
	f.human(t, "dave", storage.GrantSpec{Role: "operator", ScopeKind: "all"})
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "operators", Kind: storage.TargetRole,
		Role: "operator", Channels: []string{storage.DeliveryInbox}}, f.all, storage.SecretUse{}, bindAll); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateActionRule(f.ctx, "", storage.ActionRuleSpec{Name: "page-operators", On: []string{"raised"},
		Targets: []string{"operators"}}, f.all); err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(f.told(t, a.ID)); got != "[dave/inbox/sent]" {
		t.Fatalf("display-1's raise told %v, want dave alone", got)
	}
}

func TestARoleTargetIsValidatedWhenWritten(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	for _, c := range []struct {
		name string
		spec storage.NotificationTargetSpec
		use  storage.PeopleUse
		want error
	}{
		{"binding a role without the group directory's read", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin", Channels: []string{"inbox"}},
			storage.PeopleUse{Principals: true}, storage.ErrNotificationTargetPeopleForbidden},
		{"no role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Channels: []string{"inbox"}},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"an unknown role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "nobody", Channels: []string{"inbox"}},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"no channel", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a URL on a role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin", Channels: []string{"inbox"}, URL: "https://hooks.example.com/x"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a group on a role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin", Channels: []string{"inbox"}, PrincipalGroup: "on-call"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a role on a group", storage.NotificationTargetSpec{Kind: storage.TargetPrincipalGroup, PrincipalGroup: "on-call", Channels: []string{"inbox"}, Role: "admin"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a role on a webhook", storage.NotificationTargetSpec{Kind: storage.ChannelWebhook, URL: "https://hooks.example.com/x", Role: "admin"},
			bindAll, storage.ErrNotificationTargetInvalid},
	} {
		c.spec.Name = "t-1"
		if _, err := f.gw.CreateNotificationTarget(f.ctx, "", c.spec, f.all, storage.SecretUse{}, c.use); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	tg, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "operators", Kind: storage.TargetRole,
		Role: "operator", Channels: []string{"email", "inbox"}}, f.all, storage.SecretUse{}, storage.PeopleUse{Groups: true})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Role != "operator" || tg.RoleID == "" || fmt.Sprint(tg.Channels) != "[inbox email]" || tg.PrincipalGroup != "" {
		t.Fatalf("a role target reads as %+v", tg)
	}
	// The test send is a webhook's or an email address's: a role target has
	// no address of its own to try.
	if _, err := f.gw.TargetForTest(f.ctx, "operators", f.all); !errors.Is(err, storage.ErrNotificationTargetInvalid) {
		t.Fatalf("a role target's test send: %v, want ErrNotificationTargetInvalid", err)
	}
}

func TestARoleHoldersEmailIsJudgedAgainAtSend(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	f.relay(t)
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})
	a := f.platformAlarm(t, "the server's queue is backing up")
	// Between the queue and the send, ada stops holding admin.
	ada, err := f.gw.GetPrincipal(f.ctx, f.ids["ada"], f.all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateGrant(f.ctx, "", ada.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, f.all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.RevokeGrant(f.ctx, "", ada.ID, ada.Grants[0].ID, f.all); err != nil {
		t.Fatal(err)
	}
	m := &mailbox{}
	f.drain(t, m)
	if got := m.to(); len(got) != 0 {
		t.Fatalf("the drain emailed %v; ada no longer holds admin", got)
	}
	if got := f.failures(t, a)["ada"]; !strings.Contains(got, "no longer holds") {
		t.Fatalf("ada's email failed with %q, want a reason saying she no longer holds the role", got)
	}
}

func TestTheShippedRoutingIsSeededAndADisableSurvivesARestart(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	r, err := f.gw.GetActionRule(f.ctx, shippedRule)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Enabled || !r.Official || r.When != "platform" || fmt.Sprint(r.On) != "[raised cleared]" || fmt.Sprint(r.Targets) != "[platform-admins]" {
		t.Fatalf("the shipped rule reads as %+v", r)
	}
	tg, err := f.gw.GetNotificationTarget(f.ctx, "platform-admins", f.all)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Kind != storage.TargetRole || tg.Role != "admin" || fmt.Sprint(tg.Channels) != "[inbox email]" || !tg.Enabled {
		t.Fatalf("the shipped target reads as %+v", tg)
	}

	// The operator disables it, adds a mailbox, and edits its predicate.
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "ops-mailbox", Kind: storage.TargetEmail,
		Addresses: []string{"ops@example.com"}}, f.all, storage.SecretUse{}, storage.PeopleUse{}); err != nil {
		t.Fatal(err)
	}
	off, when := false, "severity_rank >= 50"
	if _, err := f.gw.UpdateActionRule(f.ctx, "", shippedRule, storage.ActionRulePatch{Enabled: &off, When: &when,
		Targets: &[]string{"platform-admins", "ops-mailbox"}}, f.all); err != nil {
		t.Fatal(err)
	}

	// The server restarts: a new gateway runs the boot seed again.
	gw, err := storage.NewPG(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(f.ctx, gw); err != nil {
		t.Fatal(err)
	}
	r, err = gw.GetActionRule(f.ctx, shippedRule)
	if err != nil {
		t.Fatal(err)
	}
	// The disable and the added target are the operator's and stay; the
	// predicate is the seed's own and is put back.
	if r.Enabled || r.When != "platform" || fmt.Sprint(r.Targets) != "[ops-mailbox platform-admins]" {
		t.Fatalf("after the restart the shipped rule reads as %+v", r)
	}

	// Disabled, it tells no one, admin or not.
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})
	if got := f.told(t, f.platformAlarm(t, "the server's queue is backing up")); len(got) != 0 {
		t.Fatalf("the disabled shipped rule told %v", got)
	}
}
