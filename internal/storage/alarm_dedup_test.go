package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestAlarmOneOpenPerCondition pins #465 (ADR-0075): the one-open invariant is
// per (component, condition), the condition identified by a raiser-supplied
// dedup key. Raising the same condition twice returns the existing open alarm
// instead of a duplicate; different conditions coexist on one component (each
// impairs the component's own verdict wholesale, #626); clearing reopens the
// key for a fresh incident.
func TestAlarmOneOpenPerCondition(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all); err != nil {
		t.Fatalf("component: %v", err)
	}

	first, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "critical", Message: "node down", DedupKey: "node-down",
	})
	if err != nil {
		t.Fatalf("raise 1: %v", err)
	}
	again, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "critical", Message: "node down (still)", DedupKey: "node-down",
	})
	if err != nil {
		t.Fatalf("raise 2: %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("re-raise minted a duplicate open alarm: %s then %s", first.ID, again.ID)
	}

	other, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "warning", Message: "fan degraded", DedupKey: "fan.degraded",
	})
	if err != nil {
		t.Fatalf("raise other condition: %v", err)
	}
	if other.ID == first.ID {
		t.Error("a different condition deduped onto the first alarm; the key must be per condition, not per component")
	}

	if err := gw.ClearAlarm(ctx, "", "codec-1", first.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	fresh, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "critical", Message: "node down again", DedupKey: "node-down",
	})
	if err != nil {
		t.Fatalf("raise after clear: %v", err)
	}
	if fresh.ID == first.ID {
		t.Error("a cleared condition must open a NEW incident row (one incident, new row per open)")
	}
}
