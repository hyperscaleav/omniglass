package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A rename of a level serializes with an ingest batch raising at it (#930
// review): the batch holds the rule it judges by FOR SHARE and its raise
// needs the level's key (the alarm's foreign key), while the rename's cascade
// rewrites the rules naming the level. Taking the level first and the rules
// second deadlocks against such a batch; the rename takes the rules first, so
// it waits for the batch and both complete. The test stalls the batch on its
// series lock after it has loaded the rule and before it raises, starts the
// rename meanwhile, then lets both go.
func TestSeverityRenameDuringARaisingBatch(t *testing.T) {
	f := newHealthFixture(t)
	ctx, all := context.Background(), scope.Set{All: true}
	if _, err := f.gw.CreateSeverityLevel(ctx, "", storage.SeverityLevel{Name: "major", Label: "Major", Color: "orange", Rank: 35}, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "at-major", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Severity: "major"}, all); err != nil {
		t.Fatal(err)
	}
	stall := holdLock(t, f, seriesLockKey(t, f, "at-major", "tcp"))
	var wg sync.WaitGroup
	var ingestErr, renameErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		ingestErr = f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now().UTC()}})
	}()
	time.Sleep(700 * time.Millisecond)
	go func() {
		defer wg.Done()
		p2 := "p2"
		_, renameErr = f.gw.UpdateSeverityLevel(ctx, "", "major", storage.SeverityLevelPatch{Name: &p2}, all)
	}()
	time.Sleep(700 * time.Millisecond)
	_ = stall.Rollback(ctx)
	wg.Wait()
	if ingestErr != nil {
		t.Fatalf("ingest: %v", ingestErr)
	}
	if renameErr != nil {
		t.Fatalf("a rename concurrent with a raising batch failed: %v", renameErr)
	}
	alarms, err := f.gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	raised := 0
	for _, a := range alarms {
		if a.RaisedByRule != nil && *a.RaisedByRule == "at-major" {
			raised++
			if a.Severity != "p2" {
				t.Errorf("the alarm reads %q after the rename, want p2", a.Severity)
			}
		}
	}
	if raised != 1 {
		t.Fatalf("the batch's raise was lost to the rename: %d alarms, want 1", raised)
	}
}
