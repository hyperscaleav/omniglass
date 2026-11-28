// Package meridianextender is the Meridian XL-1 AV extender (#2833), a Go
// device family of one model: the receiver of a transmitter and receiver pair
// that carries one video input over a cable run to a display across the room.
// The receiver is what is on the network, reporting the link to its transmitter
// and whether the transmitter hands it a signal: the two answers an operator
// wants of a dark display, whether it is the source or the cable.
//
// Its monitoring face is SNMP on the Meridian enterprise arc the switcher shares
// (the extender's branch at .2); its control face is a small HTTP JSON API
// behind the Meridian header key: a status and a reboot. An extender has no
// route, and its API does not take one, which is why it is a family of its own
// beside meridianswitcher.
//
// The signal follows the room's meetings off the lab calendar: present while a
// meeting is held, the switcher routing a source onto the cable, and absent
// between meetings and through a no-show. The link is the cable's and stays up;
// a scenario that pulls it is not undone, and a reboot retrains it. The family
// replaced the meridian-xl-1 YAML profile; wire parity with it is held by golden
// transcripts (sim/testdata/wire).
package meridianextender

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/surface/snmp"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry.
const Name = "meridian-extender"

// Row is one model of the extender line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	// Firmware is the version it ships with.
	Firmware string
}

// Summary describes the product where the console lists it.
func (r Row) Summary() string { return fmt.Sprintf("Meridian %s, AV extender", r.Model) }

// Rows is the model table: the XL line, one model.
var Rows = []Row{
	{Product: "meridian-xl-1", Model: "XL-1", Firmware: "3.2.0"},
}

// The state paths of the extender.
const (
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
	pathLink      = "status.extender.link"
	pathSignal    = "status.extender.signal"
	pathModel     = "configuration.device.model"
	pathSerial    = "configuration.device.serial"
	pathFirmware  = "configuration.firmware.version"
)

// The enterprise number of Meridian's (fictional) arc, shared with the
// switcher: the switcher's branch is .1 (not served here), the extender's .2,
// identity .5.
const enterprise = 55550

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device":   map[string]any{"boot_epoch": 0},
			"extender": map[string]any{"link": "up", "signal": behave.DefaultExtender.Present},
			"network":  map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Meridian"},
			"firmware": map[string]any{"version": r.Firmware},
			"network":  map[string]any{"hostname": ""},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Meridian", Model: "XL", DeviceType: "av-extender",
			Summary: "Meridian XL AV extender",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{
				Product: r.Product, Model: r.Model, Summary: r.Summary(),
				State: map[string]any{"configuration": map[string]any{"device": map[string]any{"model": r.Model}, "firmware": map[string]any{"version": r.Firmware}}},
			}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, Doc: wire.Doc{Summary: "Meridian JSON control API"},
				Auth: wire.Auth{Type: "header", Header: "X-Meridian-Key", APIKey: "meridian-lab-key", Enforce: true},
			},
			{
				Name: "snmp", Protocol: "snmp", Port: 161, Doc: wire.Doc{Summary: "SNMP monitoring"},
				Auth: wire.Auth{Community: "public", RefusedCommunity: "revoked"},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability"}},
		},
		State:   stateFor(Rows[0]),
		HTTP:    routes(),
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{roomMeeting()},
		Surface: surfaceOf,
	}
}

// roomMeeting carries a signal while the room's meeting is held
// (behave.ExtenderSignal). An extender whose room has no calendar keeps its
// signal.
func roomMeeting() spec.Tick {
	return spec.Tick{
		Name: "room_meeting", Interval: bind.Seconds(30), OnStart: true, Writes: []string{pathSignal},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			return []surface.Change{{Path: pathSignal, Value: behave.ExtenderSignal(spec.String(d, pathSignal), c.Room, behave.DefaultExtender)}}
		},
	}
}

// statusDoc is GET /api/status after the identity block: the link and the
// signal.
type statusDoc struct {
	Link   string `json:"link"`
	Signal string `json:"signal"`
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.Identity{Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware), Uptime: spec.Int64(d, pathUptime)}
	doc := statusDoc{Link: spec.String(d, pathLink), Signal: spec.String(d, pathSignal)}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Status(id, httpapi.ModelFirst, httpapi.Inline("extender", doc))}
}

type actionAck struct {
	OK     bool   `json:"ok"`
	Action string `json:"action"`
}

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /api/status", Summary: "Extender identity, link and signal", Handle: statusRoute},
		// A reboot retrains the link: the cable is renegotiated and the link comes
		// back up, which a pulled cable (a scenario sets link to down) would not.
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot extender (clears uptime, retrains the link)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, pathLink)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
	}
}

// surfaceOf is the SNMP agent: the MIB-II system group, then the Meridian arc.
// The extender is its branch .2: the link state at .2.1.0 and the signal on its
// input at .2.2.0; the switcher's branch .1 is not served here, and identity is
// at .5.
func surfaceOf(d spec.Device) spec.Surface {
	oids := snmp.System{
		Descr: func() string { return spec.String(d, pathModel) }, UptimeSeconds: func() int64 { return spec.Int64(d, pathUptime) },
		DescrPath: pathModel, UptimePath: pathUptime,
	}.OIDs()
	arc := func(suffix string) string { return fmt.Sprintf("1.3.6.1.4.1.%d.%s", enterprise, suffix) }
	text := func(suffix, path string) surface.OID {
		return surface.Text(arc(suffix), func() string { return spec.String(d, path) }).At(path)
	}
	oids = append(oids, text("2.1.0", pathLink), text("2.2.0", pathSignal))
	oids = append(oids, snmp.Identity{
		Enterprise: enterprise,
		Firmware:   func() string { return spec.String(d, pathFirmware) }, FirmwarePath: pathFirmware,
		Serial: func() string { return spec.String(d, pathSerial) }, SerialPath: pathSerial,
	}.OIDs()...)
	return spec.Surface{OIDs: oids}
}
