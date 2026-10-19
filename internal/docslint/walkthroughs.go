package docslint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The walkthrough lint (#926). A docs page declares the walkthroughs it embeds
// in `walkthroughs` frontmatter and embeds each with `::walkthrough{#id}`, the
// way it declares and embeds screenshots. Four things must hold, all
// deterministic, so they run in `go test` rather than waiting for a recording:
//
//   - a declared id has its script, web/e2e/walkthroughs/<id>.yaml;
//   - a declared id is embedded by a directive on its own page, and a directive
//     names a declared id (docs/scripts/check-embeds.mjs gates the latter at
//     build time too; here it is checked with the rest);
//   - a declared id has its copied assets under docs/public/walkthroughs/<id>/
//     (the manifest, the poster, and every still the manifest names), written
//     by `make docs-walkthroughs`;
//   - no copied directory outlives its declaration.
//
// Whether the stills are current is not checked: that is a re-recording, which
// needs the walkthrough stack.

var walkthroughDirective = regexp.MustCompile(`::walkthrough(\{#([A-Za-z0-9_-]+)\})?`)

// walkthroughScriptDir and walkthroughAssetDir are repo-relative.
var (
	walkthroughScriptDir = filepath.Join("web", "e2e", "walkthroughs")
	walkthroughAssetDir  = filepath.Join("docs", "public", "walkthroughs")
)

// pageWalkthroughIDs returns the ids under the page's `walkthroughs`
// frontmatter, in order.
func pageWalkthroughIDs(content string) ([]string, error) {
	end := frontmatterEnd(content)
	if end < 0 {
		return nil, nil
	}
	var fm struct {
		Walkthroughs []struct {
			ID string `yaml:"id"`
		} `yaml:"walkthroughs"`
	}
	if err := yaml.Unmarshal([]byte(content[4:end-len("\n---")]), &fm); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(fm.Walkthroughs))
	for _, w := range fm.Walkthroughs {
		ids = append(ids, w.ID)
	}
	return ids, nil
}

// directiveLines returns each `::walkthrough` directive outside code (a fenced
// block or an inline span quotes the syntax, it does not embed), as id to
// 1-indexed line; a directive with no id is keyed "".
func directiveLines(content string) map[string]int {
	found := map[string]int{}
	start := 0
	if end := frontmatterEnd(content); end >= 0 {
		start = end
	}
	firstLine := strings.Count(content[:start], "\n") + 1
	inCode, delim := false, ""
	for i, line := range strings.Split(content[start:], "\n") {
		if inCode {
			t := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(t, delim) && strings.Trim(t, string(delim[0])+" \t") == "" {
				inCode = false
			}
			continue
		}
		if m := codeFenceDelim.FindStringSubmatch(line); m != nil {
			inCode, delim = true, m[1]
			continue
		}
		prose := inlineSpan.ReplaceAllString(line, "")
		for _, m := range walkthroughDirective.FindAllStringSubmatch(prose, -1) {
			if _, seen := found[m[2]]; !seen {
				found[m[2]] = firstLine + i
			}
		}
	}
	return found
}

// scanWalkthroughPage checks one page: every declared id has a script and a
// directive, every directive a declaration. It returns the declared ids for
// the asset check.
func scanWalkthroughPage(content string, scripts map[string]bool) ([]string, []Finding) {
	var findings []Finding
	declared, err := pageWalkthroughIDs(content)
	if err != nil {
		if strings.Contains(content, "\nwalkthroughs:") {
			findings = append(findings, Finding{Line: 1, Text: "the frontmatter declaring walkthroughs does not parse: " + err.Error()})
		}
		return nil, findings
	}
	directives := directiveLines(content)
	isDeclared := map[string]bool{}
	for _, id := range declared {
		isDeclared[id] = true
		if !scripts[id] {
			findings = append(findings, Finding{Line: 1, Text: "walkthrough \"" + id + "\" is declared but " +
				filepath.ToSlash(filepath.Join(walkthroughScriptDir, id+".yaml")) + " does not exist"})
		}
		if _, ok := directives[id]; !ok {
			findings = append(findings, Finding{Line: 1, Text: "walkthrough \"" + id + "\" is declared but the page has no ::walkthrough{#" + id + "} to embed it"})
		}
	}
	ids := make([]string, 0, len(directives))
	for id := range directives {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return directives[ids[a]] < directives[ids[b]] })
	for _, id := range ids {
		switch {
		case id == "":
			findings = append(findings, Finding{Line: directives[id], Text: "::walkthrough has no id (write ::walkthrough{#some-id})"})
		case !isDeclared[id]:
			findings = append(findings, Finding{Line: directives[id], Text: "::walkthrough{#" + id + "} has no walkthroughs frontmatter entry on this page"})
		}
	}
	return declared, findings
}

// scanWalkthroughAssets checks the copied assets under dir against the
// declared ids (id to the page declaring it): the manifest, the poster and
// every still it names; then any directory no page declares.
func scanWalkthroughAssets(dir string, declared map[string]string) []Finding {
	var findings []Finding
	ids := make([]string, 0, len(declared))
	for id := range declared {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		page := declared[id]
		at := func(f string) string { return filepath.ToSlash(filepath.Join(walkthroughAssetDir, id, f)) }
		missing := func(f string) Finding {
			return Finding{File: page, Line: 1, Text: "walkthrough \"" + id + "\" has no " + at(f) + "; run make walkthroughs ID=" + id + " then make docs-walkthroughs"}
		}
		b, err := os.ReadFile(filepath.Join(dir, id, "walkthrough.json"))
		if err != nil {
			findings = append(findings, missing("walkthrough.json"))
			continue
		}
		var m struct {
			ID    string `json:"id"`
			Steps []struct {
				Still string `json:"still"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			findings = append(findings, Finding{File: page, Line: 1, Text: at("walkthrough.json") + " does not parse: " + err.Error()})
			continue
		}
		if m.ID != id {
			findings = append(findings, Finding{File: page, Line: 1, Text: at("walkthrough.json") + " is walkthrough \"" + m.ID + "\", not \"" + id + "\""})
		}
		files := []string{"poster.png"}
		for _, s := range m.Steps {
			files = append(files, s.Still)
		}
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(dir, id, f)); err != nil {
				findings = append(findings, missing(f))
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if _, ok := declared[e.Name()]; !ok {
			findings = append(findings, Finding{File: filepath.ToSlash(walkthroughAssetDir), Line: 1,
				Text: filepath.ToSlash(filepath.Join(walkthroughAssetDir, e.Name())) + " is declared by no page's walkthroughs frontmatter (an orphan copy)"})
		}
	}
	return findings
}

// ScanWalkthroughs runs the walkthrough lint over the docs tree.
func ScanWalkthroughs() ([]Finding, error) {
	scripts := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(repoRoot, walkthroughScriptDir))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if name := e.Name(); !e.IsDir() && strings.HasSuffix(name, ".yaml") {
			scripts[strings.TrimSuffix(name, ".yaml")] = true
		}
	}
	var findings []Finding
	declared := map[string]string{}
	err = walkDocs(func(rel, content string) error {
		ids, page := scanWalkthroughPage(content, scripts)
		for _, f := range page {
			f.File = rel
			findings = append(findings, f)
		}
		for _, id := range ids {
			if _, seen := declared[id]; !seen {
				declared[id] = rel
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return append(findings, scanWalkthroughAssets(filepath.Join(repoRoot, walkthroughAssetDir), declared)...), nil
}
