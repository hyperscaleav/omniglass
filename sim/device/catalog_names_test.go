package device

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// realBrand matches real AV and UC companies and their best-known product
// names. A family's identity, responses and web pages are what an operator sees
// of a simulated device, so like the boot seed it names only the fictional
// catalog; internal/seed/brands_test.go is the seed's sibling guard.
var realBrand = regexp.MustCompile(`(?i)\b(crestron|biamp|qsc|q-sys|shure|cisco|extron|sony|samsung|zoom|polycom|roomos|webex|tesira)\b`)

// TestCatalogNamesNoRealCompany holds every non-test source and embedded asset
// of the families to the fictional vendors.
func TestCatalogNamesNoRealCompany(t *testing.T) {
	files := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") {
			return err
		}
		switch filepath.Ext(path) {
		case ".go", ".html", ".js", ".css", ".txt", ".xml", ".json", ".tmpl":
		default:
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for n, line := range strings.Split(string(b), "\n") {
			if m := realBrand.FindString(line); m != "" {
				t.Errorf("%s:%d names the real company %q; the catalog uses fictional vendors", path, n+1, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("walked no family source")
	}
}

func TestRealBrandMatcher(t *testing.T) {
	for _, s := range []string{"Crestron TSS-1070", "Q-SYS QRC", "a Zoom Room", "roomos.app"} {
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

// TestDevicePagesKeepTheirAPICallsRelative guards the device admin pages'
// apiURL() convention (originally omniglass-lab#51). Each page is served by the
// simulated device's own HTTP listener, and its apiURL() helper prepends
// location.pathname so absolute "/api/..." paths stay relative to wherever the
// page is served instead of resolving against the bare origin. Every family
// page's script wraps its calls in apiURL(), and no call bypasses it with a
// bare fetch('/api/...').
func TestDevicePagesKeepTheirAPICallsRelative(t *testing.T) {
	bareAbsolute := regexp.MustCompile(`fetch\(\s*['"]/api/`)
	pages, err := filepath.Glob("*/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) == 0 {
		t.Fatal("found no family page script; they may have moved")
	}
	for _, path := range pages {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		if !strings.Contains(text, "apiURL(") {
			t.Errorf("%s: missing the apiURL(...) helper: wrap fetch paths with it so absolute /api/... paths stay relative to wherever the page is served", path)
		}
		if loc := bareAbsolute.FindStringIndex(text); loc != nil {
			t.Errorf("%s:%d: a bare fetch('/api/...') bypasses the page's served path; wrap it with apiURL()", path, strings.Count(text[:loc[0]], "\n")+1)
		}
	}
}
