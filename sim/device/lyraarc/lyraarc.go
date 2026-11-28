// Package lyraarc is the Lyra Arc A2 ceiling array microphone (#2835), a Go
// device family: powered and connected over one network cable, feeding its DSP
// over the network. An operator watches it for mute and for a level that says it
// hears the room it is in. It answers an HTTP JSON status and mute behind the
// Lyra header key and an SNMP agent on the Lyra enterprise arc, and follows its
// room's meeting off the lab calendar: active, with its level up and mute moving
// now and then, while the room is in use; idle and at its noise floor between
// meetings.
//
// The wireless HX-1 speaks the same API with a battery beside it but is another kind of device with
// its own collector, so it is its own family (lyrahx); the body they share is
// sim/device/internal/lyra. The family replaced the lyra-arc-a2 YAML profile; its wire matched the
// profile's before the profile was deleted (#2826); the driver contract test now holds it.
package lyraarc

import (
	"github.com/hyperscaleav/omniglass/sim/device/internal/lyra"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Name is the family's id, the profile name its devices carry.
const Name = "lyra-arc"

// Rows is the model table: the Arc line, one model.
var Rows = []lyra.Row{
	{Product: "lyra-arc-a2", Model: "Arc A2", Firmware: "2.6.0", DeviceType: "ceiling-mic", Kind: "ceiling array microphone"},
}

// Spec is the family.
func Spec() spec.Spec { return lyra.Line{Name: Name, Rows: Rows}.Spec() }
