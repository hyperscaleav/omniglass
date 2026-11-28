//go:build linux

package sim

import (
	"regexp"
	"strings"
	"testing"
)

// Wire parity of the UCC platform family with the YAML profile it replaced
// (#2836). The golden transcript was recorded from the profile; the test replays
// the same script against the Go family and holds its bytes, statuses, headers,
// state, webhook posts (signed as the server checks them), drift sequences and room
// days to it.
//
// One divergence is pinned here, with its reason in ADR-2836: the dashboard names a
// room the platform holds no name for by its id and reads a room with no state as
// idle, as the room list always did, where the profile's template printed the markup
// <no value> into the page.

var (
	noName  = regexp.MustCompile(`<span class="k"><no value> \(([^)]*)\)</span>`)
	noState = `<span><no value></span>`
)

// asTheFamilyNamesARoom is the golden dashboard with the room names and states the
// family reads where the profile printed <no value>.
func asTheFamilyNamesARoom(_ *testing.T, _, golden string) string {
	if !strings.Contains(golden, "<!DOCTYPE html>") {
		return golden
	}
	golden = noName.ReplaceAllString(golden, `<span class="k">$1 ($1)</span>`)
	return strings.ReplaceAll(golden, noState, `<span>idle</span>`)
}

func TestUCCPlatformFamilyReadsAsTheProfileDid(t *testing.T) {
	fam := uccWire
	fam.Body = asTheFamilyNamesARoom
	replayWire(t, fam, uccProduct)
}
