package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestAlarmOneOpenPerIdentity pins #465 (ADR-0075): the one-open invariant is
// per alarm identity (component, dedup key), the key supplied by the raiser.
// Raising the same identity twice returns the existing open alarm
// instead of a duplicate; different identities coexist on one component (each
// impairs the component's own verdict wholesale, #626); clearing reopens the
// key for a fresh alarm.
func TestAlarmOneOpenPerIdentity(t *testing.T) {
	t.Parallel()
	gw, _ := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}

	first, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "high", Message: "outpost down", DedupKey: "outpost-down",
	})
	if err != nil {
		t.Fatalf("raise 1: %v", err)
	}
	again, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "high", Message: "outpost down (still)", DedupKey: "outpost-down",
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
		t.Fatalf("raise other identity: %v", err)
	}
	if other.ID == first.ID {
		t.Error("a different identity deduped onto the first alarm; the key must be per dedup key, not per component")
	}

	if err := gw.ClearAlarm(ctx, "", "codec-1", first.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	fresh, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "high", Message: "outpost down again", DedupKey: "outpost-down",
	})
	if err != nil {
		t.Fatalf("raise after clear: %v", err)
	}
	if fresh.ID == first.ID {
		t.Error("a cleared alarm's identity must open a NEW alarm row (one row per open)")
	}
}
