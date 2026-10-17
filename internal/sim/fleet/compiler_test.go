package fleet

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

func TestCompile(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	manifest, err := ParseManifestFS(profiles.Fleets(), "training-default.yaml")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	fleet, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if fleet.Name != "training-default" {
		t.Errorf("fleet name = %q, want training-default", fleet.Name)
	}
	if fleet.Domain != "acme.lab" {
		t.Errorf("domain = %q, want acme.lab", fleet.Domain)
	}

	// The manifest defines:
	// BOI > 17C > 4th floor: 2 rooms (415B, 420A) × large-conference-a (5 devices each) = 10
	// BOI > 17C > 3rd floor: 2 rooms (count=2, start 300) × large-conference-a (5 devices each) = 10
	// Total: 20 devices (codec + touchpanel + 2 displays + DSP per room)
	expectedDevices := 20
	if len(fleet.Devices) != expectedDevices {
		t.Fatalf("expected %d devices, got %d", expectedDevices, len(fleet.Devices))
	}
}

func TestHostnameGeneration(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	manifest, err := ParseManifestFS(profiles.Fleets(), "training-default.yaml")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	fleet, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Check that expected hostnames exist.
	hostnames := make(map[string]bool)
	for _, d := range fleet.Devices {
		hostnames[d.Hostname] = true
	}

	// training-default has one site (BOI), one building (17C), and
	// uses the legacy nested-floors structure. The compiler keeps
	// the building segment because room numbers (415B, 300, …)
	// don't encode building, and drops floor because room numbers
	// already encode floor.
	expected := []string{
		"BOI-17C-415B-COD1",
		"BOI-17C-415B-TP1",
		"BOI-17C-415B-DS1",
		"BOI-17C-415B-DS2",
		"BOI-17C-420A-COD1",
		"BOI-17C-300-COD1",
		"BOI-17C-301-COD1",
	}
	for _, h := range expected {
		if !hostnames[h] {
			t.Errorf("expected hostname %q not found", h)
		}
	}
}

// digString walks a nested map[string]any by key path and returns the
// string leaf (empty if any segment is missing or not a string).
func digString(t *testing.T, m map[string]any, path ...string) string {
	t.Helper()
	cur := any(m)
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	s, _ := cur.(string)
	return s
}

// TestDeviceDefaultsTemplatedPerRoom asserts that a shared device profile's
// identity defaults resolve per room rather than carrying a baked
// single-room constant. The kestrel-vroom (video bar) profile fans out to
// every room, so its ucc_signin must be room-scoped and its touch_pair must
// name the actual paired touch panel in the SAME room. The touch panel's
// hostname suffix is system-specific (TP in large-conference-a), so
// touch_pair is resolved across devices via {room-device:<profile>}.
func TestDeviceDefaultsTemplatedPerRoom(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	manifest, err := ParseManifestFS(profiles.Fleets(), "training-default.yaml")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	est, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	byHost := make(map[string]CompiledDevice)
	for _, d := range est.Devices {
		byHost[d.Hostname] = d
	}

	// training-default uses large-conference-a (codec=COD, touch panel=TP).
	for _, room := range []string{"415B", "420A", "300", "301"} {
		codec, ok := byHost["BOI-17C-"+room+"-COD1"]
		if !ok {
			t.Fatalf("no codec for room %s", room)
		}
		if got, want := digString(t, codec.Defaults, "status", "app", "ucc_signin"),
			"room-BOI-17C-"+room+"@shinra.lan"; got != want {
			t.Errorf("room %s ucc_signin = %q, want %q", room, got, want)
		}
		if got, want := digString(t, codec.Defaults, "status", "peripherals", "touch_pair"),
			"BOI-17C-"+room+"-TP1"; got != want {
			t.Errorf("room %s touch_pair = %q, want %q (paired panel in the same room)", room, got, want)
		}
	}
}

func TestHostnamesUnique(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	manifest, err := ParseManifestFS(profiles.Fleets(), "training-default.yaml")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	fleet, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	seen := make(map[string]bool)
	for _, d := range fleet.Devices {
		if seen[d.Hostname] {
			t.Errorf("duplicate hostname: %s", d.Hostname)
		}
		seen[d.Hostname] = true
	}
}

func TestSlugsUnique(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	manifest, err := ParseManifestFS(profiles.Fleets(), "training-default.yaml")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	fleet, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	seen := make(map[string]bool)
	for _, d := range fleet.Devices {
		if seen[d.HostID] {
			t.Errorf("duplicate slug: %s", d.HostID)
		}
		seen[d.HostID] = true
	}
}

func TestIPAllocation(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	manifest, err := ParseManifestFS(profiles.Fleets(), "training-default.yaml")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	fleet, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	_, cidr, _ := net.ParseCIDR("127.1.0.0/16")

	seen := make(map[string]bool)
	for _, d := range fleet.Devices {
		ip := d.LoopbackIP.String()
		if seen[ip] {
			t.Errorf("duplicate IP: %s", ip)
		}
		seen[ip] = true

		if !cidr.Contains(d.LoopbackIP) {
			t.Errorf("IP %s not in expected CIDR", ip)
		}
	}

	// First IP should be network+1.
	first := fleet.Devices[0].LoopbackIP.String()
	if first != "127.1.0.1" {
		t.Errorf("first IP = %s, want 127.1.0.1", first)
	}
}

func TestCompileDeterministic(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	manifest, err := ParseManifestFS(profiles.Fleets(), "training-default.yaml")
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	fleet1, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("first Compile: %v", err)
	}

	fleet2, err := Compile(manifest, store)
	if err != nil {
		t.Fatalf("second Compile: %v", err)
	}

	if len(fleet1.Devices) != len(fleet2.Devices) {
		t.Fatal("different device counts across compilations")
	}

	for i := range fleet1.Devices {
		d1, d2 := fleet1.Devices[i], fleet2.Devices[i]
		if d1.Hostname != d2.Hostname {
			t.Errorf("device %d hostname mismatch: %s vs %s", i, d1.Hostname, d2.Hostname)
		}
		if d1.HostID != d2.HostID {
			t.Errorf("device %d slug mismatch: %s vs %s", i, d1.HostID, d2.HostID)
		}
		if !d1.LoopbackIP.Equal(d2.LoopbackIP) {
			t.Errorf("device %d IP mismatch: %s vs %s", i, d1.LoopbackIP, d2.LoopbackIP)
		}
	}
}

func TestEnrichDefaults(t *testing.T) {
	mkState := func() map[string]any {
		return map[string]any{
			"configuration": map[string]any{
				"device":  map[string]any{"serial": "", "model": "Edge 86"},
				"network": map[string]any{"hostname": ""},
			},
			"status": map[string]any{
				"network": map[string]any{
					"ipv4": map[string]any{"address": "", "mac": ""},
				},
			},
		}
	}

	d := CompiledDevice{
		Hostname:   "BOI-17C-415B-DS1",
		HostID:     "boreal-edge-a1b2c3",
		LoopbackIP: net.ParseIP("10.40.1.7"),
		Defaults:   mkState(),
	}
	enrichDefaults(&d)

	cfg := d.Defaults["configuration"].(map[string]any)
	if got := cfg["network"].(map[string]any)["hostname"]; got != "BOI-17C-415B-DS1" {
		t.Errorf("hostname = %q, want the generated hostname", got)
	}
	if got := cfg["device"].(map[string]any)["serial"].(string); got == "" {
		t.Error("serial still blank after enrich")
	}
	if got := cfg["device"].(map[string]any)["model"]; got != "Edge 86" {
		t.Errorf("model = %q, enrich must not stomp non-blank fields", got)
	}
	ipv4 := d.Defaults["status"].(map[string]any)["network"].(map[string]any)["ipv4"].(map[string]any)
	if ipv4["address"] != "10.40.1.7" {
		t.Errorf("ipv4.address = %q, want 10.40.1.7", ipv4["address"])
	}
	if mac := ipv4["mac"].(string); mac == "" || mac[:3] != "02:" {
		t.Errorf("mac = %q, want locally-administered 02:* MAC", mac)
	}

	// Determinism: same HostID -> same serial and MAC.
	if generateSerial("boreal-edge-a1b2c3") != generateSerial("boreal-edge-a1b2c3") {
		t.Error("serial not deterministic")
	}
	if generateMAC("boreal-edge-a1b2c3") != generateMAC("boreal-edge-a1b2c3") {
		t.Error("MAC not deterministic")
	}
	if generateSerial("a") == generateSerial("b") {
		t.Error("serial should vary by HostID")
	}

	// Missing path: a profile that omits a field gets nothing injected.
	sparse := map[string]any{"configuration": map[string]any{"device": map[string]any{}}}
	enrichDefaults(&CompiledDevice{HostID: "x", Defaults: sparse})
	if _, ok := sparse["configuration"].(map[string]any)["device"].(map[string]any)["serial"]; ok {
		t.Error("setBlankLeaf created a key that the profile did not declare")
	}
}

func TestIPAllocatorExhaustion(t *testing.T) {
	alloc, err := newIPAllocator("127.0.0.0/30") // Only 2 usable IPs
	if err != nil {
		t.Fatal(err)
	}
	_, err = alloc.next()
	if err != nil {
		t.Fatalf("first IP: %v", err)
	}
	_, err = alloc.next()
	if err != nil {
		t.Fatalf("second IP: %v", err)
	}
	_, err = alloc.next()
	if err == nil {
		t.Error("expected exhaustion error")
	}
}

func TestDisplayNamesUnique(t *testing.T) {
	gen := newDisplayNameGenerator()
	seen := make(map[string]bool)
	// Generate more names than we'd typically need. Empty seed
	// triggers the internal sequence counter so each call still
	// returns a unique name (preserving the prior contract).
	for i := 0; i < 500; i++ {
		name := gen.next("")
		if seen[name] {
			t.Errorf("duplicate display name at index %d: %s", i, name)
		}
		seen[name] = true
	}
}

func TestDisplayNamesSpreadAcrossAdjectives(t *testing.T) {
	// With seeded inputs the generator should NOT clump every name
	// into a single adjective bucket, that was the bug operators
	// hit at scale (every system started with "Boring *"). Hash
	// 1000 distinct seeds and confirm we touch a healthy number
	// of distinct adjectives.
	gen := newDisplayNameGenerator()
	adj := map[string]int{}
	for i := 0; i < 1000; i++ {
		name := gen.next(fmt.Sprintf("seed-%d", i))
		// adjective is everything before the first space
		// ("Affectionate Bose" -> "Affectionate")
		for j := 0; j < len(name); j++ {
			if name[j] == ' ' {
				adj[name[:j]]++
				break
			}
		}
	}
	if len(adj) < 50 {
		t.Errorf("expected at least 50 distinct adjectives across 1000 seeds, got %d", len(adj))
	}
}

func TestDisplayNamesAreHumanized(t *testing.T) {
	// Direct shape contract: each generated name must be
	// "Adjective Surname", capitalized first letter on each word,
	// single space separator, no underscore. Lab console, omniglass
	// UI, and any other consumer all see the same friendly form
	// because we humanize at the source.
	gen := newDisplayNameGenerator()
	for i := 0; i < 50; i++ {
		name := gen.next(fmt.Sprintf("seed-%d", i))
		if name == "" {
			t.Fatal("generator returned empty name")
		}
		if !strings.Contains(name, " ") {
			t.Errorf("expected space separator, got %q", name)
		}
		if strings.Contains(name, "_") {
			t.Errorf("did not expect underscore in humanized name, got %q", name)
		}
		if name[0] < 'A' || name[0] > 'Z' {
			t.Errorf("expected leading uppercase, got %q", name)
		}
	}
}

func TestManifestValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name:    "valid",
			yaml:    "name: test\ndomain: test.lab\nsites:\n- code: A\n  cidr: \"127.0.0.0/16\"\n",
			wantErr: false,
		},
		{
			name:    "missing name",
			yaml:    "domain: test.lab\nsites:\n- code: A\n  cidr: \"127.0.0.0/16\"\n",
			wantErr: true,
		},
		{
			name:    "missing domain",
			yaml:    "name: test\nsites:\n- code: A\n  cidr: \"127.0.0.0/16\"\n",
			wantErr: true,
		},
		{
			name:    "no sites",
			yaml:    "name: test\ndomain: test.lab\nsites: []\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "manifest.yaml")
			os.WriteFile(path, []byte(tt.yaml), 0644)
			_, err := ParseManifest(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseManifest() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
