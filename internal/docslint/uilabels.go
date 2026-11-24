package docslint

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The UI label lint (#2599): a console control named in current-tense prose
// ("select **Add device**") must exist in the console source. The label set is
// read from web/src directly, never a committed copy, so it cannot drift: every
// string literal and every run of JSX text in a non-test source file counts.
// Over-collecting is deliberate (a rename removes the old string; the lint's job
// is to catch that), while the mention side stays narrow: a bold or code span
// right after a UI verb. Fence-aware per #434: a mention inside a :::design
// fence is target design and exempt.

var (
	webSrcRoot = filepath.Join("..", "..", "web", "src")

	// A quoted string (single, double or template), no escapes beyond \x.
	quotedString = regexp.MustCompile("\"((?:[^\"\\\\\\n]|\\\\.)*)\"|'((?:[^'\\\\\\n]|\\\\.)*)'|`((?:[^`\\\\]|\\\\.)*)`")
	// JSX text: characters between a closing > and the next < or {.
	jsxText = regexp.MustCompile(`>([^<>{}=;]+)(?:<|\{)`)

	uiVerbSpan = regexp.MustCompile("(?i)\\b(?:click(?:ing)?|select(?:ing)?|choose|choosing|press(?:ing)?|open(?:ing)?|tap|toggle|toggling|pick)\\b(?:\\s+(?:on|the|a|an|to))*\\s+(?:\\*\\*([^*]+)\\*\\*|`([^`]+)`)")
)

// uiLabelAllowlist holds real labels the extractor cannot see, each with its
// reason. Keys are lowercase.
var uiLabelAllowlist = map[string]string{
	"sign in with sso":                     "built at runtime from the identity provider's name",
	"operators":                            "a role word after \"open to\", not a control",
	"bug":                                  "a GitHub issue type, not a console control",
	"field on the omniglass project board": "a GitHub Project board field, not a console control",
}

// notControlSpan marks a span that is not a control label whatever verb
// precedes it: a keyboard chord, a URL, or a machine identifier.
var notControlSpan = regexp.MustCompile(`(?i)^(?:(?:ctrl|cmd|alt|shift|esc|enter|tab)\b[-+ ]?\w*|https?://\S+|\S*__\S*)$`)

func normalizeLabel(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// collectLabels adds every string literal and JSX text run in src to set.
func collectLabels(src string, set map[string]bool) {
	for _, m := range quotedString.FindAllStringSubmatch(src, -1) {
		for _, g := range m[1:] {
			if g != "" {
				set[normalizeLabel(g)] = true
			}
		}
	}
	for _, m := range jsxText.FindAllStringSubmatch(src, -1) {
		if t := normalizeLabel(m[1]); t != "" {
			set[t] = true
		}
	}
}

// LoadUILabels reads the label set from the console source.
func LoadUILabels() (map[string]bool, error) {
	set := map[string]bool{}
	err := filepath.WalkDir(webSrcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n := d.Name()
		if !(strings.HasSuffix(n, ".ts") || strings.HasSuffix(n, ".tsx")) ||
			strings.Contains(n, ".test.") || strings.HasSuffix(n, ".d.ts") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		collectLabels(string(b), set)
		return nil
	})
	return set, err
}

// uiLabelKnown reports whether the mention matches a label: exactly, or as a
// trailing-ellipsis or trailing-colon variant of one.
func uiLabelKnown(label string, labels map[string]bool) bool {
	l := normalizeLabel(label)
	if notControlSpan.MatchString(l) {
		return true
	}
	l = strings.TrimRight(l, ".…:")
	l = strings.TrimSpace(l)
	if l == "" || labels[l] || labels[l+"…"] || labels[l+"..."] || labels[l+":"] {
		return true
	}
	_, ok := uiLabelAllowlist[l]
	return ok
}

// scanUILabelsIn checks one line of prose.
func scanUILabelsIn(line string, labels map[string]bool) []string {
	var out []string
	for _, m := range uiVerbSpan.FindAllStringSubmatch(line, -1) {
		label := m[1]
		if label == "" {
			label = m[2]
		}
		if !uiLabelKnown(label, labels) {
			out = append(out, "console label \""+strings.TrimSpace(label)+"\" is not in web/src: the control was renamed or removed, or the mention is design (fence it)")
		}
	}
	return out
}

// scanUILabelsInDoc runs the lint over one document's unfenced prose.
func scanUILabelsInDoc(content string, labels map[string]bool) []Finding {
	var findings []Finding
	for _, r := range Regions(content) {
		if r.Fenced {
			continue
		}
		inCode, codeDelim := false, ""
		for i, line := range strings.Split(r.Text, "\n") {
			switch {
			case inCode:
				t := strings.TrimLeft(line, " \t")
				if strings.HasPrefix(t, codeDelim) && strings.Trim(t, string(codeDelim[0])+" \t") == "" {
					inCode = false
				}
			case codeFenceDelim.MatchString(line):
				inCode, codeDelim = true, codeFenceDelim.FindStringSubmatch(line)[1]
			default:
				for _, text := range scanUILabelsIn(line, labels) {
					findings = append(findings, Finding{Line: r.StartLine + i, Text: text})
				}
			}
		}
	}
	return findings
}

// ScanUILabelReferences runs the lint over the docs tree.
func ScanUILabelReferences() ([]Finding, error) {
	labels, err := LoadUILabels()
	if err != nil {
		return nil, err
	}
	var findings []Finding
	err = walkDocs(func(rel, content string) error {
		if vocabularyAllowed(rel) {
			return nil
		}
		for _, f := range scanUILabelsInDoc(content, labels) {
			findings = append(findings, Finding{File: rel, Line: f.Line, Text: f.Text})
		}
		return nil
	})
	return findings, err
}
