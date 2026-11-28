package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// retiredYAMLFindings is every path in a repo tree that brings the simulator's
// YAML device profiles or their behavior templates back (#2837). A simulated
// device is a Go family (sim/device/<family>, ADR-2829): its identity, faces,
// state and behavior are typed Go, and its responses are typed values the
// engine encodes. So the tree holds, under sim/ and internal/simfamilygen/:
//
//   - no YAML file: a profile, a surface fragment, a schema or a fixture of one;
//   - no response or behavior template (*.gotempl, *.gotmpl), the files a
//     profile's routes and commands rendered;
//   - no sim/profiles package, the embed of both;
//   - no import of the YAML decoder: a loader for any such file.
//
// A simulated device's web page is html/template markup embedded in its own
// package (*.html), which is neither.
func retiredYAMLFindings(root string) ([]string, error) {
	var found []string
	err := filepath.Walk(filepath.Join(root, "sim"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if rel == "sim/profiles" {
				found = append(found, rel+": the simulator's YAML catalog package is retired")
			}
			return nil
		}
		switch ext := strings.ToLower(filepath.Ext(path)); {
		case ext == ".yaml" || ext == ".yml":
			found = append(found, rel+": a YAML file under sim/ (a device is a Go family)")
		case strings.HasSuffix(path, ".gotempl") || strings.HasSuffix(path, ".gotmpl"):
			found = append(found, rel+": a behavior or response template (a device answers in Go)")
		case ext == ".go":
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if strings.Contains(string(b), `"gopkg.in/yaml.v3"`) {
				found = append(found, rel+": imports the YAML decoder (nothing under sim/ reads a profile file)")
			}
		}
		return nil
	})
	sort.Strings(found)
	return found, err
}

// TestNoYAMLDeviceProfileOrBehaviorTemplateReappears fails the build when a
// YAML device profile, a behavior template or a loader for either comes back.
func TestNoYAMLDeviceProfileOrBehaviorTemplateReappears(t *testing.T) {
	found, err := retiredYAMLFindings(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("the simulator's YAML profiles are retired (#2837); a device is a Go family under sim/device:\n  %s", strings.Join(found, "\n  "))
	}
}

// The guard sees each way a profile comes back, and passes a tree of Go and
// the web pages a family embeds.
func TestRetiredYAMLGuardSeesWhatItGuards(t *testing.T) {
	write := func(root, rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clean := t.TempDir()
	write(clean, "sim/device/lamp/lamp.go", "package lamp\n")
	write(clean, "sim/device/lamp/dashboard.html", "<html></html>\n")
	write(clean, "internal/seed/products.yaml", "a: b\n") // outside sim/: not the guard's
	if got, err := retiredYAMLFindings(clean); err != nil || len(got) != 0 {
		t.Fatalf("a Go-only tree: %v, %v", got, err)
	}
	for name, file := range map[string][2]string{
		"a device profile":      {"sim/profiles/devices/lamp.yaml", "name: lamp\n"},
		"a profile under sim":   {"sim/lamp.yml", "name: lamp\n"},
		"a behavior template":   {"sim/device/lamp/status.json.gotempl", "{}\n"},
		"a template, other ext": {"sim/device/lamp/status.gotmpl", "{}\n"},
		"a YAML loader":         {"sim/loader/load.go", "package loader\nimport \"gopkg.in/yaml.v3\"\n"},
	} {
		root := t.TempDir()
		write(root, file[0], file[1])
		if got, err := retiredYAMLFindings(root); err != nil || len(got) == 0 {
			t.Errorf("%s: findings %v (%v), want it caught", name, got, err)
		}
	}
}
