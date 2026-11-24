import { describe, it, expect } from "vitest";
import { ROUTE_MANIFEST } from "./lib/routemanifest";
import { PAGES } from "./routes";

// The route manifest is the single source the router renders from (routes.tsx maps
// it into <Route> elements) and the e2e smoke spec drives from (web/e2e/routes.spec.ts
// imports the same array), so a route cannot exist without a smoke address. These
// pins hold the construction together: every entry resolves to a real page, no
// entry is a stub (nothing in the console says "soon", #2445), and every entry carries a concrete address the spec can visit.
describe("route manifest", () => {
  it("resolves every entry to a registered page component", () => {
    for (const r of ROUTE_MANIFEST) {
      expect(PAGES[r.page], `${r.path} names page "${r.page}"`).toBeTypeOf("function");
    }
  });

  it("carries no stub page, and the retired stub addresses fall to the not-found route", () => {
    expect(ROUTE_MANIFEST.filter((r) => r.page === "SectionStub")).toEqual([]);
    const paths = ROUTE_MANIFEST.map((r) => r.path);
    for (const gone of ["/dashboards", "/learn", "/config", "/log-types"]) expect(paths).not.toContain(gone);
    expect(ROUTE_MANIFEST.find((r) => r.path === "*")).toMatchObject({ page: "NotFound" });
  });

  it("gives every route a concrete smoke address the e2e spec can visit", () => {
    for (const r of ROUTE_MANIFEST) {
      expect(r.smoke, r.path).toMatch(/^\//);
      // A parametrized pattern cannot be visited literally; its smoke address must
      // substitute something concrete (the well-formed-unknown uuid renders the
      // honest miss face, which is itself a render worth smoking).
      if (r.path.includes(":")) expect(r.smoke.includes(":")).toBe(false);
    }
  });

  it("carries the Platform health page, on the protected shell (#1644)", () => {
    const r = ROUTE_MANIFEST.find((e) => e.path === "/platform");
    expect(r).toMatchObject({ shell: "protected", page: "PlatformHealth", smoke: "/platform" });
  });

  it("smokes one drawer address, an ?open= naming no row (#2429)", () => {
    expect(ROUTE_MANIFEST.filter((r) => r.smoke.includes("?open="))).toHaveLength(1);
  });

  it("keeps paths unique", () => {
    const paths = ROUTE_MANIFEST.map((r) => `${r.shell}:${r.path}`);
    expect(new Set(paths).size).toBe(paths.length);
  });
});
