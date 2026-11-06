package storagetest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheParallelGuardTellsParallelSerialAndNeither(t *testing.T) {
	t.Parallel()
	src := []byte(`package p

import "testing"

func TestMain(m *testing.M) {}
func TestParallel(t *testing.T) {
	t.Parallel()
}
func TestSerialWithReason(t *testing.T) {
	// Serial: it lowers a package-level cap.
}
func TestNeither(t *testing.T) {
	_ = 1
}
func TestParallelNotFirst(t *testing.T) {
	_ = 1
	t.Parallel()
}
func TestEmpty(t *testing.T) {}
func helper(t *testing.T)   {}
func TestOther(x int)       {}
`)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p_test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(testsNeitherParallelNorSerial(fset, f, src), ",")
	if want := "TestNeither,TestParallelNotFirst,TestEmpty"; got != want {
		t.Fatalf("flagged %q, want %q", got, want)
	}
}

// The directory scan reports file and test, and refuses a directory with nothing
// to scan rather than passing vacuously.
func TestUnparallelTestsScansADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := "package p\n\nimport \"testing\"\n\nfunc TestSlow(t *testing.T) {}\nfunc TestFast(t *testing.T) { t.Parallel() }\n"
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := UnparallelTests(dir)
	if err != nil || len(got) != 1 || got[0] != "a_test.go: TestSlow" {
		t.Fatalf("UnparallelTests = %v, %v; want [a_test.go: TestSlow]", got, err)
	}
	if _, err := UnparallelTests(t.TempDir()); err == nil {
		t.Error("an empty directory passed the scan")
	}
}
