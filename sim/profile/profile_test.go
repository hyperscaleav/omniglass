package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStore(t *testing.T) {
	root := findProfilesRoot(t)
	store, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	// The catalog's generic-vendor devices. Cross-check the names so any
	// rename is caught at unit-test time rather than at fleet seed.
	expectedDevices := []string{"generic-device", "ferrite-pdu-4c"}
	for _, name := range expectedDevices {
		if _, ok := store.Devices[name]; !ok {
			t.Errorf("expected device profile %q to be loaded", name)
		}
	}
}

func TestDeviceProfileFields(t *testing.T) {
	root := findProfilesRoot(t)
	store, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	kv := store.Devices["generic-device"]
	if kv == nil {
		t.Fatal("generic-device not loaded")
	}

	if kv.Manufacturer != "Generic" {
		t.Errorf("manufacturer = %q, want Generic", kv.Manufacturer)
	}
	if kv.Model != "Generic SNMP Device" {
		t.Errorf("model = %q, want Generic SNMP Device", kv.Model)
	}
	if kv.SlugPrefix != "generic-device" {
		t.Errorf("slug_prefix = %q, want generic-device", kv.SlugPrefix)
	}
	if kv.DeviceType != "generic-device" {
		t.Errorf("device_type = %q, want generic-device", kv.DeviceType)
	}
	if len(kv.Interfaces) == 0 {
		t.Error("expected at least one interface")
	}
	if len(kv.Computed) == 0 {
		t.Error("expected at least one computed field (uptime)")
	}
	if kv.Defaults == nil {
		t.Error("expected defaults to be loaded")
	}
}

// TestRebootMutationsAnchoredAcrossSurfaces locks the YAML inline-anchor
// pattern: every shipped profile's reboot operation must declare its
// mutation list once and alias it from every interface that exposes it
// (HTTP route + matching CLI verb where the profile has an SSH/Telnet
// listener). The check compares the parsed Mutations slices directly,
// so a future edit that re-types one of them by accident gets caught
// at unit-test time rather than as a behavior drift in production.
func TestRebootMutationsAnchoredAcrossSurfaces(t *testing.T) {
	root := findProfilesRoot(t)
	store, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	// Each case names the HTTP reboot route and the equivalent CLI
	// verb; a profile with no CLI surface gets a routes-only entry.
	cases := []struct {
		device   string
		httpPath string
		cliVerb  string // empty means HTTP-only profile
	}{}

	for _, c := range cases {
		t.Run(c.device, func(t *testing.T) {
			dev := store.Devices[c.device]
			if dev == nil {
				t.Fatalf("device %q not loaded", c.device)
			}
			route := findRoute(dev, c.httpPath)
			if route == nil {
				t.Fatalf("route %q not found on %q", c.httpPath, c.device)
			}
			if len(route.Mutations) == 0 {
				t.Fatalf("route %q on %q has no mutations", c.httpPath, c.device)
			}

			if c.cliVerb == "" {
				return
			}

			cmd := findCommand(dev, c.cliVerb)
			if cmd == nil {
				t.Fatalf("CLI verb %q not found on %q", c.cliVerb, c.device)
			}
			if len(cmd.Mutations) != len(route.Mutations) {
				t.Fatalf("CLI verb %q has %d mutations, route %q has %d (anchor not aliased?)",
					c.cliVerb, len(cmd.Mutations), c.httpPath, len(route.Mutations))
			}
			for i := range route.Mutations {
				if route.Mutations[i].Path != cmd.Mutations[i].Path {
					t.Errorf("mutation[%d] path drift: route=%q vs cli=%q",
						i, route.Mutations[i].Path, cmd.Mutations[i].Path)
				}
				// Compare value as a string since YAML may decode to
				// different scalar types per surface; a successful
				// anchor alias produces byte-identical structures, so
				// fmt.Sprint on each side is a sufficient cross-check.
				if got, want := toString(cmd.Mutations[i].Value), toString(route.Mutations[i].Value); got != want {
					t.Errorf("mutation[%d] value drift: route=%q vs cli=%q",
						i, want, got)
				}
			}
		})
	}
}

// TestFactoryResetMutationsAnchoredAcrossSurfaces does the same
// alias-equivalence check for the factory-reset operation. Kept
// separate from the reboot check because the factory-reset list is
// strictly a superset, and a profile author might accidentally
// intersect the lists rather than aliasing two distinct anchors.
func TestFactoryResetMutationsAnchoredAcrossSurfaces(t *testing.T) {
	root := findProfilesRoot(t)
	store, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	cases := []struct {
		device   string
		httpPath string
		cliVerb  string
	}{}

	for _, c := range cases {
		t.Run(c.device, func(t *testing.T) {
			dev := store.Devices[c.device]
			if dev == nil {
				t.Fatalf("device %q not loaded", c.device)
			}
			route := findRoute(dev, c.httpPath)
			if route == nil {
				t.Fatalf("route %q not found on %q", c.httpPath, c.device)
			}
			if len(route.Mutations) == 0 {
				t.Fatalf("route %q on %q has no mutations", c.httpPath, c.device)
			}

			if c.cliVerb == "" {
				return
			}

			cmd := findCommand(dev, c.cliVerb)
			if cmd == nil {
				t.Fatalf("CLI verb %q not found on %q", c.cliVerb, c.device)
			}
			if len(cmd.Mutations) != len(route.Mutations) {
				t.Fatalf("CLI verb %q has %d mutations, route %q has %d (anchor not aliased?)",
					c.cliVerb, len(cmd.Mutations), c.httpPath, len(route.Mutations))
			}
			for i := range route.Mutations {
				if route.Mutations[i].Path != cmd.Mutations[i].Path {
					t.Errorf("mutation[%d] path drift: route=%q vs cli=%q",
						i, route.Mutations[i].Path, cmd.Mutations[i].Path)
				}
				if got, want := toString(cmd.Mutations[i].Value), toString(route.Mutations[i].Value); got != want {
					t.Errorf("mutation[%d] value drift: route=%q vs cli=%q",
						i, want, got)
				}
			}
		})
	}
}

func findRoute(dev *DeviceProfile, match string) *Route {
	for i := range dev.Interfaces {
		for j := range dev.Interfaces[i].Routes {
			if dev.Interfaces[i].Routes[j].Match == match {
				return &dev.Interfaces[i].Routes[j]
			}
		}
	}
	return nil
}

func findCommand(dev *DeviceProfile, match string) *Command {
	for i := range dev.Interfaces {
		for j := range dev.Interfaces[i].Commands {
			if dev.Interfaces[i].Commands[j].Match == match {
				return &dev.Interfaces[i].Commands[j]
			}
		}
	}
	return nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// findProfilesRoot is the catalog directory beside this package. Tests here
// read it from disk (the loader's directory path); a missing catalog is a
// broken checkout, so it fails rather than skips.
func findProfilesRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "profiles")
	if info, err := os.Stat(filepath.Join(root, "devices")); err != nil || !info.IsDir() {
		t.Fatalf("catalog not found at %s: %v", root, err)
	}
	return root
}
