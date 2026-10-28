package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// people is an API fixture with humans who can sign in: each username has a
// bearer token minted straight into the credential table, as `omniglass
// token` does.
type people struct {
	t     *testing.T
	ctx   context.Context
	dsn   string
	gw    *storage.PG
	c     *apiClient
	owner string
	toks  map[string]string
	ids   map[string]string
	comps map[string]string
}

func newPeople(t *testing.T) *people {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	// A custom role, written before the server reads its role index.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into role (name, permissions, inherits) values ('target-admin', '{"notification_target:*"}', '{}'),
		('settings-admin', '{"settings:*","platform:*","secret:read"}', '{}')`); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	p := &people{t: t, ctx: ctx, dsn: dsn, gw: gw, c: &apiClient{t: t, ctx: ctx, base: srv.URL},
		owner: bootstrapOwnerTok(t, ctx, gw), toks: map[string]string{}, ids: map[string]string{}, comps: map[string]string{}}
	all := scope.Set{All: true}
	for _, n := range []string{"display-1", "display-2"} {
		c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: n}, all, all, all, all)
		if err != nil {
			t.Fatal(err)
		}
		p.comps[n] = c.ID
	}
	return p
}

// human creates a person through the API, grants them a role, and mints
// their token.
func (p *people) human(username, role, scopeKind, scopeID string) {
	p.t.Helper()
	var pr struct{ ID string }
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/principals", map[string]any{"username": username, "email": username + "@example.com"}, http.StatusCreated), &pr); err != nil {
		p.t.Fatal(err)
	}
	g := map[string]any{"role": role, "scope_kind": scopeKind}
	if scopeID != "" {
		g["scope_id"] = scopeID
	}
	p.c.do(p.owner, http.MethodPost, "/principals/"+pr.ID+"/grants", g, http.StatusCreated)
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		p.t.Fatal(err)
	}
	conn, err := pgx.Connect(p.ctx, p.dsn)
	if err != nil {
		p.t.Fatal(err)
	}
	defer conn.Close(p.ctx)
	if _, err := conn.Exec(p.ctx, `insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, pr.ID, hash, prefix); err != nil {
		p.t.Fatal(err)
	}
	p.toks[username], p.ids[username] = tok, pr.ID
}

type inboxWire struct {
	Notifications []struct {
		ID        string `json:"id"`
		Alarm     string `json:"alarm"`
		Channel   string `json:"channel"`
		Recipient string `json:"recipient"`
		Subject   string `json:"subject"`
		ReadAt    *string
	} `json:"notifications"`
	Unread int `json:"unread"`
}

func (p *people) inbox(user string) inboxWire {
	p.t.Helper()
	var w inboxWire
	if err := json.Unmarshal(p.c.do(p.toks[user], http.MethodGet, "/auth/me/notifications", nil, http.StatusOK), &w); err != nil {
		p.t.Fatal(err)
	}
	return w
}

// TestAGroupIsToldInTheirInboxOnlyWhatEachCanRead drives the person half of
// the lane over HTTP (#1341, #1352): an admin binds a group to a target and
// a rule, a raise reaches only the members who can read the component, each
// in their own inbox; another's entry is a 404; a revoked grant empties the
// inbox at once; the recipient's name is the recipient's and the admin's
// alone; and the bound group cannot be deleted.
func TestAGroupIsToldInTheirInboxOnlyWhatEachCanRead(t *testing.T) {
	p := newPeople(t)
	p.human("alice", "viewer", "all", "")
	p.human("bob", "viewer", "component", p.comps["display-2"])
	p.c.do(p.owner, http.MethodPost, "/principal-groups", map[string]any{"name": "on-call"}, http.StatusCreated)
	for _, u := range []string{"alice", "bob"} {
		p.c.do(p.owner, http.MethodPost, "/principal-groups/on-call/members", map[string]any{"principal_id": u}, http.StatusNoContent)
	}
	// Binding a group takes the directory's read: a target admin without it
	// (a custom role holding notification_target:* alone) is refused, and
	// learns nothing about which groups exist.
	p.human("tara", "target-admin", "all", "")
	p.c.do(p.toks["tara"], http.MethodPost, "/notification-targets", map[string]any{"name": "ops-chat", "kind": "webhook",
		"url": "https://hooks.example.com/x"}, http.StatusCreated)
	for _, g := range []string{"on-call", "no-such-group"} {
		if code, body := p.c.send(p.toks["tara"], http.MethodPost, "/notification-targets", map[string]any{"name": "t-" + g, "kind": "principal_group",
			"principal_group": g, "channels": []string{"inbox"}}); code != http.StatusForbidden {
			t.Fatalf("binding %s without principal_group:read:admin = %d %s, want 403", g, code, body)
		}
	}

	p.c.do(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "on-call-page", "kind": "principal_group",
		"principal_group": "on-call", "channels": []string{"inbox"}}, http.StatusCreated)
	p.c.do(p.owner, http.MethodPost, "/action-rules", map[string]any{"name": "page-on-call", "on": []string{"raised"},
		"targets": []string{"on-call-page"}}, http.StatusCreated)

	var one, two struct{ ID string }
	_ = json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated), &one)
	_ = json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/components/display-2/alarms", map[string]any{"severity": "high", "message": "no audio"}, http.StatusCreated), &two)

	a, b := p.inbox("alice"), p.inbox("bob")
	if len(a.Notifications) != 2 || a.Unread != 2 {
		t.Fatalf("alice reads both displays; her inbox = %+v", a)
	}
	if len(b.Notifications) != 1 || b.Notifications[0].Alarm != two.ID || b.Notifications[0].Channel != "inbox" {
		t.Fatalf("bob reads display-2 alone; his inbox = %+v", b)
	}

	// bob cannot mark alice's display-1 entry read: it is not his.
	var alicesOne string
	for _, n := range a.Notifications {
		if n.Alarm == one.ID {
			alicesOne = n.ID
		}
	}
	p.c.do(p.toks["bob"], http.MethodPost, "/auth/me/notifications/"+alicesOne+":markRead", nil, http.StatusNotFound)
	p.c.do(p.toks["alice"], http.MethodPost, "/auth/me/notifications/"+alicesOne+":markRead", nil, http.StatusOK)
	if got := p.inbox("alice"); got.Unread != 1 {
		t.Fatalf("after marking one read alice has %d unread, want 1", got.Unread)
	}

	// The recipient's name: bob sees his own, not alice's; the owner sees both.
	var list struct {
		Notifications []struct {
			Recipient string `json:"recipient"`
			Withheld  bool   `json:"recipient_withheld"`
			Channel   string `json:"channel"`
		} `json:"notifications"`
	}
	_ = json.Unmarshal(p.c.do(p.toks["bob"], http.MethodGet, "/notifications?alarm="+two.ID, nil, http.StatusOK), &list)
	seen := []string{}
	for _, n := range list.Notifications {
		if n.Withheld {
			seen = append(seen, "withheld")
		} else {
			seen = append(seen, n.Recipient)
		}
	}
	if strings.Join(seen, ",") != "withheld,bob" && strings.Join(seen, ",") != "bob,withheld" {
		t.Fatalf("bob's view of display-2's rows = %v, want his own name and alice's withheld", seen)
	}
	tl := string(p.c.do(p.toks["bob"], http.MethodGet, "/components/display-2/alarms/"+two.ID+":timeline", nil, http.StatusOK))
	if strings.Contains(tl, `"alice"`) || !strings.Contains(tl, `"bob"`) {
		t.Fatalf("bob's timeline names someone it should not, or not him: %s", tl)
	}
	if owner := string(p.c.do(p.owner, http.MethodGet, "/notifications?alarm="+two.ID, nil, http.StatusOK)); !strings.Contains(owner, `"alice"`) || !strings.Contains(owner, `"bob"`) {
		t.Fatalf("the owner's view withholds a name: %s", owner)
	}
	// An admin grant scoped to one component carries principal:read:admin in
	// its permissions but reads no directory (the directory takes an
	// all-scope grant): the names stay withheld from it.
	p.human("lena", "admin", "component", p.comps["display-2"])
	if lena := string(p.c.do(p.toks["lena"], http.MethodGet, "/notifications?alarm="+two.ID, nil, http.StatusOK)); strings.Contains(lena, `"alice"`) || strings.Contains(lena, `"bob"`) || !strings.Contains(lena, `"recipient_withheld":true`) {
		t.Fatalf("a component-scoped admin read a recipient's name: %s", lena)
	}
	// A target admin without the directory reads the target, not who it names.
	if tg := string(p.c.do(p.toks["tara"], http.MethodGet, "/notification-targets/on-call-page", nil, http.StatusOK)); strings.Contains(tg, "on-call\"") {
		t.Fatalf("a target read named the group to a caller without the directory: %s", tg)
	}

	// alice loses her fleet-wide read and gains display-1 alone: the
	// display-2 entry is gone from her inbox at once.
	var grants struct {
		Grants []struct{ ID string } `json:"grants"`
	}
	_ = json.Unmarshal(p.c.do(p.owner, http.MethodGet, "/principals/"+p.ids["alice"], nil, http.StatusOK), &grants)
	p.c.do(p.owner, http.MethodPost, "/principals/"+p.ids["alice"]+"/grants", map[string]any{"role": "viewer", "scope_kind": "component", "scope_id": p.comps["display-1"]}, http.StatusCreated)
	p.c.do(p.owner, http.MethodDelete, "/principals/"+p.ids["alice"]+"/grants/"+grants.Grants[0].ID, nil, http.StatusNoContent)
	if got := p.inbox("alice"); len(got.Notifications) != 1 || got.Notifications[0].Alarm != one.ID {
		t.Fatalf("after losing display-2 alice's inbox = %+v, want the display-1 entry alone", got)
	}

	// The bound group cannot be deleted; the refusal names the target.
	if body := string(p.c.do(p.owner, http.MethodDelete, "/principal-groups/on-call", nil, http.StatusConflict)); !strings.Contains(body, "on-call-page") {
		t.Fatalf("deleting a bound group = %s, want a refusal naming on-call-page", body)
	}
}
