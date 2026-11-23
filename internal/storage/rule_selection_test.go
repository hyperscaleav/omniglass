package storage_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestReselectComponentRules proves the re-selection step (#2475): a
// component whose type moves enters the rules that now select it, priming
// them from the newest sample of a transition-only input that already holds
// (so the condition opens after its sustain with no further sample), leaves
// the rules that no longer select it (ending what they held there), leaves
// a rule that selects it on both sides alone, and never touches a rule whose
// selector names neither type.
func TestReselectComponentRules(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	gw := f.gw

	// bar-1 is a video-bar (under collaboration-device); it moves to a
	// wireless-presenter (under the same parent) and back.
	rule := func(name string, types ...string) {
		t.Helper()
		if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "rule-fired", Name: name,
			SampleKey: "endpoint-reachable", Selector: storage.EventRuleSelector{ComponentTypes: types},
			Fire: criteria("eq", "down"), FireSustain: 10 * time.Second, Clear: ptrCriteria(criteria("eq", "up")),
			Severity: "high"}, f.all); err != nil {
			t.Fatal(err)
		}
	}
	rule("presenter-reach", "wireless-presenter")
	rule("collab-reach", "collaboration-device")
	rule("camera-reach", "camera")

	var barID string
	if err := f.conn.QueryRow(ctx, `select id::text from component where name = 'bar-1'`).Scan(&barID); err != nil {
		t.Fatal(err)
	}
	count := func(q, name string) int {
		t.Helper()
		var n int
		if err := f.conn.QueryRow(ctx, q, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	firings := func(name string) int {
		return count(`select count(*) from event_rule_firing x join event_rule r on r.id = x.rule_id where r.name = $1`, name)
	}
	streaks := func(name string) int {
		return count(`select count(*) from event_rule_state x join event_rule r on r.id = x.rule_id where r.name = $1`, name)
	}
	events := func(name string) int {
		return count(`select count(*) from event e join event_rule r on r.id = e.derived_by_rule_id where r.name = $1`, name)
	}
	sweep := func(after time.Duration) {
		t.Helper()
		if err := gw.SweepEventRules(ctx, time.Now().Add(after)); err != nil {
			t.Fatal(err)
		}
	}
	selection := func() []string {
		t.Helper()
		sel, err := gw.ExportComponentRuleSelection(ctx, barID)
		if err != nil {
			t.Fatal(err)
		}
		return sel
	}
	moveTo := func(product string) (before, after []string) {
		t.Helper()
		before = selection()
		if _, err := gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &product}, f.all, f.all); err != nil {
			t.Fatal(err)
		}
		return before, selection()
	}

	// The input already holds down, the way a transition-only input sits:
	// one sample, nothing after it.
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	sweep(11 * time.Second)
	if n := openRuleAlarms(t, f, "collab-reach"); n != 1 {
		t.Fatalf("collab-reach selects the video-bar and its input held down past the sustain: %d alarms open, want 1", n)
	}
	if n := firings("presenter-reach"); n != 0 {
		t.Fatalf("presenter-reach does not select a video-bar yet holds %d firings", n)
	}
	// A firing camera-reach holds on bar-1, planted (it never selects it): the
	// step must not act on the rule, so the row survives both moves.
	if _, err := f.conn.Exec(ctx, `insert into event_rule_firing (rule_id, component_id, instance, since)
		select id, $1::uuid, 'tcp', now() from event_rule where name = 'camera-reach'`, barID); err != nil {
		t.Fatal(err)
	}
	collabEvents := events("collab-reach")

	// Enter: the move alone changes nothing (no caller is wired yet, #941);
	// the step primes presenter-reach from the held sample.
	presenter := storagetest.MintProduct(t, ctx, gw, "wireless-presenter")
	before, after := moveTo(presenter.Name)
	if len(before) == 0 || len(after) == 0 || slices.Equal(before, after) {
		t.Fatalf("the move did not change the selection: before %v, after %v", before, after)
	}
	// camera-reach's row is held FOR UPDATE meanwhile, as a rule write holds
	// it: a step that read every rule would wait on it, one bounded to the
	// rules naming the old or new type never reads it.
	held, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := held.Exec(ctx, `select 1 from event_rule where name = 'camera-reach' for update`); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = gw.ExportReselectComponentRules(bounded, barID, before, after)
	cancel()
	_ = held.Rollback(ctx)
	if err != nil {
		t.Fatalf("the step waited on, or failed over, a rule naming neither type: %v", err)
	}
	if n := streaks("presenter-reach"); n != 1 {
		t.Fatalf("entering presenter-reach on a held input started %d streaks, want 1", n)
	}
	if n := openRuleAlarms(t, f, "presenter-reach"); n != 0 {
		t.Fatalf("entering presenter-reach opened %d alarms before its 10s sustain", n)
	}
	sweep(11 * time.Second)
	if n := openRuleAlarms(t, f, "presenter-reach"); n != 1 {
		t.Fatalf("past its sustain, presenter-reach left %d alarms open, want 1", n)
	}
	if n := firings("presenter-reach"); n != 1 {
		t.Fatalf("past its sustain, presenter-reach holds %d firings, want 1", n)
	}

	// Leave: moving back ends presenter-reach's condition, its alarm, and
	// its streak there.
	before, after = moveTo(f.bar.Name)
	if err := gw.ExportReselectComponentRules(ctx, barID, before, after); err != nil {
		t.Fatal(err)
	}
	if n := firings("presenter-reach"); n != 0 {
		t.Fatalf("leaving presenter-reach left %d firings", n)
	}
	if n := openRuleAlarms(t, f, "presenter-reach"); n != 0 {
		t.Fatalf("leaving presenter-reach left %d alarms open", n)
	}
	if n := streaks("presenter-reach"); n != 0 {
		t.Fatalf("leaving presenter-reach left %d streaks", n)
	}
	// The rule names no clear event type, so its episode wrote its fire alone.
	if n := events("presenter-reach"); n != 1 {
		t.Fatalf("presenter-reach wrote %d events across enter and leave, want its one fire", n)
	}

	// Stay: collab-reach selected bar-1 on both sides of both moves.
	if n := openRuleAlarms(t, f, "collab-reach"); n != 1 {
		t.Fatalf("collab-reach, selected throughout, has %d alarms open, want still 1", n)
	}
	if n := events("collab-reach"); n != collabEvents {
		t.Fatalf("collab-reach, selected throughout, wrote %d events, want still %d", n, collabEvents)
	}

	// Neither: camera-reach names neither type, so its row is untouched.
	if n := firings("camera-reach"); n != 1 {
		t.Fatalf("camera-reach names neither type, yet the step left %d of its firings, want the 1 untouched", n)
	}
}

// TestReselectComponentRulesSameSelection proves the step is a no-op when
// the selection did not move: a rule already holding stays as it is.
func TestReselectComponentRulesSameSelection(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	var barID string
	if err := f.conn.QueryRow(ctx, `select id::text from component where name = 'bar-1'`).Scan(&barID); err != nil {
		t.Fatal(err)
	}
	sel, err := f.gw.ExportComponentRuleSelection(ctx, barID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sel) != 2 {
		t.Fatalf("a video-bar's selection is %v, want its type and collaboration-device", sel)
	}
	if err := f.gw.ExportReselectComponentRules(ctx, barID, sel, sel); err != nil {
		t.Fatal(err)
	}
}
