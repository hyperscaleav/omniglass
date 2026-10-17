package api

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestSimulationBodyConnectedWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	devs := []storage.SimDevice{{ComponentID: "b", Name: "zeta"}, {ComponentID: "a", Name: "alpha"}}
	fresh := simulationBody(devs, simhub.Status{LastSeen: now.Add(-30 * time.Second)}, true, now)
	if !fresh.Body.Connected {
		t.Error("a pull 30s ago reads disconnected")
	}
	stale := simulationBody(devs, simhub.Status{LastSeen: now.Add(-5 * time.Minute)}, true, now)
	if stale.Body.Connected || stale.Body.LastSeen == nil {
		t.Error("a pull 5m ago reads connected, or lost its last-seen time")
	}
	if got := fresh.Body.Devices; got[0].Name != "alpha" || got[0].Hits != 0 || got[0].LastHitAt != nil {
		t.Errorf("devices = %+v, want sorted by name with no hits", got)
	}
}
