package simfleet

import (
	"fmt"
	"sort"
	"strings"
)

// Role is one role of a standard, as the system planner reads it: how many
// it needs, the component types it accepts (empty is any), the products it
// is pinned to (empty is any of an accepted type), and the choice alternate
// it belongs to ("choice/alternate", empty when unconditional).
type Role struct {
	Name           string
	Quorum         int
	Capacity       *int
	AcceptedTypes  []string
	PinnedProducts []string
	Alternate      string
}

// Product is a catalog product and its component type.
type Product struct {
	Name          string
	ComponentType string
}

// SystemCatalog is what the system planner consults: whether the simulator
// can serve a product, and whether a component type falls within an
// accepted type (self-inclusive).
type SystemCatalog struct {
	Simulated func(product string) bool
	Within    func(componentType, accepted string) bool
}

// RoleFill is the plan for one role: the product and how many of it, or,
// when nothing simulated fits, how many it is short and why.
type RoleFill struct {
	Role      string
	Alternate string
	Product   string
	Count     int
	Missing   int
	Reason    string
}

// SystemPlan is a standard staffed with simulated products: a fill per role
// taken, and the alternate chosen for each choice.
type SystemPlan struct {
	Roles      []RoleFill
	Alternates map[string]string
}

// Components is how many simulated components the plan creates.
func (p SystemPlan) Components() int {
	n := 0
	for _, f := range p.Roles {
		n += f.Count
	}
	return n
}

// PlanSystem staffs a standard's roles with simulated products. Each role
// takes the first simulated product that fits it (its pins in the order
// given, else products by name), as many as its quorum and at least one, capped at its
// capacity; a role nothing simulated fits is left unstaffed, short its
// quorum, with the reason. Of each choice it takes one alternate: the first
// by name whose roles can all be staffed, else the one staffing the most.
// Pure: the catalog comes in, the plan goes out.
func PlanSystem(roles []Role, products []Product, cat SystemCatalog) SystemPlan {
	sorted := append([]Product(nil), products...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	byName := make(map[string]Product, len(sorted))
	for _, p := range sorted {
		byName[p.Name] = p
	}
	fill := func(r Role) RoleFill {
		f := RoleFill{Role: r.Name, Alternate: r.Alternate}
		want := r.Quorum
		if want < 1 {
			want = 1
		}
		if r.Capacity != nil && *r.Capacity < want {
			want = *r.Capacity
		}
		fits := func(p Product) bool {
			if !cat.Simulated(p.Name) {
				return false
			}
			if len(r.AcceptedTypes) == 0 {
				return true
			}
			for _, t := range r.AcceptedTypes {
				if cat.Within(p.ComponentType, t) {
					return true
				}
			}
			return false
		}
		if len(r.PinnedProducts) > 0 {
			for _, name := range r.PinnedProducts {
				if p, ok := byName[name]; ok && fits(p) {
					f.Product, f.Count = p.Name, want
					return f
				}
			}
			f.Missing = want
			simulated := []string{}
			for _, name := range r.PinnedProducts {
				if cat.Simulated(name) {
					simulated = append(simulated, name)
				}
			}
			if len(simulated) == 0 {
				f.Reason = fmt.Sprintf("no pinned product (%s) is simulated", strings.Join(r.PinnedProducts, ", "))
			} else {
				f.Reason = fmt.Sprintf("the simulated pinned products (%s) are not a %s", strings.Join(simulated, ", "), strings.Join(r.AcceptedTypes, " or "))
			}
			return f
		}
		for _, p := range sorted {
			if fits(p) {
				f.Product, f.Count = p.Name, want
				return f
			}
		}
		f.Missing = want
		if len(r.AcceptedTypes) > 0 {
			f.Reason = fmt.Sprintf("no simulated product is %s %s", article(r.AcceptedTypes[0]), strings.Join(r.AcceptedTypes, " or "))
		} else {
			f.Reason = "no product is simulated"
		}
		return f
	}

	plan := SystemPlan{Alternates: map[string]string{}}
	// Group alternates by choice to pick one per choice.
	type alt struct {
		name  string
		fills []RoleFill
	}
	choices := map[string]map[string]*alt{}
	for _, r := range roles {
		if r.Alternate == "" {
			continue
		}
		choice, name, _ := strings.Cut(r.Alternate, "/")
		if choices[choice] == nil {
			choices[choice] = map[string]*alt{}
		}
		a := choices[choice][name]
		if a == nil {
			a = &alt{name: name}
			choices[choice][name] = a
		}
		a.fills = append(a.fills, fill(r))
	}
	for choice, alts := range choices {
		names := make([]string, 0, len(alts))
		for n := range alts {
			names = append(names, n)
		}
		sort.Strings(names)
		best, bestStaffed := "", -1
		for _, n := range names {
			staffed, full := 0, true
			for _, f := range alts[n].fills {
				if f.Count > 0 {
					staffed++
				} else {
					full = false
				}
			}
			if full {
				best = n
				break
			}
			if staffed > bestStaffed {
				best, bestStaffed = n, staffed
			}
		}
		plan.Alternates[choice] = best
	}
	for _, r := range roles {
		if r.Alternate != "" {
			choice, name, _ := strings.Cut(r.Alternate, "/")
			if plan.Alternates[choice] != name {
				continue
			}
		}
		plan.Roles = append(plan.Roles, fill(r))
	}
	return plan
}

// article is "a" or "an" for a component type name.
func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}
