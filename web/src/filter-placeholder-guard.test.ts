import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// One casing for the filter bar's placeholder (#2447): "filter by ...", lower case,
// like the FilterBar's own default. Files and Explore read "Filter by ..." beside
// twenty lists that read "filter by ...".
const SRC = __dirname;

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return f === "node_modules" ? [] : sources(p);
    return /\.tsx$/.test(f) && !/\.test\.tsx$/.test(f) ? [p] : [];
  });
}

const PLACEHOLDER = /(?:filterPlaceholder\s*[:=]\s*\{?|placeholder=)\s*["'`]([^"'`]*)["'`]/g;

export function badPlaceholders(file: string, text: string): string[] {
  return [...text.matchAll(PLACEHOLDER)]
    .filter((m) => /^filter/i.test(m[1]) && !/^filter/.test(m[1]))
    .map((m) => `${file}:${text.slice(0, m.index).split("\n").length}: "${m[1]}" starts with a capital`);
}

describe("filter placeholders", () => {
  it("flags a capitalized one and passes a lower-case one", () => {
    expect(badPlaceholders("x", `filterPlaceholder: "Filter by name"`)).toHaveLength(1);
    expect(badPlaceholders("x", `placeholder="Filter by name"`)).toHaveLength(1);
    expect(badPlaceholders("x", `filterPlaceholder: "filter by name"`)).toHaveLength(0);
    expect(badPlaceholders("x", `placeholder="Search"`)).toHaveLength(0);
  });

  it("every filter placeholder in the console is lower case", () => {
    const hits = sources(SRC).flatMap((f) => badPlaceholders(relative(SRC, f), readFileSync(f, "utf8")));
    expect(hits, hits.join("\n")).toEqual([]);
  });
});
