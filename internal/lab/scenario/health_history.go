package scenario

import (
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// HealthHistory is a scenario's health storyline (#2728): the past a lab seeded
// with --health-history carries. Every system of the plan reads healthy across
// the window of Days, ending as the lab starts, except the Stretches: the times
// a named room spent away from healthy, each with the alarm that explained it.
// It is hand-shaped, labelled synthetic when it lands (ADR-2726), until epic
// #1796 derives a lab's past through the real rules on a virtual clock.
type HealthHistory struct {
	Days      int       `yaml:"days"`
	Stretches []Stretch `yaml:"stretches"`
	Line      int       `yaml:"-"`
}

// Stretch is one time a system spent away from healthy: Health from DaysAgo
// days before the lab started, for Minutes, explained by an alarm the named
// event Rule raised on the system's Component (on its endpoint Instance) at
// Severity, raised as the stretch began and cleared as it ended.
type Stretch struct {
	System    Ref    `yaml:"system"`
	Component Ref    `yaml:"component"`
	Rule      Ref    `yaml:"rule"`
	Instance  string `yaml:"instance"`
	Severity  string `yaml:"severity"`
	Message   string `yaml:"message"`
	Health    string `yaml:"health"`
	DaysAgo   int    `yaml:"days_ago"`
	Minutes   int    `yaml:"minutes"`
	Line      int    `yaml:"-"`
}

// MaxHealthHistoryDays is the longest window a storyline may span: the health
// read's default window, so a seeded past is all on screen.
const MaxHealthHistoryDays = 30

// UnmarshalYAML decodes a storyline and keeps its line.
func (h *HealthHistory) UnmarshalYAML(n *yaml.Node) error {
	type plain HealthHistory
	if err := decodeKnown(n, (*plain)(h), "days", "stretches"); err != nil {
		return err
	}
	h.Line = n.Line
	return nil
}

// UnmarshalYAML decodes a stretch and keeps its line.
func (s *Stretch) UnmarshalYAML(n *yaml.Node) error {
	type plain Stretch
	if err := decodeKnown(n, (*plain)(s), "system", "component", "rule", "instance", "severity", "message", "health", "days_ago", "minutes"); err != nil {
		return err
	}
	s.Line = n.Line
	return nil
}

// Window is the stretch's start and end, given when the lab started.
func (s Stretch) Window(end time.Time) (time.Time, time.Time) {
	from := end.Add(-time.Duration(s.DaysAgo) * 24 * time.Hour)
	return from, from.Add(time.Duration(s.Minutes) * time.Minute)
}

func checkHealthHistory(m *Manifest) []Problem {
	h := m.HealthHistory
	if h == nil {
		return nil
	}
	var probs []Problem
	add := func(line int, format string, args ...any) {
		probs = append(probs, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	if h.Days < 1 || h.Days > MaxHealthHistoryDays {
		add(h.Line, "health_history days %d is not 1 to %d", h.Days, MaxHealthHistoryDays)
	}
	type span struct {
		from, to time.Time
		line     int
	}
	end := time.Unix(0, 0)
	bySystem := map[string][]span{}
	for _, s := range h.Stretches {
		switch {
		case s.System.Name == "":
			add(s.Line, "a health_history stretch names no system")
			continue
		case s.Component.Name == "" || s.Rule.Name == "" || s.Severity == "" || s.Message == "":
			add(s.Line, "the stretch on %q names no component, rule, severity or message", s.System.Name)
		}
		switch s.Health {
		case "degraded", "outage", "incomplete":
		default:
			add(s.Line, "the stretch on %q has health %q, not degraded, outage or incomplete", s.System.Name, s.Health)
		}
		if s.DaysAgo < 1 || s.DaysAgo >= h.Days {
			add(s.Line, "the stretch on %q is days_ago %d, not inside the %d-day window", s.System.Name, s.DaysAgo, h.Days)
		}
		if s.Minutes < 1 {
			add(s.Line, "the stretch on %q lasts minutes %d", s.System.Name, s.Minutes)
		}
		from, to := s.Window(end)
		bySystem[s.System.Name] = append(bySystem[s.System.Name], span{from, to, s.Line})
	}
	for name, spans := range bySystem {
		sort.Slice(spans, func(i, j int) bool { return spans[i].from.Before(spans[j].from) })
		for i := 1; i < len(spans); i++ {
			if !spans[i].from.After(spans[i-1].to) {
				add(spans[i].line, "the stretch on %q overlaps the one on line %d", name, spans[i-1].line)
			}
		}
	}
	return probs
}

// MissingStorylineSystems names the systems a storyline names that this plan
// does not have, so a lab seeded at a size without them refuses before it seeds.
func (p *Plan) MissingStorylineSystems(h *HealthHistory) []string {
	if h == nil {
		return nil
	}
	have := map[string]bool{}
	for _, site := range p.Sites {
		for _, s := range site.Systems {
			have[s.Name] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range h.Stretches {
		if !have[s.System.Name] && !seen[s.System.Name] {
			out = append(out, s.System.Name)
			seen[s.System.Name] = true
		}
	}
	return out
}
