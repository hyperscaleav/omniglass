// Package lyrahx is the Lyra HX-1 wireless microphone system (#2835), a Go
// device family: a wireless handheld microphone and its networked receiver,
// which is what is on the network, reporting the transmitter it hears. Beside
// mute and level, an operator watches its battery: a wireless mic that dies mid
// lecture is the call the help desk gets. It answers an HTTP JSON status and
// mute behind the Lyra header key and an SNMP agent on the Lyra enterprise arc
// (mute, the input level and the pack's battery charge).
//
// It follows its room's meeting off the lab calendar: the pack transmits while
// the room is in use, and its battery is a pure function of the calendar,
// draining in use and recharging between meetings, so the charge reads the same
// at any instant on any run.
//
// The ceiling Arc A2 speaks the same API with no pack but is another kind of
// device with its own collector, so it is its own family (lyraarc); the body they
// share is sim/device/internal/lyra. The family replaced the lyra-hx-1 YAML
// profile; wire parity with it is held by golden transcripts (sim/testdata/wire).
package lyrahx

import (
	"github.com/hyperscaleav/omniglass/sim/device/internal/lyra"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Name is the family's id, the profile name its devices carry.
const Name = "lyra-hx"

// Rows is the model table: the HX line, one model.
var Rows = []lyra.Row{
	{Product: "lyra-hx-1", Model: "HX-1", Firmware: "1.4.2", DeviceType: "wireless-mic", Kind: "wireless microphone system", Wireless: true},
}

// Spec is the family.
func Spec() spec.Spec { return lyra.Line{Name: Name, Rows: Rows}.Spec() }
