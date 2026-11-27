//go:build linux

package sim

import (
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// TestRecordBorealGolden writes the golden transcripts from the YAML
// profiles (#2832). It runs only when OMNIGLASS_RECORD_GOLDEN is set, and only
// against a tree that still holds the Boreal YAML profiles; it is deleted with
// them.
func TestRecordBorealGolden(t *testing.T) {
	if os.Getenv("OMNIGLASS_RECORD_GOLDEN") == "" {
		t.Skip("set OMNIGLASS_RECORD_GOLDEN to record the golden transcripts")
	}
	yamlEngine := func() *Engine {
		store, err := profile.LoadStoreFS(profiles.FS)
		if err != nil {
			t.Fatal(err)
		}
		return NewEngine(store, profiles.Templates())
	}
	record := func(product string, script, writes []wireStep, oids []string, ticks []tickPlan, walks []string) {
		t.Run(product, func(t *testing.T) {
			e := yamlEngine()
			d := wireDevice(t, e, product)
			tr := wireTranscript{Product: product, Source: wireSource, State: stateJSON(d)}
			tr.SNMP = map[string][]wireOID{}
			tr.Walks = map[string][]wireOID{}
			if oids != nil {
				tr.SNMP["rest"] = wireSNMPGet(t, d, oids)
				for _, w := range walks {
					tr.Walks[w+"@rest"] = wireSNMPWalk(t, d, w)
				}
				runWireHTTP(t, d, writes)
				tr.SNMP["written"] = wireSNMPGet(t, d, oids)
				for _, w := range walks {
					tr.Walks[w+"@written"] = wireSNMPWalk(t, d, w)
				}
				// back to rest for the HTTP script
				e2 := yamlEngine()
				d = wireDevice(t, e2, product)
			}
			tr.HTTP = runWireHTTP(t, d, script)
			e3 := yamlEngine()
			td := wireDevice(t, e3, product)
			tr.Ticks = recordTicks(td, ticks)
			tr.RoomDays = roomDayFor(t, yamlEngine, product)
			writeGolden(t, tr)
		})
	}
	walks := []string{"1.3.6.1.4.1.55520", "1.3.6.1.2.1.1"}
	for _, p := range borealEdgeProducts {
		record(p, edgeScript(), edgeWrites(), edgeOIDs, edgeTicks(), walks)
	}
	record(borealBeamProduct, beamScript(), beamWrites(), beamOIDs, beamTicks(), walks)
	record(borealCastProduct, castScript(), nil, nil, castTicks(), nil)
}
