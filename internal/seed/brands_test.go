package seed_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// realBrand matches the manufacturers the boot seed used to ship (#658). The
// seed is upserted into every operator's database, so a real company named
// here reads as an affiliation nobody chose; the catalog is fictional (the
// omniglass-lab brand universe) and this guard keeps it that way the next time
// a realistic-sounding example is wanted. Operators may still type any of these
// as their own data; only what the platform ships is held to the rule.
var realBrand = regexp.MustCompile(`(?i)\b(crestron|biamp|qsc|shure|cisco|extron|sony|samsung)\b`)

// shippedSeedFiles is every file in this package except the tests, which is
// what the binary embeds plus the code that loads it.
func shippedSeedFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read seed dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".go") {
			files = append(files, n)
		}
	}
	if len(files) == 0 {
		t.Fatal("no seed files found; the guard would pass vacuously")
	}
	return files
}

func TestRealBrandMatcher(t *testing.T) {
	for _, s := range []string{"Crestron TSS-1070", "vendor_id: cisco", "a Samsung QM55", "label: QSC"} {
		if !realBrand.MatchString(s) {
			t.Errorf("realBrand misses %q", s)
		}
	}
	// Word-bounded: a fictional name that merely contains a brand's letters passes.
	for _, s := range []string{"Boreal", "a Kestrel codec", "francisco", "sonya"} {
		if realBrand.MatchString(s) {
			t.Errorf("realBrand flags %q", s)
		}
	}
}

func TestSeedNamesNoRealCompany(t *testing.T) {
	for _, f := range shippedSeedFiles(t) {
		raw, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if m := realBrand.FindString(line); m != "" {
				t.Errorf("%s:%d names the real company %q; the shipped seed uses the fictional catalog (#658)", f, i+1, m)
			}
		}
	}
}
