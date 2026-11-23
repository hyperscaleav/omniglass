package storage_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// reselectRig is bar-1 (a video bar) reading down on a transition-only
// input an hour ago, and four rules on that input by type: one on video
// bars, one on display devices (with a sustain), one on both, and one on
// cameras. Moving bar-1 to an interactive-display product takes it out of
// the first, into the second, and leaves it in the third.
func newReselectRig(t *testing.T) *healthFixture {
	t.Helper()
	f := newHealthFixture(t)
	ctx := context.Background()
	if err := insertProperties(ctx, f.gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now().UTC().Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		name    string
		types   []string
		sustain time.Duration
	}{
		{"on-bars", []string{"video-bar"}, 0},
		{"on-displays", []string{"display-device"}, 10 * time.Second},
		{"on-both", []string{"collaboration-device", "display"}, 0},
		{"on-cameras", []string{"camera"}, 0},
	} {
		if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "rule-fired", Name: r.name,
			SampleKey: "endpoint-reachable", Selector: storage.EventRuleSelector{ComponentTypes: r.types},
			Fire: criteria("eq", "down"), FireSustain: r.sustain, Clear: ptrCriteria(criteria("eq", "up")),
			Severity: "high"}, f.all); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *healthFixture) moveBarTo(t *testing.T, componentType string) {
	t.Helper()
	ctx := context.Background()
	p := storagetest.MintProduct(t, ctx, f.gw, componentType)
	if err := storage.ExportReselectComponentRules(ctx, f.gw, "bar-1",
		`update component set product_id = (select id from product where name = $1) where name = 'bar-1'`, p.Name); err != nil {
		t.Fatal(err)
	}
}

// ruleEpisode is a rule's fire events and firing rows on bar-1, and when its
// firing there began ("" for none).
func ruleEpisode(t *testing.T, f *healthFixture, rule string) (fires, firings int, since string) {
	t.Helper()
	if err := f.conn.QueryRow(context.Background(), `select
			(select count(*) from event e join event_rule r on r.id = e.derived_by_rule_id
				join event_type et on et.id = e.event_type_id
				where r.name = $1 and et.name = 'rule-fired' and e.component_id = c.id),
			(select count(*) from event_rule_firing x join event_rule r on r.id = x.rule_id where r.name = $1 and x.component_id = c.id),
			coalesce((select x.since::text from event_rule_firing x join event_rule r on r.id = x.rule_id
				where r.name = $1 and x.component_id = c.id), '')
		from component c where c.name = 'bar-1'`, rule).Scan(&fires, &firings, &since); err != nil {
		t.Fatal(err)
	}
	return fires, firings, since
}

// A component whose type changes is re-judged by the rules whose selection
// of it changed (#2475): a rule it came into judges its newest sample, so a
// transition-only input already down opens the condition once the sustain
// holds; a rule it left ends its condition there; a rule that held it before
// and after is left as it was.
func TestReselectComponentRules(t *testing.T) {
	t.Parallel()
	f := newReselectRig(t)
	ctx := context.Background()
	for rule, want := range map[string]int{"on-bars": 1, "on-displays": 0, "on-both": 1, "on-cameras": 0} {
		if n := openRuleAlarms(t, f, rule); n != want {
			t.Fatalf("before the move, %s holds %d alarms on bar-1, want %d", rule, n, want)
		}
	}
	_, _, bothSince := ruleEpisode(t, f, "on-both")

	f.moveBarTo(t, "interactive-display")

	if n := openRuleAlarms(t, f, "on-bars"); n != 0 {
		t.Errorf("bar-1 left on-bars, which still holds %d alarms on it", n)
	}
	if _, firings, _ := ruleEpisode(t, f, "on-bars"); firings != 0 {
		t.Errorf("bar-1 left on-bars, which still holds %d firings on it", firings)
	}
	if n := openRuleAlarms(t, f, "on-displays"); n != 0 {
		t.Fatalf("bar-1 came into on-displays inside its sustain: %d alarms, want 0 yet", n)
	}
	if err := f.gw.SweepEventRules(ctx, time.Now().Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := openRuleAlarms(t, f, "on-displays"); n != 1 {
		t.Errorf("bar-1 came into on-displays, down an hour, past the sustain: %d alarms, want 1", n)
	}
	fires, firings, since := ruleEpisode(t, f, "on-both")
	if n := openRuleAlarms(t, f, "on-both"); n != 1 || fires != 1 || firings != 1 || since != bothSince {
		t.Errorf("on-both held bar-1 before and after: %d alarms, %d fire events, %d firings since %q (was %q), want one of each unchanged",
			n, fires, firings, since, bothSince)
	}
	if n := openRuleAlarms(t, f, "on-cameras"); n != 0 {
		t.Errorf("on-cameras selects bar-1 neither before nor after, yet holds %d alarms", n)
	}
}

// Moving back reverses it: the rule the component returns to opens again
// with no sustain, and the one it leaves ends.
func TestReselectComponentRulesBack(t *testing.T) {
	t.Parallel()
	f := newReselectRig(t)
	f.moveBarTo(t, "interactive-display")
	f.moveBarTo(t, "video-bar")
	if n := openRuleAlarms(t, f, "on-bars"); n != 1 {
		t.Errorf("bar-1 returned to on-bars, down: %d alarms, want 1 at once", n)
	}
	if fires, _, _ := ruleEpisode(t, f, "on-bars"); fires != 2 {
		t.Errorf("bar-1 returned to on-bars: %d fire events, want 2 (one each episode)", fires)
	}
	if _, firings, _ := ruleEpisode(t, f, "on-displays"); firings != 0 {
		t.Errorf("bar-1 left on-displays: %d firings, want 0", firings)
	}
	var streaks int
	if err := f.conn.QueryRow(context.Background(), `select count(*) from event_rule_state s join event_rule r on r.id = s.rule_id
		where r.name = 'on-displays'`).Scan(&streaks); err != nil {
		t.Fatal(err)
	}
	if streaks != 0 {
		t.Errorf("bar-1 left on-displays inside its sustain, which still keeps %d streaks on it", streaks)
	}
}

// The rules a reselect considers are bounded to those naming a type in the
// old or new lineage: a rule naming neither (on-cameras), and one selecting
// every component, are never read, so a type change never re-primes every
// rule.
func TestReselectCandidatesBounded(t *testing.T) {
	t.Parallel()
	f := newReselectRig(t)
	got, err := storage.ExportReselectCandidates(context.Background(), f.gw,
		[]string{"collaboration-device", "video-bar"}, []string{"display", "display-device", "interactive-display"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"on-bars", "on-displays", "on-both"} {
		if !slices.Contains(got, want) {
			t.Errorf("candidates %v miss %s, which names a type in a lineage", got, want)
		}
	}
	if slices.Contains(got, "on-cameras") {
		t.Errorf("candidates %v include on-cameras, which names neither lineage", got)
	}
	rules, err := f.gw.ListEventRules(context.Background(), f.all)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		if len(r.Selector.ComponentTypes) == 0 && slices.Contains(got, r.Name) {
			t.Errorf("candidates include %s, which names no type", r.Name)
		}
	}
}
