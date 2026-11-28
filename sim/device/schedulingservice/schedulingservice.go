// Package schedulingservice is the scheduling service (#2836), a Go device family
// of one: the centralized cloud service that owns room bookings and panel config,
// the calendaring backend a room-booking platform is. It is a standalone HTTP
// service behind a token, with ICMP for the network band and no SNMP, command line
// or raw TCP.
//
// Course datapoints (HTTP, token auth, one way in):
//
//	status.bookings.today        booked meetings today across the rooms it serves
//	status.bookings.remaining    of those, the ones not over (the one running included)
//	status.bookings.in_progress  rooms a booked meeting holds now
//	configuration.rooms_served   rooms the platform speaks for
//	configuration.sync_ok        the last config sync succeeded
//
// The counts are the lab calendar's booked entries (a walk-in is no booking, a
// no-show is one) for the rooms the world says the service speaks for or, placed in
// a room, that room, recomputed each tick on the device's clock. A service the world
// covers no room for has no calendar and keeps today's count the fixed 8.
//
// GET /api/rooms lists every room it serves as a paged record (ADR-1282): its id is
// the system the room belongs to, with that room's booking state, booked meetings
// today and left, the organizer holding it now and the next start. A driver polls it
// and each record's values land on the system its room id is bound to; the
// unassigned-room record is bound to nothing and lands in the conduit's unbound
// inbox.
//
// The family replaced the scheduling-service YAML profile; wire parity with it is
// held by golden transcripts (sim/testdata/wire).
package schedulingservice

import (
	"encoding/json"
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/httpapi"
)

// Name is the family's id, the profile name its devices carry: the product it is
// a family of one for.
const Name = "scheduling-service"

// Row is one model of the service.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
}

// Rows is the model table: the scheduling platform, one model.
var Rows = []Row{{Product: Name, Model: "Scheduling Platform", Firmware: "cloud-2026.06"}}

// The state paths of the service.
const (
	pathBootEpoch = "status.device.boot_epoch"
	pathUptime    = "status.device.uptime"
	pathToday     = "status.bookings.today"
	pathRemaining = "status.bookings.remaining"
	pathProgress  = "status.bookings.in_progress"
	pathRooms     = "status.rooms"
	pathServed    = "configuration.rooms_served"
	pathSyncOK    = "configuration.sync_ok"
	pathModel     = "configuration.device.model"
	pathFirmware  = "configuration.firmware.version"
)

// unassigned is the room the platform knows and nobody bound: its values land in
// the conduit's unbound inbox.
const unassigned = "unassigned-room"

// fixedToday is the day's meetings of a service with no room to serve.
const fixedToday = 8

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device":   map[string]any{"boot_epoch": 0},
			"bookings": map[string]any{"today": fixedToday, "remaining": 0, "in_progress": 0},
			"rooms":    map[string]any{},
		},
		"configuration": map[string]any{
			"rooms_served": 0,
			"sync_ok":      true,
			"device":       map[string]any{"serial": "", "model": r.Model, "manufacturer": "Shinra Cloud"},
			"firmware":     map[string]any{"version": r.Firmware},
			"network":      map[string]any{"hostname": ""},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Shinra Cloud", Model: "Scheduling Platform", DeviceType: "cloud-service",
			Summary: "Cloud room-booking / scheduling platform (bookings + config)",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model}
		}),
		Interfaces: []profile.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 443, TLS: true, APISurface: "scheduling-service",
				Doc:  profile.Doc{Summary: "Scheduling admin HTTP API + dashboard", Tags: []string{"management", "api", "cloud"}},
				Auth: profile.Auth{Type: "header", Header: "X-Sched-Token", APIKey: "sched-lab-token", Enforce: true, PublicPaths: []string{"/"}},
			},
			{Name: "icmp", Protocol: "icmp", Doc: profile.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: stateFor(Rows[0]),
		HTTP: []spec.HTTPRoute{
			{Match: "GET /", Summary: "Scheduling service dashboard", Handle: dashboardRoute},
			{Match: "GET /api/status", Summary: "Service status (today's bookings, sync health)", Handle: statusRoute},
			{Match: "GET /api/rooms", Summary: "Every room the platform serves with its bookings, one record each", Handle: roomsRoute},
			{Match: "GET /api/bookings", Summary: "Today's bookings + config sync flag", Handle: bookingsRoute},
		},
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks:   []spec.Tick{syncTick()},
	}
}

// statusDoc is GET /api/status.
type statusDoc struct {
	Service struct {
		Model    string `json:"model"`
		Firmware string `json:"firmware"`
		Uptime   int64  `json:"uptime"`
	} `json:"service"`
	Bookings struct {
		Today      int64 `json:"today"`
		Remaining  int64 `json:"remaining"`
		InProgress int64 `json:"in_progress"`
	} `json:"bookings"`
	Config struct {
		RoomsServed int64 `json:"rooms_served"`
		SyncOK      bool  `json:"sync_ok"`
	} `json:"config"`
}

func statusRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	var doc statusDoc
	doc.Service.Model, doc.Service.Firmware, doc.Service.Uptime = spec.String(d, pathModel), spec.String(d, pathFirmware), spec.Int64(d, pathUptime)
	doc.Bookings.Today, doc.Bookings.Remaining, doc.Bookings.InProgress = spec.Int64(d, pathToday), spec.Int64(d, pathRemaining), spec.Int64(d, pathProgress)
	doc.Config.RoomsServed, doc.Config.SyncOK = spec.Int64(d, pathServed), spec.Bool(d, pathSyncOK)
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.Document(doc)}
}

// bookingsRoute is GET /api/bookings: the day's counts and the sync flag, each
// under a blank indented line, as the template that first wrote it laid them out.
func bookingsRoute(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
	body := fmt.Sprintf("{\n  \n  \"today\": %d,\n  \n  \"remaining\": %d,\n  \n  \"in_progress\": %d,\n  \n  \"rooms_served\": %d,\n  \n  \"sync_ok\": %t\n}\n",
		spec.Int64(d, pathToday), spec.Int64(d, pathRemaining), spec.Int64(d, pathProgress), spec.Int64(d, pathServed), spec.Bool(d, pathSyncOK))
	return spec.HTTPResponse{ContentType: "application/json", Body: []byte(body)}
}

func quote(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// orText is a record's text field, or the default when it holds none.
func orText(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// roomsRoute lists the rooms the service serves as a paged list, one record each.
func roomsRoute(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
	rooms, _ := d.Read(pathRooms).(map[string]any)
	ids := make([]string, 0, len(rooms))
	for id := range rooms {
		ids = append(ids, id)
	}
	page, next := httpapi.Cut(ids, httpapi.Cloud.After(r.Query), httpapi.Cloud.Size(r.Query))
	records := make([]string, len(page))
	for i, id := range page {
		room, _ := rooms[id].(map[string]any)
		records[i] = fmt.Sprintf(`{"id": %s, "name": %s, "address": %s, "state": %s, "today": %d, "remaining": %d, "current": %s, "next_start": %d}`,
			quote(id), quote(orText(room["name"], id)), quote(orText(room["address"], "")), quote(orText(room["state"], behave.StateFree)),
			spec.Whole(room["today"]), spec.Whole(room["remaining"]), quote(orText(room["current"], "")), spec.Whole(room["next_start"]))
	}
	return spec.HTTPResponse{ContentType: "application/json", Body: httpapi.PagedDocument("rooms", records, 1, next)}
}

// syncTick is the service's day: the bookings each room it serves holds, summed
// into the counts and listed room by room, on the device's clock. A service with
// no room to serve keeps the day it had.
func syncTick() spec.Tick {
	return spec.Tick{
		Name: "bookings_sync", Interval: bind.Seconds(30), OnStart: true,
		Writes: []string{pathToday, pathRemaining, pathProgress, pathRooms, pathServed},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			world := spec.RoomsOf(d)
			served := world.Served()
			reads := make([]behave.Bookings, len(served))
			for i, r := range served {
				reads[i] = world.Bookings(r.Calendar())
			}
			sum := behave.SumBookings(int(spec.Int64(d, pathToday)), reads)
			out := []surface.Change{
				{Path: pathToday, Value: int64(sum.Today)},
				{Path: pathRemaining, Value: int64(sum.Remaining)},
				{Path: pathProgress, Value: int64(sum.InProgress)},
			}
			if len(served) > 0 {
				// The rooms it serves, one record each, named by the system the room
				// belongs to; unassigned-room is one the platform knows and nobody
				// bound.
				records := map[string]any{}
				for i, r := range served {
					s := reads[i].Summary()
					records[orText(r.System, r.Room)] = map[string]any{
						"name": orText(r.Label, r.Room), "address": r.Room, "state": s.State,
						"today": int64(s.Today), "remaining": int64(s.Remaining), "current": s.Current, "next_start": s.NextStart,
					}
				}
				records[unassigned] = map[string]any{"name": unassigned, "address": "", "state": behave.StateFree, "today": int64(0), "remaining": int64(0), "current": "", "next_start": int64(0)}
				out = append(out, surface.Change{Path: pathRooms, Value: records})
			}
			return append(out, surface.Change{Path: pathServed, Value: int64(len(served))})
		},
	}
}
