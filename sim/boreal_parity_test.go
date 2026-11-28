//go:build linux

package sim

import (
	"strings"
	"testing"
)

// Wire parity of the Boreal families with the YAML profiles they replaced
// (#2832). The golden transcripts were recorded from the profiles; each test
// here replays the same script against the Go family and holds its bytes,
// statuses, headers, state, SNMP types and values, drift sequences and room-day
// behavior to them. This file is the worked example of how a family proves it
// reads identically to the profile it replaces.

// The two places the YAML dashboard named "Edge 86" whatever the size: the
// title and the bar. A product reports its own model in both now (#2832).
const (
	yamlTitle = ", Boreal Edge 86</title>"
	yamlBar   = `<div class="model">Edge 86</div>`
)

// asOwnModel is the golden dashboard with the model the product reports.
func asOwnModel(t *testing.T, product, golden string) string {
	t.Helper()
	if product == "boreal-edge-86" || !strings.Contains(golden, "<!DOCTYPE html>") {
		return golden
	}
	if !strings.Contains(golden, yamlTitle) || !strings.Contains(golden, yamlBar) {
		t.Fatalf("%s: the golden dashboard no longer names Edge 86 in its title and bar", product)
	}
	model := "Edge " + strings.TrimPrefix(product, "boreal-edge-")
	golden = strings.Replace(golden, yamlTitle, ", Boreal "+model+"</title>", 1)
	return strings.Replace(golden, yamlBar, `<div class="model">`+model+`</div>`, 1)
}

func TestBorealEdgeFamilyReadsAsTheProfileDidAtEverySize(t *testing.T) {
	for _, p := range borealEdgeProducts {
		t.Run(p, func(t *testing.T) { replayWire(t, borealEdgeWire, p) })
	}
}

func TestBorealBeamFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, borealBeamWire, borealBeamProduct)
}

func TestBorealCastFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, borealCastWire, borealCastProduct)
}
