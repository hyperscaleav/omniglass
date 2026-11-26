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

// An edit that reuses names the SVG already draws (an FK column moved to another
// table, a type changed to one used elsewhere) leaves every identifier equal, so
// RenderDrift cannot see it; the source hash the render carries can.
func TestRenderStaleSeesAnEditThatReusesEveryName(t *testing.T) {
	moved := strings.Replace(source, "user_id: uuid {constraint: primary_key}", "user_id: uuid {constraint: foreign_key}", 1)
	if missing, extra := RenderDrift(moved, drawn); len(missing)+len(extra) != 0 {
		t.Fatalf("the fixture should reuse every name, got missing %v extra %v", missing, extra)
	}
	svg := drawn + "<!-- d2-source-sha256: " + D2SourceHash(source) + " -->"
	if stale, why := RenderStale(source, svg); stale {
		t.Fatalf("a render of this source read as stale: %s", why)
	}
	stale, why := RenderStale(moved, svg)
	if !stale || !strings.Contains(why, "changed since") {
		t.Fatalf("stale %v (%q), want the changed source named", stale, why)
	}
}

func TestRenderStaleNamesARenderWithNoSourceHash(t *testing.T) {
	stale, why := RenderStale(source, drawn)
	if !stale || !strings.Contains(why, "no source hash") {
		t.Fatalf("stale %v (%q), want a render with no hash read as stale", stale, why)
	}
}

func TestD2SourceHashIgnoresTheTrailingNewlineOnly(t *testing.T) {
	if D2SourceHash(source) != D2SourceHash(strings.TrimSuffix(source, "\n")) {
		t.Fatal("the hash should not depend on the trailing newline a fence carries")
	}
	if D2SourceHash(source) == D2SourceHash(source+"x") {
		t.Fatal("the hash did not change with the source")
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
	if stale, why := RenderStale(d2, string(svg)); stale {
		missing, extra := RenderDrift(d2, string(svg))
		t.Errorf("the committed ERD render is stale: %s (the source has %v the SVG never drew, the SVG draws %v the source dropped); "+
			"re-render it with `cd docs && pnpm diagrams` (needs the d2 binary) and commit the SVG", why, missing, extra)
	}
}
