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

	reach, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "reach-lost", Label: "Reachability lost",
		SampleKey: "endpoint-reachable", Fire: criteria("eq", "down"), Severity: "high"}, all)
	if err != nil {
		t.Fatal(err)
	}
	prop := func(component, instance, value string) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: component,
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
	if len(got) != 1 || got[0].Severity != "high" || got[0].DedupKey != "rule:reach-lost:tcp" || got[0].RaisedByRuleID == nil ||
		*got[0].RaisedByRuleID != reach.ID || got[0].Message != "Reachability lost" {
		t.Fatalf("after down on tcp: %+v, want one high alarm keyed rule:reach-lost:tcp, raised by the rule, messaged with its label", got)
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
	if len(got) != 1 || got[0].DedupKey != "rule:reach-lost:icmp" {
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
	if _, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Enabled: &off}, all, all); err != nil {
		t.Fatal(err)
	}
	prop("bar-1", "tcp", "down")
	if n := len(open("bar-1")); n != 0 {
		t.Fatalf("a disabled rule opened %d alarms", n)
	}
	if _, err := gw.UpdateEventRule(ctx, "", "reach-lost", storage.EventRulePatch{Enabled: &on}, all, all); err != nil {
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
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "too-hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 30), Clear: &clear, Severity: "warning", Message: "Running hot",
		Selector: storage.EventRuleSelector{Components: []string{"bar-1"}}}, all); err != nil {
		t.Fatal(err)
	}
	metric := func(component string, v float64) {
		t.Helper()
		if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: component,
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
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "display-hot", SampleKey: "room-temperature",
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
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "reach-lost", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	w := func(v string, at time.Time) storage.PropertySampleWrite {
		return storage.PropertySampleWrite{OwnerKind: "component", OwnerID: "bar-1", Key: "endpoint-reachable", Instance: "tcp", Value: v, TS: at}
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{w("up", now)}); err != nil {
		t.Fatal(err)
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{w("down", now.Add(-time.Minute))}); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 0 {
		t.Fatalf("a late down opened %d alarms", len(got))
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{w("up", now.Add(time.Minute)), w("down", now.Add(30*time.Second))}); err != nil {
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
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 30), Severity: "warning"}, all); err != nil {
		t.Fatal(err)
	}
	hand, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "info", Message: "hot"})
	if err != nil {
		t.Fatal(err)
	}
	metric := func(component string, v float64) {
		t.Helper()
		if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: component, Key: "room-temperature", Value: v, TS: time.Now()}}); err != nil {
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
	if _, err := gw.UpdateEventRule(ctx, "", "hot", storage.EventRulePatch{Selector: &narrow}, all, all); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "other-c", storage.AlarmFilter{}); len(got) != 0 {
		t.Fatalf("narrowing the selector left %d alarms on other-c", len(got))
	}
	off := false
	if _, err := gw.UpdateEventRule(ctx, "", "hot", storage.EventRulePatch{Enabled: &off}, all, all); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 1 || got[0].ID != hand.ID {
		t.Fatalf("disabling the rule left %+v on bar-1, want only the hand-raised alarm", got)
	}

	// Deleting a rule clears what it holds open.
	on := true
	if _, err := gw.UpdateEventRule(ctx, "", "hot", storage.EventRulePatch{Enabled: &on}, all, all); err != nil {
		t.Fatal(err)
	}
	metric("bar-1", 36)
	if err := gw.DeleteEventRule(ctx, "", "hot", all, all); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 1 || got[0].ID != hand.ID {
		t.Fatalf("deleting the rule left %+v on bar-1, want only the hand-raised alarm", got)
	}
}
