import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { ENDPOINT_FIELDS } from "./endpoint_detail";

// The guard for #2436, in the style of fleetBlades.guard.test.ts: an endpoint
// has no page, so the component page's expanded endpoint row and the endpoint
// drawer must render ONE body. A host that grows markup of its own (the reduced
// glance the drawer had) has forked, and the two drift apart again.

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel: string) => readFileSync(join(here, rel), "utf8");

describe("an endpoint's row and drawer render one body", () => {
  it("the row mounts EndpointDetail in the row host", () => {
    expect(read("../components/ReachabilityPanel.tsx")).toMatch(/<EndpointDetail id=\{[^}]+\} host="row" \/>/);
  });
  it("the drawer mounts EndpointDetail in the blade host", () => {
    expect(read("../components/endpointBlades.tsx")).toMatch(/<EndpointDetail[\s\S]*?host="blade"/);
  });
  it("the body renders every field id, and only those", () => {
    const body = read("../components/EndpointDetail.tsx");
    const used = [...body.matchAll(/(?:data-field|field)="([a-z-]+)"/g)].map((m) => m[1]);
    expect([...new Set(used)].sort()).toEqual([...ENDPOINT_FIELDS].sort());
  });
  it("the body never branches a field on the host", () => {
    const body = read("../components/EndpointDetail.tsx");
    // The one host difference is the session notice the row already carries above itself.
    expect((body.match(/props\.host/g) ?? []).length).toBe(2);
  });
});
