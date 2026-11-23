import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { fleetRegistry } from "./fleetBlades";

// The guard for #2423: a fleet blade shows everything its detail page shows
// because it renders the page's own body. A kind that registers a blade body
// of its own (the reduced "glance" the console had before) has forked, and
// the two drift apart again; this test fails on the fork.

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel: string) => readFileSync(join(here, rel), "utf8");

const DETAIL: Record<string, { body: string; page: string }> = {
  component: { body: "ComponentDetail", page: "../pages/ComponentLeaf.tsx" },
  system: { body: "SystemDetail", page: "../pages/SystemZoom.tsx" },
  location: { body: "LocationDetail", page: "../pages/LocationZoom.tsx" },
};

describe("a fleet blade's body is its detail page's body", () => {
  for (const [kind, d] of Object.entries(DETAIL)) {
    it(`${kind}: the blade and the page both mount ${d.body}`, () => {
      const def = fleetRegistry[kind];
      expect(def, `${kind} is a fleet kind`).toBeTruthy();
      // The registry's blade for the kind is the EntityBlade def whose body
      // renders the detail body in the blade host, and nothing else.
      expect(read("fleetBlades.ts")).toMatch(new RegExp(`\\b${kind}: ${kind}Blade\\b`));
      expect(read("../components/EntityBlade.tsx")).toMatch(
        new RegExp(`export const ${kind}Blade: BladeDef = \\{[\\s\\S]*?Body: \\(p\\) => <${d.body} id=\\{p\\.id\\} host="blade" />,`),
      );
      expect(def.width).toBe("wide");
      // The page mounts the same body in the page host.
      expect(read(d.page)).toMatch(new RegExp(`<${d.body} id=\\{[^}]+\\} host="page" />`));
    });
  }

  it("the blade file defines no body of its own", () => {
    const src = read("../components/EntityBlade.tsx");
    expect(src).not.toMatch(/function \w+Body\(/);
  });
});
