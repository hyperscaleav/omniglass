// Package scenario is the scale lab's scenario manifest (#1553, epic #1357):
// a vertical environment described once, as site archetypes (one headquarters
// and the kinds of office repeated around it), each archetype's location tree,
// the standards placed at its sites as ratios, and the catalog product filling
// each role of each standard. A size knob turns it into a concrete plan of
// sites, systems and components (Resolve), the number of sites growing with
// the size between the bounds the manifest declares. Pure: the manifest, the catalog and the
// size come in, the plan goes out, the same plan every time.
//
// The vocabulary is the platform's (#914): a system type here is a standard
// from the template catalog, and every product is a shipped catalog product
// the simulator serves, so the plan is one the dev seed and the simulator can
// both build without new products or profiles.
package scenario

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is one scenario manifest as written. Every name it holds carries
// the line it was written on, so a refusal names the line to fix.
type Manifest struct {
	// File is the manifest's file name, the prefix of every refusal.
	File        string      `yaml:"-"`
	Name        string      `yaml:"name"`
	Label       string      `yaml:"label"`
	Description string      `yaml:"description"`
	Sites       SiteBounds  `yaml:"sites"`
	Standards   []Standard  `yaml:"standards"`
	Archetypes  []Archetype `yaml:"archetypes"`
	// Pushers are the outside actors each site has (#1743), declared like
	// its devices: an emulator profile with a push face, how many a site,
	// and how many of the site's systems each is scoped to.
	Pushers []Pusher `yaml:"pushers"`
}

// Pusher is one outside actor a scenario places at every site: an emulator
// device whose profile has a push face (a booking service, say), PerSite of
// them a site, each granted telemetry:push on Systems of the site's systems.
// The lab seeds each one's principal and mints its token per run.
type Pusher struct {
	Name    Ref `yaml:"name"`
	Profile Ref `yaml:"profile"`
	PerSite int `yaml:"per_site"`
	Systems int `yaml:"systems"`
	Line    int `yaml:"-"`
}

// UnmarshalYAML decodes a pusher and keeps its line.
func (p *Pusher) UnmarshalYAML(n *yaml.Node) error {
	type plain Pusher
	if err := decodeKnown(n, (*plain)(p), "name", "profile", "per_site", "systems"); err != nil {
		return err
	}
	p.Line = n.Line
	return nil
}

// SiteBounds are the fewest and the most sites the scenario resolves to,
// whatever the size: the site count grows with the size and is clamped here.
type SiteBounds struct {
	Min  int `yaml:"min"`
	Max  int `yaml:"max"`
	Line int `yaml:"-"`
}

// Standard is one system type the scenario places: a standard from the
// template catalog, the system type its systems are classified under, and the
// product filling each role it staffs.
type Standard struct {
	Name       Ref            `yaml:"name"`
	SystemType Ref            `yaml:"system_type"`
	Products   map[string]Ref `yaml:"products"`
	Line       int            `yaml:"-"`
}

// Archetype is one kind of site, each site a campus with its own node (one
// node per site, #1357): the buildings its rooms are spread over, the ratio of
// each standard among its systems, and how many systems a site of it
// typically has (TypicalSystems), which weighs its share of the size. The
// headquarters is placed exactly once; every other archetype is repeated, its
// Weight its share of the sites besides the headquarters.
type Archetype struct {
	Name           Ref        `yaml:"name"`
	Label          string     `yaml:"label"`
	Headquarters   bool       `yaml:"headquarters"`
	Weight         int        `yaml:"weight"`
	TypicalSystems int        `yaml:"typical_systems"`
	Buildings      []Building `yaml:"buildings"`
	Systems        Ratios     `yaml:"systems"`
	Line           int        `yaml:"-"`
}

// Building is one building of a site and how many floors it has.
type Building struct {
	Name   Ref `yaml:"name"`
	Floors int `yaml:"floors"`
}

// Ref is a name and the manifest line it was written on.
type Ref struct {
	Name string
	Line int
}

// UnmarshalYAML reads a scalar name and keeps its line.
func (r *Ref) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: want a name, got a %s", n.Line, nodeKind(n))
	}
	r.Name, r.Line = n.Value, n.Line
	return nil
}

// Ratio is one standard's share of an archetype's systems.
type Ratio struct {
	Standard Ref
	Weight   int
}

// Ratios are an archetype's standards and their ratios, in the order written: the
// order breaks a rounding tie, so it is kept rather than read into a map.
type Ratios []Ratio

// UnmarshalYAML reads a mapping of standard name to ratio, in order.
func (rs *Ratios) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: want a mapping of standard to ratio, got a %s", n.Line, nodeKind(n))
	}
	out := make(Ratios, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		var w int
		if err := v.Decode(&w); err != nil {
			return fmt.Errorf("line %d: the ratio of %q is not a whole number", v.Line, k.Value)
		}
		out = append(out, Ratio{Standard: Ref{Name: k.Value, Line: k.Line}, Weight: w})
	}
	*rs = out
	return nil
}

// UnmarshalYAML decodes a standard and keeps its line.
func (s *Standard) UnmarshalYAML(n *yaml.Node) error {
	type plain Standard
	if err := decodeKnown(n, (*plain)(s), "name", "system_type", "products"); err != nil {
		return err
	}
	s.Line = n.Line
	return nil
}

// UnmarshalYAML decodes an archetype and keeps its line.
func (a *Archetype) UnmarshalYAML(n *yaml.Node) error {
	type plain Archetype
	if err := decodeKnown(n, (*plain)(a), "name", "label", "headquarters", "weight", "typical_systems", "buildings", "systems"); err != nil {
		return err
	}
	a.Line = n.Line
	return nil
}

// UnmarshalYAML decodes the site bounds and keeps their line.
func (b *SiteBounds) UnmarshalYAML(n *yaml.Node) error {
	type plain SiteBounds
	if err := decodeKnown(n, (*plain)(b), "min", "max"); err != nil {
		return err
	}
	b.Line = n.Line
	return nil
}

// UnmarshalYAML decodes a building, refusing an undeclared field.
func (b *Building) UnmarshalYAML(n *yaml.Node) error {
	type plain Building
	return decodeKnown(n, (*plain)(b), "name", "floors")
}

// decodeKnown decodes a mapping node refusing a key it does not list: a
// misspelt key is a refusal, not a silently empty field. It decodes the node
// itself, so the lines nested names keep are the file's own.
func decodeKnown(n *yaml.Node, v any, keys ...string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: want a mapping, got a %s", n.Line, nodeKind(n))
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := n.Content[i]; !slices.Contains(keys, k.Value) {
			return fmt.Errorf("line %d: unknown field %q (want one of %s)", k.Line, k.Value, strings.Join(keys, ", "))
		}
	}
	return n.Decode(v)
}

func nodeKind(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "list"
	default:
		return "scalar"
	}
}

// Parse reads a manifest's YAML. It checks the shape only; Validate checks
// the names against the catalog.
func Parse(file string, data []byte) (*Manifest, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("%s: the manifest is empty", file)
	}
	m := &Manifest{}
	type plain Manifest
	if err := decodeKnown(root.Content[0], (*plain)(m), "name", "label", "description", "sites", "standards", "archetypes", "pushers"); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	m.File = file
	return m, nil
}

//go:embed manifests/*.yaml
var shipped embed.FS

// Names lists the shipped scenario manifests, by name.
func Names() []string {
	entries, err := fs.ReadDir(shipped, "manifests")
	if err != nil {
		panic(err) // the directory is a compile-time constant inside the embed
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(out)
	return out
}

// ErrUnknown is a scenario name no shipped manifest has.
var ErrUnknown = errors.New("scenario: no scenario manifest of that name")

// Shipped parses the shipped manifest of that name. An unknown name is
// ErrUnknown, naming the manifests that exist.
func Shipped(name string) (*Manifest, error) {
	file := name + ".yaml"
	b, err := fs.ReadFile(shipped, "manifests/"+file)
	if err != nil {
		return nil, fmt.Errorf("%w: %q (known: %s)", ErrUnknown, name, strings.Join(Names(), ", "))
	}
	return Parse(file, b)
}
