package docslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanWalkthroughPage pins the per-page walkthrough lint (#926): a
// declared id needs its script and a directive on the same page, and a
// directive needs a declaration; a directive quoted in code is teaching, not
// an embed.
func TestScanWalkthroughPage(t *testing.T) {
	scripts := map[string]bool{"sample-simulation": true}
	page := func(fm, body string) string { return "---\ntitle: X\n" + fm + "---\n\n" + body + "\n" }
	cases := []struct {
		name    string
		content string
		want    []string // substrings, one per expected finding
	}{
		{"declared, scripted and used", page("walkthroughs:\n  - id: sample-simulation\n", "::walkthrough{#sample-simulation}"), nil},
		{"no walkthroughs at all", page("", "prose"), nil},
		{"declared with no script", page("walkthroughs:\n  - id: ghost\n", "::walkthrough{#ghost}"), []string{"web/e2e/walkthroughs/ghost.yaml"}},
		{"declared, never embedded", page("walkthroughs:\n  - id: sample-simulation\n", "prose"), []string{"no ::walkthrough{#sample-simulation}"}},
		{"embedded, never declared", page("", "::walkthrough{#sample-simulation}"), []string{"no walkthroughs frontmatter entry"}},
		{"a directive with no id", page("", "::walkthrough"), []string{"has no id"}},
		{"quoted in an inline span", page("", "write `::walkthrough{#sample-simulation}` to embed"), nil},
		{"quoted in a code block", page("", "```markdown\n::walkthrough{#sample-simulation}\n```"), nil},
		{"embedded only inside a code block", page("walkthroughs:\n  - id: sample-simulation\n", "```markdown\n::walkthrough{#sample-simulation}\n```"), []string{"no ::walkthrough{#sample-simulation}"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, got := scanWalkthroughPage(c.content, scripts)
			if len(got) != len(c.want) {
				t.Fatalf("findings = %v, want %d matching %v", got, len(c.want), c.want)
			}
			for i, w := range c.want {
				if !strings.Contains(got[i].Text, w) {
					t.Errorf("finding %d = %q, want it to mention %q", i, got[i].Text, w)
				}
			}
		})
	}
}

// TestScanWalkthroughPageDeclared pins what the page scan hands the asset
// check: the declared ids, in frontmatter order.
func TestScanWalkthroughPageDeclared(t *testing.T) {
	content := "---\ntitle: X\nwalkthroughs:\n  - id: b\n  - id: a\n---\n\n::walkthrough{#a}\n::walkthrough{#b}\n"
	declared, findings := scanWalkthroughPage(content, map[string]bool{"a": true, "b": true})
	if len(findings) != 0 || strings.Join(declared, ",") != "b,a" {
		t.Fatalf("declared = %v, findings = %v; want [b a] and none", declared, findings)
	}
}

// TestScanWalkthroughAssets pins the asset half: a declared walkthrough has
// its copied manifest, poster and every still the manifest names; a copied
// directory no page declares is an orphan.
func TestScanWalkthroughAssets(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"id":"a","title":"A","steps":[{"n":1,"kind":"goto","caption":"c","still":"step-01.png"},{"n":2,"kind":"wait","caption":"c","still":"step-02.png"}],"poster":"step-02.png"}`
	write("a/walkthrough.json", manifest)
	write("a/poster.png", "x")
	write("a/step-01.png", "x")
	write("orphan/walkthrough.json", "{}")

	got := scanWalkthroughAssets(dir, map[string]string{"a": "p.md", "b": "q.md"})
	want := []string{"step-02.png", "b/walkthrough.json", "orphan"}
	if len(got) != len(want) {
		t.Fatalf("findings = %v, want %d matching %v", got, len(want), want)
	}
	for i, w := range want {
		if !strings.Contains(got[i].Text, w) {
			t.Errorf("finding %d = %q, want it to mention %q", i, got[i].Text, w)
		}
	}
}

func TestWalkthroughReferences(t *testing.T) {
	findings, err := ScanWalkthroughs()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "walkthrough", findings, true)
}
