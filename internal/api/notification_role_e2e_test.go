package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestARoleTargetReachesItsHoldersOverHTTP drives the role target over HTTP
// (#1987): binding one takes the role read and the user read, its role is
// shown only to a caller who reads roles, a raise reaches the role's holders
// who can read the component, a :test of it is a 422, and the shipped routing
// is there to read and refuses what the seed owns.
func TestARoleTargetReachesItsHoldersOverHTTP(t *testing.T) {
	t.Parallel()
	p := newPeople(t)
	p.person("alice", "operator", "all", "")
	p.person("bob", "operator", "component", p.comps["display-2"])
	p.person("carol", "viewer", "all", "")
	p.person("tara", "target-admin", "all", "")
	p.person("uma", "target-user-admin", "all", "")

	for who, why := range map[string]string{"tara": "the user and role reads", "uma": "role:read:admin"} {
		if code, body := p.c.send(p.toks[who], http.MethodPost, "/notification-targets", map[string]any{"name": "operators-page", "kind": "role",
			"role": "operator", "channels": []string{"inbox"}}); code != http.StatusForbidden {
			t.Fatalf("binding a role without %s = %d %s, want 403", why, code, body)
		}
	}
	if code, body := p.c.send(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "nobody-page", "kind": "role",
		"role": "no-such-role", "channels": []string{"inbox"}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("binding an unknown role = %d %s, want 422", code, body)
	}
	type targetWire struct {
		Kind     string   `json:"kind"`
		Role     string   `json:"role"`
		RoleID   string   `json:"role_id"`
		Channels []string `json:"channels"`
		Official bool     `json:"official"`
	}
	var tg targetWire
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "operators-page", "kind": "role",
		"role": "operator", "channels": []string{"inbox"}}, http.StatusCreated), &tg); err != nil {
		t.Fatal(err)
	}
	if tg.Kind != "role" || tg.Role != "operator" || tg.RoleID == "" || fmt.Sprint(tg.Channels) != "[inbox]" || tg.Official {
		t.Fatalf("the role target reads back %+v, want kind role, role operator, its uuid and [inbox]", tg)
	}
	// A target read is not a role read: tara reads the target, not its role.
	var hidden targetWire
	if err := json.Unmarshal(p.c.do(p.toks["tara"], http.MethodGet, "/notification-targets/operators-page", nil, http.StatusOK), &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden.Kind != "role" || hidden.Role != "" || hidden.RoleID != "" {
		t.Fatalf("tara reads the role target as %+v, want its kind and no role", hidden)
	}
	p.c.do(p.owner, http.MethodPost, "/notification-targets/operators-page:test", nil, http.StatusUnprocessableEntity)
	p.c.do(p.owner, http.MethodPost, "/action-rules", map[string]any{"name": "page-operators", "on": []string{"raised"},
		"targets": []string{"operators-page"}}, http.StatusCreated)

	var one struct{ ID string }
	_ = json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated), &one)
	if a := p.inbox("alice"); len(a.Notifications) != 1 || a.Notifications[0].Alarm != one.ID {
		t.Fatalf("alice holds operator over everything; her inbox = %+v", a)
	}
	if b := p.inbox("bob"); len(b.Notifications) != 0 {
		t.Fatalf("bob holds operator on display-2 alone and cannot read display-1; his inbox = %+v", b)
	}
	if c := p.inbox("carol"); len(c.Notifications) != 0 {
		t.Fatalf("carol does not hold operator; her inbox = %+v", c)
	}

	// The shipped routing reads as any rule and target do.
	var rule struct {
		When     string   `json:"when"`
		Targets  []string `json:"targets"`
		Official bool     `json:"official"`
		Enabled  bool     `json:"enabled"`
	}
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodGet, "/action-rules/platform-alarms-to-admins", nil, http.StatusOK), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.When != "platform" || fmt.Sprint(rule.Targets) != "[platform-admins platform-owners]" || !rule.Official || !rule.Enabled {
		t.Fatalf("the shipped rule reads %+v, want when platform, both shipped targets, official and enabled", rule)
	}
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodGet, "/notification-targets/platform-owners", nil, http.StatusOK), &tg); err != nil {
		t.Fatal(err)
	}
	if tg.Kind != "role" || tg.Role != "owner" || fmt.Sprint(tg.Channels) != "[inbox]" || !tg.Official {
		t.Fatalf("the shipped owner target reads %+v, want role owner on [inbox], official", tg)
	}

	// What the seed owns is a 409; the enabled switch is the operator's.
	p.c.do(p.owner, http.MethodDelete, "/action-rules/platform-alarms-to-admins", nil, http.StatusConflict)
	p.c.do(p.owner, http.MethodPatch, "/action-rules/platform-alarms-to-admins", map[string]any{"when": "true"}, http.StatusConflict)
	p.c.do(p.owner, http.MethodDelete, "/notification-targets/platform-admins", nil, http.StatusConflict)
	p.c.do(p.owner, http.MethodPatch, "/notification-targets/platform-admins", map[string]any{"channels": []string{"inbox", "email"}}, http.StatusConflict)
	p.c.do(p.owner, http.MethodPatch, "/notification-targets/platform-admins", map[string]any{"enabled": false}, http.StatusOK)
}

// TestTheBootstrapOwnerHearsAPlatformAlarm is the default's point (#1987): on
// a fresh install, the one person there is (the bootstrap owner, who holds
// owner, not admin) is told in their inbox when the platform raises an alarm.
func TestTheBootstrapOwnerHearsAPlatformAlarm(t *testing.T) {
	t.Parallel()
	p := newPeople(t)
	id, err := p.gw.EnsureServerComponent(p.ctx, "og-server-1")
	if err != nil {
		t.Fatal(err)
	}
	var a struct{ ID string }
	_ = json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/components/"+id+"/alarms", map[string]any{"severity": "high",
		"message": "the server's queue is backing up"}, http.StatusCreated), &a)
	var w inboxWire
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodGet, "/auth/me/notifications", nil, http.StatusOK), &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Notifications) != 1 || w.Notifications[0].Alarm != a.ID || w.Notifications[0].Channel != "inbox" {
		t.Fatalf("the bootstrap owner's inbox = %+v, want the platform alarm", w)
	}
}
