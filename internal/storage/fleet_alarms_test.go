package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	got, _, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{}, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 || got[0].ID != worst.ID || got[1].ID != high.ID || got[2].ID != warn.ID {
		t.Fatalf("fleet alarms = %v, want [no audio, no video, fan slow]: active only, worst severity first", names(got))
	}
	if got[0].ComponentName != "fleet-mic" {
		t.Errorf("the worst alarm names component %q, want fleet-mic", got[0].ComponentName)
	}

	if got, _, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{Unacknowledged: true}, 100); err != nil || len(got) != 2 {
		t.Fatalf("unacknowledged = %v (%v), want the two nobody has looked at", names(got), err)
	}
	if got, _, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{}, 1); err != nil || len(got) != 1 || got[0].ID != worst.ID {
		t.Fatalf("capped at 1 = %v (%v), want the worst alone", names(got), err)
	}

	scoped := scope.Set{IDs: []string{ids["fleet-dsp"]}}
	if got, _, err := gw.ListFleetAlarms(ctx, scoped, storage.AlarmFilter{}, 100); err != nil || len(got) != 1 || got[0].ID != warn.ID {
		t.Fatalf("scoped to fleet-dsp = %v (%v), want its one alarm and nothing else", names(got), err)
	}
	if got, _, err := gw.ListFleetAlarms(ctx, scope.Set{}, storage.AlarmFilter{}, 100); err != nil || len(got) != 0 {
		t.Fatalf("empty scope = %v (%v), want nothing", names(got), err)
	}
	if got, _, err := gw.ListFleetAlarms(ctx, scope.Set{IDs: []string{"not-a-uuid"}}, storage.AlarmFilter{}, 100); err != nil || len(got) != 0 {
		t.Fatalf("malformed scope root = %v (%v), want nothing", names(got), err)
	}
}

// The alarm queue (#2333) is the fleet list an operator triages from: each row
// names where the component sits and which systems it belongs to, the list
// narrows by severity, location subtree, acknowledgement and snooze, and a long
// queue pages by a cursor in the same worst-first order rather than truncating.
func TestListFleetAlarmsFiltersPagesAndNamesLocationAndSystems(t *testing.T) {
	t.Parallel()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	for _, l := range []storage.LocationSpec{
		{Name: "q-hq", LocationType: "campus"},
		{Name: "q-r1", LocationType: "room", ParentName: strptr("q-hq")},
		{Name: "q-other", LocationType: "campus"},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all); err != nil {
			t.Fatalf("location %s: %v", l.Name, err)
		}
	}
	mk := func(name, loc string) {
		t.Helper()
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, LocationName: strptr(loc)}, all, all, all, all); err != nil {
			t.Fatalf("component %s: %v", name, err)
		}
	}
	mk("q-a", "q-r1")
	mk("q-b", "q-other")
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "q-sys"}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := gw.AddMember(ctx, "", "q-sys", "q-a", all, all, all, all); err != nil {
		t.Fatalf("member: %v", err)
	}
	raise := func(comp, sev, key string) *storage.Alarm {
		t.Helper()
		a, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{Severity: sev, Message: key, DedupKey: key})
		if err != nil {
			t.Fatalf("raise %s: %v", key, err)
		}
		return a
	}
	crit := raise("q-a", "disaster", "crit")
	warn := raise("q-a", "warning", "warn")
	far := raise("q-b", "high", "far")
	if _, err := gw.AcknowledgeAlarm(ctx, ackActor(t, dsn, "q-ops"), "q-a", warn.ID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if _, err := gw.SnoozeAlarm(ctx, ackActor(t, dsn, "q-ops2"), "q-b", far.ID, time.Hour); err != nil {
		t.Fatalf("snooze: %v", err)
	}

	ids := func(as []storage.Alarm) []string {
		out := []string{}
		for _, a := range as {
			out = append(out, a.Message)
		}
		return out
	}
	list := func(f storage.AlarmFilter) []string {
		t.Helper()
		got, _, err := gw.ListFleetAlarms(ctx, all, f, 100)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		return ids(got)
	}
	eq := func(got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	tr, fa := true, false

	rows, _, _ := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{}, 100)
	if len(rows) != 3 || rows[0].ID != crit.ID {
		t.Fatalf("rows = %v", ids(rows))
	}
	if rows[0].LocationName != "q-r1" || strings.Join(rows[0].LocationPath, ".") != "q-hq.q-r1" || strings.Join(rows[0].Systems, ",") != "q-sys" {
		t.Errorf("crit row location %q path %v systems %v, want q-r1 / q-hq.q-r1 / [q-sys]", rows[0].LocationName, rows[0].LocationPath, rows[0].Systems)
	}
	if rows[1].LocationName != "q-other" || len(rows[1].Systems) != 0 {
		t.Errorf("far row location %q systems %v, want q-other and no systems", rows[1].LocationName, rows[1].Systems)
	}

	eq(list(storage.AlarmFilter{Severities: []string{"disaster", "warning"}}), "crit", "warn")
	eq(list(storage.AlarmFilter{Location: "q-hq"}), "crit", "warn")
	eq(list(storage.AlarmFilter{Location: "q-other"}), "far")
	eq(list(storage.AlarmFilter{Acknowledged: &tr}), "warn")
	eq(list(storage.AlarmFilter{Acknowledged: &fa}), "crit", "far")
	eq(list(storage.AlarmFilter{Snoozed: &tr}), "far")
	eq(list(storage.AlarmFilter{Snoozed: &fa}), "crit", "warn")

	// Paging: one at a time walks the whole queue in order, then ends.
	var walked []string
	token := ""
	for range 5 {
		page, next, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{PageToken: token}, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		walked = append(walked, ids(page)...)
		if next == "" {
			break
		}
		token = next
	}
	eq(walked, "crit", "far", "warn")
	if _, _, err := gw.ListFleetAlarms(ctx, all, storage.AlarmFilter{PageToken: "garbage"}, 1); !errors.Is(err, storage.ErrPageToken) {
		t.Fatalf("bad token err = %v, want ErrPageToken", err)
	}
}
