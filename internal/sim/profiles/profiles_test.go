package profiles

import (
	"bufio"
	"bytes"
	"io/fs"
	"regexp"
	"testing"
)

// realBrand matches real AV and UC companies. The catalog renders into the
// console (device docs, the API surface drawer), so like the boot seed it
// names only the fictional catalog; internal/seed/brands_test.go is the
// seed's sibling guard.
var realBrand = regexp.MustCompile(`(?i)\b(crestron|biamp|qsc|q-sys|shure|cisco|extron|sony|samsung|zoom|polycom)\b`)

func TestCatalogNamesNoRealCompany(t *testing.T) {
	files := 0
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		data, err := fs.ReadFile(FS, path)
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 1; sc.Scan(); n++ {
			if m := realBrand.FindString(sc.Text()); m != "" {
				t.Errorf("%s:%d names the real company %q; the catalog uses fictional vendors", path, n, m)
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("walked an empty catalog")
	}
}

func TestRealBrandMatcher(t *testing.T) {
	for _, s := range []string{"Crestron TSS-1070", "Q-SYS QRC", "a Zoom Room"} {
		if !realBrand.MatchString(s) {
			t.Errorf("realBrand missed %q", s)
		}
	}
	for _, s := range []string{"Shinra Cloud", "a Kestrel codec", "francisco", "zoomed-in"} {
		if realBrand.MatchString(s) {
			t.Errorf("realBrand matched %q", s)
		}
	}
}
