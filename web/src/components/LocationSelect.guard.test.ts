import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// Every location field is LocationSelect (#2301). This scans the console source
// for the two ways a location field has been built by hand, so a new one fails
// here, naming the file and line, instead of shipping a flat list:
//
//   1. a native <select> or a raw <TreeSelect> whose markup mentions a location
//      (its items, value, options or label), and
//   2. location rows mapped into tree nodes outside lib/location_tree.
//
// A location TYPE picker is a different field (a catalog row, not a place) and
// is not flagged. A static scan, not a render-time check: it is cheap, it covers
// every surface without mounting it, and an allowlist entry here is the visible
// cost of an exception.
const SRC = join(__dirname, "..");
const ALLOW = new Set(["components/LocationSelect.tsx", "lib/location_tree.ts"]);

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return f === "node_modules" ? [] : sources(p);
    return /\.tsx?$/.test(f) && !/\.test\.tsx?$|\.gen\.ts$|\.testkit\.tsx?$/.test(f) ? [p] : [];
  });
}

// A location, but not a location type.
const MENTIONS_LOCATION = /location(?!_?types?\b|Types?\b)/i;
const lineOf = (text: string, at: number) => text.slice(0, at).split("\n").length;

export function handBuiltLocationFields(file: string, text: string): string[] {
  const hits: string[] = [];
  for (const m of text.matchAll(/<select\b[\s\S]*?<\/select>|<TreeSelect\b[\s\S]*?\/>/g)) {
    if (MENTIONS_LOCATION.test(m[0])) hits.push(`${file}:${lineOf(text, m.index!)}: location field built from ${m[0].startsWith("<select") ? "a native <select>" : "a raw <TreeSelect>"}`);
  }
  for (const m of text.matchAll(/locations?\b[^\n]*\.map\([^\n]*parentId/gi)) {
    hits.push(`${file}:${lineOf(text, m.index!)}: location rows mapped to tree nodes outside lib/location_tree`);
  }
  return hits;
}

describe("location fields", () => {
  it("flags the hand-built shapes and passes LocationSelect", () => {
    expect(handBuiltLocationFields("x", `<select value={location()}><option value="">(unplaced)</option></select>`)).toHaveLength(1);
    expect(handBuiltLocationFields("x", `<TreeSelect items={locationItems()} value={v()} onChange={s} />`)).toHaveLength(1);
    expect(handBuiltLocationFields("x", `const n = (locations.data ?? []).map((l) => ({ id: l.id, parentId: l.parent_id }));`)).toHaveLength(1);
    expect(handBuiltLocationFields("x", `<select value={locationType()} onChange={f}><For each={types}>{(t) => <option>{t}</option>}</For></select>`)).toEqual([]);
    expect(handBuiltLocationFields("x", `<LocationSelect value={location()} onChange={setLocation} rootLabel="None" />`)).toEqual([]);
  });

  it("are all LocationSelect", () => {
    const hits = sources(SRC)
      .map((p) => [relative(SRC, p), p] as const)
      .filter(([rel]) => !ALLOW.has(rel))
      .flatMap(([rel, p]) => handBuiltLocationFields(rel, readFileSync(p, "utf8")));
    expect(hits).toEqual([]);
  });
});
