package docslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// enforceVocabulary makes the vocabulary lint a red gate. It was warn-only
// while the wave-2 sweep (#428) cleared the pre-existing findings.
const enforceVocabulary = true

// enforceDecisions makes the decisions-format lint a red gate. It was
// warn-only while the wave-3 hygiene pass (#428) cleared the backlog.
const enforceDecisions = true

// TestVocabulary scans the docs for retired vocabulary (the Banned denylist).
// Every ADR that retires a term appends its entry to Banned in the same PR,
// so a retirement can never half-land again the way ADR-0065 did (the audit
// found `datapoint` surviving on twelve pages).
func TestVocabulary(t *testing.T) {
	findings, err := ScanVocabulary()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "vocabulary", findings, enforceVocabulary)
}

// TestDecisionsFormat checks the decision log's mechanical structure, now one
// file per decision under docs/src/content/decisions/ (#1095): the index row's
// fields in each file's frontmatter, Date and Status in its body, one file per
// number, the legacy range (0001 to 0163) with no gap, and every later number
// one of its own refs, the issue it was decided under (#1108). The audit found a duplicate
// ADR-0036, sixteen missing index rows, and entries with no status at all; the
// index is generated from the files now, so a missing row cannot recur, and two
// PRs that took the same number fail here naming both files.
func TestDecisionsFormat(t *testing.T) {
	findings, err := ScanDecisions()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "decisions-format", findings, enforceDecisions)
}

// TestBuildLogFormat checks the build log's entry files (#1095): named for
// their date, carrying title, date and refs, one section each.
func TestBuildLogFormat(t *testing.T) {
	findings, err := ScanBuildLog()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "build-log-format", findings, true)
}

// TestLogsStayFragments is the monolith guard (#1095): the build log and the
// decision log pages carry their directives and no entries, and the
// anti-pattern catalog's single file stays gone. An entry appended to a page
// is the merge conflict this layout exists to end.
func TestLogsStayFragments(t *testing.T) {
	findings, err := ScanLogHosts()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "log-hosts", findings, true)
}

// TestAntiPatternCatalog checks the adversarial-review catalog's entry files:
// each opens with its own id and status (#1095).
func TestAntiPatternCatalog(t *testing.T) {
	findings, err := ScanAntiPatterns()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "anti-patterns", findings, true)
}

// TestBannedPatternsStaySane pins the denylist entries against over-matching:
// current vocabulary must never trip a banned pattern.
func TestBannedPatternsStaySane(t *testing.T) {
	current := []string{
		"property_type", "the property catalog", "a sample lands in metric",
		"the current value cache", "source_event_id", "omniglass outpost run",
		"ListShell", "FlatList", "TreeList", "UpsertPropertyType",
		"value_type on the variable", // a live column, distinct from the retired data-type usage
		// ADR-0071 redefines "template" rather than retiring the word, so the prose
		// form must stay legal: an operator really does start from a template.
		"system templates you can import",
		"a component template to clone",
		"start from a template",
		// ADR-0827 reclaims the bare kind names for the template catalog.
		"component_template", "system_template", "standard_template", "location_template",
		"POST /standards:createFromTemplate", "an operator forks a shipped row",
	}
	for _, s := range current {
		for _, term := range Banned {
			if m := term.Pattern.FindString(s); m != "" {
				t.Errorf("banned pattern for %q matches current vocabulary %q (hit %q)", term.Replacement, s, m)
			}
		}
	}
}

// TestTemplateRetirementStillBans pins the other side of ADR-0827's narrowing:
// the kind names are legal again (TestBannedPatternsStaySane), but the
// versioned-shape tables ADR-0071 retired still trip the lint, so a typo in
// either pattern cannot silently switch the ban off.
func TestTemplateRetirementStillBans(t *testing.T) {
	for _, retired := range []string{"component_template_version", "system_template_version", "system_template_member", "POST /standards:fork", "POST /systems:instantiate"} {
		hit := false
		for _, term := range Banned {
			if term.Pattern.FindString(retired) != "" {
				hit = true
			}
		}
		if !hit {
			t.Errorf("%q no longer trips a banned pattern", retired)
		}
	}
}

func report(t *testing.T, name string, findings []Finding, enforce bool) {
	t.Helper()
	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  " + f.File + ":" + itoa(f.Line) + ": " + f.Text)
	}
	if enforce {
		t.Errorf("%d %s finding(s):%s", len(findings), name, b.String())
		return
	}
	t.Logf("WARN (not yet enforced, #437): %d %s finding(s):%s", len(findings), name, b.String())
}

// TestScanLineSkipsLinkTargets pins scanner fix 1. Six of the eight
// property_value hits in the corpus are the ADR-0047 anchor slug
// (#adr-0047-the-fields-fold-product_property-and-property_value), cited from
// six pages. That slug is an immutable historical ADR title: citing it is
// correct, and a lint that fires on it would make the corpus unfixable without
// breaking its own cross-references.
func TestScanLineSkipsLinkTargets(t *testing.T) {
	cases := []struct {
		name string
		line string
		want int
	}{
		{
			"the ADR-0047 anchor slug is a link target, not prose",
			"see [the fields fold](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)",
			0,
		},
		{
			"a bare markdown target is skipped too",
			"[x](#adr-0063-property_value-was-the-store)",
			0,
		},
		{
			"but the same term in prose still fires",
			"the `property_value` table holds the latest value",
			1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := len(scanLine(c.line)); got != c.want {
				t.Fatalf("scanLine(%q) = %d findings, want %d", c.line, got, c.want)
			}
		})
	}
}

// TestScanLineAllowsRetirementProse pins scanner fix 2. Every remaining hit for
// field_definition and field_value is prose that names the term precisely in
// order to retire it. Without this escape, adding those entries turns `make
// test` red on the sentences that do the teaching, which would push authors
// toward deleting the explanation instead of keeping it.
// (Neither component_type nor system_type is in this list: each left the
// denylist entirely when a later ADR reused the identifier for a new shape
// (ADR-0085 over ADR-0047, ADR-0096 over ADR-0048), so neither needs the
// escape any more.)
func TestScanLineAllowsRetirementProse(t *testing.T) {
	retiring := []string{
		"(the `field_value` store retired with the product catalog)",
		"replaces the old `field_definition`-as-shape notion",
		"was `property_value`, built",
		"Replaces the retired `field_definition`",
		"the former `field_value` store",
	}
	for _, line := range retiring {
		if got := scanLine(line); len(got) != 0 {
			t.Errorf("retirement prose fired the lint: %q -> %v", line, got)
		}
	}
	// The escape must be narrow: a nearby retirement word does not license an
	// unrelated current-tense claim later on the same line.
	live := "the `field_definition` catalog stores the contract"
	if got := scanLine(live); len(got) == 0 {
		t.Errorf("current-tense claim did not fire: %q", live)
	}
}

// TestScanLineFirstMatchWins pins scanner fix 3: a log_datapoint line must
// report its own entry (which points at the log_line lane) and not also the
// generic *datapoint* entry, whose replacement text is wrong for it.
func TestScanLineFirstMatchWins(t *testing.T) {
	got := scanLine("the `log_datapoint` table holds a component's own words")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want exactly 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "log_line") {
		t.Fatalf("finding %q does not point at the log_line lane", got[0])
	}
}

// TestScanLineEscapeIsProximityScoped pins that the retirement escape is scoped
// to the neighbourhood of the match, not the whole line. A long line can mention
// some unrelated thing that "was replaced" while still making a current-tense
// claim about a retired term, and a whole-line escape waved exactly that through.
func TestScanLineEscapeIsProximityScoped(t *testing.T) {
	drift := "The `field_definition` catalog stores the contract, unlike the old alarm model which was replaced."
	if got := scanLine(drift); len(got) == 0 {
		t.Errorf("a distant retirement word exempted current-tense drift: %q", drift)
	}
	near := "the `field_definition` catalog retired with the fields fold"
	if got := scanLine(near); len(got) != 0 {
		t.Errorf("an adjacent retirement marker failed to exempt: %q -> %v", near, got)
	}
}

// TestOperatorStrings enforces the denylist on the text an operator actually
// reads outside the docs tree. This is the lint that would have caught "the
// lineage cause of that intended datapoint" sitting in a seeded event_type
// description, upserted at every boot and rendered verbatim on the Event Types
// console page and in the API payload, right through a vocabulary migration
// that renamed the noun everywhere else.
func TestOperatorStrings(t *testing.T) {
	findings, err := ScanOperatorStrings()
	if err != nil {
		t.Fatal(err)
	}
	report(t, "operator-string vocabulary", findings, true)
}

// TestOperatorStringPathsResolve guards the failure mode that makes
// TestOperatorStrings meaningless: it reports zero findings both when the text
// is clean and when it read nothing at all. A reviewer read the ../../ prefixes
// as repo-relative and called the lint dead; the prefixes are right (a Go test
// runs with its package directory as the working directory, which is the same
// convention DocsRoot has always used) but nothing proved it. Now something does.
func TestOperatorStringPathsResolve(t *testing.T) {
	if len(operatorStrings) == 0 {
		t.Fatal("operatorStrings is empty, so ScanOperatorStrings can never find anything")
	}
	for _, p := range operatorStrings {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("operator-string path does not resolve from the package directory: %s (%v)", p, err)
		}
	}
}

// TestNearRetirementMarkerIsRuneSafe pins the window against multi-byte text:
// byte slicing at start-30 could split a rune and hand the matcher invalid UTF-8.
func TestNearRetirementMarkerIsRuneSafe(t *testing.T) {
	// Curly quotes and arrows are three bytes each, so a byte window would cut
	// through one of them here.
	line := "the “shape” arrow → ✓ marker sits here, and the `field_definition` catalog stores it"
	if got := scanLine(line); len(got) == 0 {
		t.Errorf("multi-byte line did not fire: %q", line)
	}
	near := "“retired” → the `field_definition` contract"
	if got := scanLine(near); len(got) != 0 {
		t.Errorf("multi-byte retirement prose fired: %q -> %v", near, got)
	}
}

// TestLinkTargetHandlesNestedParens pins the stripper against a URL carrying
// parentheses, which a naive [^)]* truncates, leaving a tail that reads as prose.
func TestLinkTargetHandlesNestedParens(t *testing.T) {
	line := "see [the fold](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)"
	if got := scanLine(line); len(got) != 0 {
		t.Errorf("plain link target fired: %v", got)
	}
	nested := "see [a note](https://example.test/x_(property_value)_y) for context"
	if got := scanLine(nested); len(got) != 0 {
		t.Errorf("nested-paren link target fired: %v", got)
	}
}

// TestADR0073EntriesFire proves the two ADR-0073 denylist entries match the
// prose they were written to retire: the real clause from ADR-0067 and the real
// phrase that described interface_type before it was named the transport.
func TestADR0073EntriesFire(t *testing.T) {
	cases := []struct{ name, line, wantOrigin string }{
		{"adr-0067 clause", "exposing a `calendar` **interface whose `interface_type` is a driver** (`graph-calendar`)", "ADR-0073"},
		{"as-a variant", "an interface_type as a driver is the wrong seam", "ADR-0073"},
		{"stale registry phrase", "the protocol-and-style registry (`ssh`, `http`, `snmp`)", "ADR-0073"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var hit bool
			for _, b := range Banned {
				if b.Origin == c.wantOrigin && b.Pattern.MatchString(c.line) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("no %s entry matched: %q", c.wantOrigin, c.line)
			}
		})
	}
}

// TestCLIReferenceDescriptions fails on any generated CLI flag published with
// an empty Description cell (#472): the blank traces to a missing `doc:` tag
// on the Huma input field, which this gate makes non-recurring.
func TestCLIReferenceDescriptions(t *testing.T) {
	findings, err := ScanCLIReference()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "cli-reference-descriptions", findings, true)
}

// TestADR0115RestoreShippedFires proves the renamed destructive slot's denylist
// entry matches the prose it was written to retire, in the sentence shape the
// operator guide actually used, and that it does NOT catch the word "restore"
// on its own (the other restores, an archived user's and the pen's, keep their
// wording).
func TestADR0115RestoreShippedFires(t *testing.T) {
	fires := func(line string) bool {
		for _, b := range Banned {
			if b.Origin == "ADR-0115" && b.Pattern.MatchString(line) {
				return true
			}
		}
		return false
	}
	if !fires("Once you have changed one, that slot becomes **Restore shipped**, which discards your version.") {
		t.Error("the guide's own sentence does not fire, so the entry guards nothing")
	}
	if fires("An archived user shows **Restore** in the left slot.") {
		t.Error("the bare word restore fired: the entry is too broad")
	}
	if fires("that slot becomes **Restore default**, which discards your version") {
		t.Error("the REPLACEMENT wording fired, so the entry would refuse the fix it asks for")
	}
}

// TestADR2030PlaceAsANounFires proves the place-as-a-noun entry catches the
// noun in the phrasings the sweep retired, and passes the English that keeps
// the word: the verb, the idioms, and the other words built on it (placed,
// placement, decimal places). It runs through scanLine, so the entry's
// allowed phrasings are part of what it proves.
func TestADR2030PlaceAsANounFires(t *testing.T) {
	fires := func(line string) bool {
		for _, msg := range scanLine(line) {
			if strings.Contains(msg, "ADR-2030") {
				return true
			}
		}
		return false
	}
	for _, line := range []string{
		"the place card names the system's type",
		"A place holding exactly one system lands on it.",
		"Start a subtree of places from a template",
		"**Configure place**",
		"Every place in the fleet, and what is in it.",
		"a component's own place must be at or below its system's",
		"a `place_template` lands a subtree",
		"the alarm's facts (`component`, `place`, `severity`)",
		"`?edit=place` opens that one",
	} {
		if !fires(line) {
			t.Errorf("place as a noun passed: %q", line)
		}
	}
	for _, line := range []string{
		"the components placed at a location",
		"a `placement: central` endpoint",
		"the decimal places a rendered value keeps",
		"the port edited in place connects",
		"InfoTip takes children in place of its (i)",
		"a new assertion earns its place by mutation",
		"the only place a metric's latest value is said",
		"theme tokens in one place",
		"N inlined copies are N places to drift",
		"a lab is never a place for real data",
		"`corporate` places one booking service a site",
		"an outpost delete re-places its tasks",
		"`group_weight` places a group",
		"where you place them, escaped",
		"is not a number in the first place",
		"both end up in the same place",
		"an identity no binding places lands in the inbox",
		"something in the middle answering in its place",
		"the fix-an-endpoint-in-place walkthrough",
	} {
		if fires(line) {
			t.Errorf("legitimate English fired: %q", line)
		}
	}
}

// TestADR2031NodeAsTheCollectorFires proves the node-as-the-collector entry
// catches the retired noun in every spelling the rename carried (the prose
// noun, the column, the env prefix, the route, the permission, the command)
// and passes the senses the word keeps: a node of a tree, a DOM node, a
// query plan's scan node, the Node.js runtime and its node: imports, the
// explore ?node= parameter, and the D2 shape class the diagrams share.
func TestADR2031NodeAsTheCollectorFires(t *testing.T) {
	fires := func(line string) bool {
		for _, msg := range scanLine(line) {
			if strings.Contains(msg, "ADR-2031") {
				return true
			}
		}
		return false
	}
	for _, line := range []string{
		"the node polls each endpoint on its interval",
		"A node holding a worklist heartbeats.",
		"Every node in the fleet reports its loss.",
		"the `node_id` column on endpoint_assignment",
		"set `OMNIGLASS_NODE_TOKEN` before the first start",
		"`GET /nodes` lists them",
		"a role granting `node:read`",
		"run `omniglass node run` on the edge host",
		"the omniglass-node component",
	} {
		if !fires(line) {
			t.Errorf("node as the collector passed: %q", line)
		}
	}
	for _, line := range []string{
		"a tree node carries its children",
		"the leaf node wins over its ancestor nodes",
		"the deepest node wins",
		"A scan node can name an index",
		"a focusable node hidden from assistive tech",
		"the root node of the forest",
		"a DOM node the test queries",
		"built with Node.js 22",
		"import { readFile } from 'node:fs'",
		"import path from \"node:path\"",
		"web/node_modules is not committed",
		"the explore view's `?node=` parameter",
		"node e2e/shot.mjs out.png",
		"classes: { node: { style.border-radius: 8 } }",
		"  node: { style.border-radius: 8 }",
		`  db: "Postgres" { class: node; shape: cylinder }`,
		"a grant on this node only",
	} {
		if fires(line) {
			t.Errorf("node in a kept sense fired: %q", line)
		}
	}
}

// TestADR2032SystemRoleFires proves the system-role entry catches what a
// system needs filled in every spelling the rename carried (the prose noun,
// the tables, the Go types, the routes, the accessor, the CLI subgroup) and
// passes the senses role keeps: the IAM role an operator is granted, its
// routes and permissions, and the ARIA role attribute.
func TestADR2032SystemRoleFires(t *testing.T) {
	fires := func(line string) bool {
		for _, msg := range scanLine(line) {
			if strings.Contains(msg, "ADR-2032") {
				return true
			}
		}
		return false
	}
	for _, line := range []string{
		"the system role is impaired",
		"Each system role carries a quorum and an impact.",
		"the `system_role` table",
		"a row in `system_role_assignment`",
		"`ListSystemRoles` resolves the effective set",
		"`PUT /systems/{name}/roles/{role}` declares one",
		"`GET /standards/{id}/roles` lists them",
		"a lookup through `$role.display`",
		"the `role_choice` table holds the alternates",
		"a role assignment names the component",
		"`omniglass system role assign`",
		"the effective roles of a system",
	} {
		if !fires(line) {
			t.Errorf("system role passed: %q", line)
		}
	}
	for _, line := range []string{
		"the operator role grants",
		"`GET /roles` lists them",
		"a grant needs `role:read`",
		"the viewer and operator roles ship seeded",
		"an agent token is bounded by the roles its person chose",
		`<div role="button" tabindex="0">`,
		"the Admin Roles page",
		"`omniglass role list`",
		"`principal_grant.role_id` names the role",
	} {
		if fires(line) {
			t.Errorf("role in a kept sense fired: %q", line)
		}
	}
}

// TestADR2035RegistryAsTheCatalogFires proves the registry-as-the-catalog entry
// catches the word where it names the database rows the fleet is typed by, and
// passes the lookups compiled into the binary that keep it: the transport
// registry and its entries, the expression diagnostics, the declarative env
// registry, the settings namespace registry, the simulator registry, the
// console's blade and page registries, the agent skills, and a container or
// package registry an image is pushed to or pulled from.
func TestADR2035RegistryAsTheCatalogFires(t *testing.T) {
	fires := func(line string) bool {
		for _, msg := range scanLine(line) {
			if strings.Contains(msg, "ADR-2035") {
				return true
			}
		}
		return false
	}
	for _, line := range []string{
		"the component_type registry is a hierarchy above product",
		"Every registry lists alphabetically by label.",
		"the three ingest registries share one name space",
		"an operator forks a shipped registry row",
		"the unit registry carries the symbol",
		"Registries are seeded official and operator-extensible.",
	} {
		if !fires(line) {
			t.Errorf("registry as the catalog passed: %q", line)
		}
	}
	for _, line := range []string{
		"the transport registry declares each rung",
		"each registry transport runs on a dev-fleet component",
		"The transports are a code registry the binary ships",
		"is its own registry entry over the tls handshake",
		"the engine keeps a registry of diagnostics",
		"generated from the registry (`make gen`)",
		"the declarative env registry in `internal/config`",
		"the environment registry (`make gen`)",
		"the namespace registry, Huma reflects it",
		"The simulator registry is per server process",
		"the fleet's cross-entity blade registry",
		"resolved through the PAGES registry",
		"the skill registry lacks it",
		"pushes to the GitHub Container Registry",
		"build with `--push` to a registry",
		"a fork run has no registry credential",
		"the binary never asks the registry",
		"the npm registry serves the package",
		"[forks](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row)",
		"the capability registry was retired by #626",
		"<BladeStack registry={fleetRegistry} />",
	} {
		if fires(line) {
			t.Errorf("registry in a kept sense fired: %q", line)
		}
	}
}

// TestADR2033LabelTemplateAndBlueprintFire proves the two ADR-2033 entries:
// the string a label renders from is a rule, never a label template (the
// prose noun and the column that spelled it), and a standard is the contract
// a system is built to, never a blueprint. The senses template keeps pass:
// a label rule, the Go text/template engine it is written in, the catalog
// template an operator clones once, and a template's own label.
func TestADR2033LabelTemplateAndBlueprintFire(t *testing.T) {
	fires := func(line string) bool {
		for _, msg := range scanLine(line) {
			if strings.Contains(msg, "ADR-2033") {
				return true
			}
		}
		return false
	}
	for _, line := range []string{
		"a new label template",
		"The label template systems of this type get",
		"Label templates resolve through four tiers.",
		"each label-template compiles at write time",
		"the `default_template` column the boot seed owns",
		"a standard is the blueprint a system conforms to",
		"the blueprints your systems are built against",
	} {
		if !fires(line) {
			t.Errorf("retired by ADR-2033 but passed: %q", line)
		}
	}
	for _, line := range []string{
		"a new label rule",
		"a Go text/template",
		"The rule language is Go `text/template` over a closed map.",
		"create a standard from the `meeting-room` standard template",
		"a template's label is the name the catalog shows",
		"the `label_rule` column's `default_rule` and `rule`",
		"a standard is the contract a system is built to",
	} {
		if fires(line) {
			t.Errorf("kept sense fired: %q", line)
		}
	}
}

// TestADR2036TheUserFires proves the ADR-2036 entries catch the retired IAM
// words in prose (principal, principal group, service account, and human as a
// kind) and pass what the words keep until the schema, API and permission
// rename (#2075) lands: a backticked identifier, a route, a permission, a
// command, a JSON field, and the English senses of principal and human.
func TestADR2036TheUserFires(t *testing.T) {
	fires := func(line string) bool {
		for _, msg := range scanLine(line) {
			if strings.Contains(msg, "ADR-2036") {
				return true
			}
		}
		return false
	}
	for _, line := range []string{
		"a principal holds grants",
		"Principals sign in with a password or a token.",
		"every principal in the install",
		"add a principal group for the night shift",
		"a principal-group's members inherit its grants",
		"a service account calls the API with a token",
		"Service accounts never sign in to the console.",
		"a service-account token",
		"a human principal signs in with a password",
		"human users and machine users alike",
		"the human kind carries a username",
		"humans and service principals",
		"a human or machine user",
	} {
		if !fires(line) {
			t.Errorf("retired IAM word passed: %q", line)
		}
	}
	for _, line := range []string{
		"a user holds grants",
		"a machine user calls the API with a token",
		"add a user group for the night shift",
		"a person signs in with a password",
		"the `principal` table keys every user",
		"`GET /principals` lists them",
		"`/principal-groups/{group}/members` adds one",
		"a role granting `principal:read`",
		"principal:read:admin gates the page",
		`perm: "principal_group:read:admin"`,
		"run `omniglass principal list`",
		"the `principal_id` column",
		"each grant row carries principal_id",
		"the `human` table holds the username",
		"`kind=human` filters the list",
		"me.principal.id is the caller",
		"omniglass principal-group member create night-shift --principal-id alice",
		"  --principal-group on-call --channels inbox",
		"  principal: {",
		`<SchemaTable table="principal" />`,
		"the principal reason is cost",
		"the principal axes of the design",
		"the three principal surfaces",
		"a human-in-the-loop gate",
		"a human reads the label",
		"the name is a human identifier",
		"a human approves the PR",
	} {
		if fires(line) {
			t.Errorf("IAM word in a kept sense fired: %q", line)
		}
	}
}

// TestPendingRenameKeepsOnlyADR2036 proves the #2075 carve-out waves through
// only ADR-2036 hits, and only in the generated files that rename rewrites.
func TestPendingRenameKeepsOnlyADR2036(t *testing.T) {
	adr := "principal is retired (ADR-2036); write user"
	other := "node is retired (ADR-2031); write outpost"
	if !pendingRenameHit("openapi.json", adr) || !pendingRenameHit(filepath.Join("reference", "mcp", "index.md"), adr) {
		t.Error("an ADR-2036 hit in a generated file the rename rewrites was reported")
	}
	if pendingRenameHit("openapi.json", other) {
		t.Error("another ADR's hit in openapi.json was waved through")
	}
	if pendingRenameHit(filepath.Join("architecture", "identity-access.mdx"), adr) {
		t.Error("an ADR-2036 hit in hand-written docs was waved through")
	}
}
