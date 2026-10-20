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
// its copied manifest, carrying the budget mark the recorder writes only on a
// pass, and every still the manifest names, its poster one of them; the
// recorder's poster.png copy is not required. A copied directory no page
// declares is an orphan.
func TestScanWalkthroughAssets(t *testing.T) {
	dir, scripts := t.TempDir(), t.TempDir()
	write := func(root, rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	steps := `"steps":[{"n":1,"kind":"goto","caption":"c1","still":"step-01.png"},{"n":2,"kind":"wait","caption":"c2","still":"step-02.png"}]`
	script := "title: A\nsteps:\n  - goto: /x\n    caption: c1\n  - wait: { text: y }\n    caption: c2\n"
	write(dir, "a/walkthrough.json", `{"id":"a","title":"A",`+steps+`,"poster":"step-02.png","budget":{"seconds":3,"bytes":9}}`)
	write(dir, "a/step-01.png", "x")
	write(scripts, "a.yaml", script)
	write(dir, "c/walkthrough.json", `{"id":"c","title":"A",`+steps+`,"poster":"step-07.png"}`)
	write(dir, "c/step-01.png", "x")
	write(dir, "c/step-02.png", "x")
	write(scripts, "c.yaml", script)
	write(dir, "orphan/walkthrough.json", "{}")

	got := scanWalkthroughAssets(dir, scripts, map[string]string{"a": "p.md", "b": "q.md", "c": "r.md"})
	want := []string{"a/step-02.png", "b/walkthrough.json", "budget", "step-07.png", "orphan"}
	if len(got) != len(want) {
		t.Fatalf("findings = %v, want %d matching %v", got, len(want), want)
	}
	for i, w := range want {
		if !strings.Contains(got[i].Text, w) {
			t.Errorf("finding %d = %q, want it to mention %q", i, got[i].Text, w)
		}
	}
}

// TestScanWalkthroughPreview pins the animated preview (#937): a copied
// manifest that records one must name the recorder's preview.webp and carry the
// file beside it, and any preview value other than that name (empty and null
// included) is refused, as the shared JS core refuses it; a manifest that
// records none needs none, and carries none.
func TestScanWalkthroughPreview(t *testing.T) {
	dir, scripts := t.TempDir(), t.TempDir()
	write := func(root, rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	steps := `"steps":[{"n":1,"kind":"goto","caption":"c1","still":"step-01.png"}]`
	script := "title: A\nsteps:\n  - goto: /x\n    caption: c1\n"
	manifest := func(id, preview string) string {
		return `{"id":"` + id + `","title":"A",` + steps + `,"poster":"step-01.png",` + preview + `"budget":{"seconds":3,"bytes":9,"previewBytes":5}}`
	}
	ids := []string{"ok", "none", "missing", "renamed", "empty", "null", "number", "stray"}
	declared := map[string]string{}
	for _, id := range ids {
		write(dir, id+"/step-01.png", "x")
		write(scripts, id+".yaml", script)
		declared[id] = "p.md"
	}
	write(dir, "ok/walkthrough.json", manifest("ok", `"preview":"preview.webp",`))
	write(dir, "ok/preview.webp", "x")
	write(dir, "none/walkthrough.json", manifest("none", ""))
	write(dir, "missing/walkthrough.json", manifest("missing", `"preview":"preview.webp",`))
	write(dir, "renamed/walkthrough.json", manifest("renamed", `"preview":"../poster.png",`))
	write(dir, "empty/walkthrough.json", manifest("empty", `"preview":"",`))
	write(dir, "null/walkthrough.json", manifest("null", `"preview":null,`))
	write(dir, "number/walkthrough.json", manifest("number", `"preview":7,`))
	write(dir, "stray/walkthrough.json", manifest("stray", ""))
	write(dir, "stray/preview.webp", "x")

	got := scanWalkthroughAssets(dir, scripts, declared)
	// Sorted by id: empty, missing, null, number, renamed, stray.
	want := []string{`empty/walkthrough.json names preview ""`, "missing/preview.webp", "null/walkthrough.json names preview null",
		"number/walkthrough.json names preview 7", `names preview "../poster.png"`, "stray/preview.webp, but its manifest names no preview"}
	if len(got) != len(want) {
		t.Fatalf("findings = %v, want %d matching %v", got, len(want), want)
	}
	for i, w := range want {
		if !strings.Contains(got[i].Text, w) {
			t.Errorf("finding %d = %q, want it to mention %q", i, got[i].Text, w)
		}
	}
}

// TestDirectiveLines pins what counts as an embed, for both directives: a leaf
// directive alone on its line, with or without attributes; never a text
// directive, a container, or a quote in code or an HTML comment.
func TestDirectiveLines(t *testing.T) {
	for _, name := range []string{"walkthrough", "screenshot"} {
		cases := []struct {
			body string
			want map[string]int // id to line (frontmatter is 3 lines, body starts at 4)
		}{
			{"::" + name + "{#a}", map[string]int{"a": 4}},
			{"  ::" + name + "{#a title=\"y\"}", map[string]int{"a": 4}},
			{"::" + name, map[string]int{"": 4}},
			{"::" + name + "{title=\"y\"}", map[string]int{"": 4}},
			{"see :" + name + "{#a} inline", map[string]int{}},
			{"text then ::" + name + "{#a}", map[string]int{}},
			{":::" + name + "{#a}\nbody\n:::", map[string]int{}},
			{"<!-- ::" + name + "{#a} -->", map[string]int{}},
			{"<!--\n::" + name + "{#a}\n-->\n::" + name + "{#b}", map[string]int{"b": 7}},
			{"```\n::" + name + "{#a}\n```", map[string]int{}},
			{"`::" + name + "{#a}`", map[string]int{}},
		}
		for _, c := range cases {
			got := directiveLines("---\nt: x\n---\n"+c.body+"\n", name)
			if len(got) != len(c.want) {
				t.Errorf("%s: directiveLines(%q) = %v, want %v", name, c.body, got, c.want)
				continue
			}
			for id, line := range c.want {
				if got[id] != line {
					t.Errorf("%s: directiveLines(%q) = %v, want %v", name, c.body, got, c.want)
				}
			}
		}
	}
}

// TestWalkthroughDrift pins the drift gate: the copied manifest must say what
// the script says now (title, description, step count, every caption), or the
// docs show a recording of a script that has since changed.
func TestWalkthroughDrift(t *testing.T) {
	script := []byte("title: T\ndescription: D\nsteps:\n  - goto: /x\n    caption: one\n  - wait: { text: y }\n    caption: two\n")
	manifest := func(title, desc string, caps ...string) []byte {
		var steps []string
		for i, c := range caps {
			steps = append(steps, `{"n":`+itoa(i+1)+`,"caption":"`+c+`"}`)
		}
		return []byte(`{"title":"` + title + `","description":"` + desc + `","steps":[` + strings.Join(steps, ",") + `]}`)
	}
	cases := []struct {
		name     string
		manifest []byte
		want     []string
	}{
		{"in step", manifest("T", "D", "one", "two"), nil},
		{"title changed", manifest("Old", "D", "one", "two"), []string{"title"}},
		{"description changed", manifest("T", "old", "one", "two"), []string{"description"}},
		{"a step added since", manifest("T", "D", "one"), []string{"2 steps"}},
		{"a caption reworded", manifest("T", "D", "one", "TWO"), []string{"step 2"}},
	}
	for _, c := range cases {
		got := walkthroughDrift(script, c.manifest)
		if len(got) != len(c.want) {
			t.Errorf("%s: drift = %v, want %v", c.name, got, c.want)
			continue
		}
		for i, w := range c.want {
			if !strings.Contains(got[i], w) {
				t.Errorf("%s: drift %d = %q, want it to mention %q", c.name, i, got[i], w)
			}
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
