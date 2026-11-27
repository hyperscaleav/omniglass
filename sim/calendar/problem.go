package calendar

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"
)

// KindProblem is a scripted problem on a room (#2769): a fact of the world
// the lab plays, that Omniglass learns only by collecting (a device that
// stops answering, a reading pinned to a value, packets lost).
const KindProblem Kind = "problem"

// Pin holds one state path of a device at a value while a problem is active:
// a display dark, a wireless pack's battery dead.
type Pin struct {
	Path  string `yaml:"path" json:"path"`
	Value string `yaml:"value" json:"value"`
}

// Impairment is what a problem does to the devices it lands on. Exactly one
// of its three effects is set. Unplugged and loss are the network's (the
// simulator needs NET_ADMIN to apply them); a pin is the device's own.
type Impairment struct {
	Unplugged bool    `yaml:"unplugged" json:"unplugged,omitempty"`
	LossPct   float64 `yaml:"loss_pct" json:"loss_pct,omitempty"`
	Pin       *Pin    `yaml:"pin" json:"pin,omitempty"`
}

// Scope is where a problem lands, resolved from the plan and the world, never
// by product: the rooms that match every criterion it sets, and in each the
// devices that fill the Role slot.
type Scope struct {
	// Standard is a standard's name; Site a site's name or its archetype's
	// (every regional office); Rooms the names of systems.
	Standard string   `yaml:"standard" json:"standard,omitempty"`
	Site     string   `yaml:"site" json:"site,omitempty"`
	Rooms    []string `yaml:"rooms" json:"rooms,omitempty"`
	// Role is the slot of the room the problem lands on (main-display,
	// wireless-mic): the role a device plays, not the product it is.
	Role string `yaml:"role" json:"role"`
}

// Matches says whether a room is in scope: the standard it was built to, the
// site and archetype it sits in, and its system's name.
func (s Scope) Matches(standard, site, archetype, system string) bool {
	if s.Standard != "" && s.Standard != standard {
		return false
	}
	if s.Site != "" && s.Site != site && s.Site != archetype {
		return false
	}
	if len(s.Rooms) > 0 {
		found := false
		for _, r := range s.Rooms {
			found = found || r == system
		}
		if !found {
			return false
		}
	}
	return true
}

// ProblemSpec is one recurring problem a scenario scripts: when it happens
// (a cadence, a time of day and a duration, all UTC), where (a scope) and
// what it does (an impairment). Scripted, not drawn: only Spread draws, and
// only an offset inside the cadence, from the seed.
type ProblemSpec struct {
	Name string `yaml:"name" json:"name"`
	// Cadence is daily, weekly (on Weekday) or monthly (on Day of the month,
	// or on the Nth Weekday).
	Cadence string `yaml:"cadence" json:"cadence"`
	Weekday string `yaml:"weekday" json:"weekday,omitempty"`
	Day     int    `yaml:"day" json:"day,omitempty"`
	Nth     int    `yaml:"nth" json:"nth,omitempty"`
	// At is the UTC time of day it starts, "15:04"; For how long it lasts.
	At  string        `yaml:"at" json:"at"`
	For time.Duration `yaml:"for" json:"for"`
	// Spread, above zero, starts each room's occurrence a seeded whole number
	// of minutes after At, up to Spread, so a fleet does not break at one
	// instant. The offset is a pure function of the seed, the room, the
	// problem and the day.
	Spread     time.Duration `yaml:"spread" json:"spread,omitempty"`
	Scope      Scope         `yaml:"scope" json:"scope"`
	Impairment Impairment    `yaml:"impairment" json:"impairment"`
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

// maxProblemFor bounds a problem's duration: a day, so an occurrence reaches
// into at most the next day and a window needs look back one day only.
const maxProblemFor = 24 * time.Hour

func parseAt(at string) (time.Duration, error) {
	t, err := time.Parse("15:04", at)
	if err != nil {
		return 0, fmt.Errorf("at %q is not a UTC time of day like 13:30", at)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

// Validate names the first field that is out of range. Pure.
func (p ProblemSpec) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("a problem needs a name")
	}
	wd := strings.ToLower(p.Weekday)
	switch p.Cadence {
	case "daily":
		if p.Weekday != "" || p.Day != 0 || p.Nth != 0 {
			return fmt.Errorf("a daily problem takes no weekday, day or nth")
		}
	case "weekly":
		if _, ok := weekdays[wd]; !ok {
			return fmt.Errorf("a weekly problem needs a weekday (sunday to saturday), got %q", p.Weekday)
		}
		if p.Day != 0 || p.Nth != 0 {
			return fmt.Errorf("a weekly problem takes a weekday alone, not a day or nth")
		}
	case "monthly":
		switch {
		case p.Day != 0 && p.Nth != 0:
			return fmt.Errorf("a monthly problem takes a day or an nth weekday, not both")
		case p.Day != 0:
			if p.Day < 1 || p.Day > 28 {
				return fmt.Errorf("day %d is not a date every month has (1 to 28)", p.Day)
			}
			if p.Weekday != "" {
				return fmt.Errorf("a monthly problem on a day takes no weekday")
			}
		case p.Nth != 0:
			if p.Nth < 1 || p.Nth > 4 {
				return fmt.Errorf("nth %d is not 1 to 4", p.Nth)
			}
			if _, ok := weekdays[wd]; !ok {
				return fmt.Errorf("a monthly problem on the nth weekday needs a weekday, got %q", p.Weekday)
			}
		default:
			return fmt.Errorf("a monthly problem needs a day or an nth weekday")
		}
	default:
		return fmt.Errorf("cadence %q is not daily, weekly or monthly", p.Cadence)
	}
	if _, err := parseAt(p.At); err != nil {
		return err
	}
	if p.For <= 0 || p.For > maxProblemFor {
		return fmt.Errorf("for %v is not a duration up to %v", p.For, maxProblemFor)
	}
	if p.Spread < 0 || p.Spread >= 24*time.Hour {
		return fmt.Errorf("spread %v is not between 0 and a day", p.Spread)
	}
	s := p.Scope
	if s.Standard == "" && s.Site == "" && len(s.Rooms) == 0 {
		return fmt.Errorf("scope names no standard, site or rooms: a problem must say where it lands")
	}
	if s.Role == "" {
		return fmt.Errorf("scope needs a role, the slot the problem lands on")
	}
	return p.Impairment.validate()
}

func (i Impairment) validate() error {
	n := 0
	if i.Unplugged {
		n++
	}
	if i.LossPct != 0 {
		n++
	}
	if i.Pin != nil {
		n++
	}
	if n != 1 {
		return fmt.Errorf("impairment sets %d effects, want exactly one of unplugged, loss_pct or pin", n)
	}
	if !(i.LossPct >= 0 && i.LossPct <= 100) {
		return fmt.Errorf("impairment loss_pct %v is not between 0 and 100", i.LossPct)
	}
	if i.Pin != nil && (i.Pin.Path == "" || i.Pin.Value == "") {
		return fmt.Errorf("impairment pin needs a path and a value")
	}
	return nil
}

// Problem is the payload of a problem entry: what is wrong, and with which
// role of the room. The entry's Start and End are when it is.
type Problem struct {
	Name       string
	Role       string
	Impairment Impairment
}

// matchesDay says whether the cadence fires on the UTC day.
func (p ProblemSpec) matchesDay(day time.Time) bool {
	switch p.Cadence {
	case "daily":
		return true
	case "weekly":
		return day.Weekday() == weekdays[strings.ToLower(p.Weekday)]
	case "monthly":
		if p.Day != 0 {
			return day.Day() == p.Day
		}
		return day.Weekday() == weekdays[strings.ToLower(p.Weekday)] && (day.Day()-1)/7+1 == p.Nth
	}
	return false
}

// offset is the room's seeded start offset on a day, whole minutes in
// [0, Spread].
func (p ProblemSpec) offset(seed int64, room Room, day time.Time) time.Duration {
	mins := int64(p.Spread / time.Minute)
	if mins <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("problem|%d|%s|%s|%d", seed, room.Key(), p.Name, day.Unix())))
	return time.Duration(binary.BigEndian.Uint64(sum[:8])%uint64(mins+1)) * time.Minute
}

// entries is the problem's occurrences in the room overlapping [from, to), in
// start order. Pure: the same arguments give the same entries.
func (p ProblemSpec) entries(room Room, seed int64, from, to time.Time) []Entry {
	if !to.After(from) || p.Validate() != nil {
		return nil
	}
	at, _ := parseAt(p.At)
	var out []Entry
	// An occurrence lasts at most a day and starts at most a day plus a
	// spread after its day opens: look back two days so none that overlaps
	// the window is missed.
	for day := from.UTC().Truncate(24*time.Hour).AddDate(0, 0, -2); day.Before(to); day = day.AddDate(0, 0, 1) {
		if !p.matchesDay(day) {
			continue
		}
		start := day.Add(at).Add(p.offset(seed, room, day))
		end := start.Add(p.For)
		if end.After(from) && start.Before(to) {
			out = append(out, Entry{Kind: KindProblem, Start: start, End: end, Room: room,
				Problem: &Problem{Name: p.Name, Role: p.Scope.Role, Impairment: p.Impairment}})
		}
	}
	return out
}

// WithProblems is the calendar with the problems each room holds: what the
// scenario scripts for it. The meetings are untouched. A room it reports none
// for has none.
func (c *Calendar) WithProblems(problems func(Room) []ProblemSpec) *Calendar {
	if c == nil {
		c = &Calendar{}
	}
	cp := *c
	cp.problems = problems
	return &cp
}

// HasProblems says whether the calendar scripts any problem at all, so a reader
// that scans many rooms can skip the scan on a calendar with none.
func (c *Calendar) HasProblems() bool { return c != nil && c.problems != nil }

// Problems is the room's problem entries overlapping [from, to), in start
// order. A nil calendar has none.
func (c *Calendar) Problems(room Room, from, to time.Time) []Entry {
	if c == nil || c.problems == nil {
		return nil
	}
	var out []Entry
	for _, p := range c.problems(room) {
		out = append(out, p.entries(room, c.seed, from, to)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// ProblemsNow is the problems active in the room at t: those whose window
// holds t, opening at its start and closing at its end.
func (c *Calendar) ProblemsNow(room Room, t time.Time) []Entry {
	var out []Entry
	for _, e := range c.Problems(room, t, t.Add(time.Nanosecond)) {
		if !t.Before(e.Start) && t.Before(e.End) {
			out = append(out, e)
		}
	}
	return out
}

// All is every entry of the room overlapping [from, to), meetings and
// problems together, in start order. Entries is the meetings alone, which the
// readers of a meeting depend on.
func (c *Calendar) All(room Room, from, to time.Time) []Entry {
	out := append(c.Entries(room, from, to), c.Problems(room, from, to)...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// mapProblem adds a problem entry's fields to its template map.
func mapProblem(m map[string]any, p *Problem) {
	m["name"] = p.Name
	m["role"] = p.Role
	m["unplugged"] = p.Impairment.Unplugged
	m["loss_pct"] = p.Impairment.LossPct
	if pin := p.Impairment.Pin; pin != nil {
		m["pin_path"] = pin.Path
		m["pin_value"] = pin.Value
	}
}
