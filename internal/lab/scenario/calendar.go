package scenario

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// Calendar is a scenario's lab calendar block (#2757): what every simulated
// room acts out on the simulator's clock. Only meetings are declared here
// today; scripted problems join them with their own slice (#2769). The
// calendar itself is derived from these knobs and a seed (sim/calendar), never
// written out, so every node computes the same one.
type Calendar struct {
	Meetings Meetings `yaml:"meetings"`
	Line     int      `yaml:"-"`
}

// Meetings are the meeting knobs of each standard, in the order written: a
// room's meetings follow the standard its system was built to.
type Meetings []StandardMeetings

// StandardMeetings is one standard's meeting knobs and the line they were
// written on.
type StandardMeetings struct {
	Standard Ref
	Knobs    calendar.Knobs
	Line     int
}

// UnmarshalYAML decodes a calendar block, refusing a key it does not know.
func (c *Calendar) UnmarshalYAML(n *yaml.Node) error {
	type plain Calendar
	if err := decodeKnown(n, (*plain)(c), "meetings"); err != nil {
		return err
	}
	c.Line = n.Line
	return nil
}

var meetingKnobKeys = []string{"booking_rate", "slot_mix", "no_show_rate", "ended_early_rate", "ended_late_rate", "attendance", "walk_in_rate", "hours"}

// UnmarshalYAML reads a mapping of standard name to its meeting knobs, in
// order, refusing a knob it does not know.
func (ms *Meetings) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: calendar.meetings wants a mapping of standard to knobs, got a %s", n.Line, nodeKind(n))
	}
	out := make(Meetings, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		sm := StandardMeetings{Standard: Ref{Name: k.Value, Line: k.Line}, Line: k.Line}
		if err := decodeKnown(v, &sm.Knobs, meetingKnobKeys...); err != nil {
			return fmt.Errorf("calendar.meetings.%s: %w", k.Value, err)
		}
		out = append(out, sm)
	}
	*ms = out
	return nil
}

// MeetingKnobs is the meeting knobs the scenario declares for a standard.
func (m *Manifest) MeetingKnobs(standard string) (calendar.Knobs, bool) {
	if m == nil || m.Calendar == nil {
		return calendar.Knobs{}, false
	}
	for _, sm := range m.Calendar.Meetings {
		if sm.Standard.Name == standard {
			return sm.Knobs, true
		}
	}
	return calendar.Knobs{}, false
}

func checkCalendar(m *Manifest) []Problem {
	if m.Calendar == nil {
		return nil
	}
	var probs []Problem
	standards := map[string]bool{}
	for _, s := range m.Standards {
		standards[s.Name.Name] = true
	}
	seen := map[string]int{}
	for _, sm := range m.Calendar.Meetings {
		name := sm.Standard.Name
		field := "calendar.meetings." + name
		if !standards[name] {
			probs = append(probs, Problem{Line: sm.Line, Message: fmt.Sprintf("%s: %q is no standard of this scenario", field, name)})
			continue
		}
		if line, dup := seen[name]; dup {
			probs = append(probs, Problem{Line: sm.Line, Message: fmt.Sprintf("%s: the standard is given twice (first on line %d)", field, line)})
			continue
		}
		seen[name] = sm.Line
		if err := sm.Knobs.Validate(); err != nil {
			probs = append(probs, Problem{Line: sm.Line, Message: fmt.Sprintf("%s.%v", field, err)})
		}
	}
	return probs
}
