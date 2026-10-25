package docslint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The hard-coded-uuid gate (#1133, ADR-1133): every operator resource answers
// to its name, so a docs page or a demo that types a uuid to reach something is
// teaching a reader to copy an identifier they will never have, where the name
// they do have would do. The uuid stays the identity every response returns and
// a client stores; what this gate refuses is a uuid in an ADDRESS position, the
// place an operator types.
//
// # What is scanned
//
// Every line of every page under the docs root, prose and fences alike (a
// sentence telling a reader to run `omniglass endpoint retry <a uuid>` misleads
// as much as a fence does), and every line of every committed demo under
// scripts/demo, the tapes a PR's demo is recorded from, held to the stricter
// demo rule (scanDemoLine) that agrees with the tape linter's.
//
// # What is not a finding
//
// A uuid in an id-VALUE position, `"id": "..."`, `id: ...`, `"component_id":
// "..."`: that is a response body showing the identity it returns, which no
// name can replace. An elided id (`0198f...`) is not a uuid at all.
//
// # What is excused
//
// The generated reference pages, rendered from the code rather than written
// (their text is fixed at the Huma doc: tag it renders from, which this PR's
// own gate already shapes), the append-only log entries (historical records,
// as the vocabulary and example-name lints excuse them), and the files named in
// uuidAllowed with a reason, a list meant to stay short.

// uuidLiteral is a canonical uuid, either case, not embedded in a longer hex run.
var uuidLiteral = regexp.MustCompile(`(?i)(^|[^0-9a-f-])([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})($|[^0-9a-f-])`)

// idValuePrefix matches the text just before a uuid when the uuid is the value
// of an id field: `"id": "`, `id: `, `"component_id": "`, `owner_id=`.
var idValuePrefix = regexp.MustCompile(`(?i)(^|[^a-z0-9_])([a-z0-9_]*_)?id"?\s*[:=]\s*"?$`)

// uuidAllowed excuses a file (relative to the docs root) from the gate, with
// the reason it must carry a uuid. Empty today: no page has needed one.
var uuidAllowed = map[string]string{}

// uuidGenerated are the docs pages a generator writes, excused because their
// text is the code's own.
var uuidGenerated = map[string]bool{
	filepath.Join("reference", "cli", "index.md"): true,
}

// DemoRoot is where the committed demo scripts live, relative to this package.
var DemoRoot = filepath.Join("..", "..", "scripts", "demo")

// scanUUIDs returns one message per uuid on a line that sits in an address
// position. Pure, so the rule is testable without a corpus.
func scanUUIDs(line string) []string {
	var out []string
	for _, m := range uuidLiteral.FindAllStringSubmatchIndex(line, -1) {
		start, end := m[4], m[5]
		if idValuePrefix.MatchString(line[:start]) {
			continue
		}
		out = append(out, "hard-coded uuid "+line[start:end]+": address it by its name (a bare name, a dotted address, <component>/<endpoint>, or <owner>/<name>); a uuid belongs only in an id field a response returns")
	}
	return out
}

// ScanHardCodedUUIDs runs the gate over the docs corpus.
func ScanHardCodedUUIDs() ([]Finding, error) {
	var findings []Finding
	err := walkDocs(func(rel, content string) error {
		if uuidGenerated[rel] || isFragmentFile(rel) {
			return nil
		}
		if _, ok := uuidAllowed[rel]; ok {
			return nil
		}
		findings = append(findings, scanContent(rel, content)...)
		return nil
	})
	return findings, err
}

// demoUUID is the tape linter's own uuid pattern (web/e2e/tapelib.mjs, #1132):
// any canonical uuid, either case, wherever it sits in the line.
var demoUUID = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// scanDemoLine is the stricter rule a demo is held to: every uuid on the line
// is a finding, an id field or a comment included. A demo shows what an
// operator types, and an operator never types a uuid it had to look up, so the
// docs' id-field excuse has nothing to excuse here. The pattern is the tape
// linter's, so this gate refuses everything that one refuses in typed text,
// and also reaches the comments and the shell scripts the linter does not read.
func scanDemoLine(line string) []string {
	var out []string
	for _, u := range demoUUID.FindAllString(line, -1) {
		out = append(out, "hard-coded uuid "+u+" in a demo: address it by its name, and look an id up by name, off screen, where a route still needs one")
	}
	return out
}

// scanDemoDir runs the gate over every file under root (a demo is a recorder
// tape or a script), returning the findings and how many files it read.
func scanDemoDir(root string) ([]Finding, int, error) {
	var (
		findings []Finding
		read     int
	)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// A demo is a tape (or a script) at the top of the directory; what sits
		// below it (out/, the recorder's output) is a recording, not a demo.
		if info.IsDir() {
			if path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".tape" && ext != ".sh" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		read++
		rel, rerr := filepath.Rel(DocsRoot, path)
		if rerr != nil {
			rel = path
		}
		findings = append(findings, scanContentWith(rel, string(b), scanDemoLine)...)
		return nil
	})
	return findings, read, err
}

func scanContent(rel, content string) []Finding {
	return scanContentWith(rel, content, scanUUIDs)
}

func scanContentWith(rel, content string, scan func(string) []string) []Finding {
	var out []Finding
	for i, line := range strings.Split(content, "\n") {
		for _, text := range scan(line) {
			out = append(out, Finding{File: rel, Line: i + 1, Text: text})
		}
	}
	return out
}
