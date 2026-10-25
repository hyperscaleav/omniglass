package docslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanUUIDs pins the extraction rule of the hard-coded-uuid gate (#1133):
// what counts as a uuid an operator would have typed (an address, which a name
// does better), and what is a uuid a response returns (identity, which a name
// cannot replace and the docs may show).
func TestScanUUIDs(t *testing.T) {
	const u = "01a03c92-5cd8-757a-8b3c-82623d8ff57e"
	cases := []struct {
		line string
		want int
		why  string
	}{
		// Addresses: the class this exists to catch.
		{"omniglass endpoint retry " + u, 1, "a CLI positional"},
		{`$OG component get ` + u + ` | jq .`, 1, "a demo script's command"},
		{"curl -X POST $SERVER/api/v1/endpoints/" + u + ":retry", 1, "a URL path"},
		{"Retry it with `omniglass secret update " + u + "`.", 1, "inline code in prose"},
		{"PANEL=" + u, 1, "a shell variable holding an address"},
		{"omniglass component get " + strings.ToUpper(u), 1, "case does not hide one"},
		{`--scope-id ` + u + ` --scope-kind location`, 1, "a reference flag"},

		// Identity: a response's own id, or a reference field it returns.
		{`  "id": "` + u + `",`, 0, "a JSON id field"},
		{`  id: ` + u, 0, "a YAML id field"},
		{`{"name": "https", "component_id": "` + u + `"}`, 0, "a JSON reference id field"},

		// Not uuids.
		{"boi.17c.415a.$comp.display-1/https", 0, "an address"},
		{`{"parent_id": "0198f..."}`, 0, "an elided id"},
	}
	for _, c := range cases {
		if got := scanUUIDs(c.line); len(got) != c.want {
			t.Errorf("scanUUIDs(%q) = %v, want %d hit(s) (%s)", c.line, got, c.want, c.why)
		}
	}
}

// TestDocsCarryNoHardCodedUUIDs runs the gate over the real docs corpus,
// enforced: a page that hard-codes a uuid where a name would do fails here.
func TestDocsCarryNoHardCodedUUIDs(t *testing.T) {
	findings, err := ScanHardCodedUUIDs()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "hard-coded uuid", findings, true)
}

// TestDemoScriptsCarryNoHardCodedUUIDs runs the gate over every committed demo
// (scripts/demo), enforced, and guards the empty-universe failure mode: a wrong
// root would pass silently, so the walk must have read at least one script.
func TestDemoScriptsCarryNoHardCodedUUIDs(t *testing.T) {
	findings, read, err := scanDemoDir(DemoRoot)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if read == 0 {
		t.Fatalf("no demo script read under %s: the walk is not reading the demos", DemoRoot)
	}
	report(t, "hard-coded uuid in a demo", findings, true)
}

// TestDemoGateMatchesTheTapeLinter pins the demo half of the gate to the tape
// linter's rule (web/e2e/tapelib.mjs, #1132): in a demo ANY uuid is a finding,
// an id field and a comment included, since a demo never needs to show one it
// typed, and a uuid run into other hex is one too. So whatever the tape linter
// refuses in typed text this gate refuses as well, and the gate reaches the
// comment lines and shell scripts the linter does not read.
func TestDemoGateMatchesTheTapeLinter(t *testing.T) {
	const u = "01a03c92-5cd8-757a-8b3c-82623d8ff57e"
	for _, line := range []string{
		`Type ` + "`" + `omniglass endpoint get ` + u + "`",
		`# the panel is ` + u,
		`Type ` + "`" + `jq '{"id": "` + u + `"}'` + "`",
		`ffff` + u,
	} {
		if got := scanDemoLine(line); len(got) != 1 {
			t.Errorf("scanDemoLine(%q) = %v, want one finding", line, got)
		}
	}
	for _, line := range []string{
		"Type `omniglass endpoint get \"$panel/https\" | jq -c '{component, name}'`",
		"Type `demo_hush 'ep=$(omniglass endpoint get \"$panel/https\" | jq -r .id)'`",
	} {
		if got := scanDemoLine(line); len(got) != 0 {
			t.Errorf("scanDemoLine(%q) = %v, want none", line, got)
		}
	}
}

// TestTheGateCatchesAUUIDInADemo is #1133's acceptance line for the guard: a
// demo that addresses a component by uuid is a finding, named by file and line.
func TestTheGateCatchesAUUIDInADemo(t *testing.T) {
	dir := t.TempDir()
	script := "#!/usr/bin/env bash\n$OG component get boi.17c.$comp.display-1\n$OG endpoint retry 0198f2a4-7c1e-7b3d-9a55-3c1f2d7a9e10\n"
	if err := os.WriteFile(filepath.Join(dir, "regressed.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	findings, read, err := scanDemoDir(dir)
	if err != nil || read != 1 {
		t.Fatalf("scan = %v (read %d, err %v)", findings, read, err)
	}
	if len(findings) != 1 || findings[0].Line != 3 || !strings.Contains(findings[0].File, "regressed.sh") {
		t.Fatalf("findings = %+v, want one on regressed.sh line 3", findings)
	}
}
