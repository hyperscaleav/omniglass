package devseed

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// HealthHistorySource labels every row a lab's health storyline lands, as
// platformHistorySource labels the platform history (#1790).
const HealthHistorySource = "devseed"

// HealthHistory is what a storyline lands: the systems' health edges and the
// alarms that explain their stretches.
type HealthHistory struct {
	Edges  []storage.SeededHealthEdge
	Alarms []storage.SeededAlarm
}

// HealthHistoryRows turns a scenario's health storyline (#2728) into the rows a
// lab seeds, given its systems (name to id), the members of the storyline's
// systems (system name, then component name, to id) and when the lab started
// (end). Every system reads healthy from the window's start; each stretch is an
// edge to its health and one back to healthy, and the alarm its rule raised on
// the component across it. Every row is before end, so the lab's own record
// follows the seeded past. A system or component the lab lacks is refused.
// Pure: the same storyline, fleet and end make the same rows.
func HealthHistoryRows(h *scenario.HealthHistory, systems map[string]string, members map[string]map[string]string, end time.Time) (HealthHistory, error) {
	var out HealthHistory
	if h == nil {
		return out, nil
	}
	start := end.Add(-time.Duration(h.Days) * 24 * time.Hour)
	byName := map[string][]scenario.Stretch{}
	for _, s := range h.Stretches {
		name := s.System.Name
		if _, ok := systems[name]; !ok {
			return out, fmt.Errorf("health history: the storyline names system %q, which this lab has not", name)
		}
		comp, ok := members[name][s.Component.Name]
		if !ok {
			return out, fmt.Errorf("health history: system %q has no component %q", name, s.Component.Name)
		}
		byName[name] = append(byName[name], s)
		from, to := s.Window(end)
		out.Alarms = append(out.Alarms, storage.SeededAlarm{ComponentID: comp, Rule: s.Rule.Name, Instance: s.Instance,
			Severity: s.Severity, Message: s.Message, RaisedAt: from, ClearedAt: to})
	}
	sort.SliceStable(out.Alarms, func(i, j int) bool { return out.Alarms[i].RaisedAt.Before(out.Alarms[j].RaisedAt) })

	names := make([]string, 0, len(systems))
	for name := range systems {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		id := systems[name]
		out.Edges = append(out.Edges, storage.SeededHealthEdge{OwnerKind: "system", OwnerID: id, TS: start, Value: "healthy"})
		stretches := byName[name]
		sort.SliceStable(stretches, func(i, j int) bool { return stretches[i].DaysAgo > stretches[j].DaysAgo })
		for _, s := range stretches {
			from, to := s.Window(end)
			out.Edges = append(out.Edges,
				storage.SeededHealthEdge{OwnerKind: "system", OwnerID: id, TS: from, Value: s.Health},
				storage.SeededHealthEdge{OwnerKind: "system", OwnerID: id, TS: to, Value: "healthy"})
		}
	}
	return out, nil
}

// SeedHealthHistory lands a scenario's health storyline on a lab that has just
// been seeded (lab seed --health-history, #2728), ending at end, the moment
// before the lab's own record began. It is the lab's only: the lab plan refuses
// the flag outside a throwaway lab (ADR-2726). Its rows are their own sentinel,
// so a second run lands nothing.
func SeedHealthHistory(ctx context.Context, gw storage.Gateway, h *scenario.HealthHistory, end time.Time) error {
	if h == nil {
		return nil
	}
	all := scope.Set{All: true}
	list, err := gw.ListSystems(ctx, all)
	if err != nil {
		return fmt.Errorf("health history: list systems: %w", err)
	}
	systems := make(map[string]string, len(list))
	for _, s := range list {
		systems[s.Name] = s.ID
	}
	members := map[string]map[string]string{}
	for _, s := range h.Stretches {
		name := s.System.Name
		if _, done := members[name]; done {
			continue
		}
		if _, ok := systems[name]; !ok {
			continue // HealthHistoryRows names it
		}
		ms, err := gw.ListMembers(ctx, name, all)
		if err != nil {
			return fmt.Errorf("health history: members of %s: %w", name, err)
		}
		members[name] = map[string]string{}
		for _, m := range ms {
			members[name][m.ComponentID] = m.ComponentUUID
		}
	}
	rows, err := HealthHistoryRows(h, systems, members, end)
	if err != nil {
		return err
	}
	if _, err := gw.LandSeededHealth(ctx, HealthHistorySource, rows.Edges); err != nil {
		return fmt.Errorf("health history: %w", err)
	}
	if _, err := gw.LandSeededAlarms(ctx, HealthHistorySource, rows.Alarms); err != nil {
		return fmt.Errorf("health history: %w", err)
	}
	return nil
}
