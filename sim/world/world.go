// Package world is the world a simulator holds (#2773): the sites, rooms,
// devices and services it simulates, as the scenario planned them. The sim
// is; Omniglass learns. A simulator never reads its world from Omniglass's
// server or database: the lab writes a world file from the scenario, the
// simulator loads it, and Omniglass learns that world only by collecting from
// it.
//
// A world is plain data (it round-trips through JSON) with a few pure lookups
// over it. It carries no clock and no I/O beyond Load.
package world

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// Version is the world file's format. A reader refuses a file of any other.
const Version = 1

// World is everything one simulator simulates, sized to one outpost's site.
type World struct {
	Version  int    `json:"version"`
	Scenario string `json:"scenario"`
	// Outpost is the outpost whose simulator this world is for.
	Outpost string `json:"outpost"`
	// CalendarSeed seeds the lab calendar (#2757), derived from the scenario
	// so a room's meetings do not move when the lab is sized differently.
	CalendarSeed int64 `json:"calendar_seed"`
	// Meetings are the meeting knobs of each standard, by standard name. A
	// room whose standard has none holds no meetings.
	Meetings map[string]calendar.Knobs `json:"meetings,omitempty"`
	// Problems are the recurring problems the scenario scripts (#2769), each
	// with the scope it lands in; the calendar derives them per room.
	Problems []calendar.ProblemSpec `json:"problems,omitempty"`
	Sites    []Site                 `json:"sites"`
	// Services are the devices that speak for rooms and are no room's own: a
	// pusher or a webhook sender, each with the systems it covers.
	Services []Service `json:"services,omitempty"`

	once    sync.Once
	devices map[string]Placement
	slots   map[string]string
	systems map[string]Placement
	covers  map[string][]Placement
	listed  sync.Map // service name -> []Listing
}

// index builds the lookups once; a World is read-only after it is loaded.
func (w *World) index() {
	w.once.Do(func() {
		w.devices, w.systems, w.covers = map[string]Placement{}, map[string]Placement{}, map[string][]Placement{}
		w.slots = map[string]string{}
		for _, s := range w.Sites {
			for _, r := range s.Rooms {
				p := Placement{Site: s.Name, Building: r.Building, Room: r.Name}
				w.systems[r.System] = p
				for _, d := range r.Devices {
					w.devices[d.Name] = p
					w.slots[d.Name] = d.Slot
				}
			}
		}
		for _, s := range w.Services {
			out := make([]Placement, len(s.Covers))
			for i, c := range s.Covers {
				out[i] = Placement{Site: c.Site, Building: c.Building, Room: c.Room}
			}
			w.covers[s.Name] = out
		}
	})
}

// Site is a site and its rooms.
type Site struct {
	Name string `json:"name"`
	// Archetype is the kind of site it is (headquarters, regional), so a
	// problem scoped to every site of a kind finds it.
	Archetype string `json:"archetype,omitempty"`
	Rooms     []Room `json:"rooms"`
}

// Room is a place with a system built to a standard, and the devices planned
// in it. Capacity is the most a meeting there may invite (the largest
// attendance its standard's meeting knobs declare, zero with none).
type Room struct {
	Name     string   `json:"name"`
	Building string   `json:"building"`
	Standard string   `json:"standard"`
	System   string   `json:"system"`
	Capacity int      `json:"capacity"`
	Devices  []Device `json:"devices,omitempty"`
}

// Device is one planned component: the name the world gives it (unique across
// the plan, and the name the lab's seed onboards the component under, so a
// simulator joins a pulled device by an exact name), the product it is, and
// the simulator profile that serves it ("" when none does). The world owns its
// id as it owns its name (#2790): a stable, deterministic id a cloud platform
// that lists devices names it by. Platform is the kind of device a meeting
// platform lists it as ("" when it lists none: a display or an amplifier is
// no part of its account) and Firmware the version the device itself reports.
type Device struct {
	Name string `json:"name"`
	// Slot is the role the device plays in its room (main-display,
	// wireless-mic), which a scripted problem lands on (#2769).
	Slot     string `json:"slot,omitempty"`
	ID       string `json:"id,omitempty"`
	Product  string `json:"product"`
	Profile  string `json:"profile,omitempty"`
	Platform string `json:"platform,omitempty"`
	Firmware string `json:"firmware,omitempty"`
}

// Service is a device that speaks for systems rather than sitting in one
// room: a webhook sender (a pulled component) or a token pusher (apart from
// the fleet). Covers is the systems it speaks for, each with its room.
type Service struct {
	Name string `json:"name"`
	// Address is the loopback address a pulled service (a webhook sender)
	// is planned at; a token pusher is no pulled device and has none.
	Address string `json:"address,omitempty"`
	Profile string `json:"profile"`
	Webhook bool   `json:"webhook,omitempty"`
	// Polled is a service the fleet polls, a component whether or not it
	// also posts webhooks (#2765, #2790).
	Polled bool    `json:"polled,omitempty"`
	Covers []Cover `json:"covers,omitempty"`
}

// Cover is one system a service speaks for, and where that system sits.
type Cover struct {
	System   string `json:"system"`
	Site     string `json:"site"`
	Building string `json:"building"`
	Room     string `json:"room"`
}

// Placement is where a device sits: the three fields a device snapshot's
// room fields take.
type Placement struct{ Site, Building, Room string }

// CalRoom is the placement as a calendar room key.
func (p Placement) CalRoom() calendar.Room {
	return calendar.Room{Site: p.Site, Building: p.Building, Room: p.Room}
}

// Validate refuses a world that cannot be trusted: a version it does not
// read, a name given twice, knobs out of range.
func (w *World) Validate() error {
	if w.Version != Version {
		return fmt.Errorf("world: version %d, this simulator reads version %d", w.Version, Version)
	}
	for std, k := range w.Meetings {
		if err := k.Validate(); err != nil {
			return fmt.Errorf("world: meetings.%s.%w", std, err)
		}
	}
	names := map[string]bool{}
	for _, p := range w.Problems {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("world: problem %q: %w", p.Name, err)
		}
		if names[p.Name] {
			return fmt.Errorf("world: problem %q is scripted twice", p.Name)
		}
		names[p.Name] = true
	}
	seen, ids := map[string]bool{}, map[string]string{}
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			for _, d := range r.Devices {
				if seen[d.Name] {
					return fmt.Errorf("world: device %q is placed twice", d.Name)
				}
				seen[d.Name] = true
				if d.ID == "" {
					continue
				}
				if other, dup := ids[d.ID]; dup {
					return fmt.Errorf("world: devices %q and %q share the id %q", other, d.Name, d.ID)
				}
				ids[d.ID] = d.Name
			}
		}
	}
	return nil
}

// Marshal is the world file's bytes: indented, with keys in a fixed order, so
// one world is one file.
func (w *World) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Parse reads a world file's bytes and validates it. Unknown fields are
// refused: a file this reader does not understand is not read half.
func Parse(b []byte) (*World, error) {
	var w World
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("world: %w", err)
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return &w, nil
}

// Load reads and validates the world file at path.
func Load(path string) (*World, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("world: %w", err)
	}
	w, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return w, nil
}

// PlaceDevice is where the world places the device of that component name. A
// device the world does not name is not placed.
func (w *World) PlaceDevice(name string) (Placement, bool) {
	if w == nil {
		return Placement{}, false
	}
	w.index()
	p, ok := w.devices[name]
	return p, ok
}

// SlotOf is the role the world gives the device of that component name
// ("" for a device it does not name, or one with no slot).
func (w *World) SlotOf(name string) string {
	if w == nil {
		return ""
	}
	w.index()
	return w.slots[name]
}

// PlaceSystem is where the world places the system of that name: the room a
// service's push for that system is about. This is how a push's .Owner
// resolves to a room, through the world and not through Omniglass.
func (w *World) PlaceSystem(system string) (Placement, bool) {
	if w == nil {
		return Placement{}, false
	}
	w.index()
	p, ok := w.systems[system]
	return p, ok
}

// Covered is the rooms the named service speaks for, in the order the world
// lists them; none for a name that is no service.
func (w *World) Covered(service string) []Placement {
	if w == nil {
		return nil
	}
	w.index()
	return w.covers[service]
}

// Calendar is the lab calendar the world implies: the world's seed and each
// room's meeting knobs by its standard, and the problems scripted for each
// room by their scope. Nil when the world declares neither, so a world with
// none has no calendar.
func (w *World) Calendar() *calendar.Calendar {
	if w == nil || (len(w.Meetings) == 0 && len(w.Problems) == 0) {
		return nil
	}
	byRoom := map[calendar.Room]calendar.Knobs{}
	problems := map[calendar.Room][]calendar.ProblemSpec{}
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			room := calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}
			if k, ok := w.Meetings[r.Standard]; ok {
				byRoom[room] = k
			}
			for _, p := range w.Problems {
				if p.Scope.Matches(r.Standard, s.Name, s.Archetype, r.System) {
					problems[room] = append(problems[room], p)
				}
			}
		}
	}
	cal := calendar.New(w.CalendarSeed, func(room calendar.Room) (calendar.Knobs, bool) {
		k, ok := byRoom[room]
		return k, ok
	})
	if len(w.Problems) > 0 {
		cal = cal.WithProblems(func(room calendar.Room) []calendar.ProblemSpec { return problems[room] })
	}
	return cal
}

// Standards is the standards the world's rooms are built to, sorted.
func (w *World) Standards() []string {
	set := map[string]bool{}
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			set[r.Standard] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Listing is one device a platform lists (#2790): the world's id for it, its
// name, the kind the platform lists it as, the version it reports, the product
// and profile, and the system and room it sits in.
type Listing struct {
	ID, Name, Type, Firmware, Product, Profile string
	// Slot is the role the device plays in its room.
	Slot                 string
	System               string
	Site, Building, Room string
}

// Listed is the devices the named service lists, in id order: every device
// with a platform kind in a room the service covers. A world is read-only once
// loaded, so the list is built once per service. None for a name that is no
// service.
func (w *World) Listed(service string) []Listing {
	if w == nil {
		return nil
	}
	if v, ok := w.listed.Load(service); ok {
		return v.([]Listing)
	}
	covered := map[string]bool{}
	for _, s := range w.Services {
		if s.Name == service {
			for _, c := range s.Covers {
				covered[c.System] = true
			}
		}
	}
	var out []Listing
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			if !covered[r.System] {
				continue
			}
			for _, d := range r.Devices {
				if d.Platform == "" || d.ID == "" {
					continue
				}
				out = append(out, Listing{ID: d.ID, Name: d.Name, Type: d.Platform, Firmware: d.Firmware, Product: d.Product,
					Profile: d.Profile, Slot: d.Slot, System: r.System, Site: s.Name, Building: r.Building, Room: r.Name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	w.listed.Store(service, out)
	return out
}
