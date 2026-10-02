package node

import (
	"testing"
	"time"
)

// pruneCommandMemory drops outcomes past the redelivery window so the map does
// not grow for the life of a long-running node, while keeping entries young
// enough to still be redelivered (idempotence must survive a redelivery).
func TestPruneCommandMemory(t *testing.T) {
	now := time.Now()
	executed := map[int64]commandOutcome{
		1: {outcome: "", at: now.Add(-commandMemoryTTL - time.Minute)}, // stale
		2: {outcome: "boom", at: now.Add(-time.Minute)},                // fresh
		3: {outcome: "", at: now.Add(-commandMemoryTTL + time.Minute)}, // just inside the window
	}
	pruneCommandMemory(executed, now)
	if _, ok := executed[1]; ok {
		t.Fatalf("stale entry 1 survived the prune")
	}
	if _, ok := executed[2]; !ok {
		t.Fatalf("fresh entry 2 was pruned")
	}
	if _, ok := executed[3]; !ok {
		t.Fatalf("in-window entry 3 was pruned (still redeliverable)")
	}
}
