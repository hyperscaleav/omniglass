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
  ["BandCanvas", "the band canvas; Explore's columns replaced it"],
  ["fleet_canvas", "the canvas paint core; retired with the canvas"],
  ["quick-name", "the blade's jump-anchor rows; the blade renders the EntityForm"],
  ["quick-classification", "the blade's jump-anchor rows; the blade renders the EntityForm"],
  ["quick-placement", "the blade's jump-anchor rows; the blade renders the EntityForm"],
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
