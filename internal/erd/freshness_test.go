package erd

import (
	"os"
	"strings"
	"testing"
)

// The docs build inlines a pre-rendered SVG of the ERD (astro-d2 runs with
// skipGeneration), and nothing re-rendered it when the schema changed: #2075 left
// node and node_log drawn after the rename, and #2363's user_group_idp_mapping was
// never drawn (#2625). TestERDMatchesSchema keeps the D2 source fresh; these keep
// the committed render equal to that source, by the identifiers each carries, so
// they need neither Postgres nor the d2 binary and run in make test.

// erdSVG is the render the docs inline for the data-model page's first diagram.
const erdSVG = "../../docs/public/d2/docs/architecture/data-model-0.svg"

const drawn = `<svg><text>identity</text><text>person</text><text>user_id</text><text>uuid</text><text>PK</text></svg>`
const source = "identity: {\n  person: {\n    shape: sql_table\n    user_id: uuid {constraint: primary_key}\n  }\n}\n"

func TestRenderDriftIsEmptyWhenTheSVGDrawsTheSource(t *testing.T) {
	if missing, extra := RenderDrift(source, drawn); len(missing)+len(extra) != 0 {
		t.Fatalf("missing %v, extra %v, want none", missing, extra)
	}
}

func TestRenderDriftNamesATableTheSVGNeverDrew(t *testing.T) {
	grown := source + "identity: {\n  user_group_idp_mapping: {\n    shape: sql_table\n    id: uuid {constraint: primary_key}\n  }\n}\n"
	missing, extra := RenderDrift(grown, drawn)
	if strings.Join(missing, ",") != "id,user_group_idp_mapping" || len(extra) != 0 {
		t.Fatalf("missing %v, extra %v, want id and user_group_idp_mapping missing", missing, extra)
	}
}

func TestRenderDriftNamesWhatTheSVGKeepsThatTheSourceDropped(t *testing.T) {
	kept := strings.Replace(drawn, "<text>person</text>", "<text>person</text><text>node_log</text>", 1)
	missing, extra := RenderDrift(source, kept)
	if len(missing) != 0 || strings.Join(extra, ",") != "node_log" {
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
