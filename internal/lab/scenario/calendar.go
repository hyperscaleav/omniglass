package scenario

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// Calendar is a scenario's lab calendar block (#2757): what every simulated
// room acts out on the simulator's clock: its meetings, as knobs per
// standard, and the recurring problems the scenario scripts (#2769), each a
// cadence, a window, a scope and an impairment. The calendar itself is derived
// from these and a seed (sim/calendar), never written out, so every process
// computes the same one.
type Calendar struct {
	Meetings Meetings `yaml:"meetings"`
	Problems Problems `yaml:"problems"`
	Line     int      `yaml:"-"`
}

// Problems are the scripted problems, in the order written.
type Problems []ScriptedProblem

// ScriptedProblem is one scripted problem and the line it was written on.
type ScriptedProblem struct {
	calendar.ProblemSpec
	Line int
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
	if err := decodeKnown(n, (*plain)(c), "meetings", "problems"); err != nil {
		return err
	}
	c.Line = n.Line
	return nil
}

var meetingKnobKeys = []string{"booking_rate", "slot_mix", "no_show_rate", "ended_early_rate", "ended_late_rate", "attendance", "walk_in_rate", "hours", "recording_rate", "wake_lead_seconds", "cooldown_seconds", "recording_tail_seconds", "sharing"}

var sharingKeys = []string{"min_minutes", "parts", "from_part", "to_part"}

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
		if sh := mapValue(v, "sharing"); sh != nil {
			if err := knownKeys(sh, sharingKeys); err != nil {
				return fmt.Errorf("calendar.meetings.%s.sharing: %w", k.Value, err)
			}
		}
		out = append(out, sm)
	}
	*ms = out
	return nil
}

var (
	problemKeys    = []string{"name", "cadence", "weekday", "day", "nth", "at", "for", "spread", "scope", "impairment"}
	scopeKeys      = []string{"standard", "site", "rooms", "role"}
	impairmentKeys = []string{"unplugged", "loss_pct", "pin"}
	pinKeys        = []string{"path", "value"}
)

// UnmarshalYAML reads a list of problems, refusing a key it does not know at
// any depth.
func (ps *Problems) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: calendar.problems wants a list of problems, got a %s", n.Line, nodeKind(n))
	}
	out := make(Problems, 0, len(n.Content))
	for i, item := range n.Content {
		p := ScriptedProblem{Line: item.Line}
		if err := decodeKnown(item, &p.ProblemSpec, problemKeys...); err != nil {
			return fmt.Errorf("calendar.problems[%d]: %w", i, err)
		}
		for _, nested := range []struct {
			key  string
			keys []string
		}{{"scope", scopeKeys}, {"impairment", impairmentKeys}} {
			if v := mapValue(item, nested.key); v != nil {
				if err := knownKeys(v, nested.keys); err != nil {
					return fmt.Errorf("calendar.problems[%d].%s: %w", i, nested.key, err)
				}
			}
		}
		if imp := mapValue(item, "impairment"); imp != nil {
			if pin := mapValue(imp, "pin"); pin != nil {
				if err := knownKeys(pin, pinKeys); err != nil {
					return fmt.Errorf("calendar.problems[%d].impairment.pin: %w", i, err)
				}
			}
		}
		out = append(out, p)
	}
	*ps = out
	return nil
}

// mapValue is the value a mapping node holds under key, nil when it has none.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func knownKeys(n *yaml.Node, keys []string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: want a mapping, got a %s", n.Line, nodeKind(n))
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := n.Content[i]; !slices.Contains(keys, k.Value) {
			return fmt.Errorf("line %d: unknown field %q (want one of %s)", k.Line, k.Value, strings.Join(keys, ", "))
		}
	}
	return nil
}

// ScriptedProblems is the problems the scenario scripts, as the calendar
// reads them.
func (m *Manifest) ScriptedProblems() []calendar.ProblemSpec {
	if m == nil || m.Calendar == nil {
		return nil
	}
	out := make([]calendar.ProblemSpec, len(m.Calendar.Problems))
	for i, p := range m.Calendar.Problems {
		out[i] = p.ProblemSpec
	}
	return out
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
	return append(probs, checkProblems(m)...)
}

// checkProblems holds each scripted problem to what the scenario declares: a
// spec that validates, a name used once, a scope that names a standard and a
// site the scenario has, and a role that is a slot of the standards in scope.
func checkProblems(m *Manifest) []Problem {
	var probs []Problem
	slots := map[string][]string{}
	for _, s := range m.Standards {
		for slot := range s.Products {
			slots[s.Name.Name] = append(slots[s.Name.Name], slot)
		}
	}
	seen := map[string]int{}
	for _, p := range m.Calendar.Problems {
		field := "calendar.problems." + p.Name
		add := func(format string, args ...any) {
			probs = append(probs, Problem{Line: p.Line, Message: field + ": " + fmt.Sprintf(format, args...)})
		}
		if err := p.Validate(); err != nil {
			add("%v", err)
			continue
		}
		if line, dup := seen[p.Name]; dup {
			add("the problem is scripted twice (first on line %d)", line)
			continue
		}
		seen[p.Name] = p.Line
		sc := p.Scope
		if sc.Standard != "" {
			if _, ok := slots[sc.Standard]; !ok {
				add("scope.standard %q is no standard of this scenario", sc.Standard)
				continue
			}
		}
		if sc.Site != "" {
			found := false
			for _, a := range m.Archetypes {
				found = found || sc.Site == a.Name.Name || strings.HasPrefix(sc.Site, a.Name.Name+"-")
			}
			if !found {
				add("scope.site %q is neither an archetype nor a site of one", sc.Site)
				continue
			}
		}
		roleOK := false
		for std, ss := range slots {
			if sc.Standard == "" || sc.Standard == std {
				roleOK = roleOK || slices.Contains(ss, sc.Role)
			}
		}
		if !roleOK {
			add("scope.role %q is a slot of no standard in scope", sc.Role)
		}
	}
	return probs
}
