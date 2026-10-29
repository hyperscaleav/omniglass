package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The snooze routes end to end (#1318), driven as a caller drives them, on
// the acknowledgement's fixture (newAckFixture): every principal narrow, the
// owner only the control that proves a refusal is a boundary. alarm:snooze is
// the capability, and the component tier its grant is scoped at is the reach,
// resolved exactly as alarm:acknowledge's is (ADR-0109).

type snoozeView struct {
	ID           string     `json:"id"`
	Active       bool       `json:"active"`
	Acknowledged bool       `json:"acknowledged"`
	SnoozedUntil *time.Time `json:"snoozed_until"`
	SnoozedBy    string     `json:"snoozed_by"`
}

func snoozePath(component, id, verb string) string {
	return "/components/" + component + "/alarms/" + id + ":" + verb
}

func decodeSnooze(t *testing.T, raw []byte) snoozeView {
	t.Helper()
	var v snoozeView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode alarm: %v", err)
	}
	return v
}

// TestAnOperatorSnoozesAndUnsnoozesInScope is the happy path by the narrow
// principal: the alarm reads the snooze, the list read carries it, and an
// unsnooze takes it off.
func TestAnOperatorSnoozesAndUnsnoozesInScope(t *testing.T) {
	f := newAckFixture(t)

	got := decodeSnooze(t, f.c.do(f.responder, http.MethodPost, snoozePath(f.mine, f.mineAlarm, "snooze"),
		map[string]any{"duration": "1h"}, http.StatusOK))
	if got.SnoozedUntil == nil || time.Until(*got.SnoozedUntil) < 59*time.Minute || got.SnoozedBy != "responder" {
		t.Fatalf("snooze returned %+v, want about an hour by responder", got)
	}
	if got.Acknowledged || !got.Active {
		t.Errorf("snoozing moved the alarm: %+v", got)
	}

	var env struct {
		Alarms []snoozeView `json:"alarms"`
	}
	if err := json.Unmarshal(f.c.do(f.watcher, http.MethodGet, "/components/"+f.mine+"/alarms", nil, http.StatusOK), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Alarms) != 1 || env.Alarms[0].SnoozedBy != "responder" {
		t.Fatalf("a viewer reads %+v, want the snooze on the alarm", env.Alarms)
	}

	got = decodeSnooze(t, f.c.do(f.responder, http.MethodPost, snoozePath(f.mine, f.mineAlarm, "unsnooze"), nil, http.StatusOK))
	if got.SnoozedUntil != nil || got.SnoozedBy != "" {
		t.Errorf("unsnooze returned %+v, want no snooze", got)
	}
	// Unsnoozing a condition that is not snoozed is a 200 that writes nothing.
	f.c.do(f.responder, http.MethodPost, snoozePath(f.mine, f.mineAlarm, "unsnooze"), nil, http.StatusOK)
}

// TestSnoozeRefusesWithoutTheCapability: a viewer and a deploy-only principal
// hold no alarm:snooze, so the gate refuses them before scope is consulted.
func TestSnoozeRefusesWithoutTheCapability(t *testing.T) {
	f := newAckFixture(t)
	deployer := principalWithGrants(t, f.c.ctx, f.dsn, "deployer", []grant{{role: "deploy", scopeKind: "all"}})
	for _, tok := range []string{f.watcher, deployer} {
		for _, verb := range []string{"snooze", "unsnooze"} {
			f.c.do(tok, http.MethodPost, snoozePath(f.mine, f.mineAlarm, verb), map[string]any{"duration": "1h"}, http.StatusForbidden)
		}
	}
	if got := readSnooze(t, f, f.mine, f.mineAlarm); got.SnoozedUntil != nil {
		t.Error("a refused snooze landed anyway")
	}
}

// TestSnoozeRefusesOutsideTheScope is the ABAC layer: the responder holds
// alarm:snooze on one component and reads the whole fleet, so the far
// component is a truthful 403; a principal whose grant reaches one component
// and reads nothing else is owed the non-disclosing 404 on the far one.
func TestSnoozeRefusesOutsideTheScope(t *testing.T) {
	f := newAckFixture(t)
	f.c.do(f.responder, http.MethodPost, snoozePath(f.theirs, f.theirsAlarm, "snooze"), map[string]any{"duration": "1h"}, http.StatusForbidden)

	narrow := principalWithGrants(t, f.c.ctx, f.dsn, "narrow", []grant{{role: "operator", scopeKind: "component", scopeID: f.mineCompID}})
	f.c.do(narrow, http.MethodPost, snoozePath(f.theirs, f.theirsAlarm, "snooze"), map[string]any{"duration": "1h"}, http.StatusNotFound)
	f.c.do(narrow, http.MethodPost, snoozePath(f.mine, f.mineAlarm, "snooze"), map[string]any{"duration": "1h"}, http.StatusOK)

	if got := readSnooze(t, f, f.theirs, f.theirsAlarm); got.SnoozedUntil != nil {
		t.Error("the out-of-scope snooze landed anyway")
	}
}

// TestSnoozeDurationIsBoundedAndMissesAre404 pins the 422 naming the bound
// (one minute to seven days, decision 3) and the misses.
func TestSnoozeDurationIsBoundedAndMissesAre404(t *testing.T) {
	f := newAckFixture(t)
	for _, d := range []string{"30s", "169h", "8d", "soon", ""} {
		raw := f.c.do(f.owner, http.MethodPost, snoozePath(f.mine, f.mineAlarm, "snooze"), map[string]any{"duration": d}, http.StatusUnprocessableEntity)
		if !strings.Contains(string(raw), "1m") || !strings.Contains(string(raw), "7d") {
			t.Errorf("a %q snooze was refused with %s, want the bound named", d, raw)
		}
	}
	for _, d := range []string{"1m", "15m", "24h", "7d"} {
		f.c.do(f.owner, http.MethodPost, snoozePath(f.mine, f.mineAlarm, "snooze"), map[string]any{"duration": d}, http.StatusOK)
	}
	f.c.do(f.owner, http.MethodPost, snoozePath(f.mine, "00000000-0000-0000-0000-000000000000", "snooze"), map[string]any{"duration": "1h"}, http.StatusNotFound)
	f.c.do(f.owner, http.MethodPost, snoozePath("codec-404", f.mineAlarm, "snooze"), map[string]any{"duration": "1h"}, http.StatusNotFound)
	f.c.do(f.owner, http.MethodPost, snoozePath(f.mine, f.theirsAlarm, "unsnooze"), nil, http.StatusNotFound)
}

func readSnooze(t *testing.T, f *ackFixture, component, id string) snoozeView {
	t.Helper()
	var env struct {
		Alarms []snoozeView `json:"alarms"`
	}
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodGet, "/components/"+component+"/alarms?include_cleared=true", nil, http.StatusOK), &env); err != nil {
		t.Fatal(err)
	}
	for _, a := range env.Alarms {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("alarm %s not in %+v", id, env.Alarms)
	return snoozeView{}
}
