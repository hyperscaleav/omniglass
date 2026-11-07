package scenario

import (
	"fmt"
	"sort"

	"github.com/hyperscaleav/omniglass/internal/sim/fleet"
)

// MaxSystems bounds the size knob: the name generator's space of distinct
// labels is about 13,500, and the scale lab's largest target is 3,000 (#1357).
const MaxSystems = 10000

// Count is how many of a named thing: systems of a standard, or components
// of a product.
type Count struct {
	Name  string
	Count int
}

// Plan is a manifest resolved at a size: every site, every system in it and
// the components each takes, with totals per site, per archetype and for the
// whole plan. The bring-up (#1554) builds what a plan says; `lab run --plan`
// prints it.
type Plan struct {
	Scenario string
	Label    string
	// Size is the number of systems asked for, which Totals.Systems equals.
	Size       int
	Archetypes []ArchetypePlan
	Sites      []SitePlan
	Totals     Totals
}

// ArchetypePlan is one archetype of a plan: how many sites it placed, the
// floors each has, and their totals together.
type ArchetypePlan struct {
	Name   string
	Label  string
	Sites  int
	Floors int
	Totals Totals
}

// SitePlan is one site of a plan: its archetype, its buildings, its systems
// and its totals.
type SitePlan struct {
	Name      string
	Label     string
	Archetype string
	Buildings []BuildingPlan
	Systems   []SystemPlan
	Totals    Totals
}

// BuildingPlan is one building and its floor count.
type BuildingPlan struct {
	Name   string
	Floors int
}

// SystemPlan is one planned system: its name, its generated label, its
// standard and system type, the room it sits in, and its components.
type SystemPlan struct {
	Name       string
	Label      string
	Standard   string
	SystemType string
	Building   string
	Floor      string
	Room       string
	Components []Count
	// Staffing is which role each of those components fills: the seed
	// assigns each one to its role, so the system is staffed as its
	// standard declares.
	Staffing []Staff
}

// Staff is one role of a planned system and how many of which product fill
// it.
type Staff struct {
	Role    string
	Product string
	Count   int
}

// SiteNode is the name of the edge node a site's components are placed on:
// the scale lab runs one node per site (#1554), named for the site.
func SiteNode(site string) string { return "edge-" + site }

// Totals counts systems and components, systems per standard (in manifest
// order) and components per product (by name).
type Totals struct {
	Systems    int
	Components int
	Standards  []Count
	Products   []Count
}

// MinSystems is the smallest size the manifest resolves at: one system for
// each of its fewest sites.
func MinSystems(m *Manifest) int { return max(m.Sites.Min, 1) }

// Resolve validates the manifest and resolves it at size systems: the size
// picks the number of sites (SiteCount) and the repeated archetypes share the
// sites besides the headquarters by weight; each site takes one system and
// the rest are split across the sites by their archetype's typical size; each
// site splits its systems across its standards by ratio (every split by
// apportion); and each system lands in a room and takes a label. The same
// manifest, catalog and size give the same plan.
func Resolve(m *Manifest, cat Catalog, size int) (*Plan, error) {
	c, err := compile(m, cat)
	if err != nil {
		return nil, err
	}
	if lo := MinSystems(m); size < lo || size > MaxSystems {
		return nil, fmt.Errorf("scenario %s: %d systems is out of range; ask for %d to %d", m.Name, size, lo, MaxSystems)
	}

	type site struct {
		name, label string
		arch        *Archetype
	}
	var sites []site
	perArchetype := archetypeSites(m, SiteCount(m, size))
	for i := range m.Archetypes {
		a := &m.Archetypes[i]
		if a.Headquarters {
			sites = append(sites, site{name: a.Name.Name, label: a.Label, arch: a})
			continue
		}
		for k := 1; k <= perArchetype[i]; k++ {
			sites = append(sites, site{name: fmt.Sprintf("%s-%02d", a.Name.Name, k), label: siteLabel(a, k), arch: a})
		}
	}
	weights := make([]int, len(sites))
	for i, s := range sites {
		weights[i] = s.arch.TypicalSystems
	}
	perSite := apportion(size-len(sites), weights)

	names := fleet.NewNameGenerator()
	plan := &Plan{Scenario: m.Name, Label: m.Label, Size: size}
	byArchetype := map[string][]SystemPlan{}
	for i, s := range sites {
		sp := resolveSite(m.Name, s.name, s.label, s.arch, perSite[i]+1, c, names)
		sp.Totals = count(m, sp.Systems)
		plan.Sites = append(plan.Sites, sp)
		byArchetype[sp.Archetype] = append(byArchetype[sp.Archetype], sp.Systems...)
	}
	for i, a := range m.Archetypes {
		n := perArchetype[i]
		if a.Headquarters {
			n = 1
		}
		floors := 0
		for _, b := range a.Buildings {
			floors += b.Floors
		}
		plan.Archetypes = append(plan.Archetypes, ArchetypePlan{Name: a.Name.Name, Label: a.Label, Sites: n,
			Floors: floors, Totals: count(m, byArchetype[a.Name.Name])})
	}
	plan.Totals = count(m, plan.allSystems())
	return plan, nil
}

// SiteCount is how many sites the manifest places at size systems: the
// headquarters, plus one site for each average repeated site's worth of
// systems beyond the headquarters' typical size, rounded half up and clamped
// to the manifest's site bounds. The average repeated site is the repeated
// archetypes' typical sizes weighted by their weights. It never falls as the
// size grows. The manifest is valid.
func SiteCount(m *Manifest, size int) int {
	weight, systems, hq := 0, 0, 0
	for _, a := range m.Archetypes {
		if a.Headquarters {
			hq = a.TypicalSystems
			continue
		}
		weight += a.Weight
		systems += a.Weight * a.TypicalSystems
	}
	n := 1
	if beyond := size - hq; beyond > 0 && systems > 0 {
		// beyond / (systems/weight), rounded half up, in whole numbers.
		n += (2*beyond*weight + systems) / (2 * systems)
	}
	return min(max(n, m.Sites.Min), m.Sites.Max)
}

// archetypeSites is how many sites each archetype has, in manifest order,
// when the plan has n sites: one headquarters, and the rest shared among the
// repeated archetypes by weight.
func archetypeSites(m *Manifest, n int) []int {
	var repeated, weights []int
	for i, a := range m.Archetypes {
		if !a.Headquarters {
			repeated = append(repeated, i)
			weights = append(weights, a.Weight)
		}
	}
	out := make([]int, len(m.Archetypes))
	for j, k := range apportion(n-1, weights) {
		out[repeated[j]] = k
	}
	for i, a := range m.Archetypes {
		if a.Headquarters {
			out[i] = 1
		}
	}
	return out
}

func siteLabel(a *Archetype, k int) string {
	if a.Label == "" {
		return ""
	}
	return fmt.Sprintf("%s %02d", a.Label, k)
}

// resolveSite splits a site's systems across its archetype's standards and
// places each one. Systems are placed in the archetype's standard order, then
// by index: system j lands on floor j mod F of the site's F floors (buildings
// in order, floors bottom up) in that floor's room j/F+1, so every floor
// fills evenly.
func resolveSite(scenario, name, label string, a *Archetype, n int, c *compiled, names *fleet.NameGenerator) SitePlan {
	sp := SitePlan{Name: name, Label: label, Archetype: a.Name.Name}
	type floor struct{ building, name string }
	var floors []floor
	for _, b := range a.Buildings {
		building := name + "-" + b.Name.Name
		sp.Buildings = append(sp.Buildings, BuildingPlan{Name: building, Floors: b.Floors})
		for f := 1; f <= b.Floors; f++ {
			floors = append(floors, floor{building: building, name: fmt.Sprintf("%s-%d", building, f)})
		}
	}

	ratios := make([]int, len(a.Systems))
	for i, r := range a.Systems {
		ratios[i] = r.Weight
	}
	perStandard := apportion(n, ratios)

	j := 0
	for i, r := range a.Systems {
		cs := c.standards[r.Standard.Name]
		for k := 1; k <= perStandard[i]; k++ {
			fl := floors[j%len(floors)]
			sp.Systems = append(sp.Systems, SystemPlan{
				Name:       fmt.Sprintf("%s-%s-%d", name, cs.name, k),
				Label:      names.Next(fmt.Sprintf("%s/%s/%s/%d", scenario, name, cs.name, k)),
				Standard:   cs.name,
				SystemType: cs.systemType,
				Building:   fl.building,
				Floor:      fl.name,
				Room:       fmt.Sprintf("%s-%02d", fl.name, j/len(floors)+1),
				Components: cs.components,
				Staffing:   cs.staffing,
			})
			j++
		}
	}
	return sp
}

func (p *Plan) allSystems() []SystemPlan {
	var out []SystemPlan
	for i := range p.Sites {
		out = append(out, p.Sites[i].Systems...)
	}
	return out
}

// count totals systems per standard (manifest order) and components per
// product (by name).
func count(m *Manifest, systems []SystemPlan) Totals {
	t := Totals{Systems: len(systems)}
	perStandard := map[string]int{}
	perProduct := map[string]int{}
	for _, s := range systems {
		perStandard[s.Standard]++
		for _, c := range s.Components {
			perProduct[c.Name] += c.Count
			t.Components += c.Count
		}
	}
	for _, s := range m.Standards {
		t.Standards = append(t.Standards, Count{Name: s.Name.Name, Count: perStandard[s.Name.Name]})
	}
	products := make([]string, 0, len(perProduct))
	for p := range perProduct {
		products = append(products, p)
	}
	sort.Strings(products)
	for _, p := range products {
		t.Products = append(t.Products, Count{Name: p, Count: perProduct[p]})
	}
	return t
}

// apportion splits total across weights by the largest remainder (Hamilton)
// method, in exact integer arithmetic: each share takes the whole part of
// total*w/W, and the seats left over go one each to the largest remainders,
// a tie going to the earlier weight. Every share is within one of its exact
// quota, and the shares sum to total. Weights are positive.
func apportion(total int, weights []int) []int {
	sum := 0
	for _, w := range weights {
		sum += w
	}
	out := make([]int, len(weights))
	if sum == 0 {
		return out
	}
	rem := make([]int, len(weights))
	left := total
	for i, w := range weights {
		out[i] = total * w / sum
		rem[i] = total * w % sum
		left -= out[i]
	}
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return rem[order[a]] > rem[order[b]] })
	for _, i := range order[:left] {
		out[i]++
	}
	return out
}
