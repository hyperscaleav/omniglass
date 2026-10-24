package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestRuleEvaluator proves rules raise and clear alarms as samples land, in
// the same transaction as the sample: fire opens one alarm per component and
// instance (a repeat and a redelivery change nothing), the clear criteria
// (or the negation of fire) clears it, the alarm names its rule and moves
// the component's verdict exactly as a hand-raised one does, a disabled rule
// evaluates nothing, and a selector confines a rule to its components and
// component types.
func TestRuleEvaluator(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}

	reach, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "reach-lost", Label: "Reachability lost",
		SampleKey: "endpoint-reachable", Fire: criteria("eq", "down"), Severity: "high"}, all)
	if err != nil {
		t.Fatal(err)
	}
	prop := func(component, instance, value string) {
		t.Helper()
		if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: component,
			Key: "endpoint-reachable", Instance: instance, Value: value, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	open := func(component string) []storage.Alarm {
		t.Helper()
		alarms, err := gw.ListAlarms(ctx, component, storage.AlarmFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return alarms
	}

	prop("bar-1", "tcp", "down")
	got := open("bar-1")
	if len(got) != 1 || got[0].Severity != "high" || got[0].DedupKey != "rule:"+reach.ID+":tcp" || got[0].RaisedByRuleID == nil ||
		*got[0].RaisedByRuleID != reach.ID || got[0].Message != "Reachability lost" {
		t.Fatalf("after down on tcp: %+v, want one high alarm keyed rule:<uuid>:tcp, raised by the rule, messaged with its label", got)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "outage" {
		t.Fatalf("a rule-raised high alarm left bar-1 %q, want outage", v)
	}
	prop("bar-1", "tcp", "down") // a repeat or a redelivery
	if n := len(open("bar-1")); n != 1 {
		t.Fatalf("a repeated down opened %d alarms, want still 1", n)
	}
	prop("bar-1", "icmp", "down") // another endpoint holds its own alarm
	if n := len(open("bar-1")); n != 2 {
		t.Fatalf("down on a second endpoint left %d alarms open, want 2", n)
	}
	prop("bar-1", "tcp", "up") // the negation of fire clears tcp, not icmp
	got = open("bar-1")
	if len(got) != 1 || got[0].DedupKey != "rule:"+reach.ID+":icmp" {
		t.Fatalf("after up on tcp: %+v, want only icmp's alarm open", got)
	}
	prop("bar-1", "icmp", "up")
	if n := len(open("bar-1")); n != 0 {
		t.Fatalf("both endpoints up left %d alarms", n)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "healthy" {
		t.Fatalf("recovered bar-1 reads %q, want healthy", v)
	}

	// A disabled rule evaluates nothing; re-enabled, it resumes.
	off, on := false, true
	if _, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Enabled: &off}, all); err != nil {
		t.Fatal(err)
	}
	prop("bar-1", "tcp", "down")
	if n := len(open("bar-1")); n != 0 {
		t.Fatalf("a disabled rule opened %d alarms", n)
	}
	if _, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Enabled: &on}, all); err != nil {
		t.Fatal(err)
	}
	prop("bar-1", "tcp", "down")
	if n := len(open("bar-1")); n != 1 {
		t.Fatalf("a re-enabled rule opened %d alarms, want 1", n)
	}
	prop("bar-1", "tcp", "up")

	// A selector confines a rule: a metric rule on one named component, with
	// an explicit clear criterion.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-c"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	clear := criteria("lt", 28)
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "too-hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 30), Clear: &clear, Severity: "warning", Message: "Running hot",
		Selector: storage.EventRuleSelector{Components: []string{"bar-1"}}}, all); err != nil {
		t.Fatal(err)
	}
	metric := func(component string, v float64) {
		t.Helper()
		if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: component,
			Key: "room-temperature", Value: v, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	metric("other-c", 35)
	if n := len(open("other-c")); n != 0 {
		t.Fatalf("a rule selecting bar-1 raised %d alarms on other-c", n)
	}
	metric("bar-1", 35)
	if got := open("bar-1"); len(got) != 1 || got[0].Severity != "warning" || got[0].Message != "Running hot" {
		t.Fatalf("too-hot on bar-1: %+v", got)
	}
	metric("bar-1", 29) // between clear and fire: holds
	if n := len(open("bar-1")); n != 1 {
		t.Fatalf("29 (not below 28) cleared the alarm; want it held")
	}
	metric("bar-1", 27)
	if n := len(open("bar-1")); n != 0 {
		t.Fatalf("27 left %d alarms, want cleared", n)
	}

	// A component-type selector watches components of that type (a subtype
	// counts); others are left alone.
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "wall-1", ProductName: &disp.Name}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "display-hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 40), Severity: "info", Selector: storage.EventRuleSelector{ComponentTypes: []string{"display"}}}, all); err != nil {
		t.Fatal(err)
	}
	metric("other-c", 45)
	if n := len(open("other-c")); n != 0 {
		t.Fatalf("a display rule raised %d alarms on a generic component", n)
	}
	metric("wall-1", 45)
	if n := len(open("wall-1")); n != 1 {
		t.Fatalf("a display rule raised %d alarms on a display, want 1", n)
	}
}

// A sample older than the series' newest is history, not the present: a
// late or redelivered down after an up opens nothing, in another batch or
// the same one.
func TestRuleEvaluatorJudgesOnlyTheNewestSample(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "reach-lost", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	w := func(v string, at time.Time) storage.PropertySampleWrite {
		return storage.PropertySampleWrite{OwnerKind: "component", OwnerID: "bar-1", Key: "endpoint-reachable", Instance: "tcp", Value: v, TS: at}
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{w("up", now)}); err != nil {
		t.Fatal(err)
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{w("down", now.Add(-time.Minute))}); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 0 {
		t.Fatalf("a late down opened %d alarms", len(got))
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{w("up", now.Add(time.Minute)), w("down", now.Add(30*time.Second))}); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 0 {
		t.Fatalf("an out-of-order down in one batch opened %d alarms", len(got))
	}
}

// A rule's alarms are its own: they never collide with a hand-raised alarm
// whose message matches the rule's name, and a rule disabled, deleted or
// narrowed clears the open alarms it no longer watches.
func TestRuleAlarmsBelongToTheirRule(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-c"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 30), Severity: "warning"}, all); err != nil {
		t.Fatal(err)
	}
	hand, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "info", Message: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	metric := func(component string, v float64) {
		t.Helper()
		if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: component, Key: "room-temperature", Value: v, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	metric("bar-1", 35)
	metric("other-c", 35)
	got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if len(got) != 2 {
		t.Fatalf("bar-1 holds %d alarms, want the hand one and the rule's", len(got))
	}
	metric("bar-1", 20) // the rule clears its own, not the operator's
	got, _ = gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if len(got) != 1 || got[0].ID != hand.ID {
		t.Fatalf("after the rule's clear bar-1 holds %+v, want only the hand-raised alarm", got)
	}

	// Narrowing the selector clears the alarm on a component it no longer
	// watches; disabling clears the rest.
	metric("bar-1", 35)
	narrow := storage.EventRuleSelector{Components: []string{"bar-1"}}
	if _, err := gw.UpdateEventRule(ctx, "", "hot", storage.EventRulePatch{Selector: &narrow}, all); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "other-c", storage.AlarmFilter{}); len(got) != 0 {
		t.Fatalf("narrowing the selector left %d alarms on other-c", len(got))
	}
	off := false
	if _, err := gw.UpdateEventRule(ctx, "", "hot", storage.EventRulePatch{Enabled: &off}, all); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 1 || got[0].ID != hand.ID {
		t.Fatalf("disabling the rule left %+v on bar-1, want only the hand-raised alarm", got)
	}

	// Deleting a rule clears what it holds open.
	on := true
	if _, err := gw.UpdateEventRule(ctx, "", "hot", storage.EventRulePatch{Enabled: &on}, all); err != nil {
		t.Fatal(err)
	}
	metric("bar-1", 36)
	if err := gw.DeleteEventRule(ctx, "", "hot", all); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 1 || got[0].ID != hand.ID {
		t.Fatalf("deleting the rule left %+v on bar-1, want only the hand-raised alarm", got)
	}
}

// A rule's live state is what it holds open and what it is waiting on: the
// alarms it raised that are still open, and its pending streaks.
func TestEventRuleState(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "reach-lost", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "slow-hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 30), FireSustain: time.Minute, Severity: "info"}, all); err != nil {
		t.Fatal(err)
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "room-temperature", Value: 35, TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	st, err := gw.EventRuleState(ctx, "reach-lost", all)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Open) != 1 || st.Open[0].ComponentName != "bar-1" || st.Open[0].Instance != "tcp" || len(st.Pending) != 0 {
		t.Fatalf("reach-lost state = %+v, want one open alarm on bar-1/tcp and nothing pending", st)
	}
	st, err = gw.EventRuleState(ctx, "slow-hot", all)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Open) != 0 || len(st.Pending) != 1 || st.Pending[0].Pending != "fire" || st.Pending[0].ComponentName != "bar-1" {
		t.Fatalf("slow-hot state = %+v, want a pending fire on bar-1 and nothing open", st)
	}
	// The rule reads like a registry, but its state names components: a
	// reader sees only the alarms and streaks on components its component
	// read scope admits, and counts only those.
	other, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-c"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "other-c",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	blind, err := gw.EventRuleState(ctx, "reach-lost", scope.Set{})
	if err != nil || len(blind.Open) != 0 || len(blind.Pending) != 0 {
		t.Fatalf("a reader with no component scope: state %+v (%v), want the rule with nothing visible", blind, err)
	}
	onlyOther := scope.Set{IDs: []string{other.ID}}
	st, err = gw.EventRuleState(ctx, "reach-lost", onlyOther)
	if err != nil || len(st.Open) != 1 || st.Open[0].ComponentName != "other-c" {
		t.Fatalf("a reader scoped to other-c: state %+v (%v), want only other-c's alarm", st, err)
	}
	if r, err := gw.GetEventRule(ctx, "reach-lost", onlyOther); err != nil || r.OpenAlarms != 1 {
		t.Fatalf("a reader scoped to other-c gets %+v (%v), want 1 open alarm counted", r, err)
	}
	if r, err := gw.GetEventRule(ctx, "slow-hot", scope.Set{}); err != nil || r.OpenAlarms != 0 || r.PendingStreaks != 0 {
		t.Fatalf("a reader with no component scope gets %+v (%v), want the rule with zero counts", r, err)
	}
	scoped, err := gw.ListEventRules(ctx, onlyOther)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range scoped {
		want, ours := map[string][2]int{"reach-lost": {1, 0}, "slow-hot": {0, 0}}[r.Name]
		if ours && [2]int{r.OpenAlarms, r.PendingStreaks} != want {
			t.Fatalf("a reader scoped to other-c lists %s with %d open, %d pending; want %v", r.Name, r.OpenAlarms, r.PendingStreaks, want)
		}
	}

	// The list and a get carry the same state as counts, so a page of rules
	// reads every rule's live state in one query instead of one per row.
	list, err := gw.ListEventRules(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string][2]int{}
	for _, r := range list {
		counts[r.Name] = [2]int{r.OpenAlarms, r.PendingStreaks}
	}
	if counts["reach-lost"] != [2]int{2, 0} || counts["slow-hot"] != [2]int{0, 1} {
		t.Fatalf("listed counts %v, want reach-lost 2 open, slow-hot 1 pending", counts)
	}
	got, err := gw.GetEventRule(ctx, "slow-hot", all)
	if err != nil || got.OpenAlarms != 0 || got.PendingStreaks != 1 {
		t.Fatalf("get slow-hot = %+v (%v), want 1 pending", got, err)
	}
	label := "Reach lost"
	upd, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Label: &label}, all)
	if err != nil || upd.OpenAlarms != 2 {
		t.Fatalf("updated reach-lost = %+v (%v), want its two open alarms counted", upd, err)
	}
}
