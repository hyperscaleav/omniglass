package api

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Both alarm wires carry a rule alarm's instance (the endpoint a reachability
// alarm names), so a list showing two alarms of one rule on one component can
// tell them apart; a hand-raised alarm carries none.
func TestAlarmBodiesCarryInstance(t *testing.T) {
	alarms := []storage.Alarm{
		{ID: "a1", Message: "Reachability lost", DedupKey: "rule:01a2133c-0270-7888-aad8-4dd37246c331:http"},
		{ID: "a2", Message: "Bezel cracked", DedupKey: "Bezel cracked"},
	}
	health := toHealthAlarmBodies(alarms)
	if health[0].Instance != "http" || health[1].Instance != "" {
		t.Errorf("health alarm instances = %q, %q; want http and none", health[0].Instance, health[1].Instance)
	}
	if got := toAlarmBody(&alarms[0]).Instance; got != "http" {
		t.Errorf("alarm instance = %q, want http", got)
	}
	if got := toAlarmBody(&alarms[1]).Instance; got != "" {
		t.Errorf("hand-raised alarm instance = %q, want none", got)
	}
}
