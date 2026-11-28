// Package genericdevice is the generic device (#2836), a Go device family of
// one: the unclassified SNMP box that every component nobody has classified
// resolves to. It answers the two objects every SNMP agent does, MIB-II sysDescr
// and sysUpTime, and ping, so the generic SNMP driver stays exercised and a device
// nobody has written a driver for honestly reports a model number and an uptime
// and nothing more. There is no vendor arc and nothing to switch.
//
// The family replaced the generic-device YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package genericdevice

import (
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry: the product it
// is a family of one for.
const Name = "generic-device"

// Row is the one model of the family.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
}

// Rows is the model table: the generic device is its own only row.
var Rows = []Row{{Product: Name, Model: "Generic SNMP Device"}}

// The state paths the agent reads.
const (
	pathModel     = "configuration.device.model"
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
)

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Generic", Model: "Generic SNMP Device", DeviceType: "generic-device",
			Summary: "Generic unclassified SNMP device",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "snmp", Protocol: "snmp", Port: 161, Doc: wire.Doc{Summary: "SNMP monitoring", Tags: []string{"monitoring"}},
				// A community the agent knows and refuses answers
				// authorizationError, where any other is dropped silently.
				Auth: wire.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: map[string]any{
			"status": map[string]any{
				"device":  map[string]any{"boot_epoch": 0},
				"network": map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
			},
			"configuration": map[string]any{
				"device":  map[string]any{"serial": "", "model": "Generic SNMP Device", "manufacturer": "Generic"},
				"network": map[string]any{"hostname": ""},
			},
		},
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Surface: func(d spec.Device) spec.Surface {
			return spec.Surface{OIDs: snmp.System{
				Descr:         func() string { return spec.String(d, pathModel) },
				UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
				DescrPath:     pathModel, UptimePath: pathUptime,
			}.OIDs()}
		},
	}
}
