//go:build linux

package sim

import "testing"

// Wire parity of the room control and switching families with the YAML profiles
// they replaced (#2833). The golden transcripts were recorded from the
// profiles; each test replays the same script against the Go family and holds
// its bytes, statuses, headers, state, line replies and pushes, SNMP types and
// values, drift sequences and room-day behavior to them. The one divergence the
// families make, the refusals that were a template engine's own words, is
// recorded as a Quirk in the script and in ADR-2833.

func TestNewtronCoreFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, newtronCoreWire, newtronCoreProduct)
}

func TestNewtronPanelFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, newtronPanelWire, newtronPanelProduct)
}

func TestNewtronSlateFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, newtronSlateWire, newtronSlateProduct)
}

func TestMeridianSwitcherFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, meridianSwitcherWire, meridianSwitcherProduct)
}

func TestMeridianExtenderFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, meridianExtenderWire, meridianExtenderProduct)
}
