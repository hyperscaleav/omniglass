import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// Guard (#826): the surfaces the explorer refit retired stay retired. A KPI
// chip row, a band canvas, and the blade's jump-anchor rows each came back
// once as a "small" reintroduction in the pilot; this pins their identifiers
// out of the source tree in the style of the vocabulary lints, so a
// reintroduction is a failing test rather than a review catch.
const RETIRED = [
  ["fleet-summary", "the KPI summary rail; the shared header's one counts line replaced it"],
  ["fleet-tiles", "the summary board; nothing counts anything but the counts line"],
  ["badge-attention", "the rail's attention badge; the counts line carries need-attention"],
  ["BandCanvas", "the band canvas; the Explore renderers replaced it"],
  ["fleet_canvas", "the canvas paint core; retired with the canvas"],
  ["quick-name", "the blade's jump-anchor rows; the blade renders the EntityForm"],
  ["quick-classification", "the blade's jump-anchor rows; the blade renders the EntityForm"],
  ["quick-placement", "the blade's jump-anchor rows; the blade renders the EntityForm"],
  // Retired by the same epic that first built them (#839): the Miller-column
  // drill was replaced before it shipped, so these never reached a release and
  // there is no version of the console where they are the right answer.
  ["explore-column", "the Miller-column strip; a card is a level the cut works out"],
  ["explore-glance", "the columns' rightmost pane; a dot opens the system's workspace"],
  ["columnsFor", "the column builder; sectionsFor and insideOf replaced it"],
  // The last KPI chip row (ADR-0137 retires them): the Locations table's
  // summary board, a badge rail over a tile board with a type-mix donut.
  ["SummaryRail", "the list summary board; a table carries no KPI row above it"],
  ["defaultWidgets", "the summary board's config; retired with the board"],
  ["og-loc-widgets", "the summary board's stored preference; retired with the board"],
  // The inventory-era detail panels, unmounted when the classic faces retired
  // (#806): the workspace's header, Activity tab and roles surface replaced them.
  ["HealthPanel", "the classic detail's health panel; the workspace header and Activity tab replaced it"],
  ["MembersPanel", "the classic detail's members panel; staffing a role is how a member is bound"],
  // Explore's renderer library and the inventory tables (#861, #868): the
  // outline of places replaced the cards, bands, mosaic and matrix faces, the
  // saved views over them, and the table face with its kind tabs.
  ["sectionsFor", "the cards face's section builder; the outline is one tree of places"],
  ["cutTypeFor", "the level cut; the outline folds single-child chains instead"],
  ["foldToBudget", "the renderers' space budget; the outline has no canvas to budget"],
  ["matrixFor", "the standards matrix; a place's row names its standard"],
  ["layoutPx", "the mosaic's pixel layout; the outline has no canvas to tile"],
  ["fillFor", "the mosaic's share fill; a place counts its systems by verdict instead"],
  ["explore-mosaic", "the mosaic face; retired with the renderer library"],
  ["explore-matrix", "the matrix face; retired with the renderer library"],
  ["MatrixFace", "the standards matrix face; retired with the matrix"],
  ["DotField", "the dot field; a place's health is four counted slots"],
  ["STOCK_PRESETS", "the saved renderer views; the filter rides the address instead"],
  ['"explore-face"', "the renderer switch; the outline is the one view"],
  ["explore-presets", "the saved-view menu; retired with the presets"],
  ["explore-section-head", "the drilled section header; a row's + creates where you stand"],
  ["fleet-list-face", "the table face; the outline lists every place, system and component"],
  // The workspaces the audit found speaking those retired idioms (#872): a
  // place's band canvas and its cards, a counts line over each detail, the KPI
  // tiles, and the component cards. A detail view is one view per system, its
  // place as context, its components in the outline's row idiom.
  ["SystemCard", "the system card grid; a system's brief card is SystemSummary, its components are rows"],
  ["FleetShell", "the workspaces' counts-line frame; the subject card's header says each fact once"],
  ["FleetRows", "the location page's list face; a folder lists what is beneath it as the outline"],
  ["fleet_tiles", "the detail views' counts lines; the subject card's header carries them"],
  ["bandsOf", "the band builder; a folder renders the outline rooted at it"],
  ["holesUnder", "the inert + System holes; the outline's + creates where you stand"],
  ["zoomband-", "the location page's bands; retired with the band builder"],
  ["add-location-hole", "the inert + Location box; the outline's + and New create"],
  ["view-toggle", "the cards/list toggle; a folder has one face"],
  ["kpi-tiles", "the KPI tiles; the Data section says each metric once"],
  ["compcard-", "the component cards; a system's components are rows"],
  ["Location follows the primary system", "copy the model does not back: a component's place is its own (#862)"],
] as const;

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(p);
  }
  return out;
}

describe("retired surfaces stay retired", () => {
  it("no source file names a retired surface", () => {
    const files = walk(join(__dirname));
    const hits: string[] = [];
    for (const f of files) {
      if (f.endsWith("retired-surfaces-guard.test.ts")) continue;
      const src = readFileSync(f, "utf8");
      for (const [token, why] of RETIRED) {
        if (src.includes(token)) hits.push(`${f.replace(__dirname + "/", "")}: ${token} (${why})`);
      }
    }
    expect(hits).toEqual([]);
  });
});
