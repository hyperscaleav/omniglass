package docslint

import (
	"strings"
	"testing"
)

// TestScanNamesInFence pins the extraction rule, which is the whole design of
// this lint (#571): what counts as a NAME POSITION, and what deliberately does
// not. No filesystem, so the rule is testable without a corpus.
//
// The rule matters more than the validator, because the validator is one line
// (storage.ValidateEntityName) and every judgement call lives in deciding which
// literals to hand it.
func TestScanNamesInFence(t *testing.T) {
	cases := []struct {
		line string
		want int
		why  string
	}{
		// Name positions, illegal values: the class this exists to catch.
		{"      - { key: snmp, type: snmp_community, required: true }", 1, "an inline-flow type"},
		{"  name: serial_number", 1, "a block-mapping name"},
		{`    "name": "serial_number",`, 1, "a JSON name"},
		{"omniglass property create --name serial_number", 1, "a CLI flag"},
		{"omniglass property create --name=serial_number", 1, "a CLI flag with ="},
		{"  type: icmp.rtt-avg", 1, "a dot, illegal since the rule collapsed to one segment"},
		{"  name: SerialNumber", 1, "uppercase"},

		// Name positions, legal values: must not fire.
		{"      - { key: snmp, type: snmp-community, required: true }", 0, "the corrected form"},
		{"  name: serial-number", 0, "legal kebab"},
		{"  type: string", 0, "a JSON-schema primitive is a legal kebab token"},
		{"  type: object", 0, "likewise"},
		{"omniglass property create --name serial-number", 0, "legal via CLI"},

		// Not name positions: a label is free text and a description is prose, so
		// neither is validated. Firing here would be the lint's worst failure,
		// since the fix would be to mangle correct English.
		{`  label: "SNMP community"`, 0, "a label is free text"},
		{`  description: The manufacturer serial_number of the device.`, 0, "prose in a description"},

		// Placeholders and substitutions are not names. A docs example is full of
		// them and every one would be a false positive.
		{"  type: ${input.snmp}", 0, "a template substitution"},
		{"  name: <your-property>", 0, "an angle-bracket placeholder"},
		{"  name: {{.TypeName}}", 0, "a Go template"},
		{"      - { key: ssh, default: $var:crestron-ssh }", 0, "a $var reference is not a name position"},

		// An id is a uuid, not a name, so it is never validated: the name rule
		// REFUSES a uuid (ErrEntityNameIsUUID), so scanning id: would fire on
		// every correct example that shows one.
		{"  id: 01a03c92-5cd8-757a-8b3c-82623d8ff57e", 0, "a uuid id"},
	}
	for _, c := range cases {
		got := scanNamesInFence(c.line)
		if len(got) != c.want {
			t.Errorf("scanNamesInFence(%q) = %v, want %d hit(s) (%s)", c.line, got, c.want, c.why)
		}
	}
}

// TestFenceCommentsAreProse pins the one exemption that is load-bearing rather
// than convenient.
//
// A page teaching the name rule has to WRITE the illegal spellings to teach it:
// guides/admin/properties.mdx says registering `serial-number` once means it
// cannot drift into `serialNumber` or `serial_number`, and that sentence is
// correct and must never be "fixed". Prose is therefore out of scope entirely,
// and a comment inside a fence is prose that happens to sit in a code block. The
// first draft of this lint scanned them and reported `--name flag.` from the
// sentence "there is no --name flag" in guides/cli.md, which is the same defect
// in miniature.
func TestFenceCommentsAreProse(t *testing.T) {
	for _, line := range []string{
		"# It is named by its protocol: there is no --name flag.",
		"  # snmp_community shape; community field is secret",
		"// name: serial_number is what a caller used to write",
	} {
		if got := scanNamesInFence(line); len(got) != 0 {
			t.Errorf("scanNamesInFence(%q) = %v, want no hits: a comment is prose", line, got)
		}
	}
}

// TestFenceMarkersBalance guards the assumption eachFenceLine rests on: fences
// toggle, so a file with an odd number of markers would leave the scanner inside
// a fence to the end of the file and read prose as if it were code.
//
// It holds today because no page nests a fence (which markdown spells with four
// backticks around three). Asserted rather than assumed, so the day a page does,
// this fails here instead of the name gate quietly changing what it reads.
func TestFenceMarkersBalance(t *testing.T) {
	err := walkDocs(func(rel, content string) error {
		markers := 0
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "````") {
				t.Errorf("%s: a nested (four-backtick) fence: eachFenceLine's toggle cannot read it", rel)
			}
			if strings.HasPrefix(trimmed, "```") {
				markers++
			}
		}
		if markers%2 != 0 {
			t.Errorf("%s: %d fence markers, an odd number, so a fence never closes", rel, markers)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestDocsExampleNamesAreLegal runs the gate over the real corpus, enforced. An
// example name the API would refuse fails here (#571).
func TestDocsExampleNamesAreLegal(t *testing.T) {
	findings, err := ScanExampleNames()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "example-name", findings, true)
}

// TestScanExampleNamesReadsFencesOnly guards the empty-universe failure mode, the
// way TestSchemaColumnsLoad does for the table lint: it proves the walk actually
// read fenced lines, so a green TestDocsExampleNamesAreLegal means "the fences are
// clean" rather than "nothing was read". A wrong root or a broken fence toggle
// both present as a silent pass, and the throwaway script used to calibrate this
// lint hit exactly that, reporting zero findings from a working directory where
// the docs root did not resolve.
func TestScanExampleNamesReadsFencesOnly(t *testing.T) {
	var fences, names int
	err := walkDocs(func(rel, content string) error {
		eachFenceLine(content, func(_ int, line string) {
			fences++
			names += len(scanNamesInFence(line))
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if fences < 500 {
		t.Errorf("only %d fenced lines seen; the corpus has thousands, so the walk is not reading it", fences)
	}
	if names != 0 {
		t.Errorf("%d illegal example name(s) survived the corpus scan", names)
	}
}
