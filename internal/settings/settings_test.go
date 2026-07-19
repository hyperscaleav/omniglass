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

func TestLoadFileMissingPathIsEmpty(t *testing.T) {
	d, err := LoadFile("/nonexistent/settings.json")
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(d) != 0 {
		t.Fatalf("missing file should yield empty doc, got %v", d)
	}
}
