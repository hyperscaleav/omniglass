package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func criteria(op string, v any) rules.Criteria {
	b, _ := json.Marshal(v)
	return rules.Criteria{Op: op, Value: b}
}

// TestEventRules proves an event rule is authored config the gateway keeps
// honest: it reads a registered sample key on its own lane, compares with
// criteria that lane can evaluate, names a severity level that exists, and
// selects component types and components that exist; an update bumps its
// version; and rules are fleet-wide config, read and written only with an
// all-scope grant.
func TestEventRules(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	none := scope.Set{}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "rule-target"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}

	reach := storage.EventRuleSpec{
		Name: "reach-lost", Label: "Reachability lost", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), FireSustain: 30 * time.Second, Severity: "high",
		Selector: storage.EventRuleSelector{Components: []string{"rule-target"}},
	}
	got, err := gw.CreateEventRule(ctx, "", reach, all)
	if err != nil {
		t.Fatal(err)
	}
	if got.SampleLane != "property" || got.Version != 1 || !got.Enabled || got.Official || got.ClearCriteria != nil ||
		got.FireSustain != 30*time.Second || len(got.Selector.Components) != 1 || got.Selector.Components[0] == "rule-target" {
		t.Fatalf("created %+v, want a property-lane rule, enabled, version 1, its component resolved to an id", got)
	}
	cpu := storage.EventRuleSpec{Name: "room-hot", SampleKey: "room-temperature", Fire: criteria("gt", 90), Severity: "warning"}
	hot, err := gw.CreateEventRule(ctx, "", cpu, all)
	if err != nil || hot.SampleLane != "metric" {
		t.Fatalf("metric rule %+v (%v), want the metric lane", hot, err)
	}

	for name, bad := range map[string]storage.EventRuleSpec{
		"unknown key":      {Name: "r1", SampleKey: "no-such-key", Fire: criteria("eq", "x"), Severity: "high"},
		"ordered on text":  {Name: "r2", SampleKey: "endpoint-reachable", Fire: criteria("gt", "down"), Severity: "high"},
		"text on a metric": {Name: "r3", SampleKey: "room-temperature", Fire: criteria("eq", "hot"), Severity: "high"},
		"unknown level":    {Name: "r4", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "critical"},
		"unknown type":     {Name: "r5", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high", Selector: storage.EventRuleSelector{ComponentTypes: []string{"no-such-type"}}},
		"unknown target":   {Name: "r6", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high", Selector: storage.EventRuleSelector{Components: []string{"nobody"}}},
		"bad clear":        {Name: "r7", SampleKey: "room-temperature", Fire: criteria("gt", 1), Clear: ptrCriteria(criteria("eq", "x")), Severity: "high"},
		"negative sustain": {Name: "r8", SampleKey: "room-temperature", Fire: criteria("gt", 1), FireSustain: -time.Second, Severity: "high"},
		"bad name":         {Name: "Bad Name", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high"},
	} {
		if _, err := gw.CreateEventRule(ctx, "", bad, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid", name, err)
		}
	}
	if _, err := gw.CreateEventRule(ctx, "", reach, all); !errors.Is(err, storage.ErrEventRuleExists) {
		t.Fatalf("a duplicate name: err %v, want ErrEventRuleExists", err)
	}

	// Rules are fleet-wide: only an all-scope grant reads or writes them.
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "scoped", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high"}, none); !errors.Is(err, storage.ErrEventRuleForbidden) {
		t.Fatalf("a scoped create: err %v, want ErrEventRuleForbidden", err)
	}
	if list, err := gw.ListEventRules(ctx, none); err != nil || len(list) != 0 {
		t.Fatalf("a scoped reader lists %d rules (%v), want none", len(list), err)
	}
	if list, _ := gw.ListEventRules(ctx, all); len(list) != 2 {
		t.Fatalf("an all-scope reader lists %d rules, want 2", len(list))
	}

	// An update bumps the version; disabling is an update.
	off := false
	clear := criteria("eq", "up")
	upd, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Enabled: &off, Clear: &clear}, all, all)
	if err != nil || upd.Version != 2 || upd.Enabled || upd.ClearCriteria == nil {
		t.Fatalf("updated %+v (%v), want version 2, disabled, a clear criterion", upd, err)
	}
	if err := gw.DeleteEventRule(ctx, "", "room-hot", all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.GetEventRule(ctx, "room-hot", all); !errors.Is(err, storage.ErrEventRuleNotFound) {
		t.Fatalf("a deleted rule: err %v, want not found", err)
	}
}

func ptrCriteria(c rules.Criteria) *rules.Criteria { return &c }

// A rule outlives what it points at: deleting a component it selects or the
// sample key it reads leaves the rule editable (an operator can still
// disable it), it just matches nothing; a patch that changes nothing bumps
// nothing; and the sustains and clear fields refuse what they cannot mean.
func TestEventRuleEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "doomed"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "doomed-metric", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "on-doomed", SampleKey: "doomed-metric",
		Fire: criteria("gt", 1), Severity: "info", Selector: storage.EventRuleSelector{Components: []string{"doomed"}}}, all); err != nil {
		t.Fatal(err)
	}
	if err := gw.DeleteComponent(ctx, "", "doomed", all, all); err != nil {
		t.Fatal(err)
	}
	if err := gw.DeleteMetricType(ctx, "", "doomed-metric"); err != nil {
		t.Fatal(err)
	}
	off := false
	upd, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Enabled: &off}, all, all)
	if err != nil || upd.Enabled {
		t.Fatalf("disabling a rule whose component and key were deleted: %+v (%v), want it disabled", upd, err)
	}

	// A patch that changes nothing changes nothing, version included.
	same, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Enabled: &off}, all, all)
	if err != nil || same.Version != upd.Version {
		t.Fatalf("a no-op patch moved the version from %d to %d (%v)", upd.Version, same.Version, err)
	}

	clear := criteria("lt", 1)
	if _, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Clear: &clear, ClearNegates: true}, all, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
		t.Fatalf("a clear criterion and clear_negates together: err %v, want ErrEventRuleInvalid", err)
	}
	for _, d := range []time.Duration{time.Nanosecond, 1500 * time.Nanosecond, 400 * 24 * time.Hour} {
		if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "sustain-x", SampleKey: "room-temperature",
			Fire: criteria("gt", 1), FireSustain: d, Severity: "info"}, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("a sustain of %v: err %v, want ErrEventRuleInvalid", d, err)
		}
	}
}
