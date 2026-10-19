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
//   - a declared id has its copied assets under docs/public/walkthroughs/<id>/,
//     written by `make docs-walkthroughs`: the manifest, carrying the budget
//     mark the recorder adds only when the recording passed, and every still it
//     names, the poster one of them, and the animated preview (preview.webp)
//     when the manifest records one;
//   - the copied manifest has not drifted from the script: the same title,
//     description, step count and captions;
//   - no copied directory outlives its declaration.
//
// Whether the stills' pixels are current is not checked: that is a
// re-recording, which needs the walkthrough stack.

var directiveID = regexp.MustCompile(`(?:^|\s)#([A-Za-z0-9_-]+)`)

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

// directiveLines returns each embed of directive `name` as id to 1-indexed
// line; a directive with no id is keyed "". An embed is a leaf directive alone
// on its line, the shape remark-directive parses: an inline text directive, a
// `:::` container, or a quote of the syntax in a fenced block, an inline span
// or an HTML comment teaches the syntax, it does not embed. The docs build's
// pre-build gate reads the same shape (docs/scripts/lib/embed-scan.mjs).
func directiveLines(content, name string) map[string]int {
	leaf := regexp.MustCompile(`^\s*::` + regexp.QuoteMeta(name) + `(?:\{([^}]*)\})?\s*$`)
	found := map[string]int{}
	start := 0
	if end := frontmatterEnd(content); end >= 0 {
		start = end
	}
	firstLine := strings.Count(content[:start], "\n") + 1
	inCode, delim, inComment := false, "", false
	for i, line := range strings.Split(content[start:], "\n") {
		if inCode {
			t := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(t, delim) && strings.Trim(t, string(delim[0])+" \t") == "" {
				inCode = false
			}
			continue
		}
		if !inComment {
			if m := codeFenceDelim.FindStringSubmatch(line); m != nil {
				inCode, delim = true, m[1]
				continue
			}
		}
		var kept strings.Builder
		for line != "" {
			if inComment {
				j := strings.Index(line, "-->")
				if j < 0 {
					line = ""
					continue
				}
				line, inComment = line[j+3:], false
				continue
			}
			j := strings.Index(line, "<!--")
			if j < 0 {
				kept.WriteString(line)
				line = ""
				continue
			}
			kept.WriteString(line[:j])
			line, inComment = line[j+4:], true
		}
		m := leaf.FindStringSubmatch(inlineSpan.ReplaceAllString(kept.String(), ""))
		if m == nil {
			continue
		}
		id := ""
		if idm := directiveID.FindStringSubmatch(m[1]); idm != nil {
			id = idm[1]
		}
		if _, seen := found[id]; !seen {
			found[id] = firstLine + i
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
	directives := directiveLines(content, "walkthrough")
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

// walkthroughManifest is the part of a recorder manifest (walkthrough.json)
// the lint reads.
type walkthroughManifest struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Steps       []struct {
		Caption string `json:"caption"`
		Still   string `json:"still"`
	} `json:"steps"`
	Poster  string           `json:"poster"`
	Preview string           `json:"preview"`
	Budget  *json.RawMessage `json:"budget"`
}

// walkthroughPreview is the one name the recorder gives the animated preview
// it encodes beside the poster (web/e2e/walkthroughcore.mjs PREVIEW).
const walkthroughPreview = "preview.webp"

// walkthroughDrift compares a script (web/e2e/walkthroughs/<id>.yaml) with
// the manifest copied from its last recording: the title, the description,
// the step count and every caption must agree, or the docs show a recording
// of a script that has since changed. One line per disagreement.
func walkthroughDrift(script, manifest []byte) []string {
	var sc struct {
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
		Steps       []struct {
			Caption string `yaml:"caption"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(script, &sc); err != nil {
		return []string{"the script does not parse: " + err.Error()}
	}
	var m walkthroughManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return []string{"the copied manifest does not parse: " + err.Error()}
	}
	var out []string
	if sc.Title != m.Title {
		out = append(out, "the script's title is "+strconvQuote(sc.Title)+", the recording's "+strconvQuote(m.Title))
	}
	if sc.Description != m.Description {
		out = append(out, "the script's description differs from the recording's")
	}
	if len(sc.Steps) != len(m.Steps) {
		out = append(out, "the script has "+itoa(len(sc.Steps))+" steps, the recording "+itoa(len(m.Steps)))
		return out
	}
	for i := range sc.Steps {
		if sc.Steps[i].Caption != m.Steps[i].Caption {
			out = append(out, "step "+itoa(i+1)+"'s caption is "+strconvQuote(sc.Steps[i].Caption)+" in the script, "+strconvQuote(m.Steps[i].Caption)+" in the recording")
		}
	}
	return out
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

// scanWalkthroughAssets checks the copied assets under dir against the
// declared ids (id to the page declaring it) and their scripts under
// scriptDir: the manifest, carrying the budget mark the recorder writes only
// when the recording passed; every still it names, its poster one of them;
// the animated preview when the manifest records one; and no drift from the
// script. Then any directory no page declares.
func scanWalkthroughAssets(dir, scriptDir string, declared map[string]string) []Finding {
	var findings []Finding
	ids := make([]string, 0, len(declared))
	for id := range declared {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		page := declared[id]
		at := func(f string) string { return filepath.ToSlash(filepath.Join(walkthroughAssetDir, id, f)) }
		add := func(text string) { findings = append(findings, Finding{File: page, Line: 1, Text: text}) }
		redo := "; run make walkthroughs ID=" + id + " then make docs-walkthroughs"
		b, err := os.ReadFile(filepath.Join(dir, id, "walkthrough.json"))
		if err != nil {
			add("walkthrough \"" + id + "\" has no " + at("walkthrough.json") + redo)
			continue
		}
		var m walkthroughManifest
		if err := json.Unmarshal(b, &m); err != nil {
			add(at("walkthrough.json") + " does not parse: " + err.Error())
			continue
		}
		if m.ID != id {
			add(at("walkthrough.json") + " is walkthrough \"" + m.ID + "\", not \"" + id + "\"")
		}
		if m.Budget == nil {
			add(at("walkthrough.json") + " has no budget mark, so it was not copied from a recording that passed the budget gate" + redo)
		}
		posterIsStill := false
		for _, st := range m.Steps {
			posterIsStill = posterIsStill || st.Still == m.Poster
			if _, err := os.Stat(filepath.Join(dir, id, st.Still)); err != nil {
				add("walkthrough \"" + id + "\" has no " + at(st.Still) + redo)
			}
		}
		if !posterIsStill {
			add(at("walkthrough.json") + " names poster " + strconvQuote(m.Poster) + ", which is none of its step stills")
		}
		switch {
		case m.Preview == "":
		case m.Preview != walkthroughPreview:
			add(at("walkthrough.json") + " names preview " + strconvQuote(m.Preview) + ", not the recorder's " + walkthroughPreview)
		default:
			if _, err := os.Stat(filepath.Join(dir, id, m.Preview)); err != nil {
				add("walkthrough \"" + id + "\" has no " + at(m.Preview) + redo)
			}
		}
		if script, err := os.ReadFile(filepath.Join(scriptDir, id+".yaml")); err == nil {
			for _, d := range walkthroughDrift(script, b) {
				add("walkthrough \"" + id + "\" drifted from its script: " + d + redo)
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
	return append(findings, scanWalkthroughAssets(filepath.Join(repoRoot, walkthroughAssetDir), filepath.Join(repoRoot, walkthroughScriptDir), declared)...), nil
}
