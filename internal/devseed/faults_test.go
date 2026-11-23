package devseed_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/impair"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// faultStory is one of the dev fleet's seeded faults (#2434) as a test reads
// it: the component (by its room and minted name), the rule that judges it,
// and the reading that lifts it.
type faultStory struct {
	key, room, name, rule string
	recovery              any
}

// The two stories the dev fleet tells: the auditorium DSP gone dark (an
// outage, judged by the official reachability rule) and the depot display
// slow to answer (a threshold warning, judged by the fleet's own rule).
var faultStories = []faultStory{
	{key: "aud-dsp", room: "auditorium", name: "dsp-1", rule: "reachability-lost", recovery: "up"},
	{key: "bay-1-display", room: "bay-1", name: "display-1", rule: "response-slow", recovery: 3.2},
}

// The stories above are every fault the fixture seeds, and every one names a
// cause on both paths: an impairment for the simulator and a reading for a
// fleet with none.
func TestFaultStoriesAreTheFixtures(t *testing.T) {
	t.Parallel()
	doc := fixturesDoc(t)
	if len(doc.Faults) != len(faultStories) {
		t.Fatalf("the fixture seeds %d faults, the stories here cover %d", len(doc.Faults), len(faultStories))
	}
	for i, f := range doc.Faults {
		if f.Component != faultStories[i].key {
			t.Errorf("fault %d is on %q, want %q", i, f.Component, faultStories[i].key)
		}
		if f.Impairment.Impairment() == (impair.Impairment{}) {
			t.Errorf("fault on %q names no impairment for the simulator to hold", f.Component)
		}
		if f.Reading.Key == "" || f.Reading.Instance == "" || f.Reading.Value == nil {
			t.Errorf("fault on %q names no reading for a fleet without the simulator", f.Component)
		}
		if f.Why == "" {
			t.Errorf("fault on %q says nothing of why", f.Component)
		}
	}
	if len(doc.Alarms) != 0 {
		t.Errorf("the example fleet hand-raises %d alarms; every alarm it shows is raised by a rule from a cause", len(doc.Alarms))
	}
}

// openAlarms is every open alarm in the fleet.
func openAlarms(t *testing.T, ctx context.Context, gw storage.Gateway) []storage.Alarm {
	t.Helper()
	all := scope.Set{All: true}
	var out []storage.Alarm
	token := ""
	for {
		page, next, err := gw.ListFleetAlarms(ctx, all, all, storage.AlarmFilter{PageToken: token}, 100)
		if err != nil {
			t.Fatalf("list alarms: %v", err)
		}
		out = append(out, page...)
		if next == "" {
			return out
		}
		token = next
	}
}

// seedCounts is what a re-run of the seed must leave alone.
func seedCounts(t *testing.T, ctx context.Context, conn *pgx.Conn) [4]int {
	t.Helper()
	var c [4]int
	for i, q := range []string{
		`select count(*) from alarm`,
		`select count(*) from event`,
		`select count(*) from event_rule`,
		`select count(*) from sim_impairment`,
	} {
		if err := conn.QueryRow(ctx, q).Scan(&c[i]); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return c
}

// A fleet seeded without the simulator (the docs' screenshots) carries its
// faults as recorded readings, and the engine judges them: once each rule's
// sustain has passed, every open alarm was raised by a rule, carries the event
// that raised it, and the component's newest reading agrees with it. A re-run
// of the seed changes nothing, and the reading an outpost would land once the
// fault lifts clears the alarm through the same engine.
func TestSeededFaultsRaiseTrueAlarms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := devseed.BootSeededDSN(t)
	gw, err := storage.NewPG(ctx, dsn, devseed.SecretProvider())
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	noSim := devseed.Options{}
	if err := devseed.RunWith(ctx, gw, "", noSim); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, a := range openAlarms(t, ctx, gw) {
		if a.RaisedByRule == nil {
			t.Errorf("alarm %q on %s is open with no rule behind it", a.Message, a.ComponentName)
		}
	}
	// The sweep settles each streak once its sustain has passed, as the
	// server's 5 s tick does.
	if err := gw.SweepEventRules(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	open := openAlarms(t, ctx, gw)
	for _, a := range open {
		if a.RaisedByRule == nil {
			t.Errorf("alarm %q on %s is open with no rule behind it", a.Message, a.ComponentName)
		}
	}
	doc := fixturesDoc(t)
	for i, s := range faultStories {
		comp := componentIn(t, ctx, conn, gw, s.room, s.name)
		var alarm *storage.Alarm
		for j := range open {
			if open[j].ComponentID == comp.ID && open[j].RaisedByRule != nil && *open[j].RaisedByRule == s.rule {
				alarm = &open[j]
			}
		}
		if alarm == nil {
			t.Errorf("%s/%s: no open alarm raised by %s", s.room, s.name, s.rule)
			continue
		}
		var fired int
		if err := conn.QueryRow(ctx, `select count(*) from event where component_id = $1 and alarm_id = $2`,
			comp.ID, alarm.ID).Scan(&fired); err != nil {
			t.Fatal(err)
		}
		if fired == 0 {
			t.Errorf("%s/%s: the %s alarm has no triggering event", s.room, s.name, s.rule)
		}
		r := doc.Faults[i].Reading
		if got := newestReading(t, ctx, conn, comp.ID, r.Key, r.Instance); got != formatReading(r.Value) {
			t.Errorf("%s/%s: newest %s/%s reads %q, want %q, the reading the alarm stands on", s.room, s.name, r.Key, r.Instance, got, formatReading(r.Value))
		}
	}

	before := seedCounts(t, ctx, conn)
	if err := devseed.RunWith(ctx, gw, "", noSim); err != nil {
		t.Fatal(err)
	}
	if after := seedCounts(t, ctx, conn); after != before {
		t.Errorf("a re-run of the seed moved alarms, events, rules, impairments %v to %v", before, after)
	}

	// The fault lifts: the outpost reads the device healthy again, and the
	// engine clears what it raised, with no hand on the alarm.
	for i, s := range faultStories {
		comp := componentIn(t, ctx, conn, gw, s.room, s.name)
		r := doc.Faults[i].Reading
		w := storage.OutpostWriter(comp.ID, r.Instance)
		switch v := s.recovery.(type) {
		case string:
			err = gw.InsertPropertySamples(ctx, w, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: comp.ID, Key: r.Key, Instance: r.Instance, Value: v, Source: "reachability", TS: time.Now().UTC()}})
		case float64:
			err = gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp.ID, Key: r.Key, Instance: r.Instance, Value: v, Source: "snmp", TS: time.Now().UTC()}})
		}
		if err != nil {
			t.Fatalf("%s/%s: land the recovery: %v", s.room, s.name, err)
		}
	}
	if err := gw.SweepEventRules(ctx, time.Now().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, a := range openAlarms(t, ctx, gw) {
		for _, s := range faultStories {
			if a.RaisedByRule != nil && *a.RaisedByRule == s.rule && a.ComponentName == s.name && a.LocationName == s.room {
				t.Errorf("%s/%s: the %s alarm is still open after the fault lifted", s.room, s.name, s.rule)
			}
		}
	}
}

// newestReading is the newest value of one series on a component, on
// whichever lane the key is, as text.
func newestReading(t *testing.T, ctx context.Context, conn *pgx.Conn, componentID, key, instance string) string {
	t.Helper()
	var v string
	err := conn.QueryRow(ctx, `
		select v from (
			select s.ts, s.value #>> '{}' as v from property s join property_type t on t.id = s.property_type_id
			where s.component_id = $1 and t.name = $2 and s.instance = $3
			union all
			select m.ts, m.value::text from metric_series s join metric_type t on t.id = s.metric_type_id
			join metric m on m.series_id = s.id
			where s.owner_kind = 'component' and s.owner_id = $1 and t.name = $2 and s.instance = $3
		) x order by ts desc limit 1`, componentID, key, instance).Scan(&v)
	if err != nil {
		t.Fatalf("newest %s/%s on %s: %v", key, instance, componentID, err)
	}
	return v
}

// readingCount is how many samples one series on a component holds, on
// either lane.
func readingCount(t *testing.T, ctx context.Context, conn *pgx.Conn, componentID, key, instance string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(ctx, `
		select (select count(*) from property s join property_type t on t.id = s.property_type_id
			where s.component_id = $1 and t.name = $2 and s.instance = $3)
		     + (select count(*) from metric_series s join metric_type t on t.id = s.metric_type_id
			join metric m on m.series_id = s.id
			where s.owner_kind = 'component' and s.owner_id = $1 and t.name = $2 and s.instance = $3)`,
		componentID, key, instance).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func formatReading(v any) string { return fmt.Sprint(v) }

// A fleet seeded beside the simulator carries its faults as impairments the
// simulator holds, writing no reading of its own: the outpost reads the
// device as the fault leaves it. They are seeded once, with the device's
// endpoints, so a presenter who lifts one on the Simulation page finds it
// still lifted after a restart re-runs the seed.
func TestSimFleetSeedsFaultsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := devseed.BootSeededDSN(t)
	gw, err := storage.NewPG(ctx, dsn, devseed.SecretProvider())
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := devseed.RunWith(ctx, gw, "", devseed.Options{SimFleet: true}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	doc := fixturesDoc(t)
	ids := make([]string, len(faultStories))
	for i, s := range faultStories {
		comp := componentIn(t, ctx, conn, gw, s.room, s.name)
		ids[i] = comp.ID
		got, err := gw.GetSimImpairment(ctx, comp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if want := doc.Faults[i].Impairment.Impairment().Normalized(); got != want {
			t.Errorf("%s/%s impairment = %+v, want %+v", s.room, s.name, got, want)
		}
		r := doc.Faults[i].Reading
		n := readingCount(t, ctx, conn, comp.ID, r.Key, r.Instance)
		if n != 0 {
			t.Errorf("%s/%s: the seed wrote %d %s readings beside the simulator, which is the outpost's to read", s.room, s.name, n, r.Key)
		}
	}
	// A presenter lifts the first fault; a restart re-runs the seed.
	if _, err := gw.SetSimImpairment(ctx, "", ids[0], impair.Impairment{}); err != nil {
		t.Fatal(err)
	}
	if err := devseed.RunWith(ctx, gw, "", devseed.Options{SimFleet: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := gw.GetSimImpairment(ctx, ids[0]); got != (impair.Impairment{}) {
		t.Errorf("a re-run re-imposed the lifted fault: %+v", got)
	}
	if got, _ := gw.GetSimImpairment(ctx, ids[1]); got == (impair.Impairment{}) {
		t.Error("a re-run lifted the fault nobody touched")
	}
}

// landFaultReadings lands, as the outpost would, what each seeded fault reads
// as from a fleet seeded beside the simulator, where no outpost runs in a
// test: the reading the fixture names for the fleet without one.
func landFaultReadings(t *testing.T, ctx context.Context, conn *pgx.Conn, gw storage.Gateway) {
	t.Helper()
	doc := fixturesDoc(t)
	for i, s := range faultStories {
		comp := componentIn(t, ctx, conn, gw, s.room, s.name)
		r := doc.Faults[i].Reading
		w := storage.OutpostWriter(comp.ID, r.Instance)
		var err error
		switch v := r.Value.(type) {
		case string:
			err = gw.InsertPropertySamples(ctx, w, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: comp.ID, Key: r.Key, Instance: r.Instance, Value: v, Source: r.Source, TS: time.Now().UTC()}})
		case float64:
			err = gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp.ID, Key: r.Key, Instance: r.Instance, Value: v, Source: r.Source, TS: time.Now().UTC()}})
		default:
			t.Fatalf("%s/%s: reading %v", s.room, s.name, r.Value)
		}
		if err != nil {
			t.Fatalf("%s/%s: land the fault's reading: %v", s.room, s.name, err)
		}
	}
}
