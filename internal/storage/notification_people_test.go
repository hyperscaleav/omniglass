package storage_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// People as notification targets (#1341, #1352), against a real Postgres: a
// group target expands to its human members at the transition, each kept only
// when their own component read scope admits the alarm's component; an inbox
// row is delivered by its commit; the inbox reads only the caller's rows,
// scope-filtered again at read; a recipient's name is withheld from everyone
// but the recipient and the principal directory's admins; and a group or a
// person a target binds cannot be deleted from under it.

type peopleFixture struct {
	gw    *storage.PG
	dsn   string
	ctx   context.Context
	all   scope.Set
	comps map[string]string // component name -> uuid
	ids   map[string]string // username -> principal uuid
}

var bindAll = storage.PeopleUse{Groups: true, Principals: true}

func newPeopleFixture(t *testing.T) *peopleFixture {
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
	f := &peopleFixture{gw: gw, dsn: dsn, ctx: ctx, all: scope.Set{All: true}, comps: map[string]string{}, ids: map[string]string{}}
	for _, name := range []string{"display-1", "display-2"} {
		c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, f.all, f.all, f.all, f.all)
		if err != nil {
			t.Fatal(err)
		}
		f.comps[name] = c.ID
	}
	// An owner, so a member may be archived; outside the group.
	f.human(t, "root", storage.GrantSpec{Role: "owner", ScopeKind: "all"})
	// alice reads the whole fleet; bob reads display-2 alone.
	f.human(t, "alice", storage.GrantSpec{Role: "viewer", ScopeKind: "all"})
	f.human(t, "bob", storage.GrantSpec{Role: "viewer", ScopeKind: "component", ScopeID: f.comps["display-2"]})
	if _, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "on-call"}, f.all); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"alice", "bob"} {
		if err := gw.AddGroupMember(ctx, "", "on-call", u, f.all); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *peopleFixture) human(t *testing.T, username string, grants ...storage.GrantSpec) string {
	t.Helper()
	pr, err := f.gw.CreateHumanPrincipal(f.ctx, "", storage.HumanSpec{Username: username, Email: username + "@example.com"}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if _, err := f.gw.CreateGrant(f.ctx, "", pr.ID, g, f.all); err != nil {
			t.Fatal(err)
		}
	}
	f.ids[username] = pr.ID
	return pr.ID
}

func (f *peopleFixture) groupRule(t *testing.T) {
	t.Helper()
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "on-call-page", Kind: storage.TargetPrincipalGroup,
		PrincipalGroup: "on-call", Channels: []string{storage.DeliveryInbox}}, f.all, storage.SecretUse{}, bindAll); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateActionRule(f.ctx, "", storage.ActionRuleSpec{Name: "page-on-call", On: []string{"raised", "cleared"},
		When: "severity_rank >= 40", Targets: []string{"on-call-page"}}, f.all); err != nil {
		t.Fatal(err)
	}
}

func (f *peopleFixture) rows(t *testing.T, alarmID string) []storage.Notification {
	t.Helper()
	ns, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{AlarmID: alarmID}, f.all, storage.RecipientView{All: true})
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

func recipients(ns []storage.Notification) []string {
	out := []string{}
	for _, n := range ns {
		out = append(out, n.Recipient+"/"+n.Channel+"/"+n.Status)
	}
	return out
}

func TestAGroupTargetQueuesOneRowPerMemberWhoCanReadTheAlarm(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	// A service account in the group is skipped at expansion: recipients are
	// people.
	if _, err := f.gw.ExportServicePrincipal(f.ctx, "ingest-bot", "on-call"); err != nil {
		t.Fatal(err)
	}

	// display-1: alice reads it, bob does not. Only alice is told.
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if got := recipients(f.rows(t, a.ID)); fmt.Sprint(got) != "[alice/inbox/sent]" {
		t.Fatalf("display-1's raise queued %v, want alice's inbox entry alone", got)
	}
	// An inbox row is delivered by its own commit: the tick never takes it.
	if n, err := f.gw.NotificationBacklog(f.ctx); err != nil || n != 0 {
		t.Fatalf("backlog = %d, %v; an inbox row is not queued", n, err)
	}

	// display-2: both read it, so both are told, each on their own row.
	b, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatal(err)
	}
	if got := recipients(f.rows(t, b.ID)); fmt.Sprint(got) != "[alice/inbox/sent bob/inbox/sent]" && fmt.Sprint(got) != "[bob/inbox/sent alice/inbox/sent]" {
		t.Fatalf("display-2's raise queued %v, want one row each for alice and bob", got)
	}

	// A re-raise of the open condition writes no person rows.
	if _, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"}); err != nil {
		t.Fatal(err)
	}
	if got := f.rows(t, b.ID); len(got) != 2 {
		t.Fatalf("a re-raise queued more person rows: %v", recipients(got))
	}

	// A raise whose transaction rolls back writes no person row.
	before, _ := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{}, f.all, storage.RecipientView{All: true})
	if err := f.gw.ExportRaiseThenFail(f.ctx, "display-1", storage.AlarmSpec{Severity: "critical", Message: "rolled back"}); err == nil {
		t.Fatal("the raise did not fail")
	}
	after, _ := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{}, f.all, storage.RecipientView{All: true})
	if len(after) != len(before) {
		t.Fatalf("a rolled-back raise left %d rows, want %d", len(after), len(before))
	}
}

func TestGroupMembershipIsReadAtTheTransition(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	f.human(t, "carol", storage.GrantSpec{Role: "viewer", ScopeKind: "all"})
	// carol joins after the rule was written, and alice leaves: no rule edited.
	if err := f.gw.AddGroupMember(f.ctx, "", "on-call", "carol", f.all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.RemoveGroupMember(f.ctx, "", "on-call", "alice", f.all); err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if got := recipients(f.rows(t, a.ID)); fmt.Sprint(got) != "[carol/inbox/sent]" {
		t.Fatalf("after the membership change the raise queued %v, want carol alone", got)
	}
	// An archived member is told nothing.
	if err := f.gw.ArchivePrincipal(f.ctx, "", f.ids["carol"], f.all); err != nil {
		t.Fatal(err)
	}
	b, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatal(err)
	}
	if got := recipients(f.rows(t, b.ID)); fmt.Sprint(got) != "[bob/inbox/sent]" {
		t.Fatalf("with carol archived the raise queued %v, want bob alone", got)
	}
}

func TestAPersonReachedTwiceByOneRuleIsToldOnce(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "alice-direct", Kind: storage.TargetPrincipal,
		Principal: "alice", Channels: []string{storage.DeliveryInbox}}, f.all, storage.SecretUse{}, bindAll); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.UpdateActionRule(f.ctx, "", "page-on-call", storage.ActionRulePatch{Targets: &[]string{"on-call-page", "alice-direct"}}, f.all); err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	if got := recipients(f.rows(t, a.ID)); fmt.Sprint(got) != "[alice/inbox/sent]" {
		t.Fatalf("alice through the group and directly = %v, want one row", got)
	}
}

// The recipient filter is set-based: the statements a transition costs do
// not grow with the group's size.
func TestTheRecipientFilterCostDoesNotGrowWithTheGroup(t *testing.T) {
	cost := func(members int) int {
		gw, counter := storagetest.NewCountingDB(t)
		ctx := context.Background()
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatal(err)
		}
		all := scope.Set{All: true}
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		if _, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "on-call"}, all); err != nil {
			t.Fatal(err)
		}
		for i := range members {
			u := fmt.Sprintf("member-%d", i)
			pr, err := gw.CreateHumanPrincipal(ctx, "", storage.HumanSpec{Username: u}, all)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gw.CreateGrant(ctx, "", pr.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, all); err != nil {
				t.Fatal(err)
			}
			if err := gw.AddGroupMember(ctx, "", "on-call", u, all); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := gw.CreateNotificationTarget(ctx, "", storage.NotificationTargetSpec{Name: "on-call-page", Kind: storage.TargetPrincipalGroup,
			PrincipalGroup: "on-call", Channels: []string{storage.DeliveryInbox}}, all, storage.SecretUse{}, bindAll); err != nil {
			t.Fatal(err)
		}
		if _, err := gw.CreateActionRule(ctx, "", storage.ActionRuleSpec{Name: "page", On: []string{"raised"}, Targets: []string{"on-call-page"}}, all); err != nil {
			t.Fatal(err)
		}
		counter.Reset()
		if _, err := gw.RaiseAlarm(ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "x"}); err != nil {
			t.Fatal(err)
		}
		return counter.N()
	}
	if two, twenty := cost(2), cost(20); two != twenty {
		t.Fatalf("a raise to a group of 2 cost %d statements and to a group of 20 cost %d; the filter is per member", two, twenty)
	}
}

func TestTheInboxIsTheCallersAloneAndScopedAtRead(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := f.ids["alice"], f.ids["bob"]
	inbox, err := f.gw.ListInbox(f.ctx, alice, storage.InboxFilter{}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox.Notifications) != 1 || inbox.Unread != 1 || inbox.Notifications[0].AlarmID != a.ID || inbox.Notifications[0].ReadAt != nil {
		t.Fatalf("alice's inbox = %+v", inbox)
	}
	entry := inbox.Notifications[0].ID
	// bob cannot mark alice's entry read: it is not his, so it does not exist.
	if _, err := f.gw.MarkInboxRead(f.ctx, bob, bob, entry, f.all); !errors.Is(err, storage.ErrNotificationNotFound) {
		t.Fatalf("bob marking alice's entry = %v, want not found", err)
	}
	if _, err := f.gw.MarkInboxRead(f.ctx, alice, alice, entry, f.all); err != nil {
		t.Fatal(err)
	}
	inbox, _ = f.gw.ListInbox(f.ctx, alice, storage.InboxFilter{}, f.all)
	if inbox.Unread != 0 || inbox.Notifications[0].ReadAt == nil {
		t.Fatalf("after marking read alice's inbox = %+v", inbox)
	}
	// alice loses display-2: the entry about it is unreadable at once.
	only1 := scope.Set{IDs: []string{f.comps["display-1"]}}
	inbox, _ = f.gw.ListInbox(f.ctx, alice, storage.InboxFilter{}, only1)
	if len(inbox.Notifications) != 0 || inbox.Unread != 0 {
		t.Fatalf("an entry outside alice's read scope still reads: %+v", inbox)
	}
	if _, err := f.gw.MarkInboxRead(f.ctx, alice, alice, entry, only1); !errors.Is(err, storage.ErrNotificationNotFound) {
		t.Fatalf("marking an entry outside the scope = %v, want not found", err)
	}
	// Mark all read reaches only the caller's in-scope unread rows.
	b, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"})
	if err != nil {
		t.Fatal(err)
	}
	_ = b
	n, err := f.gw.MarkInboxAllRead(f.ctx, bob, bob, f.all)
	if err != nil || n != 1 {
		t.Fatalf("bob marking all read = %d, %v; want his one entry", n, err)
	}
	inbox, _ = f.gw.ListInbox(f.ctx, alice, storage.InboxFilter{}, f.all)
	if inbox.Unread != 1 {
		t.Fatalf("bob's mark-all touched alice's inbox: %+v", inbox)
	}
}

func TestARecipientsNameIsTheirsAndTheAdminsAlone(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatal(err)
	}
	alice := f.ids["alice"]
	for _, c := range []struct {
		view storage.RecipientView
		want string
	}{
		{storage.RecipientView{All: true}, "[alice bob]"},
		{storage.RecipientView{Self: alice}, "[alice -]"},
		{storage.RecipientView{Self: f.ids["bob"]}, "[- bob]"},
		{storage.RecipientView{}, "[- -]"},
	} {
		ns, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{AlarmID: a.ID}, f.all, c.view)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, n := range ns {
			if n.Channel != storage.DeliveryInbox || !n.Personal {
				t.Fatalf("a person row reads as %+v", n)
			}
			names[n.Recipient] = true
		}
		got := "["
		for i, u := range []string{"alice", "bob"} {
			if i > 0 {
				got += " "
			}
			if names[u] {
				got += u
			} else {
				got += "-"
			}
		}
		got += "]"
		if got != c.want {
			t.Fatalf("view %+v saw %s, want %s", c.view, got, c.want)
		}
		tl, err := f.gw.AlarmTimeline(f.ctx, f.comps["display-2"], a.ID, c.view)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range tl {
			if e.Kind == "notification" && e.Recipient != "" && !names[e.Recipient] {
				t.Fatalf("view %+v: the timeline named %q where the list withheld it", c.view, e.Recipient)
			}
		}
	}
}

func TestABoundGroupOrPersonCannotBeDeletedFromUnderATarget(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "bob-direct", Kind: storage.TargetPrincipal,
		Principal: "bob", Channels: []string{storage.DeliveryInbox}}, f.all, storage.SecretUse{}, bindAll); err != nil {
		t.Fatal(err)
	}
	var bound *storage.TargetBindingError
	err := f.gw.DeleteGroup(f.ctx, "", "on-call", f.all)
	if !errors.As(err, &bound) || bound.Target != "on-call-page" {
		t.Fatalf("deleting a bound group = %v, want a refusal naming on-call-page", err)
	}
	if err := f.gw.ArchivePrincipal(f.ctx, "", f.ids["bob"], f.all); err != nil {
		t.Fatal(err)
	}
	err = f.gw.PurgePrincipal(f.ctx, "", f.ids["bob"], f.all)
	if !errors.As(err, &bound) || bound.Target != "bob-direct" {
		t.Fatalf("purging a bound person = %v, want a refusal naming bob-direct", err)
	}
}

func TestAPersonTargetIsValidatedWhenWritten(t *testing.T) {
	f := newPeopleFixture(t)
	if _, err := f.gw.ExportServicePrincipal(f.ctx, "ingest-bot", ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		spec storage.NotificationTargetSpec
		use  storage.PeopleUse
		want error
	}{
		{"binding a group without the directory's read", storage.NotificationTargetSpec{Kind: storage.TargetPrincipalGroup, PrincipalGroup: "on-call", Channels: []string{"inbox"}},
			storage.PeopleUse{Principals: true}, storage.ErrNotificationTargetPeopleForbidden},
		{"binding a person without the directory's read", storage.NotificationTargetSpec{Kind: storage.TargetPrincipal, Principal: "alice", Channels: []string{"inbox"}},
			storage.PeopleUse{Groups: true}, storage.ErrNotificationTargetPeopleForbidden},
		{"an unknown group", storage.NotificationTargetSpec{Kind: storage.TargetPrincipalGroup, PrincipalGroup: "nobody", Channels: []string{"inbox"}},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a service account", storage.NotificationTargetSpec{Kind: storage.TargetPrincipal, Principal: "ingest-bot", Channels: []string{"inbox"}},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"no channel", storage.NotificationTargetSpec{Kind: storage.TargetPrincipalGroup, PrincipalGroup: "on-call"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"an unknown channel", storage.NotificationTargetSpec{Kind: storage.TargetPrincipalGroup, PrincipalGroup: "on-call", Channels: []string{"sms"}},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a URL on a group", storage.NotificationTargetSpec{Kind: storage.TargetPrincipalGroup, PrincipalGroup: "on-call", Channels: []string{"inbox"}, URL: "https://hooks.example.com/x"},
			bindAll, storage.ErrNotificationTargetInvalid},
		{"a group on a webhook", storage.NotificationTargetSpec{Kind: storage.ChannelWebhook, URL: "https://hooks.example.com/x", PrincipalGroup: "on-call"},
			bindAll, storage.ErrNotificationTargetInvalid},
	} {
		c.spec.Name = "t-1"
		if _, err := f.gw.CreateNotificationTarget(f.ctx, "", c.spec, f.all, storage.SecretUse{}, c.use); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	tg, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "on-call-page", Kind: storage.TargetPrincipalGroup,
		PrincipalGroup: "on-call", Channels: []string{"inbox", "inbox"}}, f.all, storage.SecretUse{}, bindAll)
	if err != nil {
		t.Fatal(err)
	}
	if tg.PrincipalGroup != "on-call" || fmt.Sprint(tg.Channels) != "[inbox]" || tg.URL != "" {
		t.Fatalf("a group target reads as %+v", tg)
	}
}

// The recipient filter runs inside the alarm transition, under #618's
// savepoint: when it fails (here, the role index cannot be read), the
// transition still commits, and no row about any person is written.
func TestARecipientFilterThatFailsNeverFailsTheTransition(t *testing.T) {
	f := newPeopleFixture(t)
	f.groupRule(t)
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	if _, err := conn.Exec(f.ctx, `alter table role rename to role_hidden`); err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if _, rerr := conn.Exec(f.ctx, `alter table role_hidden rename to role`); rerr != nil {
		t.Fatal(rerr)
	}
	if err != nil {
		t.Fatalf("the raise failed with its recipient filter: %v", err)
	}
	if got := f.rows(t, a.ID); len(got) != 0 {
		t.Fatalf("a failed filter wrote %v", recipients(got))
	}
	open, err := f.gw.ListAlarms(f.ctx, "display-2", storage.AlarmFilter{})
	if err != nil || len(open) != 1 {
		t.Fatalf("the alarm did not commit: %v, %v", open, err)
	}
}

// A webhook row has no recipient to withhold: its failure reason reads to
// everyone who reads the alarm.
func TestAWebhookRowsReasonIsNotWithheld(t *testing.T) {
	f := newPeopleFixture(t)
	if _, err := f.gw.CreateNotificationTarget(f.ctx, "", storage.NotificationTargetSpec{Name: "ops-chat", Kind: storage.ChannelWebhook,
		URL: "https://hooks.example.com/x"}, f.all, storage.SecretUse{}, storage.PeopleUse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateActionRule(f.ctx, "", storage.ActionRuleSpec{Name: "chat", On: []string{"raised"}, Targets: []string{"ops-chat"}}, f.all); err != nil {
		t.Fatal(err)
	}
	a, err := f.gw.RaiseAlarm(f.ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.DeliverNext(f.ctx, func(context.Context, storage.NotificationSend) storage.SendResult {
		return storage.SendResult{Status: 503, Reason: "the receiver answered 503"}
	}); err != nil {
		t.Fatal(err)
	}
	ns, err := f.gw.ListNotifications(f.ctx, storage.NotificationFilter{AlarmID: a.ID}, f.all, storage.RecipientView{Self: f.ids["bob"]})
	if err != nil || len(ns) != 1 || ns[0].LastError != "the receiver answered 503" {
		t.Fatalf("a webhook row's reason read by a non-admin = %+v, %v", ns, err)
	}
}
