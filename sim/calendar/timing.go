package calendar

import (
	"fmt"
	"time"
)

// The room phase's timing (#2797): how long before a booked meeting a room
// wakes, how long after one it stays in use, how long a recorded meeting is
// still being processed, and the stretch of a long meeting that shares
// content. They are declared once per standard in the meeting knobs and read
// through Timing, so a device profile never carries one of these numbers and
// retuning a rule retunes the whole room.
const (
	// DefaultWakeLead is how long before a booked meeting someone attends its
	// room wakes.
	DefaultWakeLead = 120 * time.Second
	// DefaultCooldownTail is how long after a meeting that took place its
	// room stays in use.
	DefaultCooldownTail = 300 * time.Second
	// DefaultRecordingTail is how long after a recorded meeting ends its
	// recording is still being processed.
	DefaultRecordingTail = 600 * time.Second
	// maxTimingSeconds bounds a lead or tail: a day.
	maxTimingSeconds = 24 * 60 * 60
)

// Sharing is the stretch of a meeting that shares content: a meeting that
// took place and lasts at least MinMinutes is cut into Parts equal parts and
// shares from the start of part FromPart to the start of part ToPart (parts
// counted from zero). Whole numbers, so the stretch is exact at every second.
type Sharing struct {
	MinMinutes int `yaml:"min_minutes" json:"min_minutes"`
	Parts      int `yaml:"parts" json:"parts"`
	FromPart   int `yaml:"from_part" json:"from_part"`
	ToPart     int `yaml:"to_part" json:"to_part"`
}

// DefaultSharing is the middle third of a meeting of half an hour or more.
var DefaultSharing = Sharing{MinMinutes: 30, Parts: 3, FromPart: 1, ToPart: 2}

// Timing is a room's resolved phase timing.
type Timing struct {
	WakeLead, CooldownTail, RecordingTail time.Duration
	Sharing                               Sharing
}

// Timing is the knobs' phase timing, each unset value read as its default.
func (k Knobs) Timing() Timing {
	t := Timing{WakeLead: DefaultWakeLead, CooldownTail: DefaultCooldownTail,
		RecordingTail: DefaultRecordingTail, Sharing: DefaultSharing}
	if k.WakeLead != nil {
		t.WakeLead = time.Duration(*k.WakeLead) * time.Second
	}
	if k.CooldownTail != nil {
		t.CooldownTail = time.Duration(*k.CooldownTail) * time.Second
	}
	if k.RecordingTail != nil {
		t.RecordingTail = time.Duration(*k.RecordingTail) * time.Second
	}
	if k.Sharing != nil {
		t.Sharing = *k.Sharing
	}
	return t
}

func (k Knobs) validateTiming() error {
	for _, c := range []struct {
		name string
		v    *int
	}{{"wake_lead_seconds", k.WakeLead}, {"cooldown_seconds", k.CooldownTail}, {"recording_tail_seconds", k.RecordingTail}} {
		if c.v != nil && (*c.v < 0 || *c.v > maxTimingSeconds) {
			return fmt.Errorf("%s %d is not between 0 and %d", c.name, *c.v, maxTimingSeconds)
		}
	}
	if s := k.Sharing; s != nil {
		switch {
		case s.MinMinutes < 1:
			return fmt.Errorf("sharing.min_minutes %d is not at least 1", s.MinMinutes)
		case s.Parts < 1 || s.Parts > 60:
			return fmt.Errorf("sharing.parts %d is not between 1 and 60", s.Parts)
		case s.FromPart < 0 || s.FromPart >= s.ToPart || s.ToPart > s.Parts:
			return fmt.Errorf("sharing from_part %d to_part %d is not a span within 0 to parts (%d)", s.FromPart, s.ToPart, s.Parts)
		}
	}
	return nil
}

// shares says whether a meeting running at t shares content: one that took
// place, lasts at least MinMinutes and is inside its sharing stretch.
func (s Sharing) shares(m Meeting, start, end, t time.Time) bool {
	if m.Outcome == OutcomeNoShow {
		return false
	}
	dur := end.Sub(start)
	if dur < time.Duration(s.MinMinutes)*time.Minute || t.Before(start) || !t.Before(end) {
		return false
	}
	into := t.Sub(start) * time.Duration(s.Parts)
	return into >= dur*time.Duration(s.FromPart) && into < dur*time.Duration(s.ToPart)
}

// Knobs is the meeting knobs the room has, false for a room with none.
func (c *Calendar) Knobs(room Room) (Knobs, bool) {
	if c == nil || c.knobs == nil {
		return Knobs{}, false
	}
	return c.knobs(room)
}
