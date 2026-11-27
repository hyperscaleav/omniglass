package api

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// LabCalendarRoom is a room the lab calendar covers and the system built in it.
type LabCalendarRoom struct {
	calendar.Room
	System string
}

// LabCalendarSource is the lab calendar the lab-only route reads (#2770): the
// rooms it covers and each room's entries in a window. The calendar is a pure
// function of the lab's world and seed, so a source holds no state of its own.
type LabCalendarSource interface {
	Rooms() []LabCalendarRoom
	All(room calendar.Room, from, to time.Time) []calendar.Entry
}

// WithLabCalendar supplies the calendar GET /lab/calendar reads. Without it a
// lab answers an empty calendar, as a lab with no plan holds no world.
func WithLabCalendar(s LabCalendarSource) Option { return func(o *options) { o.labCalendar = s } }

// WorldsCalendar is the lab calendar of one or more world files, merged: a
// room two worlds both place (a service covers another site's rooms) is one
// room, and every world of a lab shares one seed and one set of meeting knobs.
// Pure: it reads nothing after it is built.
func WorldsCalendar(worlds ...*world.World) LabCalendarSource {
	m := &worldsCalendar{cal: map[calendar.Room]*calendar.Calendar{}}
	seen := map[calendar.Room]bool{}
	for _, w := range worlds {
		if w == nil {
			continue
		}
		c := w.Calendar()
		for _, s := range w.Sites {
			for _, r := range s.Rooms {
				key := calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}
				if seen[key] {
					continue
				}
				seen[key] = true
				m.rooms = append(m.rooms, LabCalendarRoom{Room: key, System: r.System})
				if c != nil {
					m.cal[key] = c
				}
			}
		}
	}
	sort.Slice(m.rooms, func(i, j int) bool { return m.rooms[i].Key() < m.rooms[j].Key() })
	return m
}

type worldsCalendar struct {
	rooms []LabCalendarRoom
	cal   map[calendar.Room]*calendar.Calendar
}

func (m *worldsCalendar) Rooms() []LabCalendarRoom { return m.rooms }

func (m *worldsCalendar) All(room calendar.Room, from, to time.Time) []calendar.Entry {
	return m.cal[room].All(room, from, to)
}

// WorldDirCalendar is the lab calendar of the world files in dir (a scale lab's
// seed writes one per site outpost into the data volume the server shares,
// #2773). The files are the lab's own scenario resolved at its size, so the
// calendar is what the simulators are told to act out, not something learned
// from collected data. The directory is read again when a file changes, and a
// lab with no world (no plan) has an empty calendar.
func WorldDirCalendar(dir string) LabCalendarSource { return &dirCalendar{dir: dir} }

type dirCalendar struct {
	dir string
	mu  sync.Mutex
	sig string
	src LabCalendarSource
}

// current loads the directory's worlds, reusing the last load while every
// file's size and modification time are unchanged.
func (d *dirCalendar) current() LabCalendarSource {
	files, _ := filepath.Glob(filepath.Join(d.dir, "*.json"))
	sort.Strings(files)
	sig := ""
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			sig += fmt.Sprintf("%s|%d|%d;", f, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.src != nil && sig == d.sig {
		return d.src
	}
	var ws []*world.World
	for _, f := range files {
		if w, err := world.Load(f); err == nil {
			ws = append(ws, w)
		}
	}
	d.sig, d.src = sig, WorldsCalendar(ws...)
	return d.src
}

func (d *dirCalendar) Rooms() []LabCalendarRoom { return d.current().Rooms() }

func (d *dirCalendar) All(room calendar.Room, from, to time.Time) []calendar.Entry {
	return d.current().All(room, from, to)
}
