import { describe, it, expect } from "vitest";
import type { PageDescriptor } from "../components/ListView";
import { componentsDescriptor } from "./Components";
import { systemsDescriptor } from "./Systems";
import { locationsDescriptor } from "./Locations";

// The page-config conformance matrix: the analogue of the backend's
// TestAuthzConformance. Every inventory page exports a PageDescriptor and is added
// to this registry; it then inherits the config-shape contract with no bespoke
// per-page test. A new page that, say, defaults a column it never declared, or
// reuses another page's storageKey, fails here.
const registry: { name: string; d: PageDescriptor }[] = [
  { name: "components", d: componentsDescriptor },
  { name: "systems", d: systemsDescriptor },
  { name: "locations", d: locationsDescriptor },
];

describe("page descriptor matrix", () => {
  for (const { name, d } of registry) {
    describe(name, () => {
      it("declares an entity (name + plural) and a storage key", () => {
        expect(d.entity.name).toBeTruthy();
        expect(d.entity.plural).toBeTruthy();
        expect(d.storageKey).toBeTruthy();
      });

      it("declares every column key, each with a label and a width", () => {
        expect(d.columnKeys.length).toBeGreaterThan(0);
        for (const k of d.columnKeys) {
          expect(d.columns[k], `column "${k}" is in columnKeys but not declared`).toBeDefined();
          expect(d.columns[k].label).toBeTruthy();
          expect(d.columns[k].width).toBeGreaterThan(0);
        }
      });

      it("declares no column that is not a known key", () => {
        for (const k of Object.keys(d.columns)) {
          expect(d.columnKeys, `column "${k}" is declared but not in columnKeys`).toContain(k);
        }
      });

      it("defaults only columns it declares, with no duplicates", () => {
        for (const k of d.defaultCols) {
          expect(d.columnKeys, `default column "${k}" is not a known key`).toContain(k);
        }
        expect(new Set(d.defaultCols).size).toBe(d.defaultCols.length);
      });
    });
  }

  it("every page uses a distinct storage key (preferences do not collide)", () => {
    const keys = registry.map((r) => r.d.storageKey);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("every page uses a distinct authz resource name", () => {
    const resources = registry.map((r) => r.d.entity.name);
    expect(new Set(resources).size).toBe(resources.length);
  });
});
