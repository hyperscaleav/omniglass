// Package uccplatform is the UCC platform (#2836), a Go device family of one: the
// meeting platform's account-wide cloud service (#1282). It is one service that
// speaks for every room on the account, the shape of a real meeting platform's admin
// plane, where one API key reads every room and one webhook subscription hears about
// all of them. It is the proving sim for the fan-out (ADR-1282): a driver polling GET
// /api/rooms reads the room list as records, each naming its room by id, and the
// webhook posts name the room in their body, so each value lands on the system its
// room id is bound to and an unbound room lands in the conduit's inbox.
//
// It is designed to the shape of a real cloud meeting platform's Rooms and Devices
// admin APIs, under our own names (#2790): two token-paged lists, one record per room
// and one per device, and a webhook for what happens to either.
//
//	GET /api/rooms    a room's id, name, status (available, in-meeting, offline), the
//	                  meeting it holds (id, topic, start, participants), the booking
//	                  that holds it now and the next booked one
//	GET /api/devices  a device's id, name, the room it sits in, its type (codec, bar,
//	                  controller, scheduling-panel, camera, microphone), status
//	                  (online, offline), firmware and last-seen time
//
// Course datapoints (HTTP, token auth):
//
//	status.rooms.<id>.state         idle or in-call
//	status.rooms.<id>.meeting       the meeting in progress, empty when idle
//	status.rooms.<id>.participants  people in the meeting
//
// The account is one component for the whole fleet and speaks for every room the
// world covers, and lists every device of those rooms the world says a meeting
// platform manages. The ids are the world's (a room is its system's name, a device
// has the stable id the world gives it); the lab seed binds each id to its system or
// component as an installer would, and an id nobody bound lands in the conduit's
// unbound inbox. The rooms the world places read off the lab calendar: a room is in a
// call exactly when its meeting has somebody in it, a booked meeting nobody came to
// leaves the room available but still booked, and the platform posts
// room.meeting.started and room.meeting.ended at each boundary. A room or a device the
// world does not name (the dev fixture's, an id a demo invents) keeps the state its
// routes gave it.
//
// The family replaced the ucc-platform YAML profile; wire parity with it is held by
// golden transcripts (sim/testdata/wire).
package uccplatform

import (
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Name is the family's id, the profile name its devices carry: the product it is
// a family of one for.
const Name = "ucc-platform"

// Row is one model of the service.
type Row struct {
	// Product is the catalog product the row simulates, Model how it names
	// itself.
	Product, Model string
	Firmware       string
}

// Rows is the model table: the account-wide platform, one model.
var Rows = []Row{{Product: Name, Model: "UCC Platform (account)", Firmware: "cloud-2026.10"}}

// The state paths of the platform.
const (
	pathBootEpoch = "status.device.boot_epoch"
	pathUptime    = "status.device.uptime"
	pathRooms     = "status.rooms"
	pathDevices   = "status.devices"
	pathTold      = "status.sim.told"
	pathAnnounce  = "status.sim.announce"
	pathTarget    = "configuration.webhook.target_url"
	pathScheme    = "configuration.webhook.scheme"
	pathSecret    = "configuration.webhook.secret"
	pathFault     = "configuration.webhook.fault"
	pathModel     = "configuration.device.model"
)

// roomPath and devicePath are a field of one room or device the platform holds.
func roomPath(id, field string) string   { return pathRooms + "." + id + "." + field }
func devicePath(id, field string) string { return pathDevices + "." + id + "." + field }

// The rooms on the account. The dev seed binds the first four ids to the lab's
// conference systems; zr-boardroom-b is a room the platform knows and nobody bound,
// so its values land in the conduit's unbound inbox until an operator binds it.
func seedRooms() map[string]any {
	room := func(name, state, meeting string, participants int) map[string]any {
		return map[string]any{"name": name, "state": state, "meeting": meeting, "participants": participants}
	}
	return map[string]any{
		"zr-boardroom-a":   room("Boardroom A", "idle", "", 0),
		"zr-huddle-north":  room("Huddle North", "in-call", "m-1001", 3),
		"zr-briefing-av":   room("Briefing", "idle", "", 0),
		"zr-auditorium-av": room("Auditorium", "idle", "", 0),
		"zr-boardroom-b":   room("Boardroom B", "in-call", "m-1002", 2),
	}
}

// Devices the platform lists that no room of the world places: zd-spare-bar is one
// nobody bound, so its values land in the conduit's unbound inbox until an operator
// binds it.
func seedDevices() map[string]any {
	return map[string]any{
		"zd-spare-bar":   map[string]any{"name": "Spare video bar", "room_id": "zr-boardroom-b", "type": "bar", "status": "online", "firmware": "2.6.1", "last_seen": 0},
		"zd-annex-codec": map[string]any{"name": "Annex codec", "room_id": "zr-annex-2", "type": "codec", "status": "offline", "firmware": "2.6.1", "last_seen": 0},
	}
}

func stateFor(r Row) map[string]any {
	return map[string]any{
		"status": map[string]any{
			"device": map[string]any{"boot_epoch": 0},
			// What the calendar tick has told the server: the meeting each placed
			// room was last told of ("-" for none), and this tick's post.
			"sim":     map[string]any{"told": map[string]any{}, "announce": map[string]any{"kind": "", "room": "", "meeting": ""}},
			"rooms":   seedRooms(),
			"devices": seedDevices(),
		},
		"configuration": map[string]any{
			"device":   map[string]any{"serial": "", "model": r.Model, "manufacturer": "Shinra Cloud"},
			"firmware": map[string]any{"version": r.Firmware},
			"network":  map[string]any{"hostname": ""},
			"webhook":  map[string]any{"target_url": "", "scheme": "hmac", "secret": "", "fault": ""},
		},
	}
}

// Spec is the family.
func Spec() spec.Spec {
	return spec.Spec{
		Name: Name,
		Identity: spec.Identity{
			Manufacturer: "Shinra Cloud", Model: "UCC Platform (account)", DeviceType: "cloud-service",
			Summary: "Account-wide meeting platform: every room's and device's state over one API and one webhook",
		},
		Models: spec.Table(Rows, func(r Row) spec.Model {
			return spec.Model{Product: r.Product, Model: r.Model}
		}),
		Interfaces: []wire.Interface{
			{
				Name: "http-api", Protocol: "http", Port: 443, TLS: true, APISurface: "ucc-platform",
				Doc:  wire.Doc{Summary: "UCC platform admin HTTP API + dashboard", Tags: []string{"management", "api", "cloud"}},
				Auth: wire.Auth{Type: "header", Header: "X-UCC-Token", APIKey: "ucc-lab-token", Enforce: true, PublicPaths: []string{"/"}},
			},
			{Name: "icmp", Protocol: "icmp", Doc: wire.Doc{Summary: "ICMP ping reachability", Tags: []string{"monitoring"}}},
		},
		State: stateFor(Rows[0]),
		HTTP:  routes(),
		// Uptime is read-derived: now minus the boot anchor, never a timer.
		Elapsed: []spec.Elapsed{{Path: pathUptime, Since: pathBootEpoch}},
		Ticks: []spec.Tick{
			calendarTick(),
			// The account's periodic digest (#1747): every thirty seconds it posts the
			// batched rooms.state report the report route posts, so a scale lab's
			// account is heard on its own. Before the account is subscribed its target
			// is empty and nothing is sent.
			{Name: "rooms_report", Interval: bind.Seconds(30), Run: reportTick},
		},
		// How the simulator subscribes this service to its webhook endpoint (#1370,
		// ADR-1370): the subscribe route, called with the endpoint's URL and signing
		// key when the service's component holds a webhook endpoint, as the service's
		// own admin would. A lab's posts flow from the start. The driver and the
		// provision route make it a scale lab's webhook sender (#1747): the lab seeds
		// a ucc-webhook endpoint on its component, and the simulator adds each room
		// bound on that endpoint to the account through the provision route, so the
		// account's periodic report names every bound room.
		Webhook: &wire.WebhookSubscribe{Subscribe: "/api/webhooks", Driver: "ucc-webhook", Provision: "/api/rooms"},
	}
}
