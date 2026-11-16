package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestARoleTargetReachesItsHoldersOverHTTP drives the role target over HTTP
// (#1987): binding one takes the group directory's read, it reads back with
// its role, a raise reaches the role's holders who can read the component,
// a :test of it is a 422, and the shipped routing is there to read.
func TestARoleTargetReachesItsHoldersOverHTTP(t *testing.T) {
	t.Parallel()
	p := newPeople(t)
	p.human("alice", "operator", "all", "")
	p.human("bob", "operator", "component", p.comps["display-2"])
	p.human("carol", "viewer", "all", "")
	p.human("tara", "target-admin", "all", "")

	if code, body := p.c.send(p.toks["tara"], http.MethodPost, "/notification-targets", map[string]any{"name": "operators-page", "kind": "role",
		"role": "operator", "channels": []string{"inbox"}}); code != http.StatusForbidden {
		t.Fatalf("binding a role without principal_group:read:admin = %d %s, want 403", code, body)
	}
	if code, body := p.c.send(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "nobody-page", "kind": "role",
		"role": "no-such-role", "channels": []string{"inbox"}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("binding an unknown role = %d %s, want 422", code, body)
	}
	var tg struct {
		Kind     string   `json:"kind"`
		Role     string   `json:"role"`
		RoleID   string   `json:"role_id"`
		Channels []string `json:"channels"`
	}
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "operators-page", "kind": "role",
		"role": "operator", "channels": []string{"inbox"}}, http.StatusCreated), &tg); err != nil {
		t.Fatal(err)
	}
	if tg.Kind != "role" || tg.Role != "operator" || tg.RoleID == "" || fmt.Sprint(tg.Channels) != "[inbox]" {
		t.Fatalf("the role target reads back %+v, want kind role, role operator, its uuid and [inbox]", tg)
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
	if rule.When != "platform" || fmt.Sprint(rule.Targets) != "[platform-admins]" || !rule.Official || !rule.Enabled {
		t.Fatalf("the shipped rule reads %+v, want when platform, targets [platform-admins], official and enabled", rule)
	}
	if err := json.Unmarshal(p.c.do(p.owner, http.MethodGet, "/notification-targets/platform-admins", nil, http.StatusOK), &tg); err != nil {
		t.Fatal(err)
	}
	if tg.Kind != "role" || tg.Role != "admin" || fmt.Sprint(tg.Channels) != "[inbox email]" {
		t.Fatalf("the shipped target reads %+v, want role admin on [inbox email]", tg)
	}
}
