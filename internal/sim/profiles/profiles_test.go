package profiles

import (
	"bufio"
	"bytes"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// realBrand matches real AV and UC companies and their best-known product
// names. The catalog's docs and responses are what an operator sees of a
// simulated device, so like the boot seed it names only the fictional
// catalog; internal/seed/brands_test.go is the
// seed's sibling guard.
var realBrand = regexp.MustCompile(`(?i)\b(crestron|biamp|qsc|q-sys|shure|cisco|extron|sony|samsung|zoom|polycom|roomos|webex|tesira)\b`)

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

// TestCatalogCarriesNoLabSystemVocabulary holds the embedded catalog to one
// vocabulary for simulated systems (#914): the platform's. A simulated system
// is built to a standard from the template catalog, so the catalog embeds no
// room archetype (systems/) or fleet manifest (fleets/) of its own, and no
// file names a role or system type the platform's standards and system types
// do not.
func TestCatalogCarriesNoLabSystemVocabulary(t *testing.T) {
	for _, dir := range []string{"systems", "fleets"} {
		if _, err := fs.Stat(FS, dir); err == nil {
			t.Errorf("the catalog embeds %s/: a simulated system is built from the platform's standards, not a lab %s file", dir, dir)
		}
	}

	known := map[string]bool{}
	for _, st := range seed.ShippedSystemTypes() {
		known[st] = true
	}
	std := template.Shipped()
	for _, tpl := range std.List(template.KindStandard) {
		known[tpl.Name] = true
		for _, r := range tpl.Standard.Roles {
			known[r.Name] = true
		}
	}

	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".yaml" || strings.HasPrefix(p, "templates/") {
			return err
		}
		data, err := fs.ReadFile(FS, p)
		if err != nil {
			return err
		}
		var doc any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		for _, name := range systemVocabulary(doc) {
			if !known[name] {
				t.Errorf("%s names the role or system type %q, which is no standard role or system type of the platform", p, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// systemVocabulary is every value a decoded YAML tree gives a role or
// system_type key.
func systemVocabulary(n any) []string {
	var out []string
	switch v := n.(type) {
	case map[string]any:
		for k, c := range v {
			if s, ok := c.(string); ok && (k == "role" || k == "system_type") {
				out = append(out, s)
				continue
			}
			out = append(out, systemVocabulary(c)...)
		}
	case []any:
		for _, c := range v {
			out = append(out, systemVocabulary(c)...)
		}
	}
	return out
}

func TestSystemVocabularyFindsRolesAndTypes(t *testing.T) {
	var doc any
	src := "devices:\n  - role: video-bar\n  - nested: {system_type: Boardroom}\n  - name: kestrel\n"
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range systemVocabulary(doc) {
		got[n] = true
	}
	if len(got) != 2 || !got["video-bar"] || !got["Boardroom"] {
		t.Errorf("systemVocabulary = %v, want video-bar and Boardroom", got)
	}
}
