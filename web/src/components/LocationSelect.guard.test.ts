import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// Every location field is LocationSelect (#2301). This scans the console source
// for the two ways a location field has been built by hand, so a new one fails
// here, naming the file and line, instead of shipping a flat list:
//
//   1. a native <select>, a raw <TreeSelect>, or a Kobalte <Select>/<Combobox>
//      whose markup mentions a location (its items, value, options or label), and
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
const MENTIONS_LOCATION = /location(?!_?type)/i;
const lineOf = (text: string, at: number) => text.slice(0, at).split("\n").length;

// Comments blanked (newlines kept, so line numbers hold): prose about a
// <select> is not one. A "//" right after a ":" (a URL) is left alone.
const uncommented = (src: string) =>
  src.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, " ")).replace(/(^|[^:])\/\/[^\n]*/g, "$1");

export function handBuiltLocationFields(file: string, raw: string): string[] {
  const text = uncommented(raw);
  const hits: string[] = [];
  // A native select, a raw TreeSelect, or a Kobalte Select / Combobox: each
  // element up to its own close (or self-close).
  for (const m of text.matchAll(/<(select|TreeSelect|Select|Combobox)\b[\s\S]*?(?:<\/\1>|\/>)/g)) {
    if (MENTIONS_LOCATION.test(m[0])) hits.push(`${file}:${lineOf(text, m.index!)}: location field built from ${m[1] === "select" ? "a native <select>" : `a raw <${m[1]}>`}`);
  }
  // Rows mapped to nodes, the map and its parentId possibly lines apart (one
  // statement: no semicolon between them).
  for (const m of text.matchAll(/locations?\b[^;]{0,400}?\.map\([^;]{0,400}?parentId/gi)) {
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
    // The mapping split over lines is still a hand-built tree.
    expect(handBuiltLocationFields("x", `const n = (locations.data ?? []).map((l) => ({\n  id: l.id,\n  parentId: l.parent_id,\n}));`)).toHaveLength(1);
    // A Kobalte Select or Combobox over locations is a hand-built field too.
    expect(handBuiltLocationFields("x", `<Select options={locations.data ?? []} value={location()}>\n</Select>`)).toHaveLength(1);
    expect(handBuiltLocationFields("x", `<Combobox options={locationRows()} />`)).toHaveLength(1);
    // Any spelling of the location TYPE is not a location.
    expect(handBuiltLocationFields("x", `<select value={location_type_id()}><option /></select>`)).toEqual([]);
    expect(handBuiltLocationFields("x", `<select value={locationTypeParent()}><option /></select>`)).toEqual([]);
    // Prose about a select is not one.
    expect(handBuiltLocationFields("x", `// a native <select> over locations\n/* <select value={location()}></select> */`)).toEqual([]);
  });

  it("are all LocationSelect", () => {
    const hits = sources(SRC)
      .map((p) => [relative(SRC, p), p] as const)
      .filter(([rel]) => !ALLOW.has(rel))
      .flatMap(([rel, p]) => handBuiltLocationFields(rel, readFileSync(p, "utf8")));
    expect(hits).toEqual([]);
  });
});
