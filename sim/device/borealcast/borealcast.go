// Package borealcast is the Boreal Cast digital signage media player (#2832),
// a Go device family of one model. The player holds a playlist on local storage
// and plays it to the display it drives through the building's day; an operator
// watches whether it is playing, what it is playing, and whether its storage
// is filling up. Its one face is an HTTP JSON API behind an api key header
// (X-Boreal-Key), the way signage players are integrated with a content
// management system: no SNMP, no command line.
//
// The family replaced the boreal-cast-m1 YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package borealcast

import (
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry.
const Name = "boreal-cast"

// Row is one model of the player line.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
	// StorageGB is the player's local storage, UsedPercent how full it starts.
	StorageGB   int
	UsedPercent float64
}

// Rows is the model table: the Cast line, one model.
var Rows = []Row{
	{Product: "boreal-cast-m1", Model: "Cast M1", Firmware: "4.2.0", StorageGB: 64, UsedPercent: 41.5},
}

// The state paths of the player.
const (
	pathUptime    = "status.device.uptime"
	pathBootEpoch = "status.device.boot_epoch"
	pathState     = "status.playback.state"
	pathIndex     = "status.playback.index"
	pathContent   = "status.playback.content"
	pathOpen      = "status.sim.open"
	pathUsed      = "status.storage.used_percent"
	pathTotal     = "status.storage.total_gb"
	pathModel     = "configuration.device.model"
	pathSerial    = "configuration.device.serial"
	pathFirmware  = "configuration.firmware.version"
)

// castKey is the api key the player's API demands in X-Boreal-Key.
const castKey = "boreal-cast-key"

var states = []string{"playing", "paused", "stopped"}

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device":   map[string]any{"boot_epoch": 0},
			"playback": map[string]any{"state": "playing", "index": 0, "content": behave.DefaultSign.Playlist[0]},
			"sim":      map[string]any{"open": true},
			"storage":  map[string]any{"used_percent": r.UsedPercent, "total_gb": r.StorageGB},
			"network":  map[string]any{"ipv4": map[string]any{"address": "", "mac": ""}},
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Boreal"},
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
			Manufacturer: "Boreal", Model: "Cast", DeviceType: "media-player",
			Summary: "Boreal Cast digital signage media player",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model, Summary: fmt.Sprintf("Boreal %s digital signage media player", r.Model)}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 80, Doc: wire.Doc{Summary: "Cast JSON API"},
				Auth: wire.Auth{Type: "header", Header: "X-Boreal-Key", APIKey: castKey, Enforce: true},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability"}},
		},
		State:   stateFor(Rows[0]),
		HTTP:    routes(),
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks: []spec.Tick{
			buildingDay(),
			playlistAdvance(),
			// Storage fills slowly as content syncs down, between bounds.
			bind.Drift("storage_drift", bind.Seconds(300), pathUsed, behave.Band{Lo: 20, Hi: 90, Step: 0.5}, behave.Places(1)),
		},
	}
}

// signOf is the player's playback as the primitives read it.
func signOf(d spec.Device) behave.SignState {
	return behave.SignState{Open: spec.Bool(d, pathOpen), Playback: spec.String(d, pathState), Index: spec.Int(d, pathIndex), Content: spec.String(d, pathContent)}
}

// buildingDay is the sign following the building's day, not its meetings: it
// plays while the hours its building is open say so and stops outside them;
// the change at opening and closing is the edge, so an operator's pause holds
// until the next one; a player whose room has no hours to read plays on.
// Opening starts the playlist again from its first item.
func buildingDay() spec.Tick {
	return spec.Tick{
		Name: "building_day", Interval: bind.Seconds(60), OnStart: true,
		Writes: []string{pathState, pathIndex, pathContent, pathOpen},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			next := behave.BuildingDay(signOf(d), c.Hours, behave.DefaultSign)
			return []surface.Change{
				{Path: pathState, Value: next.Playback},
				{Path: pathIndex, Value: int64(next.Index)},
				{Path: pathContent, Value: next.Content},
				{Path: pathOpen, Value: next.Open},
			}
		},
	}
}

// playlistAdvance steps to the next item of the playlist while playing,
// wrapping at the end; paused or stopped, it holds.
func playlistAdvance() spec.Tick {
	return spec.Tick{
		Name: "playlist_advance", Interval: 90 * time.Second,
		Writes: []string{pathIndex, pathContent},
		Run: func(d spec.Device, _ spec.TickContext) []surface.Change {
			next := behave.PlaylistAdvance(signOf(d), behave.DefaultSign)
			return []surface.Change{{Path: pathIndex, Value: int64(next.Index)}, {Path: pathContent, Value: next.Content}}
		},
	}
}

// statusDoc is GET /api/status: identity, uptime and storage.
type statusDoc struct {
	UsedPercent float64 `json:"used_percent"`
	TotalGB     int64   `json:"total_gb"`
}

// playbackDoc is the playback route's document: the state and the item playing.
type playbackDoc struct {
	State   string `json:"state"`
	Index   int64  `json:"index"`
	Content string `json:"content"`
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	id := httpapi.Identity{
		Model: spec.String(d, pathModel), Serial: spec.String(d, pathSerial), Firmware: spec.String(d, pathFirmware), Uptime: spec.Int64(d, pathUptime),
	}
	body := httpapi.Status(id, httpapi.ModelFirst, httpapi.Block("storage", statusDoc{spec.Float(d, pathUsed), spec.Int64(d, pathTotal)}))
	return spec.HTTPResponse{ContentType: "application/json", Body: body}
}

func playback(d spec.Device) playbackDoc {
	return playbackDoc{State: spec.String(d, pathState), Index: spec.Int64(d, pathIndex), Content: spec.String(d, pathContent)}
}

func playbackRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(playback(d))}
}

type actionAck struct {
	OK     bool   `json:"ok"`
	Action string `json:"action"`
}

func routes() []spec.HTTPRoute {
	return []spec.HTTPRoute{
		{Match: "GET /api/status", Summary: "Player identity, uptime and storage", Handle: statusRoute},
		{Match: "GET /api/playback", Summary: "Playback state and the item playing", Handle: playbackRoute},
		// Setting the state answers the playback document, as the GET does.
		httpapi.Control{
			Match: "POST /api/playback", Summary: "Play, pause or stop the playlist", Example: "{\"state\": \"paused\"}\n",
			Fields:   []httpapi.Field{{Path: pathState, Name: "state", Kind: httpapi.Choice, Values: states}},
			Ack:      func(d spec.Device) any { return playback(d) },
			Indented: true,
		}.Route(),
		httpapi.Action{
			Match: "POST /api/admin/reboot", Summary: "Reboot player (clears uptime, restarts the playlist)", Example: "{}",
			Writes: func(d spec.Device) []surface.Change {
				return append([]surface.Change{{Path: pathBootEpoch, Value: d.Now().Unix()}}, httpapi.Reset(d, pathState, pathIndex, pathContent)...)
			},
			Ack: actionAck{true, "reboot"},
		}.Route(),
	}
}
