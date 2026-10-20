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
// version; and rules read like a registry, written only with an
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
		FireEventType: "condition-started", Name: "reach-lost", Label: "Reachability lost", SampleKey: "endpoint-reachable",
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
	cpu := storage.EventRuleSpec{FireEventType: "condition-started", Name: "room-hot", SampleKey: "room-temperature", Fire: criteria("gt", 90), Severity: "warning"}
	hot, err := gw.CreateEventRule(ctx, "", cpu, all)
	if err != nil || hot.SampleLane != "metric" {
		t.Fatalf("metric rule %+v (%v), want the metric lane", hot, err)
	}

	for name, bad := range map[string]storage.EventRuleSpec{
		"unknown key":      {FireEventType: "condition-started", Name: "r1", SampleKey: "no-such-key", Fire: criteria("eq", "x"), Severity: "high"},
		"ordered on text":  {FireEventType: "condition-started", Name: "r2", SampleKey: "endpoint-reachable", Fire: criteria("gt", "down"), Severity: "high"},
		"text on a metric": {FireEventType: "condition-started", Name: "r3", SampleKey: "room-temperature", Fire: criteria("eq", "hot"), Severity: "high"},
		"unknown level":    {FireEventType: "condition-started", Name: "r4", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "critical"},
		"unknown type":     {FireEventType: "condition-started", Name: "r5", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high", Selector: storage.EventRuleSelector{ComponentTypes: []string{"no-such-type"}}},
		"unknown target":   {FireEventType: "condition-started", Name: "r6", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high", Selector: storage.EventRuleSelector{Components: []string{"nobody"}}},
		"bad clear":        {FireEventType: "condition-started", Name: "r7", SampleKey: "room-temperature", Fire: criteria("gt", 1), Clear: ptrCriteria(criteria("eq", "x")), Severity: "high"},
		"negative sustain": {FireEventType: "condition-started", Name: "r8", SampleKey: "room-temperature", Fire: criteria("gt", 1), FireSustain: -time.Second, Severity: "high"},
		"bad name":         {FireEventType: "condition-started", Name: "Bad Name", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high"},
	} {
		if _, err := gw.CreateEventRule(ctx, "", bad, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid", name, err)
		}
	}
	if _, err := gw.CreateEventRule(ctx, "", reach, all); !errors.Is(err, storage.ErrEventRuleExists) {
		t.Fatalf("a duplicate name: err %v, want ErrEventRuleExists", err)
	}

	// Rules read like a registry: a reader whose component scope admits
	// nothing still lists and gets every rule. Writing one takes an all-scope
	// grant, since its selector reaches the whole fleet.
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "scoped", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "high"}, none); !errors.Is(err, storage.ErrEventRuleForbidden) {
		t.Fatalf("a scoped create: err %v, want ErrEventRuleForbidden", err)
	}
	if list, err := gw.ListEventRules(ctx, none); err != nil || len(list) != 3 {
		t.Fatalf("a scoped reader lists %d rules (%v), want all 3", len(list), err)
	}
	if r, err := gw.GetEventRule(ctx, "reach-lost", none); err != nil || r.Name != "reach-lost" {
		t.Fatalf("a scoped reader gets %+v (%v), want reach-lost", r, err)
	}
	if _, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Label: ptrString("x")}, none); !errors.Is(err, storage.ErrEventRuleForbidden) {
		t.Fatalf("a scoped update: err %v, want ErrEventRuleForbidden", err)
	}
	if err := gw.DeleteEventRule(ctx, "", "reach-lost", none); !errors.Is(err, storage.ErrEventRuleForbidden) {
		t.Fatalf("a scoped delete: err %v, want ErrEventRuleForbidden", err)
	}
	if list, _ := gw.ListEventRules(ctx, all); len(list) != 3 {
		t.Fatalf("an all-scope reader lists %d rules, want 3 (the two authored and the official reachability rule)", len(list))
	}

	// An update bumps the version; disabling is an update.
	off := false
	clear := criteria("eq", "up")
	upd, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Enabled: &off, Clear: &clear}, all)
	if err != nil || upd.Version != 2 || upd.Enabled || upd.ClearCriteria == nil {
		t.Fatalf("updated %+v (%v), want version 2, disabled, a clear criterion", upd, err)
	}
	if err := gw.DeleteEventRule(ctx, "", "room-hot", all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.GetEventRule(ctx, "room-hot", all); !errors.Is(err, storage.ErrEventRuleNotFound) {
		t.Fatalf("a deleted rule: err %v, want not found", err)
	}
}

func ptrCriteria(c rules.Criteria) *rules.Criteria { return &c }

func ptrString(s string) *string { return &s }

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
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "on-doomed", SampleKey: "doomed-metric",
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
	upd, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Enabled: &off}, all)
	if err != nil || upd.Enabled {
		t.Fatalf("disabling a rule whose component and key were deleted: %+v (%v), want it disabled", upd, err)
	}

	// A patch that changes nothing changes nothing, version included.
	same, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Enabled: &off}, all)
	if err != nil || same.Version != upd.Version {
		t.Fatalf("a no-op patch moved the version from %d to %d (%v)", upd.Version, same.Version, err)
	}

	// A patch that restates the stored selector and key (a form that sends
	// every field) is judged on what it changes: the stale references it
	// carries over are not re-resolved, so a label edit saves. A selector
	// entry or key the rule did not already hold must still exist.
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "doomed-type", Label: "Doomed", Stem: strp("doomed")}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Selector: &storage.EventRuleSelector{
		ComponentTypes: []string{"doomed-type"}, Components: upd.Selector.Components}}, all); err != nil {
		t.Fatalf("adding a component type that exists beside a stale component: %v", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "doomed-type"); err != nil {
		t.Fatal(err)
	}
	stored, err := gw.GetEventRule(ctx, "on-doomed", all)
	if err != nil {
		t.Fatal(err)
	}
	label := "Doomed, relabelled"
	key := stored.SampleKey
	fire := stored.FireCriteria
	sel := stored.Selector
	relabelled, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Label: &label, Selector: &sel, SampleKey: &key, Fire: &fire}, all)
	if err != nil || relabelled.Label != label || len(relabelled.Selector.Components) != 1 || len(relabelled.Selector.ComponentTypes) != 1 {
		t.Fatalf("a label edit restating a stale selector and key: %+v (%v), want it saved with the selector kept", relabelled, err)
	}
	for name, bad := range map[string]storage.EventRuleSelector{
		"a new component that does not exist":      {ComponentTypes: sel.ComponentTypes, Components: append([]string{"nobody"}, sel.Components...)},
		"a new component type that does not exist": {ComponentTypes: append([]string{"no-such-type"}, sel.ComponentTypes...), Components: sel.Components},
	} {
		if _, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Selector: &bad}, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid", name, err)
		}
	}
	nokey := "no-such-key"
	if _, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{SampleKey: &nokey}, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
		t.Errorf("a new sample key that does not exist: err %v, want ErrEventRuleInvalid", err)
	}

	clear := criteria("lt", 1)
	if _, err := gw.UpdateEventRule(ctx, "", "on-doomed", storage.EventRulePatch{Clear: &clear, ClearNegates: true}, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
		t.Fatalf("a clear criterion and clear_negates together: err %v, want ErrEventRuleInvalid", err)
	}
	for _, d := range []time.Duration{time.Nanosecond, 1500 * time.Nanosecond, 400 * 24 * time.Hour} {
		if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "sustain-x", SampleKey: "room-temperature",
			Fire: criteria("gt", 1), FireSustain: d, Severity: "info"}, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("a sustain of %v: err %v, want ErrEventRuleInvalid", d, err)
		}
	}
}

// The boot seed's official rules are read-only, a re-seed changes nothing,
// and an official rule never takes over an operator's rule of its name.
func TestOfficialEventRules(t *testing.T) {
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
	got, err := gw.GetEventRule(ctx, "reachability-lost", all)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Official || !got.Enabled || got.SampleKey != "endpoint-reachable" || got.Severity != "high" || got.FireSustain <= 0 {
		t.Fatalf("the official reachability rule = %+v, want official, enabled, on endpoint-reachable at high, with a sustain", got)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	if again, _ := gw.GetEventRule(ctx, "reachability-lost", all); again.Version != got.Version {
		t.Fatalf("a re-seed moved the official rule's version from %d to %d", got.Version, again.Version)
	}
	off := false
	if _, err := gw.UpdateEventRule(ctx, "", "reachability-lost", storage.EventRulePatch{Enabled: &off}, all); !errors.Is(err, storage.ErrEventRuleOfficial) {
		t.Fatalf("editing the official rule: err %v, want ErrEventRuleOfficial", err)
	}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "mine", SampleKey: "room-temperature", Fire: criteria("gt", 1), Severity: "info"}, all); err != nil {
		t.Fatal(err)
	}
	err = gw.UpsertEventRule(ctx, storage.EventRuleSpec{FireEventType: "condition-started", Name: "mine", SampleKey: "room-temperature", Fire: criteria("gt", 2), Severity: "info"})
	if !errors.Is(err, storage.ErrEventRuleExists) {
		t.Fatalf("an official rule named like an operator's: err %v, want ErrEventRuleExists", err)
	}
}
