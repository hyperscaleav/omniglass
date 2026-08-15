import { describe, it, expect } from "vitest";
import { CATALOG_GROUPS, CATALOG_STUB_PATHS, OFFICIAL_LOCK, registryLock, registryOrigin, visibleGroups } from "./catalog";
import { type Me } from "./auth";
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
// CatalogShell. Since #760 the router renders from the route manifest, so the
// nesting is a DATA fact (shell: "catalog") plus one source-level seam: the
// manifest's catalog entries must be the ones mounted inside the CatalogShell
// route block in index.tsx. Both halves are pinned here.
describe("shell route nesting", () => {
  it("keeps every catalog path on the catalog shell in the manifest", async () => {
    const { ROUTE_MANIFEST } = await import("./routemanifest");
    const shellOf = new Map(ROUTE_MANIFEST.map((r) => [r.path, r.shell]));
    const paths = [
      "/catalog", "/metrics", "/properties", "/event-types", "/command-types",
      "/vendors", "/products", "/drivers", "/standards",
      "/location-types", "/secret-types", "/tags",
    ];
    for (const p of paths) expect(shellOf.get(p), `${p} escaped the catalog shell`).toBe("catalog");
    for (const p of CATALOG_STUB_PATHS) expect(shellOf.get(p), `${p} escaped the catalog shell`).toBe("catalog");
  });

  it("mounts the manifest's catalog entries inside the CatalogShell route block", async () => {
    const src = (await import("../index.tsx?raw")).default as string;
    expect(src).toContain('<Route component={CatalogShell}>{routesFor("catalog")}</Route>');
  });
});

// registryLock is the catalog blades' one read-only verdict, the value the edit
// slot's `locked` binding carries. Three arms: an official row returns the one
// string (both buttons greyed, everyone, owner included); a custom row returns a
// per-side object, each side null when the caller holds that verb and otherwise
// naming exactly the permission that would unlock THAT button (so a delete-only
// caller keeps a live Delete beside a greyed Edit, and a greyed Delete never
// claims update would unlock it); both verbs held collapses to null (no lock).
describe("registryLock", () => {
  const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
  const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "v" }, permissions: ["*:read"], grants: [] };
  const deleteOnly: Me = { principal: { id: "u-del", kind: "human" }, human: { username: "d" }, permissions: ["*:read", "vendor:delete"], grants: [] };
  const updateOnly: Me = { principal: { id: "u-upd", kind: "human" }, human: { username: "u" }, permissions: ["*:read", "vendor:update"], grants: [] };

  it("locks an official row with the official sentence, for the owner too", () => {
    expect(registryLock({ official: true }, owner, "vendor")).toBe(OFFICIAL_LOCK);
    expect(registryLock({ official: true }, viewer, "vendor")).toBe(OFFICIAL_LOCK);
  });

  it("locks both sides for a caller holding neither verb, each side naming its own permission", () => {
    expect(registryLock({ official: false }, viewer, "vendor")).toEqual({
      edit: "Requires vendor:update",
      delete: "Requires vendor:delete",
    });
  });

  it("greys only Edit for a delete-only caller, keeping Delete live", () => {
    expect(registryLock({ official: false }, deleteOnly, "vendor")).toEqual({
      edit: "Requires vendor:update",
      delete: null,
    });
  });

  it("greys only Delete for an update-only caller, keeping Edit live", () => {
    expect(registryLock({ official: false }, updateOnly, "vendor")).toEqual({
      edit: null,
      delete: "Requires vendor:delete",
    });
  });

  it("returns null (no lock) for a caller holding both verbs", () => {
    expect(registryLock({ official: false }, owner, "vendor")).toBeNull();
  });

  it("locks nothing while the row has not resolved", () => {
    expect(registryLock(undefined, viewer, "vendor")).toBeNull();
  });

  // A registry that has adopted the fork (#655, ADR-0095) opts in with
  // forkable. Edit stops being flatly locked on a shipped row, because an edit
  // there forks rather than writes, and is judged on update like any other row.
  // Delete keeps the official sentence while the row is pristine (a shipped row
  // is never deleted and there is nothing yet to discard) and unlocks once the
  // row is forked, where the page puts Restore in that slot.
  it("opens Edit on a shipped row for a fork-adopting registry, judged on update", () => {
    expect(registryLock({ official: true }, owner, "component_type", { forkable: true }))
      .toEqual({ edit: null, delete: OFFICIAL_LOCK });
    expect(registryLock({ official: true }, viewer, "component_type", { forkable: true }))
      .toEqual({ edit: "Requires component_type:update", delete: OFFICIAL_LOCK });
  });

  it("unlocks the destructive slot on a forked shipped row, for Restore", () => {
    expect(registryLock({ official: true, forked: true }, owner, "component_type", { forkable: true }))
      .toEqual({ edit: null, delete: null });
  });

  it("leaves a registry that has not adopted the fork flatly locked, forked flag or not", () => {
    expect(registryLock({ official: true, forked: true }, owner, "vendor")).toBe(OFFICIAL_LOCK);
  });

  // The three-state origin a fork-adopting registry shows.
  it("names the origin from the two wire booleans", () => {
    expect(registryOrigin({ official: true, forked: false })).toBe("shipped");
    expect(registryOrigin({ official: true, forked: true })).toBe("overridden");
    expect(registryOrigin({ official: false, forked: false })).toBe("yours");
  });

  it("treats a row without an official flag (a tag) on the permission arm alone", () => {
    expect(registryLock({}, viewer, "tag")).toEqual({
      edit: "Requires tag:update",
      delete: "Requires tag:delete",
    });
    expect(registryLock({}, owner, "tag")).toBeNull();
  });
});
