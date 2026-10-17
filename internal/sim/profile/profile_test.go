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

	// Lab's current generic-vendor catalog. Cross-check the names so any
	// rename is caught at unit-test time rather than at fleet compile.
	expectedDevices := []string{"kestrel-vroom", "newtron-panel-7", "boreal-edge-85", "polaris-dsp-16"}
	for _, name := range expectedDevices {
		if _, ok := store.Devices[name]; !ok {
			t.Errorf("expected device profile %q to be loaded", name)
		}
	}

	if _, ok := store.Systems["large-conference-a"]; !ok {
		t.Fatal("expected system profile 'large-conference-a' to be loaded")
	}

	if err := store.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestDeviceProfileFields(t *testing.T) {
	root := findProfilesRoot(t)
	store, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	kv := store.Devices["kestrel-vroom"]
	if kv == nil {
		t.Fatal("kestrel-vroom not loaded")
	}

	if kv.Manufacturer != "Kestrel" {
		t.Errorf("manufacturer = %q, want Kestrel", kv.Manufacturer)
	}
	if kv.Model != "VRoom" {
		t.Errorf("model = %q, want VRoom", kv.Model)
	}
	if kv.SlugPrefix != "video-bar" {
		t.Errorf("slug_prefix = %q, want video-bar", kv.SlugPrefix)
	}
	if kv.DeviceType != "video-codec" {
		t.Errorf("device_type = %q, want video-codec", kv.DeviceType)
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

func TestSystemProfileFields(t *testing.T) {
	root := findProfilesRoot(t)
	store, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	sys := store.Systems["large-conference-a"]
	if sys == nil {
		t.Fatal("large-conference-a not loaded")
	}

	if len(sys.Devices) != 4 {
		t.Fatalf("expected 4 device slots, got %d", len(sys.Devices))
	}

	expected := map[string]struct {
		suffix string
		count  int
	}{
		"kestrel-vroom":   {"COD", 1},
		"newtron-panel-7": {"TP", 1},
		"boreal-edge-85":  {"DS", 2},
		"polaris-dsp-16":  {"DSP", 1},
	}
	for _, d := range sys.Devices {
		exp, ok := expected[d.Profile]
		if !ok {
			t.Errorf("unexpected device profile %q in system", d.Profile)
			continue
		}
		if d.Suffix != exp.suffix {
			t.Errorf("device %q suffix = %q, want %q", d.Profile, d.Suffix, exp.suffix)
		}
		if d.Count != exp.count {
			t.Errorf("device %q count = %d, want %d", d.Profile, d.Count, exp.count)
		}
	}
}

func TestValidateMissingProfile(t *testing.T) {
	store := &Store{
		Devices: map[string]*DeviceProfile{},
		Systems: map[string]*SystemProfile{
			"test-system": {
				Name: "test-system",
				Devices: []SystemDevice{
					{Profile: "nonexistent", Suffix: "X", Count: 1},
				},
			},
		},
	}
	if err := store.Validate(); err == nil {
		t.Error("expected validation error for missing device profile")
	}
}

// TestPowerRoutesDeclareEnum locks the boreal-edge-85 and kestrel-vroom
// power routes to the enum-validated shape. If a future change replaces
// the list mutations with the legacy map without re-adding the enum,
// this test fails, which is the point: silent regressions to the
// "yolo-accepted" state were the motivation for the validation feature.
func TestPowerRoutesDeclareEnum(t *testing.T) {
	root := findProfilesRoot(t)
	store, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	cases := []struct {
		device string
		match  string
	}{
		{"boreal-edge-85", "POST /api/display/power"},
		{"kestrel-vroom", "POST /api/system/standby"},
	}
	for _, c := range cases {
		t.Run(c.device, func(t *testing.T) {
			dev := store.Devices[c.device]
			if dev == nil {
				t.Fatalf("device %q not loaded", c.device)
			}
			var route *Route
		findRoute:
			for i := range dev.Interfaces {
				for j := range dev.Interfaces[i].Routes {
					if dev.Interfaces[i].Routes[j].Match == c.match {
						route = &dev.Interfaces[i].Routes[j]
						break findRoute
					}
				}
			}
			if route == nil {
				t.Fatalf("route %q not found on %q", c.match, c.device)
			}
			if len(route.Mutations) == 0 {
				t.Fatalf("route %q on %q has no list-shape mutations; expected enum-validated", c.match, c.device)
			}
			var found bool
			for _, m := range route.Mutations {
				if m.Path == "status.device.power" {
					found = true
					if len(m.Enum) == 0 {
						t.Errorf("status.device.power mutation has empty enum on %q", c.device)
					}
					expected := map[string]bool{"on": false, "off": false, "standby": false}
					for _, v := range m.Enum {
						if _, ok := expected[v]; ok {
							expected[v] = true
						}
					}
					for v, seen := range expected {
						if !seen {
							t.Errorf("enum on %q missing %q (got %v)", c.device, v, m.Enum)
						}
					}
				}
			}
			if !found {
				t.Errorf("route %q on %q has no status.device.power mutation in list shape", c.match, c.device)
			}
		})
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
	// verb. boreal has no CLI surface, so it gets a routes-only entry.
	cases := []struct {
		device   string
		httpPath string
		cliVerb  string // empty means HTTP-only profile
	}{
		// kestrel-vroom is now HTTP + ICMP only, the SSH/Telnet xAPI
		// surfaces were removed so the video bar has one teaching
		// interface (HTTP JSON). No CLI verb to cross-check; the reboot
		// anchor lives entirely on the HTTP route.
		{"kestrel-vroom", "POST /api/admin/reboot", ""},
		{"polaris-dsp-16", "POST /api/admin/reboot", "system reboot"},
		{"newtron-panel-7", "POST /api/admin/reboot", "reboot"},
		{"boreal-edge-85", "POST /api/admin/reboot", ""},
	}

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
	}{
		// kestrel-vroom is HTTP-only now (see reboot test note).
		{"kestrel-vroom", "POST /api/admin/factory-reset", ""},
		{"polaris-dsp-16", "POST /api/admin/factory-reset", "system factory-reset"},
		{"newtron-panel-7", "POST /api/admin/factory-reset", "factory-reset"},
		{"boreal-edge-85", "POST /api/admin/factory-reset", ""},
	}

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

// findProfilesRoot walks up to find the profiles/ directory.
func findProfilesRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "profiles")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find profiles/ directory")
		}
		dir = parent
	}
}
