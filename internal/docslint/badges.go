package docslint

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The badge-fence lint (#482): a page's status badge must agree with its
// design-fence census, which is what makes the badge derivable instead of
// asserted. Three shapes are findings:
//
//   - Partial or Design with zero fences: the page is either honest Built or
//     it is hiding unbuilt prose outside a fence;
//   - Design with substantive prose outside every fence: on a sketch page the
//     model lives inside fences, and the only unfenced text is structure
//     (headings), meta-commentary (asides, e.g. a "Built today" note), or
//     wiring (imports, code);
//   - Built with any fence: a fence is an unbuilt claim, so the page is not
//     Built.
//
// Diverged pages (built, but differing from the design) are not checked; the
// divergence contract is the inline note plus the decision log, not a fence.

var badgeText = regexp.MustCompile(`(?m)^\s*text:\s*['"]?(Design|Partial|Built|Diverged)['"]?\s*$`)

// pageBadge extracts the sidebar badge from a page's frontmatter, or "" when
// the page has none (meta pages: the spine, the logs, the roadmap).
func pageBadge(content string) string {
	end := frontmatterEnd(content)
	if end < 0 {
		return ""
	}
	m := badgeText.FindStringSubmatch(content[:end])
	if m == nil {
		return ""
	}
	return m[1]
}

// frontmatterEnd returns the byte offset of the end of the leading YAML
// frontmatter block, or -1 when the document has none.
func frontmatterEnd(content string) int {
	if !strings.HasPrefix(content, "---\n") {
		return -1
	}
	i := strings.Index(content[4:], "\n---")
	if i < 0 {
		return -1
	}
	return 4 + i + len("\n---")
}

var importLine = regexp.MustCompile(`^import\s.+from\s`)

// substantiveLines returns the 1-indexed lines that make an unfenced,
// substantive claim: prose and list lines outside every design fence, outside
// frontmatter, code, and aside containers, and not a heading, an import, or a
// directive marker. This is the census the Design-page shape counts.
func substantiveLines(content string) []int {
	var out []int
	fmEnd := frontmatterEnd(content)
	offset := 0
	var (
		inCode     bool
		codeDelim  string
		asideDepth int
	)
	for i, line := range strings.Split(content, "\n") {
		lineStart := offset
		offset += len(line) + 1
		if fmEnd >= 0 && lineStart < fmEnd {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case inCode:
			if strings.HasPrefix(trimmed, codeDelim) &&
				strings.Trim(trimmed, string(codeDelim[0])+" \t") == "" {
				inCode = false
			}
			continue
		case codeFenceDelim.MatchString(line):
			inCode = true
			codeDelim = codeFenceDelim.FindStringSubmatch(line)[1]
			continue
		case directiveOpen.MatchString(line):
			if directiveOpen.FindStringSubmatch(line)[2] != "design" {
				asideDepth++
			}
			continue
		case directiveClose.MatchString(line):
			if asideDepth > 0 {
				asideDepth--
			}
			continue
		}
		if asideDepth > 0 || trimmed == "" ||
			strings.HasPrefix(trimmed, "#") || importLine.MatchString(trimmed) {
			continue
		}
		out = append(out, i+1)
	}
	return out
}

// ScanBadgeFences checks every badged architecture page's badge against its
// fence census.
func ScanBadgeFences() ([]Finding, error) {
	var findings []Finding
	err := walkDocs(func(rel, content string) error {
		if filepath.Dir(rel) != "architecture" {
			return nil
		}
		badge := pageBadge(content)
		if badge == "" || badge == "Diverged" {
			return nil
		}
		fences := 0
		fencedLines := map[int]bool{}
		for _, r := range Regions(content) {
			if !r.Fenced {
				continue
			}
			fences++
			for l, n := r.StartLine, strings.Count(r.Text, "\n")+1; l < r.StartLine+n; l++ {
				fencedLines[l] = true
			}
		}
		switch badge {
		case "Partial", "Design":
			if fences == 0 {
				findings = append(findings, Finding{rel, 1, "badge " + badge + " but the page has no design fence: either the page is honestly Built or its unbuilt prose is unfenced"})
			}
		}
		if badge == "Design" {
			for _, l := range substantiveLines(content) {
				if !fencedLines[l] {
					findings = append(findings, Finding{rel, l, "badge Design but substantive prose sits outside every design fence"})
				}
			}
		}
		if badge == "Built" && fences > 0 {
			findings = append(findings, Finding{rel, 1, "badge Built but the page carries a design fence: a fence is an unbuilt claim"})
		}
		return nil
	})
	return findings, err
}
