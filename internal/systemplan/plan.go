// Package systemplan is the pure half of the planned-system primitive
// (#827, ADR-0827): it staffs a standard's slots from a set of candidate
// products and says which alternate each choice takes, touching nothing.
// The simulator plans with the products it serves (internal/simfleet), a
// system template with the product it names per slot; the transactional
// half is storage.CreatePlan, which lands what a plan says.
package systemplan

import (
	"fmt"
	"sort"
	"strings"
)

// Slot is one slot of a standard, as the system planner reads it: how many
// it needs, the component types it accepts (empty is any), the products it
// is pinned to (empty is any of an accepted type), and the choice alternate
// it belongs to ("choice/alternate", empty when unconditional).
type Slot struct {
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

// Catalog is what the planner consults: which products are candidates for a
// slot, and whether a component type falls within an accepted type
// (self-inclusive). Noun names the candidates in a shortfall's reason
// ("simulated" for the simulator's, "template" for a template's).
type Catalog struct {
	Candidate func(slot, product string) bool
	Within    func(componentType, accepted string) bool
	Noun      string
}

// SlotFill is the plan for one slot: the product and how many of it, or,
// when nothing simulated fits, how many it is short and why.
type SlotFill struct {
	Slot      string
	Alternate string
	Product   string
	Count     int
	Missing   int
	Reason    string
}

// SystemPlan is a standard staffed with candidate products: a fill per slot
// taken, and the alternate chosen for each choice.
type SystemPlan struct {
	Slots      []SlotFill
	Alternates map[string]string
}

// Components is how many components the plan creates.
func (p SystemPlan) Components() int {
	n := 0
	for _, f := range p.Slots {
		n += f.Count
	}
	return n
}

// PlanSystem staffs a standard's slots with candidate products. Each slot
// takes the first candidate product that fits it (its pins in the order
// given, else products by name), as many as its quorum and at least one,
// capped at its capacity; a slot no candidate fits is left unstaffed, short
// its quorum, with the reason. Of each choice it takes one alternate: the
// first by name whose slots can all be staffed, else the one staffing the
// most. Pure: the slots, the products and the catalog come in, the plan goes
// out. The simulator's candidates are the products it serves; a template's
// are the product it names for each slot.
func PlanSystem(slots []Slot, products []Product, cat Catalog) SystemPlan {
	noun := cat.Noun
	if noun == "" {
		noun = "simulated"
	}
	sorted := append([]Product(nil), products...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	byName := make(map[string]Product, len(sorted))
	for _, p := range sorted {
		byName[p.Name] = p
	}
	fill := func(r Slot) SlotFill {
		f := SlotFill{Slot: r.Name, Alternate: r.Alternate}
		want := r.Quorum
		if want < 1 {
			want = 1
		}
		if r.Capacity != nil && *r.Capacity < want {
			want = *r.Capacity
		}
		fits := func(p Product) bool {
			if !cat.Candidate(r.Name, p.Name) {
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
			candidates := []string{}
			for _, name := range r.PinnedProducts {
				if cat.Candidate(r.Name, name) {
					candidates = append(candidates, name)
				}
			}
			if len(candidates) == 0 {
				f.Reason = fmt.Sprintf("no pinned product (%s) is %s", strings.Join(r.PinnedProducts, ", "), noun)
			} else {
				f.Reason = fmt.Sprintf("the %s pinned products (%s) are not a %s", noun, strings.Join(candidates, ", "), strings.Join(r.AcceptedTypes, " or "))
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
			f.Reason = fmt.Sprintf("no %s product is %s %s", noun, article(r.AcceptedTypes[0]), strings.Join(r.AcceptedTypes, " or "))
		} else {
			f.Reason = "no product is " + noun
		}
		return f
	}

	plan := SystemPlan{Alternates: map[string]string{}}
	// Group alternates by choice to pick one per choice.
	type alt struct {
		name  string
		fills []SlotFill
	}
	choices := map[string]map[string]*alt{}
	for _, r := range slots {
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
	for _, r := range slots {
		if r.Alternate != "" {
			choice, name, _ := strings.Cut(r.Alternate, "/")
			if plan.Alternates[choice] != name {
				continue
			}
		}
		plan.Slots = append(plan.Slots, fill(r))
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
