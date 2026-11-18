package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The fleet's alarm list (#2025) is what is wrong across every component the
// caller may read, in one query: worst first, the Storage Gateway injecting the
// caller's component scope, so an agent finds what is red without walking every
// component and a scoped caller never learns an out-of-scope alarm exists.
func TestListFleetAlarmsIsTheActiveSetInScopeWorstFirst(t *testing.T) {
	t.Parallel()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	ids := map[string]string{}
	for _, c := range []string{"fleet-dsp", "fleet-mic", "fleet-cam"} {
		comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all)
		if err != nil {
			t.Fatalf("component %s: %v", c, err)
		}
		ids[c] = comp.ID
	}
	raise := func(comp, severity, msg string) *storage.Alarm {
		t.Helper()
		a, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{Severity: severity, Message: msg})
		if err != nil {
			t.Fatalf("raise %s: %v", msg, err)
		}
		return a
	}
	warn := raise("fleet-dsp", "warning", "fan slow")
	worst := raise("fleet-mic", "disaster", "no audio")
	high := raise("fleet-cam", "high", "no video")
	cleared := raise("fleet-cam", "high", "flapped")
	if err := gw.ClearAlarm(ctx, "", "fleet-cam", cleared.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := gw.AcknowledgeAlarm(ctx, ackActor(t, dsn, "fleet-ops"), "fleet-cam", high.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	names := func(as []storage.Alarm) []string {
		out := make([]string, 0, len(as))
		for _, a := range as {
			out = append(out, a.Message)
		}
		return out
	}
	got, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{}, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 || got[0].ID != worst.ID || got[1].ID != high.ID || got[2].ID != warn.ID {
		t.Fatalf("fleet alarms = %v, want [no audio, no video, fan slow]: active only, worst severity first", names(got))
	}
	if got[0].ComponentName != "fleet-mic" {
		t.Errorf("the worst alarm names component %q, want fleet-mic", got[0].ComponentName)
	}

	if got, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{Unacknowledged: true}, 100); err != nil || len(got) != 2 {
		t.Fatalf("unacknowledged = %v (%v), want the two nobody has looked at", names(got), err)
	}
	if got, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{}, 1); err != nil || len(got) != 1 || got[0].ID != worst.ID {
		t.Fatalf("capped at 1 = %v (%v), want the worst alone", names(got), err)
	}

	scoped := scope.Set{IDs: []string{ids["fleet-dsp"]}}
	if got, err := gw.ListFleetAlarms(ctx, scoped, storage.AlarmFilter{}, 100); err != nil || len(got) != 1 || got[0].ID != warn.ID {
		t.Fatalf("scoped to fleet-dsp = %v (%v), want its one alarm and nothing else", names(got), err)
	}
	if got, err := gw.ListFleetAlarms(ctx, scope.Set{}, storage.AlarmFilter{}, 100); err != nil || len(got) != 0 {
		t.Fatalf("empty scope = %v (%v), want nothing", names(got), err)
	}
	if got, err := gw.ListFleetAlarms(ctx, scope.Set{IDs: []string{"not-a-uuid"}}, storage.AlarmFilter{}, 100); err != nil || len(got) != 0 {
		t.Fatalf("malformed scope root = %v (%v), want nothing", names(got), err)
	}
}
