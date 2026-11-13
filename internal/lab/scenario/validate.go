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

// maxNameLen bounds an archetype or building name, so the site, floor and
// room names built from them stay inside the platform's 100-character name
// rule.
const maxNameLen = 40

// nameRule is the platform's entity name rule (internal/template, storage).
var nameRule = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// repeatedName is the suffix a repeated archetype's site names take.
var repeatedName = regexp.MustCompile(`^-[0-9]+$`)

// compiled is a validated manifest: each standard with the components one of
// its systems takes, in role order.
type compiled struct {
	standards map[string]compiledStandard
}

type compiledStandard struct {
	name       string
	systemType string
	components []Count
	roles      []RoleFill
}

// Validate checks a manifest against the catalog: every standard a template
// standard, every system type registered, every product a catalog product a
// simulator profile serves, every standard's products staffing it, every
// archetype well formed with exactly one headquarters, and the site bounds
// in order. A refusal is an *Error naming each problem and its line.
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

	archetypes := map[string]int{}
	for _, a := range m.Archetypes {
		probs = append(probs, checkArchetype(a, declared, archetypes)...)
	}
	probs = append(probs, checkSiteBounds(m)...)
	probs = append(probs, checkPushers(m, cat)...)

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
		cs.roles = append(cs.roles, RoleFill{Role: f.Role, Product: f.Product, Count: f.Count})
	}
	return cs, probs
}

// checkSiteBounds checks the archetypes against the site bounds: exactly one
// headquarters, the bounds in order and above zero, and room for more than
// one site only if an archetype repeats.
func checkSiteBounds(m *Manifest) []Problem {
	var probs []Problem
	add := func(line int, format string, args ...any) {
		probs = append(probs, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	if len(m.Archetypes) == 0 {
		add(1, "the manifest declares no site archetypes")
		return probs
	}
	var hqs []string
	repeated := false
	for _, a := range m.Archetypes {
		if a.Headquarters {
			hqs = append(hqs, fmt.Sprintf("%s (line %d)", a.Name.Name, a.Line))
		} else {
			repeated = true
		}
	}
	switch {
	case len(hqs) == 0:
		add(m.Archetypes[0].Line, "no archetype is the headquarters; mark the one placed exactly once with headquarters: true")
	case len(hqs) > 1:
		add(m.Archetypes[0].Line, "%d archetypes are the headquarters (%s); a scenario has one", len(hqs), strings.Join(hqs, ", "))
	}

	// A repeated archetype's sites are <name>-NN, so a headquarters named
	// like one would be planned twice.
	for _, hq := range m.Archetypes {
		for _, a := range m.Archetypes {
			if hq.Headquarters && !a.Headquarters && repeatedName.MatchString(strings.TrimPrefix(hq.Name.Name, a.Name.Name)) {
				add(hq.Name.Line, "headquarters %q is named like a site of archetype %q (%s-01, %s-02 and on)", hq.Name.Name, a.Name.Name, a.Name.Name, a.Name.Name)
			}
		}
	}

	b := m.Sites
	line := lineOr(b.Line, 1)
	switch {
	case b.Line == 0:
		add(1, "the manifest declares no site bounds; say sites: { min: N, max: M }")
	case b.Min < 1:
		add(line, "sites min is %d; a scenario has at least its headquarters, so min is 1 or more", b.Min)
	case b.Max < b.Min:
		add(line, "sites max %d is below min %d", b.Max, b.Min)
	case b.Max > 1 && !repeated:
		add(line, "sites max is %d, but every archetype is the headquarters, so the scenario has one site", b.Max)
	}
	return probs
}

// checkArchetype checks one archetype: a legal unique name, a positive
// typical size, a weight only if it repeats, its buildings, and ratios over
// declared standards.
func checkArchetype(a Archetype, standards, seen map[string]int) []Problem {
	var probs []Problem
	add := func(line int, format string, args ...any) {
		probs = append(probs, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	name := a.Name.Name
	switch {
	case name == "":
		add(a.Line, "an archetype has no name")
	case !legalName(name):
		add(a.Name.Line, "archetype name %q is not a name (lowercase letters, digits and dashes, at most %d)", name, maxNameLen)
	case seen[name] != 0:
		add(a.Name.Line, "archetype %q is declared twice (first on line %d)", name, seen[name])
	default:
		seen[name] = a.Name.Line
	}
	switch {
	case a.Headquarters && a.Weight != 0:
		add(a.Line, "archetype %q is the headquarters, placed exactly once, so it takes no weight", name)
	case !a.Headquarters && a.Weight < 1:
		add(a.Line, "archetype %q has weight %d; a repeated archetype's weight is a whole number above zero", name, a.Weight)
	}
	if a.TypicalSystems < 1 {
		add(a.Line, "archetype %q has typical_systems %d; a site has at least one system", name, a.TypicalSystems)
	}
	if len(a.Buildings) == 0 {
		add(a.Line, "archetype %q has no buildings to place its rooms in", name)
	}
	buildings := map[string]bool{}
	for _, b := range a.Buildings {
		switch {
		case !legalName(b.Name.Name):
			add(lineOr(b.Name.Line, a.Line), "building name %q of archetype %q is not a name (lowercase letters, digits and dashes, at most %d)", b.Name.Name, name, maxNameLen)
		case buildings[b.Name.Name]:
			add(b.Name.Line, "building %q of archetype %q is declared twice", b.Name.Name, name)
		}
		buildings[b.Name.Name] = true
		if b.Floors < 1 {
			add(lineOr(b.Name.Line, a.Line), "building %q of archetype %q has %d floors; it needs at least one", b.Name.Name, name, b.Floors)
		}
	}

	if len(a.Systems) == 0 {
		add(a.Line, "archetype %q places no systems", name)
	}
	ratios := map[string]bool{}
	for _, r := range a.Systems {
		st := r.Standard
		switch {
		case standards[st.Name] == 0:
			add(st.Line, "unknown standard %q: archetype %q places it, but the manifest's standards do not declare it", st.Name, name)
		case ratios[st.Name]:
			add(st.Line, "archetype %q places standard %q twice", name, st.Name)
		}
		ratios[st.Name] = true
		if r.Weight < 1 {
			add(st.Line, "standard %q has ratio %d at archetype %q; a ratio is a whole number above zero", st.Name, r.Weight, name)
		}
	}
	return probs
}

// checkPushers checks each pusher declaration (#1743): a name, unique, an
// emulator profile with a push face or a webhook sender's face (#1747), at
// least one a site, and at least one system to scope each to when systems are
// given, since a pusher granted nothing pushes nothing. Omitted, they are
// every system of the site.
func checkPushers(m *Manifest, cat Catalog) []Problem {
	var probs []Problem
	add := func(line int, format string, args ...any) {
		probs = append(probs, Problem{Line: line, Message: fmt.Sprintf(format, args...)})
	}
	seen := map[string]int{}
	for _, p := range m.Pushers {
		name := p.Name.Name
		switch {
		case !legalName(name):
			add(lineOr(p.Name.Line, p.Line), "pusher name %q is not a name (lowercase letters, digits and dashes, at most %d)", name, maxNameLen)
		case seen[name] != 0:
			add(lineOr(p.Name.Line, p.Line), "pusher %q is declared twice (first on line %d)", name, seen[name])
		default:
			seen[name] = lineOr(p.Name.Line, p.Line)
		}
		pusher := cat.Pusher != nil && cat.Pusher(p.Profile.Name)
		sender := cat.Sender != nil && cat.Sender(p.Profile.Name)
		if !pusher && !sender {
			add(lineOr(p.Profile.Line, p.Line), "profile %q of pusher %q has no push face and no webhook face, so it cannot push", p.Profile.Name, name)
		}
		if p.PerSite < 1 {
			add(p.Line, "pusher %q has per_site %d; a declared pusher places at least one a site", name, p.PerSite)
		}
		if p.Systems != nil && *p.Systems < 1 {
			add(p.Line, "pusher %q has no systems to scope it to; give it systems of at least 1", name)
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
