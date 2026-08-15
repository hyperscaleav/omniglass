import { describe, it, expect } from "vitest";
import { ROUTE_MANIFEST } from "./lib/routemanifest";
import { PAGES } from "./routes";
import { STUBS } from "./lib/nav";
import { CATALOG_STUB_PATHS } from "./lib/catalog";

// The route manifest is the single source the router renders from (routes.tsx maps
// it into <Route> elements) and the e2e smoke spec drives from (web/e2e/routes.spec.ts
// imports the same array), so a route cannot exist without a smoke address. These
// pins hold the construction together: every entry resolves to a real page, the
// stub sets stay equal to the nav's own lists (the one place stubs are declared for
// the sidebar), and every entry carries a concrete address the spec can visit.
describe("route manifest", () => {
  it("resolves every entry to a registered page component", () => {
    for (const r of ROUTE_MANIFEST) {
      expect(PAGES[r.page], `${r.path} names page "${r.page}"`).toBeTypeOf("function");
    }
  });

  it("keeps its stub entries equal to the nav's stub lists", () => {
    const manifestStubs = ROUTE_MANIFEST.filter((r) => r.page === "SectionStub").map((r) => r.path).sort();
    const navStubs = [...STUBS].sort();
    expect(manifestStubs).toEqual(navStubs);
    // The catalog's routed soon slots mount inside the CatalogShell; everything
    // else stubs at the top level. The shells must agree with catalog.ts.
    for (const r of ROUTE_MANIFEST.filter((r) => r.page === "SectionStub")) {
      expect(r.shell, r.path).toBe(CATALOG_STUB_PATHS.includes(r.path) ? "catalog" : "protected");
    }
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

  it("keeps paths unique", () => {
    const paths = ROUTE_MANIFEST.map((r) => `${r.shell}:${r.path}`);
    expect(new Set(paths).size).toBe(paths.length);
  });
});
