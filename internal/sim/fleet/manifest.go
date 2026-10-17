// Package fleet parses fleet manifests and compiles them into device inventory.
package fleet

import (
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// Manifest is the top-level fleet manifest authored by instructors.
//
// A manifest can declare multiple tenants by setting `tenant` on each site
// (or a default at the top level). Tenant scopes placement so two tenants
// imported from the same manifest end up under separate top-level
// location tree groupings.
type Manifest struct {
	Name          string       `yaml:"name"`
	Domain        string       `yaml:"domain"`
	DefaultTenant string       `yaml:"tenant"`
	DisplayNames  DisplayNames `yaml:"display_names"`
	Sites         []Site       `yaml:"sites"`
}

// DisplayNames configures how systems get friendly names.
type DisplayNames struct {
	Style string `yaml:"style"` // "docker" for "Adjective Surname"
}

// Site defines a physical location with its own CIDR and location tree.
//
// A site is the campus level in the Region/City/Campus/Building/Room
// taxonomy. Region+City are inherited by all hosts at the site.
// Coordinates set the dot-on-the-map for the campus; building-level
// coords override (set on Location entries below).
type Site struct {
	Code      string     `yaml:"code"`
	Name      string     `yaml:"name"`
	CIDR      string     `yaml:"cidr"`
	Tenant    string     `yaml:"tenant"`    // Overrides Manifest.DefaultTenant
	Region    string     `yaml:"region"`    // e.g. "North America", "EMEA", "APAC"
	City      string     `yaml:"city"`      // e.g. "Boise"
	Latitude  float64    `yaml:"latitude"`  // Campus dot-on-map
	Longitude float64    `yaml:"longitude"` // Campus dot-on-map
	Locations []Location `yaml:"locations"`
	// Flat systems (no building nesting).
	Systems []SystemVolume `yaml:"systems"`
}

// Location represents a node in the location tree.
//
// By convention the tree is Building > Room only,
// floors are not separate nodes (a room name like "415B" already implies
// the 4th floor). Building-level latitude/longitude override the campus
// coords for hosts placed in that building.
type Location struct {
	Building  string  `yaml:"building"`
	Latitude  float64 `yaml:"latitude"`
	Longitude float64 `yaml:"longitude"`

	// Legacy: floor-keyed sub-locations. Still parsed for backward
	// compatibility with older manifests but new manifests should put
	// rooms directly under Building via the rooms list on the system
	// volume.
	Floor   string         `yaml:"floor"`
	Floors  []Location     `yaml:"floors"`
	Systems []SystemVolume `yaml:"systems"`
}

// SystemVolume assigns a system profile to one or more rooms.
//
// SystemType is the omniglass system_type name the sync service should
// use when creating the system in omniglass. Label is a human-readable
// label template (e.g. "Boardroom {room}") used for system display names
// in the Omniglass UI; if empty, falls back to a generated
// "Adjective Surname".
type SystemVolume struct {
	Profile    string   `yaml:"profile"`
	Rooms      []string `yaml:"rooms"`
	Count      int      `yaml:"count"`
	StartRoom  int      `yaml:"start_room"`
	SystemType string   `yaml:"system_type"`
	Label      string   `yaml:"label"`
}

// ParseManifest reads and parses a fleet manifest YAML file.
func ParseManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return parseManifest(data)
}

// ParseManifestFS reads and parses a manifest from a filesystem, such as
// profiles.Fleets() for the embedded catalog.
func ParseManifestFS(fsys fs.FS, name string) (*Manifest, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return parseManifest(data)
}

func parseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("validating manifest: %w", err)
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	if m.Name == "" {
		return fmt.Errorf("missing required field 'name'")
	}
	if m.Domain == "" {
		return fmt.Errorf("missing required field 'domain'")
	}
	if len(m.Sites) == 0 {
		return fmt.Errorf("fleet must have at least one site")
	}
	for i, s := range m.Sites {
		if s.Code == "" {
			return fmt.Errorf("site %d: missing required field 'code'", i)
		}
		if s.CIDR == "" {
			return fmt.Errorf("site %q: missing required field 'cidr'", s.Code)
		}
	}
	return nil
}

// ResolveTenant returns the tenant for a site, falling back to the
// manifest default (and finally to "default") if neither is set.
func (m *Manifest) ResolveTenant(s Site) string {
	if s.Tenant != "" {
		return s.Tenant
	}
	if m.DefaultTenant != "" {
		return m.DefaultTenant
	}
	return "default"
}
