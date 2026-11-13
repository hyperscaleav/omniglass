package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// platformComponentOf is the id of node's platform component.
func (f groupFixture) platformComponentOf(t *testing.T, node string) string {
	t.Helper()
	var id string
	if err := f.conn.QueryRow(context.Background(), `select platform_component_id::text from node where name = $1`, node).Scan(&id); err != nil {
		t.Fatalf("read node %s's platform component: %v", node, err)
	}
	return id
}

// selfInstances is the instances a node reports key under: a session state
// for session-held-count, the pool for worker-busy-fraction (#1686), every
// kind for a delivery key (#1659), else none.
func selfInstances(key string) []string {
	switch key {
	case writerclass.SessionHeldCount:
		return []string{"open"}
	case writerclass.WorkerBusyFraction:
		return []string{"lane"}
	case writerclass.DeliveryInCount, writerclass.DeliveryOutCount, writerclass.DeliveryLossRatio:
		out := make([]string, 0, len(delivery.Kinds))
		for _, k := range delivery.Kinds {
			out = append(out, string(k))
		}
		return out
	}
	return []string{""}
}

// selfReport is a node-self report of every declared node-self key, one
// sample per instance it is reported under (selfInstances).
func selfReport(node string, ts time.Time) *storage.NodeSelfReport {
	r := &storage.NodeSelfReport{Node: node}
	for i, k := range writerclass.NodeSelfKeys() {
		for _, inst := range selfInstances(k) {
			r.Metrics = append(r.Metrics, storage.NodeSelfSample{Key: k, Instance: inst, Value: float64(i + 1), TS: ts})
		}
	}
	return r
}

// TestNodeSelfReportLandsEveryKeyOnThePlatformComponent is the node-self
// lane (#1625): a node's tick carries its report of itself beside its task
// groups, and every node-self key lands on the node's own platform component,
// observed, with no collector (the process writes it, not a task), each
// series under its instance: the session state, and for a delivery key
// (#1659) one series per kind.
func TestNodeSelfReportLandsEveryKeyOnThePlatformComponent(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	platform := f.platformComponentOf(t, "edge-1")
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	report := selfReport("edge-1", ts)
	res, err := f.gw.LandBatch(ctx, storage.IngestBatch{
		BatchID:  "tick-self",
		Writer:   storage.NodeWriter("", ""),
		Groups:   []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", ts)},
		NodeSelf: report,
	})
	if err != nil {
		t.Fatalf("land the tick: %v", err)
	}
	if !res.Landed || len(res.Refused) != 0 {
		t.Fatalf("result = %+v, want landed with nothing refused", res)
	}
	rows, err := f.conn.Query(ctx, `select t.name, m.instance, m.provenance, m.node_id is null, m.source
		from `+storagetest.MetricRows+` m join metric_type t on t.id = m.metric_type_id
		where m.component_id = $1::uuid`, platform)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[[2]string]int{}
	for rows.Next() {
		var key, instance, provenance, source string
		var noCollector bool
		if err := rows.Scan(&key, &instance, &provenance, &noCollector, &source); err != nil {
			t.Fatal(err)
		}
		if provenance != "observed" || !noCollector {
			t.Errorf("%s landed as %s with a collector %v; want observed, written by the process", key, provenance, !noCollector)
		}
		if source != "node/edge-1" {
			t.Errorf("%s source = %q, want node/edge-1", key, source)
		}
		got[[2]string{key, instance}]++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, k := range writerclass.NodeSelfKeys() {
		for _, inst := range selfInstances(k) {
			if n := got[[2]string{k, inst}]; n != 1 {
				t.Errorf("%s[%s] landed %d series on the node's platform component, want 1", k, inst, n)
			}
		}
	}
	if len(got) != len(report.Metrics) {
		t.Errorf("landed %d series on the platform component, want the report's %d", len(got), len(report.Metrics))
	}
	if want := (landedRows{metric: 1 + len(report.Metrics), property: 1, event: 1, logLine: 1}); f.rows(t) != want {
		t.Errorf("landed %+v, want the task's rows and the report %+v", f.rows(t), want)
	}
}

// TestNodeSelfReportRefusesAKeyNotItsOwn: a node reports of itself only the
// declared node-self keys. A collected key, or a platform key another writer
// owns (health, the server's checkin-age), refuses the report, alone: the
// tick's task group lands.
func TestNodeSelfReportRefusesAKeyNotItsOwn(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"room-temperature", "an-undeclared-key", writerclass.CheckinAge, writerclass.Health, writerclass.CommandPendingCount} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			f := newGroupFixture(t)
			ctx := context.Background()
			ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
			report := selfReport("edge-1", ts)
			report.Metrics = append(report.Metrics, storage.NodeSelfSample{Key: key, Value: 1, TS: ts})
			res, err := f.gw.LandBatch(ctx, storage.IngestBatch{
				BatchID:  "tick-spoof",
				Writer:   storage.NodeWriter("", ""),
				Groups:   []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", ts)},
				NodeSelf: report,
			})
			if err != nil {
				t.Fatalf("land the tick: %v", err)
			}
			if len(res.Refused) != 1 || res.Refused[0].Index != storage.NodeSelfIndex || !errors.Is(res.Refused[0].Err, storage.ErrKeyWriterRefused) {
				t.Fatalf("refused = %+v, want the node-self report alone, ErrKeyWriterRefused", res.Refused)
			}
			if want := (landedRows{metric: 1, property: 1, event: 1, logLine: 1}); f.rows(t) != want {
				t.Errorf("landed %+v, want the task's rows alone %+v", f.rows(t), want)
			}
		})
	}
}

// TestPlatformKeyInATaskGroupIsRefused is #1094's anti-spoof guard for the
// node-self keys: a task's rows are the node writer's, which the platform
// class does not admit, so a task that reports any node-self key (even on the
// node's own platform component) is refused, alone.
func TestPlatformKeyInATaskGroupIsRefused(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	for _, k := range writerclass.NodeSelfKeys() {
		spoof := taskGroup("task-b", f.disp2, f.node, "on", ts)
		spoof.Metrics = append(spoof.Metrics, storage.MetricSampleWrite{OwnerKind: "component", OwnerID: f.disp2,
			Key: k, Value: 5, Source: "poll", TS: ts})
		res, err := f.gw.LandBatch(ctx, storage.IngestBatch{
			BatchID: "spoof-" + k,
			Writer:  storage.NodeWriter("", ""),
			Groups:  []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", ts), spoof},
		})
		if err != nil {
			t.Fatalf("land the %s spoof: %v", k, err)
		}
		if len(res.Refused) != 1 || res.Refused[0].Index != 1 || !errors.Is(res.Refused[0].Err, storage.ErrKeyWriterRefused) {
			t.Fatalf("%s: refused = %+v, want task-b alone, ErrKeyWriterRefused", k, res.Refused)
		}
		var kwe *storage.KeyWriterError
		if !errors.As(res.Refused[0].Err, &kwe) || kwe.Key != k || kwe.Class != writerclass.Platform {
			t.Errorf("refusal = %v, want %s refused as a platform key", res.Refused[0].Err, k)
		}
	}
}

// TestNodeSelfReportWithNoPlatformComponentIsRefused: a node registered
// while the database had no platform system has no component until the next
// boot adopts it; its report is refused, alone, not failed for redelivery.
func TestNodeSelfReportWithNoPlatformComponentIsRefused(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	if _, err := f.conn.Exec(ctx, `update node set platform_component_id = null where name = 'edge-1'`); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	res, err := f.gw.LandBatch(ctx, storage.IngestBatch{
		BatchID:  "tick-orphan",
		Writer:   storage.NodeWriter("", ""),
		Groups:   []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", ts)},
		NodeSelf: selfReport("edge-1", ts),
	})
	if err != nil {
		t.Fatalf("land the tick: %v", err)
	}
	if !res.Landed || len(res.Refused) != 1 || res.Refused[0].Index != storage.NodeSelfIndex || !errors.Is(res.Refused[0].Err, storage.ErrKeyWriterRefused) {
		t.Fatalf("result = %+v, want landed with the node-self report refused", res)
	}
	if want := (landedRows{metric: 1, property: 1, event: 1, logLine: 1}); f.rows(t) != want {
		t.Errorf("landed %+v, want the task's rows alone %+v", f.rows(t), want)
	}
}
