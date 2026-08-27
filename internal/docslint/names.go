package docslint

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The example-name gate (#571): a name in a documented example is run through
// the platform's own name rule, so a reader who copies one cannot receive a 422
// from the docs.
//
// The audit that produced this found roughly 35 illegal example names and
// concluded the fix is a gate, not a sweep: two were caught by eye during #545
// and thirty-odd were not.
//
// # What is scanned, and why it is this narrow
//
// Only NAME POSITIONS inside FENCED BLOCKS: a `name:`/`type:`/`key:` in fenced
// YAML, a `"name":` in fenced JSON, a `--name` in a fenced command. Two limits
// are deliberate rather than incidental.
//
// Prose is out of scope, permanently. A page teaching the name rule has to write
// the illegal spellings in order to teach them: guides/admin/properties.mdx says
// registering `serial-number` once means it cannot drift into `serialNumber` in
// one place and `serial_number` in another, and that sentence is correct. A lint
// firing on it would make the corpus fixable only by deleting the explanation,
// which is the failure mode the vocabulary lint's retirement-prose exemption
// already exists to avoid. A comment inside a fence is prose too (see
// scanNamesInFence).
//
// That narrowing is not a concession, it is the point: a fence is what a reader
// COPIES. "An operator copying one gets a 422" is the harm, and nobody pastes a
// sentence into the API.
//
// Fields whose value is free text are not name positions and are never
// validated. `label:` and `description:` hold English, so firing on them would
// demand mangling correct prose to get green. `id:` is not scanned either, for
// the opposite reason: an id is a uuid, and the name rule REFUSES a uuid
// (ErrEntityNameIsUUID), so scanning it would fire on every correct example.

// nameKeys are the keys whose value is an entity name. Kept as one alternation
// used by all three extractors so the three cannot drift apart.
const nameKeys = `name|type|key`

var (
	// A block mapping (`  type: snmp-community`) or an inline flow entry
	// (`- { key: snmp, type: snmp-community }`). The leading class refuses a
	// match mid-token, so `display_name:` cannot be read as `name:`, and refuses
	// a JSON key, which jsonNameLiteral handles with its quotes.
	yamlNameLiteral = regexp.MustCompile(`(?:^|[\s{,])(` + nameKeys + `)\s*:\s*([^\s,}#'"]+)`)
	jsonNameLiteral = regexp.MustCompile(`"(` + nameKeys + `)"\s*:\s*"([^"]*)"`)
	// `--name x` and `--name=x`, the two spellings a CLI example uses.
	cliNameLiteral = regexp.MustCompile(`--(` + nameKeys + `)(?:[= ])([^\s\\]+)`)
)

// exampleNameAllowed lists files exempt from this lint: historical records where
// an illegal name was the vocabulary at the time it was written.
//
// The generated CLI reference is deliberately NOT here, unlike in
// vocabularyAllowed. An illegal name in it is a real defect with a real fix (the
// Huma `doc:` tag it renders from), not a historical artifact, and the whole
// argument of the audit is that the generated artifacts are the ones worth
// trusting.
var exampleNameAllowed = map[string]bool{
	filepath.Join("architecture", "decisions.md"): true,
	filepath.Join("architecture", "build-log.md"): true,
}

// isNamePlaceholder reports whether v is a stand-in rather than a name. A
// documented example is full of them, and every one would otherwise be a false
// positive: `${input.snmp}`, `<your-property>`, `{{.TypeName}}`, a YAML block
// scalar marker.
func isNamePlaceholder(v string) bool {
	if v == "" || strings.Contains(v, "${") {
		return true
	}
	switch v {
	case "|", ">", "-":
		return true
	}
	return strings.HasPrefix(v, "$") || strings.HasPrefix(v, "<") ||
		strings.HasPrefix(v, "{") || strings.HasPrefix(v, "...") ||
		strings.HasPrefix(v, "&") || strings.HasPrefix(v, "*") ||
		strings.HasSuffix(v, ">") || strings.HasSuffix(v, "}")
}

// scanNamesInFence returns one message per illegal name literal on a single line
// of a fenced block. Pure: no I/O, which is what makes the extraction rule
// testable without a corpus.
//
// A comment line returns nothing. A comment inside a fence is prose that happens
// to sit in a code block, and prose names illegal spellings on purpose: the first
// draft of this lint reported `--name flag.` out of the sentence "there is no
// --name flag" in guides/cli.md.
func scanNamesInFence(line string) []string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return nil
	}
	var out []string
	for _, rx := range []*regexp.Regexp{yamlNameLiteral, jsonNameLiteral, cliNameLiteral} {
		for _, m := range rx.FindAllStringSubmatch(line, -1) {
			key, value := m[1], m[2]
			if isNamePlaceholder(value) {
				continue
			}
			if err := storage.ValidateEntityName(value); err != nil {
				out = append(out, "example "+key+" "+value+" is a name the API refuses: "+err.Error())
			}
		}
	}
	return out
}

// eachFenceLine calls fn for every line INSIDE a fenced block, with its 1-indexed
// line number. The fence markers themselves are not yielded.
//
// Deliberately line-based rather than a markdown parse: the corpus mixes .md and
// .mdx, and every fence in it opens and closes on its own line.
func eachFenceLine(content string, fn func(n int, line string)) {
	inFence := false
	for i, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			fn(i+1, line)
		}
	}
}

// ScanExampleNames runs the gate over the docs corpus: every name literal in a
// fenced example, validated against the platform's own rule.
func ScanExampleNames() ([]Finding, error) {
	var findings []Finding
	err := walkDocs(func(rel, content string) error {
		if exampleNameAllowed[rel] {
			return nil
		}
		eachFenceLine(content, func(n int, line string) {
			for _, text := range scanNamesInFence(line) {
				findings = append(findings, Finding{File: rel, Line: n, Text: text})
			}
		})
		return nil
	})
	return findings, err
}
