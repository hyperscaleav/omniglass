import { describe, it, expect } from "vitest";
import { CATALOG_GROUPS, CATALOG_STUB_PATHS, visibleGroups } from "./catalog";
import { navByPath, routeTokens, STUBS } from "./nav";

const entry = (header: string, label: string) =>
  CATALOG_GROUPS.find((g) => g.header === header)!.entries.find((e) => e.label === label)!;

// The catalog group table is data; these pin the facts the shell and the
// Overview render from it, and the no-bare-TODO rule on its pending slots.
describe("the catalog table's soon slots", () => {
  it("names each pending registry's tracking issue in the table", () => {
    expect(entry("Components", "Templates").issue).toBe(615);
    expect(entry("Systems", "Templates").issue).toBe(616);
    expect(entry("Locations", "Templates").issue).toBe(617);
    expect(entry("Actions", "Notifications").issue).toBe(618);
    expect(entry("Actions", "Rules").issue).toBe(624);
  });

  it("mirrors nav.ts on every routed soon slot, so the stub page shows the number the table names", () => {
    // A routed soon slot renders SectionStub, which resolves its issue through
    // navByPath: the two tables must agree (Rules carries none in either).
    for (const g of CATALOG_GROUPS)
      for (const e of g.entries)
        if (e.soon && e.path) expect(navByPath[e.path]?.issue, e.path).toBe(e.issue);
  });

  it("registers every routed soon slot as a stub route", () => {
    for (const p of CATALOG_STUB_PATHS) expect(STUBS, `${p} routes to SectionStub`).toContain(p);
  });
});

describe("visibleGroups", () => {
  it("keeps the shell for a caller who may read nothing: soon slots and their groups survive", () => {
    const groups = visibleGroups(() => false);
    expect(groups.map((g) => g.header)).toEqual(["Actions", "Components", "Systems", "Locations"]);
    expect(groups.flatMap((g) => g.entries.map((e) => e.label))).toEqual([
      "Rules", "Notifications", "Templates", "Templates", "Templates",
    ]);
  });

  it("keeps every entry for a caller who may read everything", () => {
    const groups = visibleGroups(() => true);
    expect(groups.map((g) => g.header)).toEqual(CATALOG_GROUPS.map((g) => g.header));
    expect(groups.flatMap((g) => g.entries.length)).toEqual(CATALOG_GROUPS.map((g) => g.entries.length));
  });
});

// The gate mirror: every pathed entry's gate must equal what the route guard
// enforces for its URL. Without this, editing a catalog.ts gate desyncs the
// subrail and Overview from RouteGuard (an entry vanishes for a principal the
// route still admits, or renders for one it 403s), the exact two-surface drift
// the one-table design exists to prevent.
describe("catalog gates mirror the route guard", () => {
  it("agrees with routeTokens for every pathed entry", () => {
    for (const g of CATALOG_GROUPS)
      for (const e of g.entries) {
        if (!e.path) continue;
        expect(routeTokens(`/web${e.path}`), `${g.header} > ${e.label} (${e.path})`).toEqual(e.gate ?? null);
      }
  });
});

// The nesting pin: the shell's value is that these URLs render INSIDE
// CatalogShell. Route structure lives in JSX (index.tsx), which no runtime
// test reaches without mounting the whole app, so this pins it at the source
// level: every shell path must appear between the CatalogShell route's opening
// and closing tags. Crude by design; it fails loudly if a route migrates out.
describe("shell route nesting (source-level pin)", () => {
  it("keeps every catalog path inside the CatalogShell route block", async () => {
    const src = (await import("../index.tsx?raw")).default as string;
    const open = src.indexOf("<Route component={CatalogShell}>");
    const close = src.indexOf("</Route>", open);
    expect(open).toBeGreaterThan(-1);
    const block = src.slice(open, close);
    const paths = [
      "/catalog", "/metrics", "/properties", "/event-types", "/command-types",
      "/vendors", "/products", "/drivers", "/capabilities", "/standards",
      "/location-types", "/secret-types", "/tags",
    ];
    for (const p of paths) expect(block, `${p} escaped the shell block`).toContain(`path="${p}"`);
    expect(block).toContain("CATALOG_STUB_PATHS.map");
  });
});
