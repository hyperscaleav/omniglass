// Package template is the template catalog (ADR-0071, ADR-0827): example
// configurations an operator clones, shipped in the binary as kind-tagged
// YAML and embedded exactly like the seed files. The difference is the
// loader. A seed inserts rows unasked; this package inserts nothing and never
// touches the database. It parses, validates and serves a read-only catalog,
// and the fork and instantiate primitives read a template from it and write
// ordinary rows the operator owns outright, with nothing pointing back.
//
// Four kinds ship: a standard_template (a standard's attributes, roles,
// choices and contract lines), a component_template (a product, its driver's
// attach preset and declared values), a system_template (a standard and a
// component per role) and a place_template (a subtree of places and the
// systems that land in them). Every template is validated at load against
// the shape below; a template naming a catalog row a release withdrew is
// caught by the dry-run test against the seeded catalogs, not here, since
// this package knows no database.
package template

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// Kind is a template's kind, the tag every catalog entry carries.
type Kind string

// The four kinds the catalog holds.
const (
	KindStandard  Kind = "standard_template"
	KindComponent Kind = "component_template"
	KindSystem    Kind = "system_template"
	KindPlace     Kind = "place_template"
)

// Kinds is every kind, in the order the catalog lists them.
var Kinds = []Kind{KindStandard, KindSystem, KindPlace, KindComponent}

// Valid reports whether k is one of the four kinds.
func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// AddressInput is the driver input a component template never presets: the
// address of one install (the attach builds its target from it), supplied by
// the caller per component.
const AddressInput = "host"

// Template is one catalog entry. Exactly one of the kind bodies is set, the
// one Kind names.
type Template struct {
	Kind        Kind   `yaml:"kind" json:"kind"`
	Name        string `yaml:"name" json:"name"`
	Label       string `yaml:"label" json:"label"`
	Description string `yaml:"description" json:"description,omitempty"`

	Standard  *Standard  `yaml:"standard,omitempty" json:"standard,omitempty"`
	Component *Component `yaml:"component,omitempty" json:"component,omitempty"`
	System    *System    `yaml:"system,omitempty" json:"system,omitempty"`
	Place     *Place     `yaml:"place,omitempty" json:"place,omitempty"`
}

// Standard is a standard_template's body: what a fork writes.
type Standard struct {
	// Parent names the standard template a variant hangs off. A variant
	// forks only once the operator holds a standard of that name.
	Parent  string         `yaml:"parent,omitempty" json:"parent,omitempty"`
	Choices []Choice       `yaml:"choices,omitempty" json:"choices,omitempty"`
	Roles   []Role         `yaml:"roles" json:"roles"`
	Props   []ContractLine `yaml:"properties,omitempty" json:"properties,omitempty"`
	Metrics []ContractLine `yaml:"metrics,omitempty" json:"metrics,omitempty"`
	// Map is the layout a conforming system's Map tab draws (ADR-0128), the
	// same {aspect, positions} value a standard's map column holds.
	Map *Map `yaml:"map,omitempty" json:"map,omitempty"`
}

// Choice is an exclusive-or group of roles (#626): a conforming system
// answers it through exactly one of its alternates.
type Choice struct {
	Name       string      `yaml:"name" json:"name"`
	Label      string      `yaml:"label" json:"label"`
	Alternates []Alternate `yaml:"alternates" json:"alternates"`
}

// Alternate is one way to answer a choice. The first is the template's
// default, the one a system template staffs.
type Alternate struct {
	Name  string `yaml:"name" json:"name"`
	Label string `yaml:"label" json:"label"`
}

// Role is one slot a conforming system needs filled.
type Role struct {
	Name           string   `yaml:"name" json:"name"`
	Label          string   `yaml:"label" json:"label"`
	Quorum         int      `yaml:"quorum,omitempty" json:"quorum,omitempty"`
	Capacity       *int     `yaml:"capacity,omitempty" json:"capacity,omitempty"`
	Impact         string   `yaml:"impact,omitempty" json:"impact,omitempty"`
	AcceptedTypes  []string `yaml:"accepted_types,omitempty" json:"accepted_types,omitempty"`
	PinnedProducts []string `yaml:"pinned_products,omitempty" json:"pinned_products,omitempty"`
	Choice         string   `yaml:"choice,omitempty" json:"choice,omitempty"`
	Alternate      string   `yaml:"alternate,omitempty" json:"alternate,omitempty"`
}

// Quorate is the role's quorum with the storage floor applied: a role no
// component need fill is not a role, so zero reads as one.
func (r Role) Quorate() int { return max(r.Quorum, 1) }

// ContractLine is one property or metric a standard declares, optionally
// with a default (a JSON value) and optionally required.
type ContractLine struct {
	Name     string          `yaml:"name" json:"name"`
	Default  json.RawMessage `yaml:"-" json:"default,omitempty"`
	Required bool            `yaml:"required,omitempty" json:"required,omitempty"`
}

// UnmarshalYAML reads a contract line, holding its default (any YAML value)
// as the JSON value storage writes.
func (c *ContractLine) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		Name     string `yaml:"name"`
		Default  any    `yaml:"default"`
		Required bool   `yaml:"required"`
	}
	if err := decodeStrict(n, &raw); err != nil {
		return err
	}
	c.Name, c.Required = raw.Name, raw.Required
	if raw.Default != nil {
		b, err := json.Marshal(raw.Default)
		if err != nil {
			return fmt.Errorf("default of %s: %w", raw.Name, err)
		}
		c.Default = b
	}
	return nil
}

// Map is a standard's layout declaration.
type Map struct {
	Aspect    float64       `yaml:"aspect" json:"aspect"`
	Positions []MapPosition `yaml:"positions" json:"positions"`
}

// MapPosition places one role's nth position on the layout, in unit
// coordinates.
type MapPosition struct {
	Role     string  `yaml:"role" json:"role"`
	Position int     `yaml:"position" json:"position"`
	X        float64 `yaml:"x" json:"x"`
	Y        float64 `yaml:"y" json:"y"`
}

// Component is a component_template's body.
type Component struct {
	Product string `yaml:"product" json:"product"`
	Attach  Attach `yaml:"attach,omitempty" json:"attach"`
	// Properties are declared values, by property name, each a JSON value.
	Properties Values `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// Attach is the driver attach preset: every input except the address. The
// driver is the product's unless named.
type Attach struct {
	Driver string            `yaml:"driver,omitempty" json:"driver,omitempty"`
	Inputs map[string]string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// System is a system_template's body.
type System struct {
	// Standard names the standard the system conforms to: a standard
	// template's name, which is also the name its fork takes by default.
	Standard   string `yaml:"standard" json:"standard"`
	SystemType string `yaml:"system_type,omitempty" json:"system_type,omitempty"`
	// Roles fills each role, by role name.
	Roles      map[string]RoleFill `yaml:"roles" json:"roles"`
	Properties Values              `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// RoleFill names what fills a role: a component template or a bare product,
// exactly one.
type RoleFill struct {
	Component string `yaml:"component,omitempty" json:"component,omitempty"`
	Product   string `yaml:"product,omitempty" json:"product,omitempty"`
}

// Place is a place_template's body, and each place within it.
type Place struct {
	Type    string        `yaml:"type" json:"type"`
	Name    string        `yaml:"name,omitempty" json:"name,omitempty"`
	Label   string        `yaml:"label,omitempty" json:"label,omitempty"`
	Systems []PlaceSystem `yaml:"systems,omitempty" json:"systems,omitempty"`
	Places  []Place       `yaml:"places,omitempty" json:"places,omitempty"`
}

// PlaceSystem is a system template landing in a place.
type PlaceSystem struct {
	Template string `yaml:"template" json:"template"`
	Label    string `yaml:"label,omitempty" json:"label,omitempty"`
}

// Values are declared property values by name, each held as the JSON value
// storage writes.
type Values map[string]json.RawMessage

// UnmarshalYAML reads each value (any YAML value) as JSON.
func (v *Values) UnmarshalYAML(n *yaml.Node) error {
	var raw map[string]any
	if err := n.Decode(&raw); err != nil {
		return err
	}
	out := make(Values, len(raw))
	for k, val := range raw {
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("value of %s: %w", k, err)
		}
		out[k] = b
	}
	*v = out
	return nil
}

// Names returns the value names, sorted, so a caller writes them in a
// stable order.
func (v Values) Names() []string {
	out := make([]string, 0, len(v))
	for k := range v {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Catalog is the loaded, validated template set.
type Catalog struct {
	list   []Template
	byName map[string]*Template
}

// ErrNotFound is a template name the catalog does not hold.
var ErrNotFound = errors.New("template: not in the catalog")

// List returns the templates of one kind (every kind when kind is empty),
// ordered by kind then name. The slice is the caller's; the templates are
// shared and must not be modified.
func (c *Catalog) List(kind Kind) []Template {
	out := []Template{}
	for _, t := range c.list {
		if kind == "" || t.Kind == kind {
			out = append(out, t)
		}
	}
	return out
}

// Get returns the named template.
func (c *Catalog) Get(name string) (*Template, error) {
	t, ok := c.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return t, nil
}

// GetKind returns the named template when it is of the kind given: a name
// of another kind reads as absent, so a caller asking for a standard
// template never gets a system one.
func (c *Catalog) GetKind(name string, kind Kind) (*Template, error) {
	t, err := c.Get(name)
	if err != nil {
		return nil, err
	}
	if t.Kind != kind {
		return nil, fmt.Errorf("%w: %q is a %s, not a %s", ErrNotFound, name, t.Kind, kind)
	}
	return t, nil
}

//go:embed catalog/*.yaml
var shipped embed.FS

var (
	shippedOnce sync.Once
	shippedCat  *Catalog
	shippedErr  error
)

// Shipped returns the catalog this binary ships. A load failure is a build
// bug in an embedded asset (the package's tests load it), so it panics
// rather than making every caller handle an error that cannot happen in a
// tested build.
func Shipped() *Catalog {
	shippedOnce.Do(func() {
		shippedCat, shippedErr = LoadFS(shipped, "catalog")
	})
	if shippedErr != nil {
		panic("template: load the shipped catalog: " + shippedErr.Error())
	}
	return shippedCat
}

// LoadFS loads every .yaml file in dir of fsys into one catalog and
// validates it.
func LoadFS(fsys fs.FS, dir string) (*Catalog, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		files[e.Name()] = b
	}
	return Load(files)
}

// Load parses the files (name to content) into one catalog and validates
// it: every entry against its kind's shape, names unique across the
// catalog, and every reference between templates resolving to a template of
// the right kind.
func Load(files map[string][]byte) (*Catalog, error) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	c := &Catalog{byName: map[string]*Template{}}
	seen := map[string]bool{}
	for _, file := range names {
		var doc struct {
			Templates []Template `yaml:"templates"`
		}
		dec := yaml.NewDecoder(bytes.NewReader(files[file]))
		dec.KnownFields(true)
		if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("template: %s: %w", file, err)
		}
		for _, t := range doc.Templates {
			if err := t.validate(); err != nil {
				return nil, fmt.Errorf("template: %s: %w", file, err)
			}
			if seen[t.Name] {
				return nil, fmt.Errorf("template: %s: %q is named twice in the catalog", file, t.Name)
			}
			seen[t.Name] = true
			c.list = append(c.list, t)
		}
	}
	sort.SliceStable(c.list, func(i, j int) bool {
		ki, kj := kindOrder(c.list[i].Kind), kindOrder(c.list[j].Kind)
		if ki != kj {
			return ki < kj
		}
		return c.list[i].Name < c.list[j].Name
	})
	for i := range c.list {
		c.byName[c.list[i].Name] = &c.list[i]
	}
	if err := c.validateRefs(); err != nil {
		return nil, err
	}
	return c, nil
}

func kindOrder(k Kind) int {
	for i, known := range Kinds {
		if k == known {
			return i
		}
	}
	return len(Kinds)
}

// nameRule is the platform's name rule for registry and fleet names: lower
// case, digits and hyphens, starting with a letter or digit.
var nameRule = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func checkName(what, name string) error {
	if name == "" {
		return fmt.Errorf("%s has no name", what)
	}
	if len(name) > 100 || !nameRule.MatchString(name) {
		return fmt.Errorf("%s name %q is not a legal name (lower case, digits and hyphens)", what, name)
	}
	return nil
}

// validate checks one template's own shape, without reference to the rest
// of the catalog.
func (t *Template) validate() error {
	if err := checkName("a template", t.Name); err != nil {
		return err
	}
	if !t.Kind.Valid() {
		return fmt.Errorf("%s: unknown kind %q", t.Name, t.Kind)
	}
	if t.Label == "" {
		return fmt.Errorf("%s: no label", t.Name)
	}
	bodies := map[Kind]bool{
		KindStandard: t.Standard != nil, KindComponent: t.Component != nil,
		KindSystem: t.System != nil, KindPlace: t.Place != nil,
	}
	for k, set := range bodies {
		if set != (k == t.Kind) {
			if set {
				return fmt.Errorf("%s: a %s carries a %s body", t.Name, t.Kind, k)
			}
			return fmt.Errorf("%s: a %s needs its body", t.Name, t.Kind)
		}
	}
	var err error
	switch t.Kind {
	case KindStandard:
		err = t.Standard.validate()
	case KindComponent:
		err = t.Component.validate()
	case KindSystem:
		err = t.System.validate()
	case KindPlace:
		err = t.Place.validate(true)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", t.Name, err)
	}
	return nil
}

var roleImpacts = map[string]bool{"": true, "outage": true, "degraded": true, "none": true}

func (s *Standard) validate() error {
	alts := map[string]bool{}
	choices := map[string]bool{}
	for _, c := range s.Choices {
		if err := checkName("a choice", c.Name); err != nil {
			return err
		}
		if choices[c.Name] {
			return fmt.Errorf("choice %q is declared twice", c.Name)
		}
		choices[c.Name] = true
		if len(c.Alternates) < 2 {
			return fmt.Errorf("choice %q needs at least two alternates", c.Name)
		}
		seen := map[string]bool{}
		for _, a := range c.Alternates {
			if err := checkName("an alternate", a.Name); err != nil {
				return err
			}
			if seen[a.Name] {
				return fmt.Errorf("choice %q names alternate %q twice", c.Name, a.Name)
			}
			seen[a.Name] = true
			alts[c.Name+"/"+a.Name] = true
		}
	}
	if len(s.Roles) == 0 {
		return errors.New("a standard template declares no roles")
	}
	roles := map[string]bool{}
	for _, r := range s.Roles {
		if err := checkName("a role", r.Name); err != nil {
			return err
		}
		if roles[r.Name] {
			return fmt.Errorf("role %q is declared twice", r.Name)
		}
		roles[r.Name] = true
		if r.Quorum < 0 {
			return fmt.Errorf("role %q has a negative quorum", r.Name)
		}
		if r.Capacity != nil && *r.Capacity < r.Quorate() {
			return fmt.Errorf("role %q has a capacity below its quorum", r.Name)
		}
		if !roleImpacts[r.Impact] {
			return fmt.Errorf("role %q has impact %q, want outage, degraded or none", r.Name, r.Impact)
		}
		if (r.Choice == "") != (r.Alternate == "") {
			return fmt.Errorf("role %q names choice %q and alternate %q, want both or neither", r.Name, r.Choice, r.Alternate)
		}
		if r.Choice != "" && !alts[r.Choice+"/"+r.Alternate] {
			return fmt.Errorf("role %q names unknown choice/alternate %s/%s", r.Name, r.Choice, r.Alternate)
		}
	}
	for _, lines := range [][]ContractLine{s.Props, s.Metrics} {
		seen := map[string]bool{}
		for _, l := range lines {
			if err := checkName("a contract line", l.Name); err != nil {
				return err
			}
			if seen[l.Name] {
				return fmt.Errorf("contract line %q is declared twice", l.Name)
			}
			seen[l.Name] = true
		}
	}
	if s.Map != nil {
		if s.Map.Aspect <= 0 {
			return errors.New("the map needs a positive aspect")
		}
		for _, p := range s.Map.Positions {
			if !roles[p.Role] {
				return fmt.Errorf("the map places role %q, which the template does not declare", p.Role)
			}
			if p.Position < 1 || p.X < 0 || p.X > 1 || p.Y < 0 || p.Y > 1 {
				return fmt.Errorf("the map places role %q out of bounds", p.Role)
			}
		}
	}
	return nil
}

func (c *Component) validate() error {
	if err := checkName("the product", c.Product); err != nil {
		return err
	}
	if c.Attach.Driver != "" {
		if err := checkName("the driver", c.Attach.Driver); err != nil {
			return err
		}
	}
	if _, ok := c.Attach.Inputs[AddressInput]; ok {
		return fmt.Errorf("the attach preset sets %q, an address the caller supplies per component", AddressInput)
	}
	for _, name := range c.Properties.Names() {
		if err := checkName("a declared value", name); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) validate() error {
	if err := checkName("the standard", s.Standard); err != nil {
		return err
	}
	if len(s.Roles) == 0 {
		return errors.New("a system template fills no roles")
	}
	for role, fill := range s.Roles {
		if (fill.Component == "") == (fill.Product == "") {
			return fmt.Errorf("role %q names %s, want exactly one of a component template or a product", role, fillWords(fill))
		}
	}
	for _, name := range s.Properties.Names() {
		if err := checkName("a declared value", name); err != nil {
			return err
		}
	}
	return nil
}

func fillWords(f RoleFill) string {
	if f.Component == "" && f.Product == "" {
		return "neither"
	}
	return "both"
}

func (p *Place) validate(root bool) error {
	if err := checkName("a place's location type", p.Type); err != nil {
		return err
	}
	if p.Name != "" {
		if err := checkName("a place", p.Name); err != nil {
			return err
		}
	}
	names := map[string]bool{}
	for i := range p.Places {
		child := &p.Places[i]
		if child.Name != "" {
			if names[child.Name] {
				return fmt.Errorf("place %q holds two places named %q", p.Type, child.Name)
			}
			names[child.Name] = true
		}
		if err := child.validate(false); err != nil {
			return err
		}
	}
	for _, s := range p.Systems {
		if err := checkName("a system template", s.Template); err != nil {
			return err
		}
	}
	return nil
}

// validateRefs checks every reference between templates: a variant's parent
// is a standard template, a system's role fills name component templates, a
// system template built on a standard template fills exactly the roles a
// conforming system needs (every unconditional role, and one alternate's
// roles in full for each choice), and a place's systems name system
// templates.
func (c *Catalog) validateRefs() error {
	for i := range c.list {
		t := &c.list[i]
		var err error
		switch t.Kind {
		case KindStandard:
			if p := t.Standard.Parent; p != "" {
				_, err = c.GetKind(p, KindStandard)
				if err == nil && p == t.Name {
					err = errors.New("a standard template is its own parent")
				}
			}
		case KindSystem:
			err = c.validateSystemRefs(t.System)
		case KindPlace:
			err = c.validatePlaceRefs(t.Place)
		}
		if err != nil {
			return fmt.Errorf("template: %s: %w", t.Name, err)
		}
	}
	return nil
}

func (c *Catalog) validateSystemRefs(s *System) error {
	for _, role := range sortedKeys(s.Roles) {
		if comp := s.Roles[role].Component; comp != "" {
			if _, err := c.GetKind(comp, KindComponent); err != nil {
				return fmt.Errorf("role %q: %w", role, err)
			}
		}
	}
	std, err := c.GetKind(s.Standard, KindStandard)
	if err != nil {
		// A system template may name a standard the catalog does not
		// ship (an operator's own, by name); its roles are checked
		// against that standard when it is instantiated.
		if errors.Is(err, ErrNotFound) && c.byName[s.Standard] == nil {
			return nil
		}
		return err
	}
	_, err = StaffedRoles(std.Standard, s)
	return err
}

// StaffedRoles returns the roles of standard st a system built from sys
// fills, in declaration order, refusing a fill that names a role st does not
// declare, an unconditional role left unfilled, or a choice answered by
// anything but exactly one alternate's roles in full.
func StaffedRoles(st *Standard, sys *System) ([]Role, error) {
	declared := map[string]Role{}
	for _, r := range st.Roles {
		declared[r.Name] = r
	}
	for _, name := range sortedKeys(sys.Roles) {
		if _, ok := declared[name]; !ok {
			return nil, fmt.Errorf("fills role %q, which standard %s does not declare", name, sysStandardName(sys))
		}
	}
	answered := map[string]string{}
	var out []Role
	for _, r := range st.Roles {
		_, filled := sys.Roles[r.Name]
		if r.Choice == "" {
			if !filled {
				return nil, fmt.Errorf("leaves role %q unfilled", r.Name)
			}
			out = append(out, r)
			continue
		}
		if !filled {
			continue
		}
		if prev, ok := answered[r.Choice]; ok && prev != r.Alternate {
			return nil, fmt.Errorf("answers choice %q with both %s and %s", r.Choice, prev, r.Alternate)
		}
		answered[r.Choice] = r.Alternate
		out = append(out, r)
	}
	for _, ch := range st.Choices {
		alt, ok := answered[ch.Name]
		if !ok {
			return nil, fmt.Errorf("answers choice %q with no alternate", ch.Name)
		}
		for _, r := range st.Roles {
			if r.Choice == ch.Name && r.Alternate == alt {
				if _, filled := sys.Roles[r.Name]; !filled {
					return nil, fmt.Errorf("answers choice %q with %s but leaves its role %q unfilled", ch.Name, alt, r.Name)
				}
			}
		}
	}
	return out, nil
}

func sysStandardName(s *System) string { return s.Standard }

func (c *Catalog) validatePlaceRefs(p *Place) error {
	for _, s := range p.Systems {
		if _, err := c.GetKind(s.Template, KindSystem); err != nil {
			return err
		}
	}
	for i := range p.Places {
		if err := c.validatePlaceRefs(&p.Places[i]); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// decodeStrict decodes n into v refusing unknown fields, the KnownFields
// rule the file decoder applies, which a custom unmarshaler would otherwise
// bypass.
func decodeStrict(n *yaml.Node, v any) error {
	b, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	return dec.Decode(v)
}
