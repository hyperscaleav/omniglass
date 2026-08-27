// Package docslint holds the documentation lint suite: mechanical checks that
// keep the hand-written docs from drifting away from the code, run as ordinary
// Go tests so the `make test` gate (and `/ship-slice`) inherits them.
//
// The suite grew out of the 2026-07-30 drift audit, which found that the two
// fully generated doc artifacts (the ERD, the CLI reference) were accurate
// while nearly every hand-written claim about a built surface had drifted.
// Each lint here turns one audited drift class into a check.
//
// Slice 1 (#437): the banned-vocabulary lint and the decisions-format lint.
// Later slices add route, permission, make-target, env-var, and file-path
// lints, and a storage-table lint over the generated schema facts.
package docslint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DocsRoot is the content root the lints scan, relative to this package.
var DocsRoot = filepath.Join("..", "..", "docs", "src", "content", "docs")

// BannedTerm is a retired identifier or vocabulary item that must not appear
// in current-tense documentation. Every ADR that retires a term appends an
// entry here in the same PR (the rename ripple).
type BannedTerm struct {
	// Pattern matches the retired term. Word-ish boundaries are the entry's
	// own responsibility so each term can decide how strict to be.
	Pattern *regexp.Regexp
	// Replacement names what to write instead.
	Replacement string
	// Origin is the decision that retired the term.
	Origin string
}

// Banned is the denylist. Order is presentation only.
var Banned = []BannedTerm{
	{
		// The create form's precondition field, retired by ADR-0104's #702-review
		// amendment: it binds the drafted NAME, because a name carries the stem
		// and the suppression rule as well as the number and an ordinal claim was
		// met by a create that landed a name the form never showed. Both spellings,
		// since the CLI flag is the one an operator types. The three files that
		// still carry the old word (the decision log, the build log, and the
		// generated CLI reference) are the allowlisted historical and generated
		// ones, so this catches a current-tense page reintroducing it.
		Pattern:     regexp.MustCompile(`\bexpected_ordinal\b|--expected-ordinal\b`),
		Replacement: "expected_name, the drafted NAME a create form posts back",
		Origin:      "ADR-0104",
	},
	{
		// The registry blade's destructive slot, renamed by ADR-0115: it named
		// the thing being restored FROM rather than the thing being restored TO,
		// and the console's other restore (Settings, the pen) already said
		// default. Not a symbol, so nothing else catches it, and it is live
		// operator copy on two registries. The decision log and the build log
		// keep it, as vocabularyAllowed historical records.
		Pattern:     regexp.MustCompile(`(?i)\bRestore shipped\b`),
		Replacement: "Restore default, the one restore vocabulary the console uses",
		Origin:      "ADR-0115",
	},
	{
		// Two synonyms the corpus invented for the identifier. Neither is a symbol,
		// so nothing else catches them, and both survived the first vocabulary sweep
		// on ten pages: "technical name" appeared 21 times in the generated CLI
		// reference alone, from a Huma doc: tag. A third word for the identifier is
		// exactly what the triad exists to stop (ADR-0076).
		Pattern:     regexp.MustCompile(`(?i)\btechnical names?\b|\bkebab handles?\b`),
		Replacement: "name (the identifier), or label (the friendly string)",
		Origin:      "ADR-0076",
	},
	{
		// The identity triad settled on id / name / label (ADR-0076). These
		// two validators were the platform's other name rules and are DELETED, not
		// renamed: ValidateName picks the rule from the table's declared identity
		// shape, so a page naming either one is telling a contributor to call
		// something that no longer exists.
		Pattern:     regexp.MustCompile(`\bValidateEntityKey\b|\bkey\.ValidateKey\b`),
		Replacement: "storage.ValidateName, which picks the rule from the table's declared identity shape",
		Origin:      "ADR-0076",
	},
	{
		// The sentinels moved with the vocabulary.
		Pattern:     regexp.MustCompile(`\bErrInvalidEntityKey\b|\bErrEntityKeyIsUUID\b`),
		Replacement: "ErrInvalidEntityName / ErrEntityNameIsUUID",
		Origin:      "ADR-0076",
	},
	{
		// Split out of the generic datapoint entry below and placed BEFORE it,
		// because first-match-wins and the generic replacement is wrong here: a
		// log_datapoint did not become a property or a sample, it was retired
		// into a different lane entirely (ADR-0066).
		Pattern:     regexp.MustCompile(`(?i)log_datapoints?`),
		Replacement: "log_line (the raw ingest lane) or event (the derived occurrence)",
		Origin:      "ADR-0066",
	},
	{
		// The datapoint noun family: datapoint, datapoints, datapoint_type,
		// metric_datapoint, state_datapoint.
		Pattern:     regexp.MustCompile(`(?i)[a-z_]*datapoint[a-z_]*`),
		Replacement: "property (the signal), sample (the observation), or current value (the latest)",
		Origin:      "ADR-0065",
	},
	{
		// The one column in the schema where `label` meant an IDENTIFIER, renamed
		// by ADR-0111 ahead of the sweep that gives the word to the friendly string
		// on eighteen other tables. Spelled with the table, so it cannot catch the
		// ordinary noun.
		Pattern:     regexp.MustCompile(`\bservice\.label\b|\bsvcBody\.Label\b`),
		Replacement: "service.name, a service account's identifier",
		Origin:      "ADR-0111",
	},
	{
		// The other end of that sweep, and the reason ADR-0111 ran first: the
		// friendly string an operator reads is `label` on all twenty-three tables
		// that carry one, and the pen beside it is `label_generated` (ADR-0118).
		// Three spellings are banned together because a page can reach for any of
		// them: the column, the CLI flag, and the two-word noun the docs used for
		// the concept. The noun matters most, since it is the one nothing else
		// catches: a symbol lint sees `display_name` in a code fence, and no
		// compiler sees "the display name" in a sentence.
		Pattern:     regexp.MustCompile(`\bdisplay_names?\b|\bdisplay_name_generated\b|--display-name\b|(?i)\bdisplay names?\b`),
		Replacement: "label (the friendly string), or label_generated (the pen that says the platform rendered it)",
		Origin:      "ADR-0118",
	},
	{
		// The totality-of-managed-things noun, renamed by ADR-0123: the word an
		// operator's whole managed environment goes by is FLEET (a fleet of
		// systems, stationed across locations), and estate is retired with it.
		// The word boundary keeps the restate family (restate, restated,
		// restatement) out of reach, which is the only English family that
		// carries the old noun as a substring.
		Pattern:     regexp.MustCompile(`(?i)\bestates?\b|\bestate-wide\b`),
		Replacement: "fleet (the systems an operator runs, stationed across locations), or fleet-wide",
		Origin:      "ADR-0123",
	},
	{
		// The stored function that resolved a principal to its identifier, dropped
		// by ADR-0110. It is a retired schema OBJECT, so a page naming it is telling
		// a contributor to call something that is not there; and it is a retired
		// WORD, since both its branches returned an identifier and never a label.
		Pattern:     regexp.MustCompile(`\bprincipal_label\b`),
		Replacement: "the gateway's identifier resolution (internal/storage/principal_ident.go)",
		Origin:      "ADR-0110",
	},
	{
		Pattern:     regexp.MustCompile(`caused_by_event_id`),
		Replacement: "source_event_id",
		Origin:      "ADR-0066",
	},
	{
		Pattern:     regexp.MustCompile(`--mode[ =]node`),
		Replacement: "omniglass node run",
		Origin:      "ADR-0058",
	},
	{
		Pattern:     regexp.MustCompile(`\bListView\b`),
		Replacement: "ListShell with a FlatList or TreeList body",
		Origin:      "the list-surface split (build-log.md)",
	},
	{
		Pattern:     regexp.MustCompile(`\bUpsertOfficial\b`),
		Replacement: "the per-registry upserts (UpsertPropertyType and siblings)",
		Origin:      "internal/storage/registries.go",
	},

	// The fleet-model wave. Each of these was retired by an ADR that shipped
	// without its denylist entry, which is why the terms survived in the docs
	// long enough for the 2026-07-30 audit to find them.
	//
	// component_type is NOT here. ADR-0047 retired it (a flat classifier beside
	// component); ADR-0085 partially reverses that, returning it as a nested
	// registry above product, so the identifier is current vocabulary again,
	// deliberately in a different shape. A denylist entry cannot express
	// "banned, except in its own reintroduced meaning", so the fix is removal,
	// not an exemption; the term's dead sense (component.component_type, the
	// flat table) survives only in the historical ADR-0047 prose, which the
	// retirement-marker exemption below already protects.
	//
	// system_type is NOT here either, and left for exactly the same reason
	// (ADR-0096). ADR-0048 retired it as the COLUMN name for what is now
	// system.standard_id; ADR-0096 reuses the identifier for a new TABLE, the
	// coarse space taxonomy, with a system.system_type_id pointing at it. The
	// two are different objects and the live one is current vocabulary, so the
	// entry had to go rather than gain an exemption every sentence about the
	// registry would need. The dead sense survives in the historical ADR-0048
	// prose, under the same retirement-marker exemption.
	{
		Pattern:     regexp.MustCompile(`\bfield_definition\b`),
		Replacement: "product_property (the declared-property contract)",
		Origin:      "ADR-0047",
	},
	{
		Pattern:     regexp.MustCompile(`\bfield_value\b`),
		Replacement: "a declared property series row",
		Origin:      "ADR-0047",
	},

	// The naming rules of ADR-0072. Scoped tightly: a bare \bEvent\b would collide
	// with the live event table, event_type, event_rule, and EventWrite, which are
	// all current vocabulary. Only the retired identifiers are named.
	{
		Pattern:     regexp.MustCompile(`\b(MetricSampleEvent|StateSampleEvent|EventOccurrence)\b`),
		Replacement: "MetricSampleWrite, PropertySampleWrite, or EventWrite",
		Origin:      "ADR-0072",
	},
	{
		// The proto message and its old file, not the word "event".
		Pattern:     regexp.MustCompile("(?i)protobuf `?Event`?|og/v1/event\\.proto|event\\.pb\\.go"),
		Replacement: "TelemetryBatch (proto/og/v1/telemetry.proto)",
		Origin:      "ADR-0072",
	},

	// The template retirement (ADR-0071). Scoped to the snake_case IDENTIFIERS and
	// their _version rows on purpose: the word "template" is not retired, it is
	// redefined. A clonable example is exactly what an operator means by "start
	// from a template", so prose like "system templates you can import" is correct
	// and must not trip the lint. What died is the versioned shape an instance pins.
	{
		Pattern:     regexp.MustCompile(`\bcomponent_template(_version)?\b`),
		Replacement: "product (the device shape) with its product_property contract",
		Origin:      "ADR-0071",
	},
	{
		Pattern:     regexp.MustCompile(`\bsystem_template(_version|_member)?\b`),
		Replacement: "standard (the composition shape) and system_role (the slots)",
		Origin:      "ADR-0071",
	},

	// The ADR-0063 name foundation retired property_value for the store; the
	// ADR-0079 fold then retired the store itself, so the replacement names the
	// series model rather than the intermediate cache.
	{
		Pattern:     regexp.MustCompile(`\bproperty_value\b`),
		Replacement: "a property series row (a declared value is a row with provenance='declared'; the current value is the latest row per series)",
		Origin:      "ADR-0079",
	},
	{
		// Safe against property_type_id, which does not contain property_id.
		Pattern:     regexp.MustCompile(`\bproperty_id\b`),
		Replacement: "property_type_id",
		Origin:      "ADR-0063",
	},
	{
		// ADR-0067 sourced calendars through an "interface_type is a driver"
		// clause, which a table invites: any string fits, so a vendor API name
		// lands where a transport belongs. ADR-0073 retracts it and makes the
		// transport a code registry. Matches the claim, not the two nouns.
		Pattern:     regexp.MustCompile(`(?i)interface_type[^a-zA-Z0-9]{1,6}(?:is|as)[^a-zA-Z0-9]{1,6}a[^a-zA-Z0-9]{1,6}driver`),
		Replacement: "a transport plus a driver (a driver consumes transports, it is never one)",
		Origin:      "ADR-0073",
	},
	{
		// interface_type was described as the "protocol-and-style registry"
		// before ADR-0039 named it the transport; the phrase reads as though the
		// registry owns protocol handling, which is the driver's job.
		Pattern:     regexp.MustCompile(`(?i)protocol-and-style registry`),
		Replacement: "transport registry",
		Origin:      "ADR-0073",
	},

	// The five-lane wave (ADR-0079). The word "state" alone is NOT bannable
	// (a health state is a concept, the record/state lane is current
	// vocabulary), so only the precise table senses are named: the bare token
	// in backticks (the only code entity ever called `state` was the sample
	// table, now `property`; hyphenated names like `power-state` cannot match
	// because the backtick must sit immediately before the word), the
	// state-plus-storage-noun compounds, and the old two-table pairing.
	{
		// The compound arm refuses a leading hyphen ([^-\w]) rather than using \b,
		// so "steady-state rows" and its solid-state cousins stay legal prose.
		Pattern:     regexp.MustCompile("(?i)`state`|(?:^|[^-\\w])state (?:tables?|series|samples?|rows?|sinks?|kinds?)\\b|\\bmetric`? ?/ ?`?state\\b"),
		Replacement: "property (the value lane): the sample tables are `metric` and `property`",
		Origin:      "ADR-0079",
	},
	{
		// The Go identifiers of the state-table era, deleted with the rename:
		// the write struct, the insert, and the latest/transition readers.
		Pattern:     regexp.MustCompile(`\b(?:StateSampleWrite|InsertStateSamples|LatestState\w*|StateTransitions)\b`),
		Replacement: "PropertySampleWrite / InsertPropertySamples / LatestProperty / PropertyTransitions",
		Origin:      "ADR-0079",
	},
	{
		// The dotted-name vocabulary of the two-rule era, retired when the name
		// rule collapsed to one kebab token (#586). The bare word "dotted" stays
		// legal (retirement prose needs it); these compounds only ever described
		// the dead rule, and each survived a manual sweep at least once before
		// this entry existed.
		Pattern:     regexp.MustCompile(`(?i)\bdot-(?:joined|hierarchied|segmented)\b`),
		Replacement: "a single kebab token: every name is one segment, no dots",
		Origin:      "ADR-0079",
	},
	{
		// The generic permission resource of the pre-split Types page. The bare
		// word "type" is everywhere and legal; only the resource:action stamp
		// form is dead, and the verb suffix keeps "Issue Type" and its cousins
		// out of reach.
		Pattern:     regexp.MustCompile("`?\\btype:(?:read|create|update|delete)\\b`?"),
		Replacement: "location_type:<action>, the registry's own resource",
		Origin:      "ADR-0082",
	},
	{
		// The lane collective noun. Four lanes are telemetry; the command lane
		// is an instruction you issue, so calling all five "telemetry" makes a
		// command a reading. The lane STRUCTURE (ADR-0079) stands; only the
		// noun retired. ADR anchors and history quote it under the standing
		// exemptions.
		Pattern:     regexp.MustCompile(`(?i)\btelemetry lanes\b`),
		Replacement: "signal lanes (four inbound, one outbound; a command is not a reading)",
		Origin:      "ADR-0084",
	},

	// The capability retirement (#626). "Capability" alone is NOT bannable: the
	// word is ordinary English throughout the corpus in senses that have nothing
	// to do with the registry (a governed AI capability, a capability-checked
	// route meaning permission-gated, the capability-wrapping test-tier carve-out
	// for raw sockets and ICMP, a template's capability manifest, a page's
	// Design/Partial/Built status per documented capability). Only the retired
	// registry's own identifiers are named: the five dropped tables, the Go
	// symbols the health rollup and role assignment used to call, the routes,
	// and the permission stamp. capability-gated staffing retires with them: an
	// occupant now satisfies its slot when its component's own verdict is
	// healthy (internal/health), nothing about what it "provides".
	{
		Pattern: regexp.MustCompile(`\b(?:component_capability|product_capability|alarm_capability|system_role_capability)\b`),
		Replacement: "the typed-slot guard (system_role_type, system_role_product) for assignment; " +
			"a component's own verdict, from its active alarms, for health",
		Origin: "#626",
	},
	{
		// The registry table itself and its CRUD surface, scoped to the
		// code-formatted token so the sentence "a governed capability" stays
		// legal: only a backticked `capability`/`capabilities`, the registry
		// phrase, the retired Go symbols, and the permission stamp are named.
		Pattern: regexp.MustCompile("`capabilit(?:y|ies)`|\\bcapability registr(?:y|ies)\\b|\\bcapability catalogs?\\b|" +
			"\\bcapability:(?:read|create|update|delete)\\b|" +
			"\\b(?:EffectiveCapabilities|ComponentCapabilities|CreateCapability|UpdateCapability|DeleteCapability|" +
			"ListCapabilities|GetCapability|UpsertCapability|CapabilityPatch|SetComponentCapability|ClearComponentCapability)\\b"),
		Replacement: "the component_type taxonomy (product classification) and the typed-slot guard " +
			"(system_role_type, system_role_product) for assignment; nothing replaces the registry itself",
		Origin: "#626",
	},
	{
		// The routes: /capabilities, /capabilities/{id}, and the component-scoped
		// /components/{name}/capabilities[/{capability}] arc. The leading slash
		// keeps this from matching prose that happens to contain the bare word.
		Pattern:     regexp.MustCompile(`/capabilities\b`),
		Replacement: "no replacement: the routes are gone, not renamed",
		Origin:      "#626",
	},
}

// vocabularyAllowed lists files (relative to DocsRoot) exempt from the
// banned-vocabulary lint: historical records where a retired term was true at
// the time it was written, and generated files whose fix belongs in the
// generator's source.
var vocabularyAllowed = map[string]bool{
	filepath.Join("architecture", "decisions.md"): true,
	filepath.Join("architecture", "build-log.md"): true,
	filepath.Join("reference", "cli", "index.md"): true,
}

// operatorStrings are the non-docs files whose text reaches an OPERATOR: seed
// descriptions are upserted into registries at every boot and render verbatim
// on catalog pages and in API payloads, the console nav labels and hints are
// read on every page, and the README is the first thing a reader of the public
// repo sees. The docs lint cannot see any of them (DocsRoot is the content
// tree), which is how "intended datapoint" survived on a live console page
// through a whole vocabulary migration.
//
// Deliberately a short explicit list rather than a tree walk: the goal is the
// text an operator reads, not every identifier in the codebase. Renaming
// internal identifiers is a code change with its own ripple, not a lint.
var operatorStrings = []string{
	filepath.Join("..", "..", "internal", "seed", "event_types.yaml"),
	filepath.Join("..", "..", "internal", "seed", "property_types.yaml"),
	filepath.Join("..", "..", "internal", "seed", "metric_types.yaml"),
	filepath.Join("..", "..", "internal", "seed", "command_types.yaml"),
	filepath.Join("..", "..", "internal", "seed", "roles.yaml"),
	filepath.Join("..", "..", "web", "src", "lib", "nav.ts"),
	filepath.Join("..", "..", "README.md"),
	// The generated OpenAPI document carries every Huma operation and field
	// description (#451): the doc: tags flow to the spec, the CLI reference,
	// and the SPA types, so a retired noun here reaches operators through
	// three surfaces at once. Scanning the generated spec rather than the Go
	// source follows whatever produces the description.
	filepath.Join("..", "..", "api", "openapi.json"),
}

// ScanOperatorStrings applies the same denylist to the operator-visible text
// outside the docs tree. Reported separately from ScanVocabulary so a failure
// says which surface drifted.
func ScanOperatorStrings() ([]Finding, error) {
	var findings []Finding
	for _, path := range operatorStrings {
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue // a seed file that has not been added yet is not a lint failure
		}
		if err != nil {
			return nil, err
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, text := range scanLine(line) {
				findings = append(findings, Finding{File: filepath.Base(path), Line: i + 1, Text: text})
			}
		}
	}
	return findings, nil
}

// Finding is one lint hit.
type Finding struct {
	File string // path relative to DocsRoot
	Line int    // 1-indexed
	Text string // what and why
}

// walkDocs yields every .md/.mdx file under DocsRoot as (relative path, content).
func walkDocs(fn func(rel string, content string) error) error {
	return filepath.Walk(DocsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".md" && ext != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(DocsRoot, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return fn(rel, string(b))
	})
}

// ScanVocabulary returns every banned-term hit in the docs content, excluding
// the allowlisted historical and generated files.
func ScanVocabulary() ([]Finding, error) {
	var findings []Finding
	err := walkDocs(func(rel, content string) error {
		if vocabularyAllowed[rel] {
			return nil
		}
		for i, line := range strings.Split(content, "\n") {
			for _, text := range scanLine(line) {
				findings = append(findings, Finding{File: rel, Line: i + 1, Text: text})
			}
		}
		return nil
	})
	return findings, err
}

// linkTarget matches a markdown link destination: the (...) of [text](dest).
// A destination is an address, not prose, and an ADR anchor slug is the ADR's
// own immutable historical title, so citing one must never trip the lint that
// retired the term the title contains.
// The inner alternation tolerates one level of nested parentheses, which real
// URLs do carry (a wiki link ending in "(disambiguation)"). A naive [^)]* stops
// at the first inner ")" and leaves a dangling tail that can look like prose.
var linkTarget = regexp.MustCompile(`\]\((?:[^()]|\([^()]*\))*\)`)

// retiringProse marks prose that names a retired term in order to RETIRE it,
// which is the one context where writing the dead word is the correct thing to
// do. Without this the denylist fires on the very sentences that teach the
// rename, and the cheapest way to green the build becomes deleting the
// explanation.
var retiringProse = regexp.MustCompile(`(?i)\b(retire[ds]?|retirement|was|were|old|former(ly)?|replaces?d?|supersed(e|es|ed)|no longer|instead of|renamed)\b`)

// retirementWindow is how far either side of a banned term the retirement
// marker must sit for the exemption to apply. Scoped rather than whole-line on
// purpose: a long line can easily mention some unrelated thing that "was
// replaced" while still making a current-tense claim about the retired term,
// and a whole-line escape would wave that through. Wide enough for the real
// phrasings ("the `field_definition` registry was replaced"), narrow enough
// that a clause about something else does not license it. Every real phrasing
// in the corpus puts the marker within ~25 characters ("replaces the old
// `field_definition`", "was `property_value`, built", "the former `field_value`
// store"), so 30 fits them all while a mention two clauses away does not reach.
const retirementWindow = 30

// nearRetirementMarker reports whether a retirement word sits within
// retirementWindow CHARACTERS of the match at byte offsets [start, end).
//
// The window is counted in runes, not bytes, so a multi-byte character near the
// boundary cannot split mid-rune and hand the matcher invalid UTF-8. The docs
// carry plenty of them (curly quotes, arrows), so this is a live concern rather
// than a theoretical one.
func nearRetirementMarker(prose string, start, end int) bool {
	before := []rune(prose[:start])
	if len(before) > retirementWindow {
		before = before[len(before)-retirementWindow:]
	}
	after := []rune(prose[end:])
	if len(after) > retirementWindow {
		after = after[:retirementWindow]
	}
	return retiringProse.MatchString(string(before)) || retiringProse.MatchString(string(after))
}

// scanLine returns one message per banned term found in a single line of docs
// prose, after the link-target and retirement-prose exemptions. Pure: no I/O,
// which is what makes the exemptions testable without a corpus.
//
// First match wins: the entries are ordered specific-before-generic, so a
// log_datapoint line reports the log_line lane rather than also reporting the
// generic datapoint entry, whose replacement text is wrong for it.
func scanLine(line string) []string {
	prose := linkTarget.ReplaceAllString(line, "")
	for _, term := range Banned {
		loc := term.Pattern.FindStringIndex(prose)
		if loc == nil {
			continue
		}
		if nearRetirementMarker(prose, loc[0], loc[1]) {
			continue // named in order to retire it
		}
		return []string{prose[loc[0]:loc[1]] + " is retired (" + term.Origin + "); write " + term.Replacement}
	}
	return nil
}

var (
	adrHeading  = regexp.MustCompile(`(?m)^### ADR-(\d{4})\b`)
	adrIndexRow = regexp.MustCompile(`(?m)^\|\s*\[ADR-(\d{4})\]`)
	adrDate     = regexp.MustCompile(`(?i)\*\*Date:?\*\*`)
	adrStatus   = regexp.MustCompile(`(?i)\*\*Status:?\*\*`)
)

// ScanDecisions checks the decision log's structure: unique ADR numbers in
// ascending order, an index-table row per entry, and Date and Status fields on
// each entry. (The status vocabulary itself is prose; uniqueness and presence
// are what rot mechanically.)
func ScanDecisions() ([]Finding, error) {
	path := filepath.Join(DocsRoot, "architecture", "decisions.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(b)
	rel := filepath.Join("architecture", "decisions.md")
	var findings []Finding

	indexed := map[string]bool{}
	for _, m := range adrIndexRow.FindAllStringSubmatch(content, -1) {
		indexed[m[1]] = true
	}

	locs := adrHeading.FindAllStringSubmatchIndex(content, -1)
	seen := map[string]int{}
	prev := 0
	for i, loc := range locs {
		num := content[loc[2]:loc[3]]
		line := 1 + strings.Count(content[:loc[0]], "\n")
		if firstLine, dup := seen[num]; dup {
			findings = append(findings, Finding{rel, line, "duplicate ADR-" + num + " (first at line " + itoa(firstLine) + ")"})
		}
		seen[num] = line
		n := atoi(num)
		if n < prev {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " is out of numeric order"})
		}
		if n > prev {
			prev = n
		}
		if !indexed[num] {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " has no index-table row"})
		}
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		entry := content[loc[0]:end]
		if !adrDate.MatchString(entry) {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " has no Date field"})
		}
		if !adrStatus.MatchString(entry) {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " has no Status field"})
		}
	}
	return findings, nil
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// cliBlankFlag matches a generated CLI-reference flag row whose Description
// cell is empty: `| `--flag` | type | default |  |`. The reference is fully
// generated (cmd/docsgen), so a blank cell is always a missing `doc:` struct
// tag on the Huma input field the flag renders from.
var cliBlankFlag = regexp.MustCompile("^\\| `--[^`]+` \\|.*\\| *\\|$")

// ScanCLIReference walks the generated CLI reference for flag rows with an
// empty Description cell (#472). The reference is the one doc artifact the
// drift audit found accurate because it is generated; this lint keeps the
// generation from quietly publishing undocumented flags when an input struct
// gains a field without a doc tag.
func ScanCLIReference() ([]Finding, error) {
	rel := filepath.Join("reference", "cli", "index.md")
	raw, err := os.ReadFile(filepath.Join(DocsRoot, rel))
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for i, line := range strings.Split(string(raw), "\n") {
		if cliBlankFlag.MatchString(line) {
			flag := line[2 : strings.Index(line, "` |")+1]
			findings = append(findings, Finding{File: rel, Line: i + 1, Text: "flag " + flag + " has an empty Description cell: add a doc tag to its Huma input field and re-run make gen"})
		}
	}
	return findings, nil
}
