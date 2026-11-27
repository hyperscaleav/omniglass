package simfamilygen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mod = "github.com/hyperscaleav/omniglass"

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const family = "package %s\n\nimport \"github.com/hyperscaleav/omniglass/sim/spec\"\n\nfunc Spec() spec.Spec { return spec.Spec{Name: %q} }\n"

func TestScanFindsEveryFamilyPackageSortedAndSkipsWhatIsNone(t *testing.T) {
	root := t.TempDir()
	write(t, root, "zeta/zeta.go", strings.NewReplacer("%s", "zeta", "%q", `"zeta"`).Replace(family))
	write(t, root, "borealedge/spec.go", strings.NewReplacer("%s", "borealedge", "%q", `"boreal-edge"`).Replace(family))
	write(t, root, "borealedge/spec_test.go", "package borealedge\n\nfunc Spec() int { return 0 }\n")
	write(t, root, "internal/testfamily/x.go", "package testfamily\n")
	write(t, root, "testdata/y.go", "package y\n")
	write(t, root, "_scratch/z.go", "package z\n")
	write(t, root, "device.go", "package device\n") // a file here is no family

	got, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Family{Dir: "borealedge", Name: "borealedge"}) || got[1] != (Family{Dir: "zeta", Name: "zeta"}) {
		t.Fatalf("families = %+v, want borealedge then zeta", got)
	}
}

func TestScanRefusesAPackageThatRegistersNothing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "orphan/orphan.go", "package orphan\n\nfunc Other() {}\n")
	// A Spec with a receiver, or taking arguments, is not the family's Spec.
	write(t, root, "wrongshape/w.go", "package wrongshape\n\nfunc Spec(x int) int { return x }\n")
	if _, err := Scan(root); err == nil || !strings.Contains(err.Error(), "declares no `func Spec() spec.Spec`") {
		t.Fatalf("err = %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "orphan")); err != nil {
		t.Skip("cannot narrow the fixture")
	}
	if _, err := Scan(root); err == nil || !strings.Contains(err.Error(), "wrongshape") {
		t.Fatalf("err = %v, want the wrong-shaped Spec refused", err)
	}
}

func TestRenderListsEachFamilyAndIsStable(t *testing.T) {
	none, err := Render(mod, nil)
	if err != nil || !strings.Contains(string(none), "return nil") || !strings.Contains(string(none), "DO NOT EDIT") {
		t.Fatalf("empty registry = %s (%v)", none, err)
	}
	fams := []Family{{Dir: "borealedge", Name: "borealedge"}, {Dir: "ferrite-pdu", Name: "ferritepdu"}}
	a, err := Render(mod, fams)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Render(mod, fams)
	if string(a) != string(b) {
		t.Fatal("render is not deterministic")
	}
	for _, want := range []string{
		`fam_borealedge "github.com/hyperscaleav/omniglass/sim/device/borealedge"`,
		`fam_ferrite_pdu "github.com/hyperscaleav/omniglass/sim/device/ferrite-pdu"`,
		"fam_borealedge.Spec(),",
		"fam_ferrite_pdu.Spec(),",
	} {
		if !strings.Contains(string(a), want) {
			t.Errorf("registry lacks %q:\n%s", want, a)
		}
	}
}

// The committed registry is what the scan renders: adding or removing a
// family package without running make gen fails here, the same drift gen-drift
// catches in CI.
func TestCommittedRegistryMatchesTheDirectories(t *testing.T) {
	root := filepath.Join("..", "..", "sim", "device")
	fams, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Render(mod, fams)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "families_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("sim/device/families_gen.go is stale: run make gen\nwant:\n%s", want)
	}
}
