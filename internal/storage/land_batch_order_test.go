package storage_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestABatchJudgesEachSampleOfASeriesInOrder holds the metric lane's rule
// semantics across #1274's batched write: a batch that carries one series
// twice is judged sample by sample, in order, exactly as two batches would be.
// The rule fires on the first sample and clears on the second, so the batch
// leaves both events and no open alarm. A write that landed every row before
// judging any would find the second row newer than the first, skip the first
// as stale, and never fire.
func TestABatchJudgesEachSampleOfASeriesInOrder(t *testing.T) {
	r := newEdgeRig(t)
	if _, err := r.f.gw.CreateEventRule(r.ctx, "", storage.EventRuleSpec{
		Name: "hot-room", SampleKey: "room-temperature", Severity: "high",
		Fire: criteria("gt", 30), Clear: ptrCriteria(criteria("lt", 25)),
		FireEventType: "condition-started", ClearEventType: "condition-ended",
		Selector: storage.EventRuleSelector{Components: []string{"bar-1"}},
	}, r.f.all); err != nil {
		t.Fatal(err)
	}
	r.name = "hot-room"

	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	landed, err := r.f.gw.LandBatch(r.ctx, storage.IngestBatch{
		BatchID: "twice-one-series",
		Writer:  storage.PushWriter(),
		Metrics: []storage.MetricSampleWrite{
			{OwnerKind: "component", OwnerID: "bar-1", Key: "room-temperature", Value: 31, Source: "push", TS: ts},
			{OwnerKind: "component", OwnerID: "bar-1", Key: "room-temperature", Value: 20, Source: "push", TS: ts.Add(time.Second)},
		},
	})
	if err != nil || !landed {
		t.Fatalf("land: landed=%v err=%v", landed, err)
	}

	evs := r.events()
	if len(evs) != 2 || evs[0].key != "condition-started" || evs[1].key != "condition-ended" {
		got := make([]string, len(evs))
		for i, e := range evs {
			got[i] = e.key
		}
		t.Fatalf("one batch carrying 31 then 20 derived %v, want [condition-started condition-ended]", got)
	}
	if a := r.openAlarm(""); a != "" {
		t.Fatalf("an alarm is still open after the clearing sample: %s", a)
	}

	// The series is the resolved owner, not how the batch names it: a push
	// naming bar-1 once by name and once by id carries one series twice, and
	// is judged sample by sample just the same.
	barID := scalar(t, r.f.conn, `select id::text from component where name = 'bar-1'`)
	landed, err = r.f.gw.LandBatch(r.ctx, storage.IngestBatch{
		BatchID: "twice-one-series-two-names",
		Writer:  storage.PushWriter(),
		Metrics: []storage.MetricSampleWrite{
			{OwnerKind: "component", OwnerID: "bar-1", Key: "room-temperature", Value: 32, Source: "push", TS: ts.Add(2 * time.Second)},
			{OwnerKind: "component", OwnerID: barID, Key: "room-temperature", Value: 21, Source: "push", TS: ts.Add(3 * time.Second)},
		},
	})
	if err != nil || !landed {
		t.Fatalf("land by name then id: landed=%v err=%v", landed, err)
	}
	if evs := r.events(); len(evs) != 4 || evs[2].key != "condition-started" || evs[3].key != "condition-ended" {
		got := make([]string, len(evs))
		for i, e := range evs {
			got[i] = e.key
		}
		t.Fatalf("a batch naming one component by name then by id derived %v, want a second fire and clear", got)
	}
}
