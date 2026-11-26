// Package calendar is the lab calendar (#2757): the meetings every room of a
// simulated lab holds, derived, never stored. Entries(room, window) is a pure
// function of the room, the knobs a scenario declares for it, a seed and the
// window, so every process that computes it computes the same calendar and no
// process need hold or ship one. Nothing here reads a clock or a global: the
// caller says what time it is.
//
// A room's day is generated whole from (seed, room key, UTC day), so a day is
// the same however it is asked for: through a one-day window, a month, or a
// pinned instant. The seed identity is the room key (site, building, room);
// #1749's stable device key has not landed in the code, and a room is the unit
// meetings are about, not a device.
//
// The entry model leaves room for a second kind, a scripted problem (#2769):
// an Entry carries its Kind and one payload pointer per kind, of which only
// Meeting exists today.
package calendar

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sort"
	"time"
)

// Room is a room's key: the engine's grouping of devices that share a place.
type Room struct{ Site, Building, Room string }

// Key is the room as one string, the identity its day is seeded from.
func (r Room) Key() string { return r.Site + "/" + r.Building + "/" + r.Room }

// Kind is what an entry is.
type Kind string

// KindMeeting is a meeting in a room. A problem kind joins it with its own
// slice (#2769).
const KindMeeting Kind = "meeting"

// Origin says how a meeting came to be.
type Origin string

const (
	// OriginBooked is a meeting reserved ahead on a slot.
	OriginBooked Origin = "booked"
	// OriginWalkIn is someone using a free room unbooked.
	OriginWalkIn Origin = "walk-in"
)

// Outcome says how a meeting went.
type Outcome string

const (
	OutcomeHeld       Outcome = "held"
	OutcomeNoShow     Outcome = "no-show"
	OutcomeEndedEarly Outcome = "ended-early"
	OutcomeEndedLate  Outcome = "ended-late"
)

// Meeting is the payload of a meeting entry. SlotStart and SlotEnd are what
// was booked (a walk-in's are its own start and end); the entry's Start and
// End are when it actually ran.
type Meeting struct {
	Origin    Origin
	Organizer string
	Expected  int // invited, zero for a walk-in
	Actual    int // who came, zero for a no-show
	Outcome   Outcome
	SlotStart time.Time
	SlotEnd   time.Time
}

// Entry is one thing on the calendar: a kind, the window it occupies on the
// simulator clock, the room it is scoped to, and the payload of its kind.
type Entry struct {
	Kind    Kind
	Start   time.Time
	End     time.Time
	Room    Room
	Meeting *Meeting
}

// Jitter bounds: a booked meeting starts up to MaxEarlyStart before and
// MaxLateStart after its slot, and ends up to MaxEarlyEnd before and
// MaxLateEnd after it. A late end pushes the next start later, so entries in
// a room never overlap.
const (
	MaxEarlyStart = 2 * time.Minute
	MaxLateStart  = 5 * time.Minute
	MaxEarlyEnd   = 5 * time.Minute
	MaxLateEnd    = 5 * time.Minute
	// SlotUnit is the grid free time is counted in: the shortest slot, and
	// the cell a walk-in uses.
	SlotUnit = 15 * time.Minute
)

// Range is an inclusive whole-number range.
type Range struct {
	Min int `yaml:"min" json:"min"`
	Max int `yaml:"max" json:"max"`
}

// Hours bounds the day meetings fall in, whole UTC hours: [Start, End). The
// zero value is the whole day.
type Hours struct {
	Start int `yaml:"start" json:"start"`
	End   int `yaml:"end" json:"end"`
}

func (h Hours) bounds() (time.Duration, time.Duration) {
	if h.Start == 0 && h.End == 0 {
		return 0, 24 * time.Hour
	}
	return time.Duration(h.Start) * time.Hour, time.Duration(h.End) * time.Hour
}

// Knobs are one kind of room's meeting behavior, as a scenario declares it.
type Knobs struct {
	// BookingRate is the chance a free slot boundary starts a booking.
	BookingRate float64 `yaml:"booking_rate" json:"booking_rate"`
	// SlotMix weights the slot lengths in minutes (15, 30, 60); a slot lands
	// on a wall-clock boundary of its own length.
	SlotMix map[int]float64 `yaml:"slot_mix" json:"slot_mix"`
	// NoShowRate is the share of bookings nobody attends.
	NoShowRate float64 `yaml:"no_show_rate" json:"no_show_rate"`
	// EndEarlyRate and EndLateRate are the shares of attended bookings that
	// end early or late; the rest end on time.
	EndEarlyRate float64 `yaml:"ended_early_rate" json:"ended_early_rate"`
	EndLateRate  float64 `yaml:"ended_late_rate" json:"ended_late_rate"`
	// Attendance is how many a booking invites.
	Attendance Range `yaml:"attendance" json:"attendance"`
	// WalkInRate is the chance a free 15-minute cell, no booking touches,
	// holds a walk-in.
	WalkInRate float64 `yaml:"walk_in_rate" json:"walk_in_rate"`
	Hours      Hours   `yaml:"hours" json:"hours"`
}

// slotSizes are the slot lengths a mix may name, in minutes.
var slotSizes = []int{15, 30, 60}

// Validate names the first field that is out of range. Pure.
func (k Knobs) Validate() error {
	rate := func(name string, v float64) error {
		if v < 0 || v > 1 {
			return fmt.Errorf("%s %v is not between 0 and 1", name, v)
		}
		return nil
	}
	for _, c := range []struct {
		name string
		v    float64
	}{{"booking_rate", k.BookingRate}, {"no_show_rate", k.NoShowRate}, {"ended_early_rate", k.EndEarlyRate},
		{"ended_late_rate", k.EndLateRate}, {"walk_in_rate", k.WalkInRate}} {
		if err := rate(c.name, c.v); err != nil {
			return err
		}
	}
	if k.EndEarlyRate+k.EndLateRate > 1 {
		return fmt.Errorf("ended_early_rate and ended_late_rate add to %v, past 1", k.EndEarlyRate+k.EndLateRate)
	}
	total := 0.0
	for m, w := range k.SlotMix {
		if m != 15 && m != 30 && m != 60 {
			return fmt.Errorf("slot_mix has a %d-minute slot, not 15, 30 or 60", m)
		}
		if w < 0 {
			return fmt.Errorf("slot_mix weight of the %d-minute slot is %v, below 0", m, w)
		}
		total += w
	}
	if k.BookingRate > 0 && total <= 0 {
		return fmt.Errorf("slot_mix has no weight but booking_rate is %v", k.BookingRate)
	}
	if k.BookingRate > 0 || k.WalkInRate > 0 {
		if k.Attendance.Min < 1 || k.Attendance.Max < k.Attendance.Min {
			return fmt.Errorf("attendance %d to %d is not a range from at least 1", k.Attendance.Min, k.Attendance.Max)
		}
	}
	if !(k.Hours.Start == 0 && k.Hours.End == 0) {
		if k.Hours.Start < 0 || k.Hours.End > 24 || k.Hours.Start >= k.Hours.End {
			return fmt.Errorf("hours %d to %d is not a span within 0 to 24", k.Hours.Start, k.Hours.End)
		}
	}
	return nil
}

var organizers = []string{
	"Avery Chen", "Blake Okafor", "Casey Moreno", "Dana Whitfield", "Elliot Park", "Frankie Duarte",
	"Gray Lindqvist", "Harper Nakamura", "Indira Rao", "Jules Bianchi", "Kai Abara", "Lena Voss",
	"Marlow Reyes", "Noor Haddad", "Oakley Frost", "Priya Menon", "Quinn Ashby", "Rowan Teague",
	"Sasha Kowalski", "Tobias Lund",
}

func daySeed(seed int64, room Room, day time.Time) int64 {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%d", seed, room.Key(), day.Unix())))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

// Generate is the room's entries overlapping [from, to), in start order. Pure:
// the same arguments give the same entries. Invalid knobs give none.
func Generate(room Room, k Knobs, seed int64, from, to time.Time) []Entry {
	if !to.After(from) || k.Validate() != nil {
		return nil
	}
	var out []Entry
	for day := from.UTC().Truncate(24 * time.Hour); day.Before(to); day = day.AddDate(0, 0, 1) {
		for _, e := range generateDay(room, k, seed, day) {
			if e.End.After(from) && e.Start.Before(to) {
				out = append(out, e)
			}
		}
	}
	return out
}

type slot struct {
	start, end time.Time
}

func generateDay(room Room, k Knobs, seed int64, day time.Time) []Entry {
	rng := rand.New(rand.NewSource(daySeed(seed, room, day)))
	hs, he := k.Hours.bounds()
	open, closeT := day.Add(hs), day.Add(he)

	var slots []slot
	for cur := open; cur.Before(closeT); {
		var eligible []int
		var weight float64
		for _, m := range slotSizes {
			d := time.Duration(m) * time.Minute
			if k.SlotMix[m] > 0 && cur.UnixNano()%int64(d) == 0 && !cur.Add(d).After(closeT) {
				eligible = append(eligible, m)
				weight += k.SlotMix[m]
			}
		}
		if len(eligible) == 0 || k.BookingRate <= 0 {
			cur = cur.Add(SlotUnit)
			continue
		}
		pick := rng.Float64() * weight
		m := eligible[len(eligible)-1]
		for _, c := range eligible {
			if pick < k.SlotMix[c] {
				m = c
				break
			}
			pick -= k.SlotMix[c]
		}
		if rng.Float64() >= k.BookingRate {
			cur = cur.Add(SlotUnit)
			continue
		}
		d := time.Duration(m) * time.Minute
		slots = append(slots, slot{cur, cur.Add(d)})
		cur = cur.Add(d)
	}

	var entries []Entry
	prevEnd := open
	for _, s := range slots {
		m := &Meeting{
			Origin:    OriginBooked,
			Organizer: organizers[rng.Intn(len(organizers))],
			Expected:  k.Attendance.Min + rng.Intn(k.Attendance.Max-k.Attendance.Min+1),
			Outcome:   OutcomeHeld,
			SlotStart: s.start,
			SlotEnd:   s.end,
		}
		noShow := rng.Float64() < k.NoShowRate
		o := rng.Float64()
		startJit := time.Duration(rng.Int63n(int64(MaxEarlyStart+MaxLateStart)/int64(time.Second)+1))*time.Second - MaxEarlyStart
		endJit := time.Duration(rng.Int63n(int64(MaxLateEnd-2*time.Minute)/int64(time.Second)+1)) * time.Second
		heldJit := time.Duration(rng.Int63n(int64(2*time.Minute)/int64(time.Second)+1))*time.Second - time.Minute
		drop := rng.Intn(m.Expected/4 + 1)

		start := s.start.Add(startJit)
		if start.Before(prevEnd) {
			start = prevEnd
		}
		var end time.Time
		switch {
		case noShow:
			m.Outcome, m.Actual = OutcomeNoShow, 0
			start, end = s.start, s.end
			if start.Before(prevEnd) {
				start = prevEnd
			}
		case o < k.EndEarlyRate:
			m.Outcome = OutcomeEndedEarly
			end = s.end.Add(-(2*time.Minute + endJit))
		case o < k.EndEarlyRate+k.EndLateRate:
			m.Outcome = OutcomeEndedLate
			end = s.end.Add(2*time.Minute + endJit)
		default:
			end = s.end.Add(heldJit)
		}
		if end.After(closeT) {
			end = closeT
			if m.Outcome == OutcomeEndedLate && !end.After(s.end) {
				m.Outcome = OutcomeHeld
			}
		}
		if m.Outcome != OutcomeNoShow {
			m.Actual = m.Expected - drop
			if m.Actual < 1 {
				m.Actual = 1
			}
		}
		entries = append(entries, Entry{Kind: KindMeeting, Start: start, End: end, Room: room, Meeting: m})
		prevEnd = end
	}

	// Walk-ins use the cells no booking touches.
	for c := open; !c.Add(SlotUnit).After(closeT); c = c.Add(SlotUnit) {
		touched := false
		for _, e := range entries {
			if e.Start.Before(c.Add(SlotUnit)) && e.End.After(c) {
				touched = true
				break
			}
		}
		if touched || rng.Float64() >= k.WalkInRate {
			continue
		}
		start := c.Add(time.Duration(rng.Int63n(int64(MaxLateStart)/int64(time.Second)+1)) * time.Second)
		end := c.Add(SlotUnit)
		who := 1
		if k.Attendance.Min > 1 {
			who += rng.Intn(k.Attendance.Min)
		}
		entries = append(entries, Entry{Kind: KindMeeting, Start: start, End: end, Room: room, Meeting: &Meeting{
			Origin: OriginWalkIn, Actual: who, Outcome: OutcomeHeld, SlotStart: start, SlotEnd: end,
		}})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Start.Before(entries[j].Start) })
	return entries
}

// Map is the entry as a template reads it: lower-case keys, times as unix
// seconds, so a profile writes {{ with meetingNow }}{{ .outcome }}{{ end }}.
func (e Entry) Map() map[string]any {
	m := map[string]any{
		"kind": string(e.Kind), "start": e.Start.Unix(), "end": e.End.Unix(),
		"site": e.Room.Site, "building": e.Room.Building, "room": e.Room.Room,
	}
	if e.Meeting != nil {
		m["origin"] = string(e.Meeting.Origin)
		m["organizer"] = e.Meeting.Organizer
		m["expected"] = e.Meeting.Expected
		m["actual"] = e.Meeting.Actual
		m["outcome"] = string(e.Meeting.Outcome)
		m["slot_start"] = e.Meeting.SlotStart.Unix()
		m["slot_end"] = e.Meeting.SlotEnd.Unix()
	}
	return m
}

// Calendar is a seed and the knobs each room has: the whole lab's calendar,
// derived on read.
type Calendar struct {
	seed  int64
	knobs func(Room) (Knobs, bool)
}

// New makes a calendar of the seed. knobs says what a room's meetings are
// like; a room it reports false for has none.
func New(seed int64, knobs func(Room) (Knobs, bool)) *Calendar {
	return &Calendar{seed: seed, knobs: knobs}
}

// Entries is the room's entries overlapping [from, to). A nil calendar has
// none.
func (c *Calendar) Entries(room Room, from, to time.Time) []Entry {
	if c == nil || c.knobs == nil {
		return nil
	}
	k, ok := c.knobs(room)
	if !ok {
		return nil
	}
	return Generate(room, k, c.seed, from, to)
}

// Now is the entry running at t, if one is.
func (c *Calendar) Now(room Room, t time.Time) (Entry, bool) {
	for _, e := range c.Entries(room, t, t.Add(time.Nanosecond)) {
		if !t.Before(e.Start) && t.Before(e.End) {
			return e, true
		}
	}
	return Entry{}, false
}

// nextHorizon is how far Next looks for the next entry.
const nextHorizon = 8

// Next is the first entry starting after t, within a week and a day.
func (c *Calendar) Next(room Room, t time.Time) (Entry, bool) {
	for _, e := range c.Entries(room, t, t.AddDate(0, 0, nextHorizon)) {
		if e.Start.After(t) {
			return e, true
		}
	}
	return Entry{}, false
}
