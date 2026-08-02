package docslint

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The make-target, env-var, and file-path lints (#448): three small scanners
// sharing one shape. A `make <target>` span must name a Makefile target; an
// OMNIGLASS_* name must be read somewhere in non-test Go (and, inversely,
// every env var the binary reads must be documented in the container guide); a
// backticked repo path must resolve. All three are existence lints, so they
// skip design fences; the audit found contributing pages naming targets and CI
// gates that did not exist, and env tables that never caught up with the code.

var (
	makeTargetRule = regexp.MustCompile(`(?m)^([a-zA-Z0-9_-]+):`)
	makeSpan       = regexp.MustCompile(`^make ([a-zA-Z0-9_-]+)`)
	envVarName     = regexp.MustCompile(`\bOMNIGLASS_[A-Z0-9_]+\b`)
	repoPathSpan   = regexp.MustCompile(`^(internal|cmd|web|db|proto|api|docs)/[A-Za-z0-9_./\-]+$`)
	lineRefSuffix  = regexp.MustCompile(`:[0-9]+(?:-[0-9]+)?$`)
)

// repoRoot is the repository root relative to this package.
var repoRoot = filepath.Join("..", "..")

// loadMakeTargets parses the Makefile's rule names.
func loadMakeTargets() (map[string]bool, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		return nil, err
	}
	targets := map[string]bool{}
	for _, m := range makeTargetRule.FindAllStringSubmatch(string(b), -1) {
		targets[m[1]] = true
	}
	return targets, nil
}

// loadEnvUniverse scans non-test Go under internal/ and cmd/ for OMNIGLASS_*
// reads. The source of truth is the whole binary, not one config package: the
// secret KEK, the CLI client, and the node runner all read their own vars.
func loadEnvUniverse() (map[string]bool, error) {
	universe := map[string]bool{}
	for _, root := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, name := range envVarName.FindAllString(string(b), -1) {
				universe[name] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return universe, nil
}

// scanUnfenced runs fn over every unfenced line of every non-history docs
// file, the shared walk of the existence lints; inCode tells fn whether the
// line sits inside a fenced code block (a shell example) or in prose.
func scanUnfenced(fn func(rel, line string, inCode bool) []string) ([]Finding, error) {
	var findings []Finding
	err := walkDocs(func(rel, content string) error {
		if vocabularyAllowed[rel] {
			return nil
		}
		for _, r := range Regions(content) {
			if r.Fenced {
				continue
			}
			inCode := false
			codeDelim := ""
			for i, line := range strings.Split(r.Text, "\n") {
				switch {
				case inCode:
					trimmed := strings.TrimLeft(line, " \t")
					if strings.HasPrefix(trimmed, codeDelim) &&
						strings.Trim(trimmed, string(codeDelim[0])+" \t") == "" {
						inCode = false
						continue
					}
				case codeFenceDelim.MatchString(line):
					inCode = true
					codeDelim = codeFenceDelim.FindStringSubmatch(line)[1]
					continue
				}
				// Code blocks stay in scope for these lints: a `make dev` in a
				// shell example must exist exactly like one in prose.
				for _, text := range fn(rel, line, inCode) {
					findings = append(findings, Finding{File: rel, Line: r.StartLine + i, Text: text})
				}
			}
		}
		return nil
	})
	return findings, err
}

// scanMakeIn returns a finding per `make <target>` naming no Makefile rule: a
// backticked span in prose, or a command line in a shell example. Bare prose is
// never matched (the words "make targets" wrap onto a line start).
func scanMakeIn(line string, inCode bool, targets map[string]bool) []string {
	var out []string
	if inCode {
		if mt := makeSpan.FindStringSubmatch(strings.TrimSpace(line)); mt != nil && !targets[mt[1]] {
			out = append(out, "make "+mt[1]+" is not a Makefile target")
		}
		return out
	}
	for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
		mt := makeSpan.FindStringSubmatch(strings.TrimSpace(m[1]))
		if mt != nil && !targets[mt[1]] {
			out = append(out, "make "+mt[1]+" is not a Makefile target")
		}
	}
	return out
}

// ScanMakeTargets runs the make-target lint over the docs tree.
func ScanMakeTargets() ([]Finding, error) {
	targets, err := loadMakeTargets()
	if err != nil {
		return nil, err
	}
	return scanUnfenced(func(rel, line string, inCode bool) []string {
		return scanMakeIn(line, inCode, targets)
	})
}

// scanEnvIn returns a finding per OMNIGLASS_* name the binary never reads.
func scanEnvIn(line string, universe map[string]bool) []string {
	if illustrativeProse.MatchString(line) {
		return nil
	}
	var out []string
	for _, name := range envVarName.FindAllString(line, -1) {
		if !universe[name] {
			out = append(out, name+" is read nowhere in the Go source: the knob is design (fence it) or the name drifted")
		}
	}
	return out
}

// ScanEnvVars runs the forward env-var lint over the docs tree.
func ScanEnvVars() ([]Finding, error) {
	universe, err := loadEnvUniverse()
	if err != nil {
		return nil, err
	}
	return scanUnfenced(func(rel, line string, inCode bool) []string {
		return scanEnvIn(line, universe)
	})
}

// ScanEnvDocsCoverage is the inverse: every env var the binary reads is
// documented somewhere in the docs tree. Which page owns it differs by
// audience (the container guide for server and node runtime, the CLI guide
// for client vars); what cannot happen is a knob the code reads and no page
// names.
func ScanEnvDocsCoverage() ([]Finding, error) {
	universe, err := loadEnvUniverse()
	if err != nil {
		return nil, err
	}
	documented := map[string]bool{}
	// A var declared in the generated registry facts is documented by
	// construction: the ConfigVars component renders docs/src/generated/
	// config.json onto the container and helm guides, so a registry var never
	// needs a hand-written mention (and hand-writing one would be the drift
	// class the registry exists to end).
	if b, err := os.ReadFile(filepath.Join(repoRoot, "docs", "src", "generated", "config.json")); err == nil {
		for _, name := range envVarName.FindAllString(string(b), -1) {
			documented[name] = true
		}
	}
	err = walkDocs(func(rel, content string) error {
		for _, name := range envVarName.FindAllString(content, -1) {
			documented[name] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for name := range universe {
		if !documented[name] {
			findings = append(findings, Finding{File: "docs", Line: 1, Text: "the binary reads " + name + " but no docs page documents it"})
		}
	}
	return findings, nil
}

// scanPathsIn returns a finding per backticked repo path that resolves to
// nothing. A trailing :line reference strips first; a path whose last segment
// carries a dot beyond its extension is a package.Symbol reference and
// resolves by its package directory; a glob is a pattern, not a path claim.
func scanPathsIn(line string) []string {
	var out []string
	for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
		span := strings.TrimSpace(m[1])
		if !repoPathSpan.MatchString(span) || strings.ContainsAny(span, "*{") {
			continue
		}
		path := lineRefSuffix.ReplaceAllString(span, "")
		if pathResolves(path) {
			continue
		}
		// internal/avatar.Normalize: try the package directory half.
		if i := strings.LastIndex(path, "."); i > strings.LastIndex(path, "/") {
			if pathResolves(path[:i]) {
				continue
			}
		}
		out = append(out, span+" does not resolve to a file or directory in the repository")
	}
	return out
}

// buildOutputPaths are docs-citable paths that exist only after a build (the
// embedded SPA dist); a fresh clone legitimately lacks them.
var buildOutputPaths = map[string]bool{
	"internal/webui/dist": true,
}

func pathResolves(path string) bool {
	if buildOutputPaths[strings.TrimSuffix(path, "/")] {
		return true
	}
	_, err := os.Stat(filepath.Join(repoRoot, path))
	return err == nil
}

// ScanRepoPaths runs the file-path lint over the docs tree.
func ScanRepoPaths() ([]Finding, error) {
	return scanUnfenced(func(rel, line string, inCode bool) []string {
		return scanPathsIn(line)
	})
}
