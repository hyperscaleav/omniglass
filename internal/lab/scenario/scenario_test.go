package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/template"
)

func corporate(t *testing.T) (*Manifest, Catalog) {
	t.Helper()
	m, err := Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return m, cat
}

func TestCorporateValidates(t *testing.T) {
	m, cat := corporate(t)
	if err := Validate(m, cat); err != nil {
		t.Fatalf("the corporate manifest is refused:\n%v", err)
	}
}

func TestNamesListsTheShippedManifests(t *testing.T) {
	if got := Names(); !reflect.DeepEqual(got, []string{"corporate"}) {
		t.Fatalf("Names() = %v, want [corporate]", got)
	}
	if _, err := Shipped("nope"); !errors.Is(err, ErrUnknown) || !strings.Contains(err.Error(), "corporate") {
		t.Fatalf("Shipped(nope) = %v, want ErrUnknown naming corporate", err)
	}
}

// TestResolveAtScale resolves corporate at the scale lab's three sizes from
// the one manifest and holds the documented rules: the site count is
// SiteCount's, inside the manifest's bounds and never falling as the size
// grows; the headquarters is placed once and the repeated archetypes share the
// rest by weight; the totals sum to the size; each site is within one system
// of its quota (one, plus its typical-size share of the rest); each standard
// within one of its ratio quota at a site; and fleet-wide each standard is
// within two per site of its exact share.
func TestResolveAtScale(t *testing.T) {
	m, cat := corporate(t)
	arch := map[string]Archetype{}
	repeatedWeight := 0
	for _, a := range m.Archetypes {
		arch[a.Name.Name] = a
		if !a.Headquarters {
			repeatedWeight += a.Weight
		}
	}
	prevSites := 0
	for _, size := range []int{100, 1000, 3000} {
		p, err := Resolve(m, cat, size)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if p.Totals.Systems != size || len(p.allSystems()) != size {
			t.Fatalf("size %d: planned %d systems (%d listed)", size, p.Totals.Systems, len(p.allSystems()))
		}

		n := len(p.Sites)
		if n != SiteCount(m, size) || n < m.Sites.Min || n > m.Sites.Max {
			t.Fatalf("size %d: %d sites, SiteCount %d, bounds %d to %d", size, n, SiteCount(m, size), m.Sites.Min, m.Sites.Max)
		}
		if n < prevSites {
			t.Fatalf("size %d: %d sites, fewer than the %d of a smaller size", size, n, prevSites)
		}
		prevSites = n
		placed := 0
		for _, ap := range p.Archetypes {
			a := arch[ap.Name]
			placed += ap.Sites
			if a.Headquarters {
				if ap.Sites != 1 {
					t.Errorf("size %d: the headquarters %s is placed %d times", size, ap.Name, ap.Sites)
				}
				continue
			}
			quota := float64(n-1) * float64(a.Weight) / float64(repeatedWeight)
			if diff := float64(ap.Sites) - quota; diff <= -1 || diff >= 1 {
				t.Errorf("size %d: archetype %s has %d sites, quota %.2f", size, ap.Name, ap.Sites, quota)
			}
		}
		if placed != n {
			t.Errorf("size %d: the archetypes place %d sites, the plan lists %d", size, placed, n)
		}

		sumW := 0
		for _, sp := range p.Sites {
			sumW += arch[sp.Archetype].TypicalSystems
		}
		exact := map[string]float64{}
		for _, sp := range p.Sites {
			a := arch[sp.Archetype]
			got := sp.Totals.Systems
			quota := 1 + float64(size-n)*float64(a.TypicalSystems)/float64(sumW)
			if diff := float64(got) - quota; diff <= -1 || diff >= 1 {
				t.Errorf("size %d: site %s has %d systems, quota %.2f", size, sp.Name, got, quota)
			}
			sumR := 0
			for _, r := range a.Systems {
				sumR += r.Weight
			}
			for _, r := range a.Systems {
				siteQuota := float64(got) * float64(r.Weight) / float64(sumR)
				have := countOf(sp.Totals.Standards, r.Standard.Name)
				if diff := float64(have) - siteQuota; diff <= -1 || diff >= 1 {
					t.Errorf("size %d: site %s has %d %s, quota %.2f", size, sp.Name, have, r.Standard.Name, siteQuota)
				}
				exact[r.Standard.Name] += quota * float64(r.Weight) / float64(sumR)
			}
		}
		bound := 2 * float64(n)
		for _, st := range p.Totals.Standards {
			if diff := float64(st.Count) - exact[st.Name]; diff < -bound || diff > bound {
				t.Errorf("size %d: %d %s, exact share %.2f, outside +/-%v", size, st.Count, st.Name, exact[st.Name], bound)
			}
		}

		components, perProduct := 0, 0
		for _, s := range p.allSystems() {
			for _, c := range s.Components {
				components += c.Count
			}
		}
		for _, c := range p.Totals.Products {
			perProduct += c.Count
		}
		if p.Totals.Components != components || perProduct != components {
			t.Errorf("size %d: components total %d, per product %d, per system %d", size, p.Totals.Components, perProduct, components)
		}
	}
}

// TestCorporateSiteCounts holds corporate to its target (#1357): an
// enterprise of 15 to 30 sites at 1,000 and 3,000 systems, at the top at
// 3,000, and its floor of 3 at 100.
func TestCorporateSiteCounts(t *testing.T) {
	m, _ := corporate(t)
	if got := SiteCount(m, 100); got != 3 {
		t.Errorf("100 systems: %d sites, want 3", got)
	}
	if got := SiteCount(m, 1000); got < 15 || got > 30 {
		t.Errorf("1,000 systems: %d sites, want 15 to 30", got)
	}
	if got := SiteCount(m, 3000); got != 30 {
		t.Errorf("3,000 systems: %d sites, want 30", got)
	}
}

// Across every size the knob takes, the site count stays inside the bounds
// and never falls.
func TestSiteCountNeverFalls(t *testing.T) {
	m, _ := corporate(t)
	prev := 0
	for size := MinSystems(m); size <= MaxSystems; size++ {
		n := SiteCount(m, size)
		if n < prev || n < m.Sites.Min || n > m.Sites.Max {
			t.Fatalf("size %d: %d sites after %d (bounds %d to %d)", size, n, prev, m.Sites.Min, m.Sites.Max)
		}
		prev = n
	}
}

// TestResolveCorporateAt100 pins the small plan, so a change to the rounding
// or the manifest shows here as a diff.
func TestResolveCorporateAt100(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 100)
	if err != nil {
		t.Fatal(err)
	}
	var perSite []Count
	for _, s := range p.Sites {
		perSite = append(perSite, Count{s.Name, s.Totals.Systems})
	}
	// 3 sites take one system each; the other 97 split 250:80:25 (sum 355)
	// are 68, 22 and 7.
	if want := []Count{{"hq", 69}, {"regional-01", 23}, {"branch-01", 8}}; !reflect.DeepEqual(perSite, want) {
		t.Fatalf("systems per site = %v, want %v", perSite, want)
	}
	// hq's 69 split 30:20:12:6:4:8 (sum 80) is 26, 17, 10, 5, 4, 7.
	want := []Count{{"huddle-room", 26}, {"mr65", 17}, {"mr75", 10}, {"mr86", 5}, {"divisible-conference", 4}, {"ds55", 7}}
	if got := p.Sites[0].Totals.Standards; !reflect.DeepEqual(got, want) {
		t.Fatalf("hq per standard = %v, want %v", got, want)
	}
}

// A repeated archetype's sites are numbered from its name, in order, the same
// at the same size; the headquarters keeps its own name.
func TestResolveNamesSitesFromTheirArchetype(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"hq"}
	for _, ap := range p.Archetypes[1:] {
		for k := 1; k <= ap.Sites; k++ {
			want = append(want, fmt.Sprintf("%s-%02d", ap.Name, k))
		}
	}
	var got []string
	for _, s := range p.Sites {
		got = append(got, s.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sites = %v, want %v", got, want)
	}
	if s := p.Sites[1]; s.Name != "regional-01" || s.Label != "Regional Office 01" || s.Archetype != "regional" {
		t.Fatalf("second site = %s %q (%s), want regional-01 \"Regional Office 01\" (regional)", s.Name, s.Label, s.Archetype)
	}
}

// Every planned component is a product the manifest names for its system's
// standard, and every system takes its standard's system type.
func TestResolveComponentsFollowTheManifest(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 1000)
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]map[string]bool{}
	types := map[string]string{}
	for _, st := range m.Standards {
		named[st.Name.Name] = map[string]bool{}
		for _, ref := range st.Products {
			named[st.Name.Name][ref.Name] = true
		}
		types[st.Name.Name] = st.SystemType.Name
	}
	for _, s := range p.allSystems() {
		if len(s.Components) == 0 {
			t.Fatalf("system %s takes no components", s.Name)
		}
		for _, c := range s.Components {
			if !named[s.Standard][c.Name] || c.Count < 1 {
				t.Fatalf("system %s (%s) takes %d %s, which the manifest does not name for it", s.Name, s.Standard, c.Count, c.Name)
			}
		}
		if s.SystemType != types[s.Standard] {
			t.Fatalf("system %s is a %s, want %s", s.Name, s.SystemType, types[s.Standard])
		}
	}
}

// A role's quorum is how many of its product each system takes, through the
// shared system planner.
func TestResolveStaffsARoleToItsQuorum(t *testing.T) {
	m, err := Parse("t.yaml", []byte(`name: t
standards:
  - name: room
    system_type: meeting
    products:
      screen: t-panel-75
      bar: t-bar
`+strings.ReplaceAll(oneArchetype, "sign-55", "room")))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Resolve(m, fakeCatalog(), 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []Count{{"t-bar", 3}, {"t-panel-75", 6}}
	if !reflect.DeepEqual(p.Totals.Products, want) || p.Totals.Components != 9 {
		t.Fatalf("products = %v (%d components), want %v: two screens and a bar a room", p.Totals.Products, p.Totals.Components, want)
	}
}

func TestResolvePlacesEverySystemInAUniqueRoom(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 3000)
	if err != nil {
		t.Fatal(err)
	}
	names, labels, rooms := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, s := range p.allSystems() {
		for what, seen := range map[string]map[string]bool{s.Name: names, s.Label: labels, s.Room: rooms} {
			if seen[what] {
				t.Fatalf("%q is planned twice", what)
			}
			seen[what] = true
		}
		if !strings.HasPrefix(s.Room, s.Floor+"-") || !strings.HasPrefix(s.Floor, s.Building+"-") {
			t.Fatalf("system %s sits in room %s, floor %s, building %s: not one path", s.Name, s.Room, s.Floor, s.Building)
		}
		for _, n := range []string{s.Name, s.Building, s.Floor, s.Room} {
			if !nameRule.MatchString(n) || len(n) > 100 {
				t.Fatalf("%q breaks the name rule", n)
			}
		}
	}
}

func TestResolveIsDeterministic(t *testing.T) {
	m, cat := corporate(t)
	a, err := Resolve(m, cat, 1000)
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := corporate(t)
	b, err := Resolve(m2, cat, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two resolutions of the same manifest at the same size differ")
	}
	var wa, wb bytes.Buffer
	if err := WritePlan(&wa, a); err != nil {
		t.Fatal(err)
	}
	if err := WritePlan(&wb, b); err != nil {
		t.Fatal(err)
	}
	if wa.String() != wb.String() {
		t.Fatal("two printed plans differ")
	}
}

func TestResolveRefusesASizeOutOfRange(t *testing.T) {
	m, cat := corporate(t)
	for _, size := range []int{0, -1, m.Sites.Min - 1, MaxSystems + 1} {
		if _, err := Resolve(m, cat, size); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("size %d resolved: %v", size, err)
		}
	}
	if _, err := Resolve(m, cat, m.Sites.Min); err != nil {
		t.Errorf("size %d, one system a site: %v", m.Sites.Min, err)
	}
}

func TestWritePlan(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 100)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := WritePlan(&b, p); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"scenario corporate (Corporate): 100 systems", "across 3 sites",
		"ARCHETYPE", "SITE ", "huddle-room", "hq ", "regional-01 ", "branch-01 ", "total ", "PRODUCT", p.Totals.Products[0].Name} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output lacks %q:\n%s", want, out)
		}
	}
}

func TestApportion(t *testing.T) {
	for _, tc := range []struct {
		total   int
		weights []int
		want    []int
	}{
		{100, []int{40, 25, 20, 15}, []int{40, 25, 20, 15}},
		{10, []int{1, 1, 1}, []int{4, 3, 3}}, // a tie goes to the earlier weight
		{7, []int{3, 3, 1}, []int{3, 3, 1}},  // 3, 3, 1 exactly
		{5, []int{1, 2}, []int{2, 3}},        // 1.67 and 3.33: the larger remainder rounds up
		{2, []int{40, 25, 20, 15}, []int{1, 1, 0, 0}},
		{0, []int{1, 2}, []int{0, 0}},
	} {
		got := apportion(tc.total, tc.weights)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("apportion(%d, %v) = %v, want %v", tc.total, tc.weights, got, tc.want)
		}
	}
}

// TestValidateNamesTheProblemAndItsLine is the refusal contract: an unknown
// product, a product no simulator profile serves, an unknown standard, an
// unknown system type and a role the standard does not declare are each named
// with the manifest line to fix.
func TestValidateNamesTheProblemAndItsLine(t *testing.T) {
	cat := fakeCatalog()
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"unknown product", `
name: t
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-player-9
` + oneArchetype, `t.yaml:7: unknown product "t-player-9"`},
		{"no simulator profile", `
name: t
standards:
  - name: room
    system_type: meeting
    products:
      screen: t-panel-75
      bar: t-bar
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-horn
` + oneArchetype, `t.yaml:12: product "t-horn" has no simulator profile`},
		{"unknown standard", `
name: t
standards:
  - name: sign-54
    system_type: sign
    products:
      panel: t-panel-55
` + oneArchetype, `t.yaml:3: unknown standard "sign-54"`},
		{"undeclared standard at an archetype", `
name: t
sites: { min: 1, max: 1 }
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-player
archetypes:
  - name: a
    headquarters: true
    typical_systems: 1
    buildings: [{ name: b, floors: 1 }]
    systems:
      mr65: 1
`, `t.yaml:15: unknown standard "mr65"`},
		{"unknown system type", `
name: t
standards:
  - name: sign-55
    system_type: signage
    products:
      panel: t-panel-55
      player: t-player
` + oneArchetype, `t.yaml:4: unknown system type "signage"`},
		{"undeclared role", `
name: t
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-player
      speaker: t-player
` + oneArchetype, `t.yaml:8: standard "sign-55" declares no role "speaker"`},
		{"product off its pin", `
name: t
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-75
      player: t-player
` + oneArchetype, `t.yaml:6: role panel of standard "sign-55" takes t-panel-75: no pinned product (t-panel-55) is named`},
		{"role left unfilled", `
name: t
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
` + oneArchetype, `leaves role "player" unfilled`},
		{"misspelt field", `
name: t
standards:
  - name: sign-55
    system_typ: sign
`, `t.yaml: line 4: unknown field "system_typ"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Parse("t.yaml", []byte(strings.TrimPrefix(tc.yaml, "\n")))
			if err == nil {
				err = Validate(m, cat)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateChecksArchetypes(t *testing.T) {
	cat := fakeCatalog()
	m, err := Parse("t.yaml", []byte(`name: t
sites: { min: 1, max: 2 }
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-player
archetypes:
  - name: hq
    headquarters: true
    typical_systems: 1
    buildings: [{ name: b, floors: 1 }]
    systems:
      sign-55: 1
  - name: Bad_Name
    weight: 0
    typical_systems: 0
    buildings: [{ name: b, floors: 0 }]
    systems:
      sign-55: 0
`))
	if err != nil {
		t.Fatal(err)
	}
	err = Validate(m, cat)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("got %v, want an *Error", err)
	}
	for _, want := range []string{`t.yaml:16: archetype name "Bad_Name" is not a name`, `has weight 0`,
		`has typical_systems 0`, `has 0 floors`, `ratio 0`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal lacks %q:\n%v", want, err)
		}
	}
	for i := 1; i < len(e.Problems); i++ {
		if e.Problems[i].Line < e.Problems[i-1].Line {
			t.Fatalf("problems are not in line order: %v", e.Problems)
		}
	}
}

// TestValidateChecksSiteBounds refuses bounds the site count cannot keep and
// a scenario without exactly one headquarters, each named with its line.
func TestValidateChecksSiteBounds(t *testing.T) {
	const standards = `standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-player
`
	archetype := func(name, placement string) string {
		return `  - name: ` + name + `
    ` + placement + `
    typical_systems: 5
    buildings: [{ name: b, floors: 1 }]
    systems:
      sign-55: 1
`
	}
	hq, office := archetype("hq", "headquarters: true"), archetype("office", "weight: 1")
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"min above max", "name: t\nsites: { min: 5, max: 3 }\n" + standards + "archetypes:\n" + hq + office,
			`t.yaml:2: sites max 3 is below min 5`},
		{"min below one", "name: t\nsites: { min: 0, max: 3 }\n" + standards + "archetypes:\n" + hq + office,
			`t.yaml:2: sites min is 0`},
		{"no bounds", "name: t\n" + standards + "archetypes:\n" + hq + office,
			`t.yaml:1: the manifest declares no site bounds`},
		{"no headquarters", "name: t\nsites: { min: 1, max: 3 }\n" + standards + "archetypes:\n" + office,
			`t.yaml:10: no archetype is the headquarters`},
		{"two headquarters", "name: t\nsites: { min: 1, max: 3 }\n" + standards + "archetypes:\n" + hq + archetype("hq2", "headquarters: true"),
			`t.yaml:10: 2 archetypes are the headquarters (hq (line 10), hq2 (line 16))`},
		{"headquarters with a weight", "name: t\nsites: { min: 1, max: 3 }\n" + standards + "archetypes:\n" + archetype("hq", "headquarters: true\n    weight: 2") + office,
			`t.yaml:10: archetype "hq" is the headquarters, placed exactly once, so it takes no weight`},
		{"many sites, nothing repeats", "name: t\nsites: { min: 1, max: 3 }\n" + standards + "archetypes:\n" + hq,
			`t.yaml:2: sites max is 3, but every archetype is the headquarters`},
		{"headquarters named like a repeated site", "name: t\nsites: { min: 1, max: 3 }\n" + standards + "archetypes:\n" + archetype("office-02", "headquarters: true") + office,
			`t.yaml:10: headquarters "office-02" is named like a site of archetype "office"`},
		{"no archetypes", "name: t\nsites: { min: 1, max: 3 }\n" + standards,
			`t.yaml:1: the manifest declares no site archetypes`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Parse("t.yaml", []byte(tc.yaml))
			if err == nil {
				err = Validate(m, fakeCatalog())
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

const oneArchetype = `sites: { min: 1, max: 1 }
archetypes:
  - name: a
    headquarters: true
    typical_systems: 1
    buildings: [{ name: b, floors: 1 }]
    systems:
      sign-55: 1
`

// fakeCatalog is a small fictional catalog, so the refusal tests name no
// seeded product and a catalog rename cannot break them (#804): a 55-inch
// sign pinned to its panel, a room taking two screens and a bar, and a
// speaker no simulator profile serves.
func fakeCatalog() Catalog {
	products := map[string]string{"t-panel-55": "display", "t-panel-75": "display", "t-player": "media-player",
		"t-bar": "video-bar", "t-horn": "speaker"}
	parents := map[string]string{"av": "", "display": "av", "media-player": "av", "video-bar": "av", "speaker": "av"}
	standards := map[string]*template.Standard{
		"sign-55": {Roles: []template.Role{
			{Name: "panel", AcceptedTypes: []string{"display"}, PinnedProducts: []string{"t-panel-55"}},
			{Name: "player", AcceptedTypes: []string{"media-player"}},
		}},
		"room": {Roles: []template.Role{
			{Name: "screen", Quorum: 2, AcceptedTypes: []string{"display"}},
			{Name: "bar", AcceptedTypes: []string{"video-bar"}},
		}},
	}
	return Catalog{
		Standard: func(name string) (*template.Standard, bool) {
			st, ok := standards[name]
			return st, ok
		},
		Product: func(name string) (string, bool) {
			ct, ok := products[name]
			return ct, ok
		},
		Simulated:  func(product string) bool { return product != "t-horn" },
		Within:     func(have, accepted string) bool { return within(parents, have, accepted) },
		SystemType: func(name string) bool { return name == "sign" || name == "meeting" },
	}
}

func countOf(cs []Count, name string) int {
	for _, c := range cs {
		if c.Name == name {
			return c.Count
		}
	}
	return 0
}

func TestWithin(t *testing.T) {
	parents := map[string]string{"av": "", "display": "av", "lcd": "display", "loop-a": "loop-b", "loop-b": "loop-a"}
	for _, tc := range []struct {
		have, accepted string
		want           bool
	}{
		{"lcd", "lcd", true},
		{"lcd", "display", true},
		{"lcd", "av", true},
		{"display", "lcd", false},
		{"unknown", "av", false},
		{"loop-a", "av", false}, // a cycle ends
	} {
		if got := within(parents, tc.have, tc.accepted); got != tc.want {
			t.Errorf("within(%s, %s) = %v, want %v", tc.have, tc.accepted, got, tc.want)
		}
	}
}

// Every planned system says which role each of its components fills, as the
// manifest names the product for the role, and the staffing is exactly the
// components it takes: the seed staffs each role from it.
func TestResolveStaffsEveryRoleWithItsProduct(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 100)
	if err != nil {
		t.Fatal(err)
	}
	fills := map[string]map[string]string{}
	for _, st := range m.Standards {
		fills[st.Name.Name] = map[string]string{}
		for role, ref := range st.Products {
			fills[st.Name.Name][role] = ref.Name
		}
	}
	for _, s := range p.allSystems() {
		perProduct := map[string]int{}
		for _, f := range s.Staffing {
			if want := fills[s.Standard][f.Role]; f.Product != want || f.Count < 1 {
				t.Fatalf("system %s staffs %s with %d %s, want the manifest's %s", s.Name, f.Role, f.Count, f.Product, want)
			}
			perProduct[f.Product] += f.Count
		}
		for _, c := range s.Components {
			if perProduct[c.Name] != c.Count {
				t.Fatalf("system %s takes %d %s but staffs %d", s.Name, c.Count, c.Name, perProduct[c.Name])
			}
			delete(perProduct, c.Name)
		}
		if len(perProduct) > 0 {
			t.Fatalf("system %s staffs products it does not take: %v", s.Name, perProduct)
		}
	}
}

// Each site's node is named for the site, so the plan's every site has a
// node of its own and two sites never share one.
func TestSiteNodeIsNamedForItsSite(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 1000)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, s := range p.Sites {
		n := SiteNode(s.Name)
		if prev, ok := seen[n]; ok {
			t.Fatalf("sites %s and %s share the node %s", prev, s.Name, n)
		}
		seen[n] = s.Name
	}
	if got := SiteNode("regional-01"); got != "edge-regional-01" {
		t.Fatalf("SiteNode(regional-01) = %q, want edge-regional-01", got)
	}
}
