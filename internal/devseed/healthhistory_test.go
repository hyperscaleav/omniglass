package devseed_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
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
	rows, err := devseed.HealthHistoryRows(storyline(), systems, members, end)
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
		name    string
		systems map[string]string
		members map[string]map[string]string
		want    string
	}{
		{"no such system", map[string]string{"room-b": "sys-b"}, nil, `"room-a"`},
		{"no such component", map[string]string{"room-a": "sys-a"}, map[string]map[string]string{"room-a": {"panel-1": "c"}}, `"display-1"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := devseed.HealthHistoryRows(storyline(), tc.systems, tc.members, end)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error naming %s", err, tc.want)
			}
		})
	}
}
