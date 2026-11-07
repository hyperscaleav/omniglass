package scenario

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/systemplan"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// Problem is one thing wrong with a manifest, at the line to fix.
type Problem struct {
	Line    int
	Message string
}

// Error is every problem a manifest has, by line: a refusal lists them all at
// once rather than one per run.
type Error struct {
	File     string
	Problems []Problem
}

func (e *Error) Error() string {
	lines := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		lines = append(lines, fmt.Sprintf("%s:%d: %s", e.File, p.Line, p.Message))
	}
	return strings.Join(lines, "\n")
}

// maxNameLen bounds a site or building name, so the floor and room names
// built from them stay inside the platform's 100-character name rule.
const maxNameLen = 40

// nameRule is the platform's entity name rule (internal/template, storage).
var nameRule = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// compiled is a validated manifest: each standard with the components one of
// its systems takes, in role order.
type compiled struct {
	standards map[string]compiledStandard
}

type compiledStandard struct {
	name       string
	systemType string
	components []Count
}

// Validate checks a manifest against the catalog: every standard a template
// standard, every system type registered, every product a catalog product a
// simulator profile serves, every standard's products staffing it, and every
// site well formed. A refusal is an *Error naming each problem and its line.
func Validate(m *Manifest, cat Catalog) error {
	_, err := compile(m, cat)
	return err
}

func compile(m *Manifest, cat Catalog) (*compiled, error) {
	var probs []Problem
	add := func(line int, format string, args ...any) {
		probs = append(probs, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	if m.Name == "" {
		add(1, "the manifest has no name")
	}

	out := &compiled{standards: map[string]compiledStandard{}}
	declared := map[string]int{}
	for _, s := range m.Standards {
		name := s.Name.Name
		switch {
		case name == "":
			add(s.Line, "a standard has no name")
			continue
		case declared[name] != 0:
			add(s.Name.Line, "standard %q is declared twice (first on line %d)", name, declared[name])
			continue
		}
		declared[name] = s.Name.Line
		cs, sprobs := compileStandard(s, cat)
		probs = append(probs, sprobs...)
		if len(sprobs) == 0 {
			out.standards[name] = cs
		}
	}

	sites := map[string]int{}
	for _, site := range m.Sites {
		probs = append(probs, checkSite(site, declared, sites)...)
	}
	if len(m.Sites) == 0 {
		add(1, "the manifest places no sites")
	}

	if len(probs) > 0 {
		sort.SliceStable(probs, func(i, j int) bool { return probs[i].Line < probs[j].Line })
		return nil, &Error{File: m.File, Problems: probs}
	}
	return out, nil
}

// compileStandard checks one standard and plans the components a system of
// it takes, through the shared system planner (internal/systemplan) with
// each role's one candidate the product the manifest names for it, the way
// a system template is planned.
func compileStandard(s Standard, cat Catalog) (compiledStandard, []Problem) {
	var probs []Problem
	add := func(line int, format string, args ...any) {
		probs = append(probs, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	name := s.Name.Name
	cs := compiledStandard{name: name, systemType: s.SystemType.Name}

	st, known := cat.Standard(name)
	if !known {
		add(s.Name.Line, "unknown standard %q: no standard template has that name", name)
	}
	switch {
	case s.SystemType.Name == "":
		add(s.Line, "standard %q has no system_type", name)
	case !cat.SystemType(s.SystemType.Name):
		add(s.SystemType.Line, "unknown system type %q", s.SystemType.Name)
	}
	if len(s.Products) == 0 {
		add(s.Line, "standard %q names no products", name)
		return cs, probs
	}

	roles := sortedRoles(s.Products)
	fills := map[string]string{}
	products := []systemplan.Product{}
	productsOK := true
	for _, role := range roles {
		ref := s.Products[role]
		ct, ok := cat.Product(ref.Name)
		switch {
		case !ok:
			add(ref.Line, "unknown product %q: the catalog has no product of that name", ref.Name)
			productsOK = false
			continue
		case !cat.Simulated(ref.Name):
			add(ref.Line, "product %q has no simulator profile, so the lab cannot simulate it", ref.Name)
			productsOK = false
		}
		fills[role] = ref.Name
		products = append(products, systemplan.Product{Name: ref.Name, ComponentType: ct})
	}
	if !known {
		return cs, probs
	}

	roleDeclared := map[string]bool{}
	for _, r := range st.Roles {
		roleDeclared[r.Name] = true
	}
	undeclared := false
	for _, role := range roles {
		if !roleDeclared[role] {
			add(s.Products[role].Line, "standard %q declares no role %q", name, role)
			undeclared = true
		}
	}
	if undeclared {
		return cs, probs
	}
	sys := &template.System{Standard: name, Roles: map[string]template.RoleFill{}}
	for role, ref := range s.Products {
		sys.Roles[role] = template.RoleFill{Product: ref.Name}
	}
	staffed, err := template.StaffedRoles(st, sys)
	if err != nil {
		add(s.Line, "standard %q: the products %v", name, err)
		return cs, probs
	}
	if !productsOK {
		return cs, probs
	}

	planRoles := make([]systemplan.Role, 0, len(staffed))
	for _, r := range staffed {
		alt := ""
		if r.Choice != "" {
			alt = r.Choice + "/" + r.Alternate
		}
		planRoles = append(planRoles, systemplan.Role{Name: r.Name, Quorum: r.Quorate(), Capacity: r.Capacity,
			AcceptedTypes: r.AcceptedTypes, PinnedProducts: r.PinnedProducts, Alternate: alt})
	}
	plan := systemplan.PlanSystem(planRoles, products, systemplan.Catalog{
		Candidate: func(role, product string) bool { return fills[role] == product },
		Within:    cat.Within,
		Noun:      "named",
	})
	for _, f := range plan.Roles {
		if f.Missing > 0 {
			ref := s.Products[f.Role]
			add(ref.Line, "role %s of standard %q takes %s: %s", f.Role, name, ref.Name, f.Reason)
			continue
		}
		cs.components = append(cs.components, Count{Name: f.Product, Count: f.Count})
	}
	return cs, probs
}

// checkSite checks one site: a legal unique name, a positive weight, its
// buildings, and ratios over declared standards.
func checkSite(site Site, standards, seen map[string]int) []Problem {
	var probs []Problem
	add := func(line int, format string, args ...any) {
		probs = append(probs, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	name := site.Name.Name
	switch {
	case name == "":
		add(site.Line, "a site has no name")
	case !legalName(name):
		add(site.Name.Line, "site name %q is not a name (lowercase letters, digits and dashes, at most %d)", name, maxNameLen)
	case seen[name] != 0:
		add(site.Name.Line, "site %q is declared twice (first on line %d)", name, seen[name])
	default:
		seen[name] = site.Name.Line
	}
	if site.Weight < 1 {
		add(site.Line, "site %q has weight %d; a site's weight is a whole number above zero", name, site.Weight)
	}

	if len(site.Buildings) == 0 {
		add(site.Line, "site %q has no buildings to place its rooms in", name)
	}
	buildings := map[string]bool{}
	for _, b := range site.Buildings {
		switch {
		case !legalName(b.Name.Name):
			add(lineOr(b.Name.Line, site.Line), "building name %q of site %q is not a name (lowercase letters, digits and dashes, at most %d)", b.Name.Name, name, maxNameLen)
		case buildings[b.Name.Name]:
			add(b.Name.Line, "building %q of site %q is declared twice", b.Name.Name, name)
		}
		buildings[b.Name.Name] = true
		if b.Floors < 1 {
			add(lineOr(b.Name.Line, site.Line), "building %q of site %q has %d floors; it needs at least one", b.Name.Name, name, b.Floors)
		}
	}

	if len(site.Systems) == 0 {
		add(site.Line, "site %q places no systems", name)
	}
	ratios := map[string]bool{}
	for _, r := range site.Systems {
		st := r.Standard
		switch {
		case standards[st.Name] == 0:
			add(st.Line, "unknown standard %q: site %q places it, but the manifest's standards do not declare it", st.Name, name)
		case ratios[st.Name]:
			add(st.Line, "site %q places standard %q twice", name, st.Name)
		}
		ratios[st.Name] = true
		if r.Weight < 1 {
			add(st.Line, "standard %q has ratio %d at site %q; a ratio is a whole number above zero", st.Name, r.Weight, name)
		}
	}
	return probs
}

func legalName(s string) bool { return len(s) <= maxNameLen && nameRule.MatchString(s) }

func lineOr(line, fallback int) int {
	if line == 0 {
		return fallback
	}
	return line
}

func sortedRoles(m map[string]Ref) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
