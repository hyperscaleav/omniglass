package settings

import "testing"

func TestDefaultsHaveSeededNamespaces(t *testing.T) {
	d := Defaults()
	if _, ok := d["ui"]; !ok {
		t.Fatalf("defaults missing ui namespace")
	}
	if d["ui"]["theme"] != "omniglass-dark" {
		t.Fatalf("ui.theme default = %v, want omniglass-dark", d["ui"]["theme"])
	}
	if _, ok := d["keybindings"]; !ok {
		t.Fatalf("defaults missing keybindings namespace")
	}
}

func TestNamespaceRegistryClassifiesProfileDomain(t *testing.T) {
	cv := ClientVisibleNamespaces()
	if !cv["ui"] || !cv["keybindings"] {
		t.Fatalf("ui and keybindings must be client-visible, got %v", cv)
	}
}

// The label namespace is the first platform-domain namespace and the first that
// is platform-domain AND client-visible at once, a pair no namespace had used.
// Both halves carry weight: platform means one install-wide dictionary an admin
// owns (a label is stored once and read by everybody, so a per-user dictionary
// would be a per-user spelling of a shared row), and client-visible means an
// ordinary authenticated caller may read the effective list without the admin
// settings read.
func TestLabelNamespaceIsPlatformDomainAndClientVisible(t *testing.T) {
	byName := map[string]Namespace{}
	for _, n := range Namespaces() {
		byName[n.Name] = n
	}
	ns, ok := byName["label"]
	if !ok {
		t.Fatalf("label namespace missing from the registry")
	}
	if ns.Domain != "platform" {
		t.Fatalf("label domain = %q, want platform", ns.Domain)
	}
	if !ns.ClientVisible {
		t.Fatalf("label must be client-visible: the console renders a label offline from this list")
	}
}

func TestShippedAcronymsCarryTheCasingTheEngineEmits(t *testing.T) {
	d := Defaults()
	got, ok := d["label"]["acronyms"].([]string)
	if !ok {
		t.Fatalf("label.acronyms default = %#v, want a []string", d["label"]["acronyms"])
	}
	// The dictionary entry IS the emitted form, so a shipped entry has to be
	// written the way it should read: an all-lowercase "dsp" would title "dsp" as
	// "dsp" and quietly do nothing.
	want := map[string]bool{"AV": false, "DSP": false, "HDMI": false, "PoE": false}
	for _, a := range got {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for a, found := range want {
		if !found {
			t.Fatalf("shipped acronyms missing %q (got %v)", a, got)
		}
	}
}

func TestLoadFileMissingPathIsEmpty(t *testing.T) {
	d, err := LoadFile("/nonexistent/settings.json")
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(d) != 0 {
		t.Fatalf("missing file should yield empty doc, got %v", d)
	}
}
