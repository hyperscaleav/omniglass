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
// the admin and owner roles on the inbox. The shipped rule and targets are
// official: an edit of what the seed owns and a delete are refused, and an
// operator's disable survives the next boot.

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

func TestTheShippedRuleTellsWhoeverHoldsAdminOrOwnerAboutAPlatformAlarm(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})

	// ada holds admin and root owner (which does not inherit admin): each
	// is told on the inbox. alice and bob hold viewer: neither is told.
	a := f.platformAlarm(t, "the server's queue is backing up")
	if got := fmt.Sprint(f.told(t, a)); got != "[ada/inbox/sent root/inbox/sent]" {
		t.Fatalf("a platform alarm told %v, want ada and root on the inbox", got)
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

	// The role is read at the transition: with ada's grant revoked, the next
	// platform alarm tells root alone.
	ada, err := f.gw.GetUser(f.ctx, f.ids["ada"], f.all)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.gw.RevokeGrant(f.ctx, "", ada.ID, ada.Grants[0].ID, f.all); err != nil {
		t.Fatal(err)
	}
	c := f.platformAlarm(t, "the server's disk is filling")
	if got := fmt.Sprint(f.told(t, c)); got != "[root/inbox/sent]" {
		t.Fatalf("after the revoke a platform alarm told %v, want root alone", got)
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
	// group alone; olive holds admin and owner, so both shipped targets name
	// her.
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})
	f.human(t, "grace")
	f.human(t, "olive", storage.GrantSpec{Role: "admin", ScopeKind: "all"}, storage.GrantSpec{Role: "owner", ScopeKind: "all"})
	for _, u := range []string{"ada", "grace"} {
		if err := f.gw.AddGroupMember(f.ctx, "", "admins", u, f.all); err != nil {
			t.Fatal(err)
		}
	}
	a := f.platformAlarm(t, "the server's queue is backing up")
	want := "[ada/inbox/sent grace/inbox/sent olive/inbox/sent root/inbox/sent]"
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
		{"binding a role without the role read", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin", Channels: []string{"inbox"}},
			storage.PeopleUse{Groups: true, Users: true}, storage.ErrNotificationTargetPeopleForbidden},
		{"binding a role without the user read", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin", Channels: []string{"inbox"}},
			storage.PeopleUse{Groups: true, Roles: true}, storage.ErrNotificationTargetPeopleForbidden},
		{"no role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Channels: []string{"inbox"}},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"an unknown role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "nobody", Channels: []string{"inbox"}},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"no channel", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a URL on a role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin", Channels: []string{"inbox"}, URL: "https://hooks.example.com/x"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a group on a role", storage.NotificationTargetSpec{Kind: storage.TargetRole, Role: "admin", Channels: []string{"inbox"}, UserGroup: "on-call"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a role on a group", storage.NotificationTargetSpec{Kind: storage.TargetUserGroup, UserGroup: "on-call", Channels: []string{"inbox"}, Role: "admin"},
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
		Role: "operator", Channels: []string{"email", "inbox"}}, f.all, storage.SecretUse{}, storage.PeopleUse{Users: true, Roles: true})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Role != "operator" || tg.RoleID == "" || fmt.Sprint(tg.Channels) != "[inbox email]" || tg.UserGroup != "" || tg.Official {
		t.Fatalf("a role target reads as %+v", tg)
	}
	// The test send is a webhook's or an email address's: a role target has
	// no address of its own to try.
	if _, err := f.gw.TargetForTest(f.ctx, "operators", f.all); !errors.Is(err, storage.ErrNotificationTargetInvalid) {
		t.Fatalf("a role target's test send: %v, want ErrNotificationTargetInvalid", err)
	}
	// The seed's own install of a name an operator's target holds lands
	// beside it in og (#1432), never over it: the bare name still reaches
	// the operator's.
	if err := f.gw.UpsertNotificationTarget(f.ctx, storage.NotificationTargetSpec{Name: "operators", Kind: storage.TargetRole,
		Role: "owner", Channels: []string{"inbox"}}); err != nil {
		t.Fatalf("a shipped target beside an operator's: %v", err)
	}
	if tg, err = f.gw.GetNotificationTarget(f.ctx, "operators", f.all); err != nil || tg.Role != "operator" || tg.Official {
		t.Fatalf("the operator's target after the shipped install reads as %+v, %v", tg, err)
	}
}

func TestARoleHoldersEmailIsJudgedAgainAtSend(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	f.relay(t)
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "admins-by-mail", Kind: storage.TargetRole,
		Role: "admin", Channels: []string{storage.DeliveryEmail}}, f.all, storage.SecretUse{}, bindAll); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateActionRule(f.ctx, "", storage.ActionRuleSpec{Name: "mail-admins", On: []string{"raised"}, When: "platform",
		Targets: []string{"admins-by-mail"}}, f.all); err != nil {
		t.Fatal(err)
	}
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})
	a := f.platformAlarm(t, "the server's queue is backing up")
	// Between the queue and the send, ada stops holding admin.
	ada, err := f.gw.GetUser(f.ctx, f.ids["ada"], f.all)
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

func TestTheShippedRoutingIsSeededOfficialAndADisableSurvivesARestart(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	r, err := f.gw.GetActionRule(f.ctx, shippedRule)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Enabled || !r.Official || r.When != "platform" || fmt.Sprint(r.On) != "[raised cleared]" ||
		fmt.Sprint(r.Targets) != "[platform-admins platform-owners]" {
		t.Fatalf("the shipped rule reads as %+v", r)
	}
	for name, role := range map[string]string{"platform-admins": "admin", "platform-owners": "owner"} {
		tg, err := f.gw.GetNotificationTarget(f.ctx, name, f.all)
		if err != nil {
			t.Fatal(err)
		}
		// Inbox alone until a send with no relay is skipped (#2016).
		if tg.Kind != storage.TargetRole || tg.Role != role || fmt.Sprint(tg.Channels) != "[inbox]" || !tg.Enabled || !tg.Official {
			t.Fatalf("the shipped target %s reads as %+v", name, tg)
		}
	}

	// What the seed owns is refused: the next boot would only put it back.
	when := "severity_rank >= 50"
	label := "Mine now"
	for what, patch := range map[string]storage.ActionRulePatch{
		"the predicate":          {When: &when},
		"the label":              {Label: &label},
		"dropping a shipped one": {Targets: &[]string{"platform-admins"}},
	} {
		if _, err := f.gw.UpdateActionRule(f.ctx, "", shippedRule, patch, f.all); !errors.Is(err, storage.ErrActionRuleOfficial) {
			t.Errorf("editing the shipped rule's %s: %v, want ErrActionRuleOfficial", what, err)
		}
	}
	if err := f.gw.DeleteActionRule(f.ctx, "", shippedRule, f.all); !errors.Is(err, storage.ErrActionRuleOfficial) {
		t.Errorf("deleting the shipped rule: %v, want ErrActionRuleOfficial", err)
	}
	if _, err := f.gw.UpdateNotificationTarget(f.ctx, "", "platform-admins", storage.NotificationTargetPatch{
		Channels: &[]string{"inbox", "email"}}, f.all, storage.SecretUse{}); !errors.Is(err, storage.ErrNotificationTargetOfficial) {
		t.Errorf("editing a shipped target's channels: %v, want ErrNotificationTargetOfficial", err)
	}
	if err := f.gw.DeleteNotificationTarget(f.ctx, "", "platform-owners", f.all); !errors.Is(err, storage.ErrNotificationTargetOfficial) {
		t.Errorf("deleting a shipped target: %v, want ErrNotificationTargetOfficial", err)
	}

	// What is the operator's is taken: the rule disabled, a mailbox added
	// beside the shipped targets, and a shipped target relabeled and
	// disabled.
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "ops-mailbox", Kind: storage.TargetEmail,
		Addresses: []string{"ops@example.com"}}, f.all, storage.SecretUse{}, storage.PeopleUse{}); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := f.gw.UpdateActionRule(f.ctx, "", shippedRule, storage.ActionRulePatch{Enabled: &off,
		Targets: &[]string{"platform-admins", "platform-owners", "ops-mailbox"}}, f.all); err != nil {
		t.Fatal(err)
	}
	owners := "Owners on call"
	if _, err := f.gw.UpdateNotificationTarget(f.ctx, "", "platform-owners", storage.NotificationTargetPatch{
		Label: &owners, Enabled: &off}, f.all, storage.SecretUse{}); err != nil {
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
	if r.Enabled || r.When != "platform" || fmt.Sprint(r.Targets) != "[ops-mailbox platform-admins platform-owners]" {
		t.Fatalf("after the restart the shipped rule reads as %+v", r)
	}
	tg, err := gw.GetNotificationTarget(f.ctx, "platform-owners", f.all)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Enabled || tg.Label != owners {
		t.Fatalf("after the restart the shipped owner target reads as %+v", tg)
	}

	// Disabled, it tells no one, admin or not.
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})
	if got := f.told(t, f.platformAlarm(t, "the server's queue is backing up")); len(got) != 0 {
		t.Fatalf("the disabled shipped rule told %v", got)
	}
}

// TestAnOperatorsTargetOfAShippedNameShadowsIt: the shipped targets live in og
// (#1432), so an operator's local target may take a shipped name. A bare name
// then reaches the operator's, the next boot leaves it alone, and the shipped
// rule keeps telling the role through the og target it links by id.
func TestAnOperatorsTargetOfAShippedNameShadowsIt(t *testing.T) {
	t.Parallel()
	f := newPeopleFixture(t)
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "platform-admins", Kind: storage.TargetEmail,
		Addresses: []string{"ops@example.com"}}, f.all, storage.SecretUse{}, storage.PeopleUse{}); err != nil {
		t.Fatalf("an operator's target named platform-admins: %v", err)
	}
	gw, err := storage.NewPG(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(f.ctx, gw); err != nil {
		t.Fatalf("a boot meeting the operator's target: %v", err)
	}
	tg, err := gw.GetNotificationTarget(f.ctx, "platform-admins", f.all)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Kind != storage.TargetEmail || tg.Official || fmt.Sprint(tg.Addresses) != "[ops@example.com]" {
		t.Fatalf("the bare name platform-admins reaches %+v, want the operator's mailbox", tg)
	}
	f.human(t, "ada", storage.GrantSpec{Role: "admin", ScopeKind: "all"})
	if got := fmt.Sprint(f.told(t, f.platformAlarm(t, "the server's queue is backing up"))); got != "[ada/inbox/sent root/inbox/sent]" {
		t.Fatalf("with the name shadowed a platform alarm told %v, want ada and root on the inbox", got)
	}
}
