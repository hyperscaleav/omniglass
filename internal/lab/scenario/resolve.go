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
// the components each takes, with totals. The bring-up (#1554) builds what a
// plan says; `lab run --plan` prints it.
type Plan struct {
	Scenario string
	Label    string
	// Size is the number of systems asked for, which Totals.Systems equals.
	Size   int
	Sites  []SitePlan
	Totals Totals
}

// SitePlan is one site of a plan: its buildings, its systems and its totals.
type SitePlan struct {
	Name      string
	Label     string
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
}

// Totals counts systems and components, systems per standard (in manifest
// order) and components per product (by name).
type Totals struct {
	Systems    int
	Components int
	Standards  []Count
	Products   []Count
}

// Resolve validates the manifest and resolves it at size systems: the sites
// split the size by weight, each site splits its share across its standards
// by ratio (both by apportion), and each system lands in a room and takes a
// label. The same manifest, catalog and size give the same plan.
func Resolve(m *Manifest, cat Catalog, size int) (*Plan, error) {
	if size < 1 || size > MaxSystems {
		return nil, fmt.Errorf("scenario %s: %d systems is out of range; ask for 1 to %d", m.Name, size, MaxSystems)
	}
	c, err := compile(m, cat)
	if err != nil {
		return nil, err
	}

	weights := make([]int, len(m.Sites))
	for i, s := range m.Sites {
		weights[i] = s.Weight
	}
	perSite := apportion(size, weights)

	names := fleet.NewNameGenerator()
	plan := &Plan{Scenario: m.Name, Label: m.Label, Size: size}
	for i, site := range m.Sites {
		sp := resolveSite(m.Name, site, perSite[i], c, names)
		sp.Totals = count(m, sp.Systems)
		plan.Sites = append(plan.Sites, sp)
	}
	plan.Totals = count(m, plan.allSystems())
	return plan, nil
}

// resolveSite splits a site's systems across its standards and places each
// one. Systems are placed in the site's standard order, then by index:
// system j lands on floor j mod F of the site's F floors (buildings in
// order, floors bottom up) in that floor's room j/F+1, so every floor fills
// evenly.
func resolveSite(scenario string, site Site, n int, c *compiled, names *fleet.NameGenerator) SitePlan {
	sp := SitePlan{Name: site.Name.Name, Label: site.Label}
	type floor struct{ building, name string }
	var floors []floor
	for _, b := range site.Buildings {
		building := site.Name.Name + "-" + b.Name.Name
		sp.Buildings = append(sp.Buildings, BuildingPlan{Name: building, Floors: b.Floors})
		for f := 1; f <= b.Floors; f++ {
			floors = append(floors, floor{building: building, name: fmt.Sprintf("%s-%d", building, f)})
		}
	}

	ratios := make([]int, len(site.Systems))
	for i, r := range site.Systems {
		ratios[i] = r.Weight
	}
	perStandard := apportion(n, ratios)

	j := 0
	for i, r := range site.Systems {
		cs := c.standards[r.Standard.Name]
		for k := 1; k <= perStandard[i]; k++ {
			fl := floors[j%len(floors)]
			sp.Systems = append(sp.Systems, SystemPlan{
				Name:       fmt.Sprintf("%s-%s-%d", site.Name.Name, cs.name, k),
				Label:      names.Next(fmt.Sprintf("%s/%s/%s/%d", scenario, site.Name.Name, cs.name, k)),
				Standard:   cs.name,
				SystemType: cs.systemType,
				Building:   fl.building,
				Floor:      fl.name,
				Room:       fmt.Sprintf("%s-%02d", fl.name, j/len(floors)+1),
				Components: cs.components,
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
