package docslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegionsSplitsFencedFromUnfenced pins the basic contract: a document with
// one :::design container yields unfenced prose, a fenced region carrying the
// container (markers included), and unfenced prose after, with 1-indexed start
// lines.
func TestRegionsSplitsFencedFromUnfenced(t *testing.T) {
	doc := strings.Join([]string{
		"# Title",          // 1
		"built prose",      // 2
		":::design[#434]",  // 3
		"planned prose",    // 4
		":::",              // 5
		"more built prose", // 6
	}, "\n")
	regions := Regions(doc)
	want := []Region{
		{Fenced: false, StartLine: 1, Text: "# Title\nbuilt prose"},
		{Fenced: true, StartLine: 3, Text: ":::design[#434]\nplanned prose\n:::"},
		{Fenced: false, StartLine: 6, Text: "more built prose"},
	}
	if len(regions) != len(want) {
		t.Fatalf("got %d regions, want %d: %#v", len(regions), len(want), regions)
	}
	for i, w := range want {
		if regions[i] != w {
			t.Errorf("region %d = %#v, want %#v", i, regions[i], w)
		}
	}
}

// TestRegionsNestedDirective pins the nesting rule: a design fence containing a
// Starlight aside closes at its own fence, not at the aside's. The outer fence
// uses four colons, the authoring form remark-directive requires for nesting.
func TestRegionsNestedDirective(t *testing.T) {
	doc := strings.Join([]string{
		"before",               // 1
		"::::design[ADR-0071]", // 2
		":::note",              // 3
		"an aside inside",      // 4
		":::",                  // 5
		"still design prose",   // 6
		"::::",                 // 7
		"after",                // 8
	}, "\n")
	regions := Regions(doc)
	if len(regions) != 3 {
		t.Fatalf("got %d regions, want 3: %#v", len(regions), regions)
	}
	if !regions[1].Fenced || regions[1].StartLine != 2 {
		t.Errorf("fenced region = %#v, want fenced starting at line 2", regions[1])
	}
	if !strings.Contains(regions[1].Text, "still design prose") {
		t.Errorf("the inner aside's close ended the design fence early: %#v", regions[1])
	}
	if regions[2].Fenced || regions[2].StartLine != 8 {
		t.Errorf("trailing region = %#v, want unfenced starting at line 8", regions[2])
	}
}

// TestRegionsIgnoresCodeFences pins the code-fence guard in both directions: a
// ":::design" line inside a code block opens nothing, and a code block inside a
// design fence cannot close it.
func TestRegionsIgnoresCodeFences(t *testing.T) {
	t.Run("directive inside a code block is text", func(t *testing.T) {
		doc := strings.Join([]string{
			"```md",
			":::design[#434]",
			"```",
			"prose",
		}, "\n")
		regions := Regions(doc)
		for _, r := range regions {
			if r.Fenced {
				t.Fatalf("a code-block example opened a fence: %#v", regions)
			}
		}
	})
	t.Run("a close cannot carry an info string", func(t *testing.T) {
		// CommonMark: a closing code fence has no info string, so a "```go"
		// line inside a "```" block is content, not a close. A parser that
		// closes there flips its code/prose parity and misreads every fence
		// after it.
		doc := strings.Join([]string{
			"```",             // 1: opens
			"```go",           // 2: content, NOT a close
			":::design[#434]", // 3: content, still in code
			"```",             // 4: closes
			"prose",           // 5
		}, "\n")
		for _, r := range Regions(doc) {
			if r.Fenced {
				t.Fatalf("an info-string line closed the code block early: %#v", Regions(doc))
			}
		}
	})
	t.Run("code block inside a fence stays fenced", func(t *testing.T) {
		doc := strings.Join([]string{
			":::design[#434]", // 1
			"```",             // 2
			"::: not a close", // 3
			"```",             // 4
			"still design",    // 5
			":::",             // 6
			"after",           // 7
		}, "\n")
		regions := Regions(doc)
		if len(regions) != 2 {
			t.Fatalf("got %d regions, want 2: %#v", len(regions), regions)
		}
		if !regions[0].Fenced || !strings.Contains(regions[0].Text, "still design") {
			t.Errorf("code block closed the fence early: %#v", regions[0])
		}
		if regions[1].Fenced || regions[1].StartLine != 7 {
			t.Errorf("trailing region = %#v, want unfenced at line 7", regions[1])
		}
	})
}

// TestRegionsUnclosedFenceRunsToEOF pins the recovery shape: an unclosed fence
// marks the rest of the file fenced rather than panicking or silently
// unfencing, so a missing close reads as "too much fenced" (visible in review)
// instead of "strict lint skipped" (invisible).
func TestRegionsUnclosedFenceRunsToEOF(t *testing.T) {
	doc := "prose\n:::design[#434]\nplanned"
	regions := Regions(doc)
	last := regions[len(regions)-1]
	if !last.Fenced || !strings.Contains(last.Text, "planned") {
		t.Fatalf("unclosed fence did not run to EOF: %#v", regions)
	}
}

// TestVocabularyFiresInsideFence pins the epic's contract split: the vocabulary
// lint runs everywhere, fenced or not, because future design must still be
// written in current nouns. Only the existence lints (routes, permissions,
// tables) will skip fenced regions.
func TestVocabularyFiresInsideFence(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "architecture")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	page := ":::design[#434]\nthe `component_type` registry stores the shape\n:::\n"
	if err := os.WriteFile(filepath.Join(sub, "sketch.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	old := DocsRoot
	DocsRoot = dir
	defer func() { DocsRoot = old }()

	findings, err := ScanVocabulary()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1 (banned term inside a fence must still fire): %v", len(findings), findings)
	}
}
