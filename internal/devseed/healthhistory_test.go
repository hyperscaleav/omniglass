package devseed_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// #2728: a scenario's health storyline becomes the rows a lab seeds, every
// system healthy from the window's start, and each stretch an edge away and
// back with the alarm that explains it, all ending before the lab starts.

func storyline() *scenario.HealthHistory {
	return &scenario.HealthHistory{Days: 30, Stretches: []scenario.Stretch{
		{System: scenario.Ref{Name: "room-a"}, Component: scenario.Ref{Name: "display-1"}, Rule: scenario.Ref{Name: "reachability-lost"},
			Instance: "snmp", Severity: "high", Message: "Reachability lost", Health: "degraded", DaysAgo: 9, Minutes: 18},
		{System: scenario.Ref{Name: "room-a"}, Component: scenario.Ref{Name: "display-1"}, Rule: scenario.Ref{Name: "reachability-lost"},
			Instance: "snmp", Severity: "high", Message: "Reachability lost", Health: "outage", DaysAgo: 23, Minutes: 47},
	}}
}

func TestHealthHistoryRowsMakeEverySystemHealthyAndEachStretchAnEdgeAwayAndBack(t *testing.T) {
	t.Parallel()
	end := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	systems := map[string]string{"room-a": "sys-a", "room-b": "sys-b"}
	members := map[string]map[string]string{"room-a": {"display-1": "comp-a1"}}
	rows, err := devseed.HealthHistoryRows(storyline(), systems, members, map[string][]string{"comp-a1": {"snmp", "http"}}, end)
	if err != nil {
		t.Fatal(err)
	}
	start := end.Add(-30 * 24 * time.Hour)
	at := func(daysAgo, minutes int) time.Time {
		return end.Add(-time.Duration(daysAgo)*24*time.Hour + time.Duration(minutes)*time.Minute)
	}
	want := []storage.SeededHealthEdge{
		{OwnerKind: "system", OwnerID: "sys-a", TS: start, Value: "healthy"},
		{OwnerKind: "system", OwnerID: "sys-a", TS: at(23, 0), Value: "outage"},
		{OwnerKind: "system", OwnerID: "sys-a", TS: at(23, 47), Value: "healthy"},
		{OwnerKind: "system", OwnerID: "sys-a", TS: at(9, 0), Value: "degraded"},
		{OwnerKind: "system", OwnerID: "sys-a", TS: at(9, 18), Value: "healthy"},
		{OwnerKind: "system", OwnerID: "sys-b", TS: start, Value: "healthy"},
	}
	if len(rows.Edges) != len(want) {
		t.Fatalf("edges = %+v, want %+v", rows.Edges, want)
	}
	for i := range want {
		if rows.Edges[i] != want[i] {
			t.Errorf("edge %d = %+v, want %+v", i, rows.Edges[i], want[i])
		}
	}
	wantAlarms := []storage.SeededAlarm{
		{ComponentID: "comp-a1", Rule: "reachability-lost", Instance: "snmp", Severity: "high", Message: "Reachability lost", RaisedAt: at(23, 0), ClearedAt: at(23, 47)},
		{ComponentID: "comp-a1", Rule: "reachability-lost", Instance: "snmp", Severity: "high", Message: "Reachability lost", RaisedAt: at(9, 0), ClearedAt: at(9, 18)},
	}
	if len(rows.Alarms) != len(wantAlarms) {
		t.Fatalf("alarms = %+v, want %+v", rows.Alarms, wantAlarms)
	}
	for i := range wantAlarms {
		if rows.Alarms[i] != wantAlarms[i] {
			t.Errorf("alarm %d = %+v, want %+v", i, rows.Alarms[i], wantAlarms[i])
		}
	}
	for _, e := range rows.Edges {
		if !e.TS.Before(end) {
			t.Errorf("edge %+v is not before the lab started", e)
		}
	}
}

func TestHealthHistoryRowsRefuseARoomOrComponentTheLabLacks(t *testing.T) {
	t.Parallel()
	end := time.Now()
	for _, tc := range []struct {
		name      string
		systems   map[string]string
		members   map[string]map[string]string
		endpoints map[string][]string
		want      string
	}{
		{"no such system", map[string]string{"room-b": "sys-b"}, nil, nil, `"room-a"`},
		{"no such component", map[string]string{"room-a": "sys-a"}, map[string]map[string]string{"room-a": {"panel-1": "c"}}, nil, `"display-1"`},
		{"no such endpoint", map[string]string{"room-a": "sys-a"}, map[string]map[string]string{"room-a": {"display-1": "c"}}, map[string][]string{"c": {"https"}}, `"snmp"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := devseed.HealthHistoryRows(storyline(), tc.systems, tc.members, tc.endpoints, end)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error naming %s", err, tc.want)
			}
		})
	}
}

// The storyline lands on a real seeded fleet (#2728): the system's health reads
// healthy from the window's start, away for the stretch and back, then its own
// record, and the alarm names the component's real endpoint. A stretch whose
// instance is not one of the component's endpoints is refused, so a storyline
// cannot drift from the catalog.
func TestSeedHealthHistoryLandsOnASeededFleet(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx, gw, _ := openSeededGateway(t)
	p := corporatePlan(t, 20)
	start := time.Now()
	if _, err := devseed.SeedPlan(ctx, gw, "", p, devseed.PlanOptions{Endpoints: true}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	room := p.Sites[0].Systems[0].Name
	members, err := gw.ListMembers(ctx, room, all)
	if err != nil || len(members) == 0 {
		t.Fatalf("members of %s: %v (%d)", room, err, len(members))
	}
	var comp, endpoint string
	for _, m := range members {
		eps, err := gw.ListComponentEndpoints(ctx, m.ComponentUUID)
		if err != nil {
			t.Fatal(err)
		}
		if len(eps) > 0 {
			comp, endpoint = m.ComponentID, eps[0].Name
			break
		}
	}
	if comp == "" {
		t.Fatalf("no member of %s has an endpoint", room)
	}
	story := func(instance string) *scenario.HealthHistory {
		return &scenario.HealthHistory{Days: 30, Stretches: []scenario.Stretch{{
			System: scenario.Ref{Name: room}, Component: scenario.Ref{Name: comp}, Rule: scenario.Ref{Name: "reachability-lost"},
			Instance: instance, Severity: "high", Message: "Reachability lost", Health: "degraded", DaysAgo: 5, Minutes: 30,
		}}}
	}
	end := start.Add(-time.Minute)
	if err := devseed.SeedHealthHistory(ctx, gw, story("no-such-endpoint"), end); err == nil || !strings.Contains(err.Error(), "no-such-endpoint") {
		t.Fatalf("a stretch on an endpoint the component lacks: %v, want a refusal naming it", err)
	}
	if err := devseed.SeedHealthHistory(ctx, gw, story(endpoint), end); err != nil {
		t.Fatal(err)
	}

	rep, err := gw.SystemHealth(ctx, room, storage.HealthRange{Window: 31 * 24 * time.Hour}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	var seededEnd time.Time
	for i, tr := range rep.Transitions {
		got = append(got, tr.Value)
		if i < 3 {
			seededEnd = tr.TS
		}
	}
	if len(got) < 4 || got[0] != "healthy" || got[1] != "degraded" || got[2] != "healthy" {
		t.Fatalf("%s transitions = %v, want healthy, degraded, healthy, then its own record", room, got)
	}
	if !seededEnd.Before(rep.Transitions[3].TS) || !seededEnd.Before(start) {
		t.Errorf("the seeded past ends at %v, not before the system's own record at %v", seededEnd, rep.Transitions[3].TS)
	}
	sys, err := gw.GetSystem(ctx, room, all)
	if err != nil {
		t.Fatal(err)
	}
	alarms, err := gw.ListSystemAlarms(ctx, sys.ID, storage.AlarmRange{Since: end.Add(-30 * 24 * time.Hour), Until: time.Now()}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(alarms) != 1 || alarms[0].Instance() != endpoint || alarms[0].ComponentName != comp {
		t.Fatalf("seeded alarms = %+v, want one on %s's endpoint %s", alarms, comp, endpoint)
	}
}
