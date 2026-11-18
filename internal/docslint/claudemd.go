package docslint

import (
	"os"
	"strings"
)

// claudeMdPath is the path to CLAUDE.md, relative to this package.
var claudeMdPath = repoRoot + "/CLAUDE.md"

// ScanClaudeMd checks CLAUDE.md for section headings that must not appear
// once Omniglass has an external deployment. The "Pre-deployment stance"
// section was written to remove itself in the PR that ships the first
// external deployment (issue #2186); its presence after that point is drift.
func ScanClaudeMd() ([]Finding, error) {
	b, err := os.ReadFile(claudeMdPath)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for i, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## Pre-deployment stance") {
			findings = append(findings, Finding{
				File: "CLAUDE.md",
				Line: i + 1,
				Text: `the "Pre-deployment stance" section must be removed: replace it with the compatibility rule (the section was written to remove itself in the PR that ships the first external deployment, issue #2186)`,
			})
		}
	}
	return findings, nil
}
