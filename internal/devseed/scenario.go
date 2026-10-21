package devseed

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A scenario is a named fleet `seed-dev --scenario` can stand up, so a
// throwaway stack can come up as the situation a test or an exploratory run
// needs (#964): a fresh install, the example fleet, that fleet in trouble, or
// that fleet at enterprise size. Every scenario other than the fresh install is
// the example fleet plus a pure expansion of it, never a second hand-written
// fixture: the expansions read the products, standards, roles and location
// types off the fixture itself, so they cannot drift from the boot seed or name
// a location type in code (location types are customer data).

// Scenario is one named fleet.
type Scenario struct {
	Name    string
	Summary string
	// Empty seeds nothing beyond what migrate and the boot seed already
	// installed: the first run of a fresh install.
	Empty  bool
	expand func(Doc) Doc
}

// LargeFleet is the large scenario's shape: the example fleet's self-contained
// rooms, copied under that many replica sites, that many times each.
var LargeFleet = ReplicaParams{Sites: 5, RoomCopies: 6}

// DegradedEvery is the degraded scenario's density: an alarm on every n-th
// component that has none.
const DegradedEvery = 3

var scenarios = []Scenario{
	{Name: "dev", Summary: "the example fleet `make dev` comes up with"},
	{Name: "empty", Summary: "nothing beyond the boot seed: a fresh install's first run", Empty: true},
	{
		Name:    "degraded",
		Summary: "the example fleet with an open alarm on one in every " + strconv.Itoa(DegradedEvery) + " components",
		expand:  func(d Doc) Doc { return Degrade(d, DegradedEvery) },
	},
	{
		Name: "large",
		Summary: fmt.Sprintf("the example fleet plus its self-contained rooms copied %d times under each of %d replica sites (only the example fleet is simulated)",
			LargeFleet.RoomCopies, LargeFleet.Sites),
		expand: func(d Doc) Doc { return Replicate(d, LargeFleet) },
	},
}

// Scenarios lists every scenario, the default first.
func Scenarios() []Scenario {
	out := make([]Scenario, len(scenarios))
	copy(out, scenarios)
	return out
}

// LookupScenario finds a scenario by name; an unknown name is refused with the
// names that exist, so the error is the help.
func LookupScenario(name string) (Scenario, error) {
	names := make([]string, 0, len(scenarios))
	for _, s := range scenarios {
		if s.Name == name {
			return s, nil
		}
		names = append(names, s.Name)
	}
	return Scenario{}, fmt.Errorf("devseed: unknown scenario %q (known: %s)", name, strings.Join(names, ", "))
}

// Doc is the fixture this scenario seeds: empty for the fresh install, else the
// example fleet run through the scenario's expansion.
func (s Scenario) Doc() (Doc, error) {
	if s.Empty {
		return Doc{}, nil
	}
	doc, err := Fixtures()
	if err != nil {
		return Doc{}, err
	}
	if s.expand != nil {
		doc = s.expand(doc)
	}
	return doc, nil
}

// RunScenario seeds one scenario idempotently. The expansions only append rows
// to the example fleet, so everything Run seeds around the fixture (the worked
// telemetry, the reachability demo, the maps) lands exactly as it does for dev.
func RunScenario(ctx context.Context, gw storage.Gateway, actorID string, s Scenario, opts Options) error {
	if s.Empty {
		return nil
	}
	doc, err := s.Doc()
	if err != nil {
		return err
	}
	return runDoc(ctx, gw, actorID, doc, opts)
}

// ReplicaParams sizes a Replicate.
type ReplicaParams struct {
	Sites      int
	RoomCopies int
}

// Replicate returns base plus copies of its self-contained rooms: under each of
// p.Sites replica sites (a copy of every location above a template room, each
// renamed for its site), p.RoomCopies copies of every template room with its
// components, systems, memberships and staffing. Copied names carry the site
// and copy number, so every location name stays fleet-unique (the seed finds an
// existing location by bare name). Components and systems stay nameless when
// the template's are, so the platform mints their names exactly as it does for
// the example fleet. Pure: the same base and params give the same document.
func Replicate(base Doc, p ReplicaParams) Doc {
	out := base
	out.Locations = append([]Location(nil), base.Locations...)
	out.Components = append([]Component(nil), base.Components...)
	out.Systems = append([]System(nil), base.Systems...)
	out.Members = append([]Member(nil), base.Members...)
	out.RoleAssignments = append([]RoleAssignment(nil), base.RoleAssignments...)

	rooms := selfContainedRooms(base)
	if len(rooms) == 0 || p.Sites <= 0 || p.RoomCopies <= 0 {
		return out
	}
	isRoom := map[string]bool{}
	for _, r := range rooms {
		isRoom[r] = true
	}
	parent := map[string]string{}
	for _, l := range base.Locations {
		parent[l.Key] = l.Parent
	}
	above := map[string]bool{}
	for _, r := range rooms {
		for k := parent[r]; k != ""; k = parent[k] {
			above[k] = true
		}
	}
	sysAt := map[string]string{}
	for _, s := range base.Systems {
		sysAt[s.Key] = s.Location
	}

	for site := 1; site <= p.Sites; site++ {
		tag := fmt.Sprintf("s%03d", site)
		siteKey := func(k string) string { return tag + "/" + k }
		for _, l := range base.Locations {
			if !above[l.Key] {
				continue
			}
			c := l
			c.Key, c.Name, c.Label = siteKey(l.Key), l.Name+"-"+tag, suffixLabel(l.Label, strconv.Itoa(site))
			if l.Parent != "" {
				c.Parent = siteKey(l.Parent)
			}
			out.Locations = append(out.Locations, c)
		}
		for _, l := range base.Locations {
			if !isRoom[l.Key] {
				continue
			}
			for n := 1; n <= p.RoomCopies; n++ {
				num := strconv.Itoa(n)
				copyKey := func(k string) string { return tag + "/" + num + "/" + k }
				room := l
				room.Key, room.Name, room.Label = copyKey(l.Key), l.Name+"-"+tag+"-"+num, suffixLabel(l.Label, num)
				if l.Parent != "" {
					room.Parent = siteKey(l.Parent)
				}
				out.Locations = append(out.Locations, room)
				for _, c := range base.Components {
					if c.Location != l.Key {
						continue
					}
					cc := c
					cc.Key, cc.Location = copyKey(c.Key), room.Key
					if c.Name != "" {
						cc.Name = c.Name + "-" + tag + "-" + num
					}
					out.Components = append(out.Components, cc)
				}
				for _, s := range base.Systems {
					if s.Location != l.Key {
						continue
					}
					sc := s
					sc.Key, sc.Location = copyKey(s.Key), room.Key
					if s.Name != "" {
						sc.Name = s.Name + "-" + tag + "-" + num
					}
					out.Systems = append(out.Systems, sc)
				}
				for _, m := range base.Members {
					if sysAt[m.System] == l.Key {
						out.Members = append(out.Members, Member{System: copyKey(m.System), Component: copyKey(m.Component)})
					}
				}
				for _, ra := range base.RoleAssignments {
					if sysAt[ra.System] == l.Key {
						out.RoleAssignments = append(out.RoleAssignments, RoleAssignment{System: copyKey(ra.System), Role: ra.Role, Component: copyKey(ra.Component)})
					}
				}
			}
		}
	}
	return out
}

// suffixLabel numbers a copied label; an absent label stays absent, so the
// label rule renders the copy from its name as it does the original.
func suffixLabel(label, n string) string {
	if label == "" {
		return ""
	}
	return label + " " + n
}

// selfContainedRooms are the locations Replicate can copy whole: a leaf of the
// tree that holds at least one system, where every system there is staffed and
// joined only by components placed there, and no component placed there serves
// a system elsewhere. A room whose devices serve a neighbour (the divisible
// pair's shared rack) is left out, since a copy of half of it would be a
// different fleet. Ordered as the fixture declares them.
func selfContainedRooms(doc Doc) []string {
	hasChild := map[string]bool{}
	for _, l := range doc.Locations {
		if l.Parent != "" {
			hasChild[l.Parent] = true
		}
	}
	compAt := map[string]string{}
	for _, c := range doc.Components {
		compAt[c.Key] = c.Location
	}
	sysAt := map[string]string{}
	holdsSystem := map[string]bool{}
	for _, s := range doc.Systems {
		sysAt[s.Key] = s.Location
		holdsSystem[s.Location] = true
	}
	broken := map[string]bool{}
	link := func(system, component string) {
		if s, c := sysAt[system], compAt[component]; s != c {
			broken[s], broken[c] = true, true
		}
	}
	for _, m := range doc.Members {
		link(m.System, m.Component)
	}
	for _, ra := range doc.RoleAssignments {
		link(ra.System, ra.Component)
	}
	var out []string
	for _, l := range doc.Locations {
		if holdsSystem[l.Key] && !hasChild[l.Key] && !broken[l.Key] {
			out = append(out, l.Key)
		}
	}
	return out
}

// Degrade returns base plus an open alarm on every n-th component (in fixture
// order) that has none, so most rooms carry a fault and the fleet reads as a bad
// day rather than a quiet one. The severities and messages cycle through the
// ones the example fleet's own alarms use, so a severity is never named here
// that the boot seed does not ship. Pure; a re-run raises the same dedup keys,
// which the seed's alarm write already treats as standing.
func Degrade(base Doc, n int) Doc {
	out := base
	out.Alarms = append([]Alarm(nil), base.Alarms...)
	if n <= 0 || len(base.Alarms) == 0 {
		return out
	}
	alarmed := map[string]bool{}
	for _, a := range base.Alarms {
		alarmed[a.Component] = true
	}
	k := 0
	for i, c := range base.Components {
		if i%n != 0 || alarmed[c.Key] {
			continue
		}
		tmpl := base.Alarms[k%len(base.Alarms)]
		out.Alarms = append(out.Alarms, Alarm{
			Component: c.Key, Severity: tmpl.Severity, Message: tmpl.Message,
			DedupKey: "degraded-" + tmpl.DedupKey,
		})
		k++
	}
	return out
}
