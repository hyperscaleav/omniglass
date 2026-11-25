package erd

import (
	"encoding/base64"
	"os"
	"slices"
	"strings"
	"testing"
)

// The docs build inlines a pre-rendered SVG of the ERD (astro-d2 runs with
// skipGeneration), and nothing re-rendered it when the schema changed: #2075 left
// node and node_log drawn after the rename, and #2363's user_group_idp_mapping was
// never drawn (#2625). TestERDMatchesSchema keeps the D2 source fresh; these keep
// the committed render equal to that source, by the structure each carries, so
// they need neither Postgres nor the d2 binary and run in make test.

// erdSVG is the render the docs inline for the data-model page's first diagram.
const erdSVG = "../../docs/public/d2/docs/architecture/data-model-0.svg"

// g is a group the renderer names with the base64 of its D2 path; row is one
// column the way the renderer draws it: a name, a type and a key label.
func g(path string) string {
	return `<g class="` + base64.StdEncoding.EncodeToString([]byte(path)) + `">`
}

func row(name, typ, key string) string {
	return `<text class="text fill-B2">` + name + `</text><text class="text fill-N2">` + typ + `</text><text class="text fill-AA2">` + key + `</text>`
}

const source = "identity: {\n  person: {\n    shape: sql_table\n    user_id: uuid {constraint: primary_key}\n    org_id: uuid {constraint: foreign_key}\n  }\n}\n" +
	"fleet: {\n  org: {\n    shape: sql_table\n    id: uuid {constraint: primary_key}\n  }\n}\n" +
	"identity.person.org_id -> fleet.org.id\n"

var drawn = "<svg>" + g("identity") + g("fleet") + g("identity.person") + g("fleet.org") + g("(identity.person -> fleet.org)[0]") +
	`<text class="text fill-N7">person</text>` + row("user_id", "uuid", "PK") + row("org_id", "uuid", "FK") +
	`<text class="text fill-N7">org</text>` + row("id", "uuid", "PK") + "</svg>"

func TestRenderDriftIsEmptyWhenTheSVGDrawsTheSource(t *testing.T) {
	if missing, extra := RenderDrift(source, drawn); len(missing)+len(extra) != 0 {
		t.Fatalf("missing %v, extra %v, want none", missing, extra)
	}
}

// Each edit below regenerates the source without adding a single new identifier, the
// case a flat word comparison passes.
func TestRenderDriftSeesEditsThatReuseExistingNames(t *testing.T) {
	cases := map[string]string{
		"a column moved to another table": strings.Replace(source, "    org_id: uuid {constraint: foreign_key}\n  }\n}\nfleet", "  }\n}\nfleet", 1),
		"a type changed":                  strings.Replace(source, "user_id: uuid", "user_id: bigint", 1),
		"a key label changed":             strings.Replace(source, "org_id: uuid {constraint: foreign_key}", "org_id: uuid {constraint: primary_key}", 1),
		"a connection redirected":         strings.Replace(source, "-> fleet.org.id", "-> identity.person.user_id", 1),
		"a column added with a seen name": strings.Replace(source, "  org: {\n    shape: sql_table\n", "  org: {\n    shape: sql_table\n    org_id: uuid {constraint: foreign_key}\n", 1),
	}
	for name, edited := range cases {
		if missing, extra := RenderDrift(edited, drawn); len(missing)+len(extra) == 0 {
			t.Errorf("%s: RenderDrift found no drift", name)
		}
	}
}

func TestRenderDriftNamesATableTheSVGNeverDrew(t *testing.T) {
	grown := source + "identity: {\n  user_group_idp_mapping: {\n    shape: sql_table\n    id: uuid {constraint: primary_key}\n  }\n}\n"
	missing, _ := RenderDrift(grown, drawn)
	if !slices.Contains(missing, "table identity.user_group_idp_mapping") {
		t.Fatalf("missing %v, want the table named", missing)
	}
}

func TestRenderDriftNamesWhatTheSVGKeepsThatTheSourceDropped(t *testing.T) {
	kept := strings.Replace(drawn, "</svg>", g("identity.node_log")+"</svg>", 1)
	missing, extra := RenderDrift(source, kept)
	if len(missing) != 0 || strings.Join(extra, ",") != "table identity.node_log" {
		t.Fatalf("missing %v, extra %v, want node_log extra", missing, extra)
	}
}

func TestCommittedERDRenderMatchesItsSource(t *testing.T) {
	d2, err := committedD2(erdPage)
	if err != nil {
		t.Fatal(err)
	}
	svg, err := os.ReadFile(erdSVG)
	if err != nil {
		t.Fatal(err)
	}
	missing, extra := RenderDrift(d2, string(svg))
	if len(missing)+len(extra) != 0 {
		t.Errorf("the committed ERD render is stale: the source has %v the SVG never drew, and the SVG draws %v the source dropped; "+
			"re-render it with `cd docs && corepack pnpm diagrams` (needs the d2 binary) and commit the SVG", missing, extra)
	}
}
