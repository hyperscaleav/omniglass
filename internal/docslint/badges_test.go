package docslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArchPage(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, "architecture")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func page(badge, body string) string {
	return "---\ntitle: X\nsidebar:\n  badge:\n    text: " + badge + "\n    variant: note\n---\n\n" + body
}

// TestBadgeFenceShapes pins the three failure shapes one by one, each against a
// minimal fixture corpus, and the passing forms beside them.
func TestBadgeFenceShapes(t *testing.T) {
	cases := []struct {
		name string
		file string
		body string
		want int // findings
	}{
		{
			"a Partial page with zero fences fires",
			"p.md", page("Partial", "prose that claims things\n"), 1,
		},
		{
			"a Partial page with a fence passes",
			"p.md", page("Partial", "built prose\n\n:::design[#434]\nplanned\n:::\n"), 0,
		},
		{
			"a Design page with substantive prose outside its fence fires",
			"d.md", page("Design", ":::design[#434]\nplanned\n:::\n\na bare model claim outside the fence\n"), 1,
		},
		{
			"a Design page fully fenced passes",
			"d.md", page("Design", ":::design[#434]\nplanned prose\n\n## A heading\n\nmore planned\n:::\n"), 0,
		},
		{
			"a Design page with only headings, asides, and links outside passes",
			"d.md", page("Design", "## Overview\n\n:::note[Built today]\nthe one built fact\n:::\n\n:::design[#434]\nplanned\n:::\n"), 0,
		},
		{
			"a Built page with a fence fires",
			"b.md", page("Built", "real prose\n\n:::design[#434]\nplanned\n:::\n"), 1,
		},
		{
			"a Built page with no fence passes",
			"b.md", page("Built", "real prose\n"), 0,
		},
		{
			"a page with no badge is skipped",
			"meta.md", "---\ntitle: Meta\n---\n\nanything at all\n", 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeArchPage(t, dir, c.file, c.body)
			old := DocsRoot
			DocsRoot = dir
			defer func() { DocsRoot = old }()
			findings, err := ScanBadgeFences()
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != c.want {
				t.Fatalf("got %d findings, want %d: %v", len(findings), c.want, findings)
			}
		})
	}
}

// TestBadgeFenceFlipWithoutFenceChange pins the acceptance line "flipping any
// page's badge without changing its fences fails the test": the same fenced
// body passes as Partial and fires as Built, and the same unfenced body passes
// as Built and fires as Partial.
func TestBadgeFenceFlipWithoutFenceChange(t *testing.T) {
	fenced := "built prose\n\n:::design[#434]\nplanned\n:::\n"
	unfenced := "built prose only\n"
	for _, c := range []struct {
		badge, body string
		want        int
	}{
		{"Partial", fenced, 0},
		{"Built", fenced, 1},
		{"Built", unfenced, 0},
		{"Partial", unfenced, 1},
	} {
		dir := t.TempDir()
		writeArchPage(t, dir, "x.md", page(c.badge, c.body))
		old := DocsRoot
		DocsRoot = dir
		findings, err := ScanBadgeFences()
		DocsRoot = old
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != c.want {
			t.Fatalf("badge %s over %q: got %d findings, want %d: %v", c.badge, c.body, len(findings), c.want, findings)
		}
	}
}

// TestBadgeFenceCoverage runs the lint over the real corpus, enforced: the
// migrated corpus keeps every badge consistent with its fence census.
func TestBadgeFenceCoverage(t *testing.T) {
	findings, err := ScanBadgeFences()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "badge-fence-coverage", findings, true)
}

// TestSubstantiveLines pins the classifier the Design-page shape relies on:
// headings, blanks, imports, directive markers, aside bodies, and code do not
// count; prose and list claims do.
func TestSubstantiveLines(t *testing.T) {
	doc := strings.Join([]string{
		"## Heading",           // no
		"",                     // no
		"import X from './x';", // no
		":::note[aside]",       // no
		"an aside body line",   // no (inside a non-design container)
		":::",                  // no
		"```",                  // no
		"code line",            // no
		"```",                  // no
		"a prose claim",        // yes
		"- a list claim",       // yes
	}, "\n")
	got := substantiveLines(doc)
	if len(got) != 2 || got[0] != 10 || got[1] != 11 {
		t.Fatalf("substantiveLines = %v, want [10 11]", got)
	}
}
