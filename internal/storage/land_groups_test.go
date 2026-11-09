package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// groupFixture is a land fixture with a second component, and the ids a
// node's tick names its owners and itself by.
type groupFixture struct {
	landFixture
	disp1, disp2, node string
}

func newGroupFixture(t *testing.T) groupFixture {
	t.Helper()
	f := groupFixture{landFixture: newLandFixture(t)}
	all := scope.Set{All: true}
	ctx := context.Background()
	if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	err := f.conn.QueryRow(ctx, `select
		(select id::text from component where name = 'disp-1'),
		(select id::text from component where name = 'disp-2'),
		(select principal_id::text from node where name = 'edge-1')`).Scan(&f.disp1, &f.disp2, &f.node)
	if err != nil {
		t.Fatalf("read ids: %v", err)
	}
	return f
}

// taskGroup is one task's rows on component, collected by node: a sample on
// every lane a task reaches.
func taskGroup(task, component, node, power string, ts time.Time) storage.IngestGroup {
	w := storage.NodeWriter(component, "ep-"+task)
	w.Node = node
	return storage.IngestGroup{
		TaskID: task,
		Writer: w,
		Metrics: []storage.MetricSampleWrite{
			{OwnerKind: "component", OwnerID: component, Key: "room-temperature", Value: 21.5, Source: "poll", TS: ts},
		},
		Properties: []storage.PropertySampleWrite{
			{OwnerKind: "component", OwnerID: component, Key: "power-state", Value: power, Source: "poll", TS: ts},
		},
		Events: []storage.EventWrite{
			{OwnerKind: "component", OwnerID: component, Key: "input-changed", Message: "hdmi-2", Source: "poll", TS: ts},
		},
		LogLines: []storage.LogLineWrite{
			{OwnerKind: "component", OwnerID: component, Source: "syslog", Severity: "info", Message: "input changed", TS: ts},
		},
	}
}

func (f groupFixture) receipts(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow(context.Background(), `select count(*) from telemetry_receipt where batch_id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count receipts: %v", err)
	}
	return n
}

// TestLandBatchRefusedGroupDoesNotBlockSiblings is the tick's contract
// (#1582): a node's tick lands as one batch of task groups, under one
// receipt, and a group the gateway refuses is dropped on its own, named in
// the result, while its siblings land. A refusal decided before anything is
// written (a row on a component the task does not collect for) and one
// Postgres decides (an event's attributes that are not JSON) drop the same
// way.
func TestLandBatchRefusedGroupDoesNotBlockSiblings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		refuse func(f groupFixture, g storage.IngestGroup) storage.IngestGroup
	}{
		{"a row on a component the task does not collect for", func(f groupFixture, g storage.IngestGroup) storage.IngestGroup {
			g.Metrics[0].OwnerID = f.disp1
			return g
		}},
		{"an event whose attributes are not JSON", func(_ groupFixture, g storage.IngestGroup) storage.IngestGroup {
			g.Events[0].Attributes = []byte("{not json")
			return g
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newGroupFixture(t)
			ctx := context.Background()
			ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
			b := storage.IngestBatch{
				BatchID: "tick-1",
				Writer:  storage.NodeWriter("", ""),
				Groups: []storage.IngestGroup{
					taskGroup("task-a", f.disp1, f.node, "on", ts),
					tc.refuse(f, taskGroup("task-b", f.disp2, f.node, "on", ts)),
				},
			}
			res, err := f.gw.LandBatch(ctx, b)
			if err != nil {
				t.Fatalf("land the tick: %v", err)
			}
			if !res.Landed {
				t.Fatal("the tick did not land")
			}
			if len(res.Refused) != 1 {
				t.Fatalf("refused = %+v, want task-b alone", res.Refused)
			}
			if r := res.Refused[0]; r.Index != 1 || r.TaskID != "task-b" || !storage.IsRefusedWrite(r.Err) {
				t.Fatalf("refusal = %+v (refused write %v), want index 1, task-b, a refusal", r, storage.IsRefusedWrite(r.Err))
			}
			if n := f.receipts(t, "tick-1"); n != 1 {
				t.Fatalf("receipts = %d, want 1", n)
			}
			want := landedRows{metric: 1, property: 1, event: 1, logLine: 1}
			if got := f.rows(t); got != want {
				t.Fatalf("landed %+v, want task-a's rows alone %+v", got, want)
			}
			var onB int
			if err := f.conn.QueryRow(ctx, `select
				(select count(*) from property where component_id = $1::uuid) +
				(select count(*) from event where component_id = $1::uuid) +
				(select count(*) from log_line where component_id = $1::uuid)`, f.disp2).Scan(&onB); err != nil {
				t.Fatal(err)
			}
			if onB != 0 {
				t.Fatalf("%d of the refused group's rows landed", onB)
			}
			again, err := f.gw.LandBatch(ctx, b)
			if err != nil || again.Landed {
				t.Fatalf("redelivery = %+v, %v; want nothing landed", again, err)
			}
			if got := f.rows(t); got != want {
				t.Fatalf("after redelivery landed %+v, want %+v", got, want)
			}
		})
	}
}

// TestLandBatchRefusedGroupLeavesNoCalcMark: a group Postgres refuses as
// its rows land (so the batch lands again, each group isolated) leaves no
// calc mark behind, while its sibling's mark stands: a calc rule reading a
// key the refused group carried is not woken for an owner nothing landed on.
func TestLandBatchRefusedGroupLeavesNoCalcMark(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	if _, err := f.gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "avg-temperature", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateCalcRule(ctx, "", storage.CalcRuleSpec{Name: "temperature-avg", OwnerKind: "component",
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temperature"}, all); err != nil {
		t.Fatalf("calc rule: %v", err)
	}
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	refused := taskGroup("task-b", f.disp2, f.node, "on", ts)
	refused.Events[0].Attributes = []byte("{not json")
	res, err := f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: "tick-1", Writer: storage.NodeWriter("", ""),
		Groups: []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", ts), refused}})
	if err != nil || !res.Landed || len(res.Refused) != 1 || res.Refused[0].TaskID != "task-b" {
		t.Fatalf("land = %+v, %v; want task-b alone refused", res, err)
	}
	marked := func(comp string) int {
		var n int
		if err := f.conn.QueryRow(ctx, `select count(*) from calc_pending where component_id = $1::uuid`, comp).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := marked(f.disp1); n != 1 {
		t.Errorf("the landed group's owner holds %d calc marks, want one", n)
	}
	if n := marked(f.disp2); n != 0 {
		t.Errorf("the refused group's owner holds %d calc marks, want none", n)
	}
}

// TestLandBatchRefusedWholeBatchFails holds the top level to what it always
// was: a refusal among the batch's own rows (no group) fails the batch.
func TestLandBatchRefusedWholeBatchFails(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)
	b := fullBatch("whole-1", "on", ts)
	b.Metrics[0].OwnerID = "no-such-display"
	b.Groups = []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", ts)}
	if _, err := f.gw.LandBatch(ctx, b); !storage.IsRefusedWrite(err) {
		t.Fatalf("err = %v, want the batch refused", err)
	}
	if got := f.rows(t); got != (landedRows{}) {
		t.Fatalf("landed %+v, want nothing", got)
	}
	if n := f.receipts(t, "whole-1"); n != 0 {
		t.Fatalf("receipts = %d, want 0", n)
	}
}

// TestLandBatchStatementsDoNotGrowWithGroups is the tick's cost (#1582): one
// batch's statements are the same for one task group as for fifty, each
// group on its own component, the series all new or all known alike, and
// whether the groups share their keys or each brings keys of its own (a tick
// of many drivers, each its own metric and property names): a key's writer
// class is read once for the whole batch, not once per group bringing it.
func TestLandBatchStatementsDoNotGrowWithGroups(t *testing.T) {
	t.Parallel()
	for _, distinct := range []bool{false, true} {
		name := "groups sharing their keys"
		if distinct {
			name = "each group its own keys"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			landStatementsDoNotGrow(t, distinct)
		})
	}
}

func landStatementsDoNotGrow(t *testing.T, distinct bool) {
	f := newGroupFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	const many = 50
	ids := make([]string, many)
	for i := range ids {
		c, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: fmt.Sprintf("tick-disp-%d", i)}, all, all, all, all)
		if err != nil {
			t.Fatalf("create component: %v", err)
		}
		ids[i] = c.ID
		if !distinct {
			continue
		}
		if _, err := f.gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: fmt.Sprintf("tick-metric-%d", i), DataType: "float"}); err != nil {
			t.Fatalf("create metric type: %v", err)
		}
		if _, err := f.gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: fmt.Sprintf("tick-property-%d", i), DataType: "string"}); err != nil {
			t.Fatalf("create property type: %v", err)
		}
	}
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, f.dsn, storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	tick := func(id string, n int, power string, ts time.Time) storage.IngestBatch {
		b := storage.IngestBatch{BatchID: id, Writer: storage.NodeWriter("", "")}
		for i := range n {
			g := taskGroup(fmt.Sprintf("task-%d", i), ids[i], f.node, power, ts)
			if distinct {
				g.Metrics[0].Key = fmt.Sprintf("tick-metric-%d", i)
				g.Properties[0].Key = fmt.Sprintf("tick-property-%d", i)
			}
			b.Groups = append(b.Groups, g)
		}
		return b
	}
	call := 0
	statements := func(n int) (first, known int) {
		t.Helper()
		call++
		base := time.Now().UTC().Add(-time.Hour)
		for round, power := range []string{"on", "off"} {
			counter.Reset()
			res, err := gw.LandBatch(ctx, tick(fmt.Sprintf("tick-%d-%d", call, round), n, power, base.Add(time.Duration(call*2+round)*time.Minute)))
			if err != nil || !res.Landed || len(res.Refused) != 0 {
				t.Fatalf("land %d groups: %+v, %v", n, res, err)
			}
			if round == 0 {
				first = counter.N()
			} else {
				known = counter.N()
			}
		}
		return first, known
	}
	// The first batch warms the gateway's caches.
	statements(1)
	oneFirst, oneKnown := statements(1)
	manyFirst, manyKnown := statements(many)
	if manyFirst != oneFirst || manyKnown != oneKnown {
		t.Fatalf("statements: one group %d then %d, %d groups %d then %d; want the same", oneFirst, oneKnown, many, manyFirst, manyKnown)
	}
}
