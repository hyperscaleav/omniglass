package docslint

import (
	"regexp"
	"strings"
)

// Region is a contiguous run of lines that is either inside a `:::design`
// container (Fenced, markers included) or outside every one. The fence is the
// machine-readable boundary between built fact and target design (#434): the
// vocabulary lint runs over every region because future design must still use
// current nouns, while the existence lints (routes, permissions, tables,
// commands; #429) run only over unfenced regions, where prose claims to
// describe something that exists.
type Region struct {
	Fenced    bool
	StartLine int // 1-indexed line of the region's first line
	Text      string
}

var (
	// codeFenceDelim matches the opening of a fenced code block. Directive
	// markers inside a code block are examples, not fences.
	codeFenceDelim = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
	// directiveOpen matches a container-directive opener, `:::name`, capturing
	// the colon run so nesting can follow remark-directive's rule (an outer
	// container uses more colons than the containers inside it).
	directiveOpen = regexp.MustCompile(`^\s*(:{3,})([a-zA-Z][\w-]*)`)
	// directiveClose matches a bare colon run, which closes the innermost open
	// container whose opener used no more colons than this line.
	directiveClose = regexp.MustCompile(`^\s*(:{3,})\s*$`)
)

// Regions splits a document into its fenced and unfenced regions. It never
// fails: an unclosed fence runs to end of file, which reads as "too much
// fenced" in review rather than silently exempting prose from the strict
// lints. The split is line-based to match the scanners, which report findings
// as (file, line).
func Regions(content string) []Region {
	type openDirective struct {
		name   string
		colons int
	}
	var (
		stack      []openDirective
		inCode     bool
		codeColons string
		regions    []Region
		lineFenced []bool
	)
	designDepth := func() int {
		n := 0
		for _, d := range stack {
			if d.name == "design" {
				n++
			}
		}
		return n
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		fenced := designDepth() > 0
		switch {
		case inCode:
			// A closing code fence is at least as long as the opener and has
			// no info string (CommonMark): "```go" inside a "```" block is
			// content, not a close.
			trimmed := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(trimmed, codeColons) &&
				strings.Trim(trimmed, string(codeColons[0])+" \t") == "" {
				inCode = false
			}
		case codeFenceDelim.MatchString(line):
			inCode = true
			codeColons = codeFenceDelim.FindStringSubmatch(line)[1]
		case directiveClose.MatchString(line):
			n := len(directiveClose.FindStringSubmatch(line)[1])
			if len(stack) > 0 && stack[len(stack)-1].colons <= n {
				// The closing marker belongs to the container it closes.
				stack = stack[:len(stack)-1]
			}
		case directiveOpen.MatchString(line):
			m := directiveOpen.FindStringSubmatch(line)
			stack = append(stack, openDirective{name: m[2], colons: len(m[1])})
			fenced = designDepth() > 0 // an opening design marker is part of its fence
		}
		lineFenced = append(lineFenced, fenced)
	}
	for i, line := range lines {
		if i == 0 || lineFenced[i] != lineFenced[i-1] {
			regions = append(regions, Region{Fenced: lineFenced[i], StartLine: i + 1, Text: line})
			continue
		}
		regions[len(regions)-1].Text += "\n" + line
	}
	return regions
}
