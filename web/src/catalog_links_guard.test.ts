import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// A catalog drawer links what it references (#2430). The render tests in
// pages/catalog_cross_links.test.tsx read the hrefs a drawer actually produces; this
// is the source half, and it fails in the two ways a page can quietly regress:
//
//   1. A catalog page drops the link for a reference it is declared to render, and
//      the handle goes back to bare text. The manifest below is the list of what
//      each page links, by the drawer kind the link opens. Adding a reference to a
//      drawer means adding it here, which is the moment to ask whether it is linked.
//   2. A page builds an `?open=` address by hand instead of through drawerHref, so
//      the next change to the address scheme breaks it silently. Only
//      lib/drawer_address.ts may spell the param.
const SRC = __dirname;

// page file -> the kinds its drawer links, each as <CatalogRef kind="..."> or
// <EntityLink kind="..."> in the page's own source, or through the shared "Used by"
// section (CatalogUsage) for the reverse lists.
const LINKED: Record<string, string[]> = {
  "pages/Products.tsx": ["vendor", "driver", "component_type"],
  "pages/ComponentTypes.tsx": ["component_type"],
  "pages/SystemTypes.tsx": ["system_type"],
  "pages/Standards.tsx": ["standard"],
  "pages/LocationTypes.tsx": ["location_type"],
  "pages/CommandTypes.tsx": ["property", "metric"],
  "pages/Rules.tsx": ["severity_level", "event_type", "component_type"],
  "pages/CalcRules.tsx": ["system_type", "component_type", "metric", "property"],
  "pages/ActionRules.tsx": ["notification_type", "notification_target"],
  "pages/NotificationTargets.tsx": ["secret", "group", "user", "role", "action_rule"],
  "components/SlotEditor.tsx": ["component_type", "product"],
};

// page file -> the shared Used by / Collected by component it mounts.
const USED_BY: Record<string, string> = {
  "pages/Products.tsx": "ProductUsedBy",
  "pages/Drivers.tsx": "DriverUsedBy",
  "pages/Vendors.tsx": "VendorUsedBy",
  "pages/ComponentTypes.tsx": "ComponentTypeUsedBy",
  "pages/Standards.tsx": "StandardUsedBy",
  "pages/Metrics.tsx": "KeyUsedBy",
  "pages/Properties.tsx": "KeyUsedBy",
  "pages/CommandTypes.tsx": "CommandTypeUsedBy",
  "pages/SeverityLevels.tsx": "SeverityUsedBy",
  "pages/Notifications.tsx": "NotificationTypeUsedBy",
};

const read = (rel: string) => readFileSync(join(SRC, rel), "utf8");

function walk(dir: string, out: string[] = []): string[] {
  for (const e of readdirSync(dir)) {
    const full = join(dir, e);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.tsx?$/.test(e) && !/\.test\.tsx?$/.test(e)) out.push(full);
  }
  return out;
}

describe("catalog drawers link what they reference", () => {
  for (const [file, kinds] of Object.entries(LINKED)) {
    it(`${file} links ${kinds.join(", ")}`, () => {
      const src = read(file);
      for (const kind of kinds) {
        const linked = new RegExp(`<(?:CatalogRef|EntityLink)\\b[^>]*\\bkind=(?:"${kind}"|\\{[^}]*"${kind}"[^}]*\\})`).test(src);
        expect(linked, `${file} renders a ${kind} reference without a link`).toBe(true);
      }
    });
  }

  for (const [file, component] of Object.entries(USED_BY)) {
    it(`${file} mounts ${component}`, () => {
      expect(read(file)).toContain(`<${component}`);
    });
  }

  it("no source file spells the drawer-address param by hand", () => {
    const offenders = walk(SRC)
      // drawer_address.ts owns the param; the route manifest's smoke path names one
      // address as data (a not-found drawer), which is a fixture and not a link.
      .filter((f) => !f.endsWith(join("lib", "drawer_address.ts")) && !f.endsWith(join("lib", "routemanifest.ts")))
      .filter((f) => /[?&]open=/.test(readFileSync(f, "utf8").split("\n").filter((l) => !l.trim().startsWith("//") && !l.trim().startsWith("*")).join("\n")));
    expect(offenders.map((f) => f.slice(SRC.length + 1))).toEqual([]);
  });
});
