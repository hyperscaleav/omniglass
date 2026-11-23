import { describe, it, expect } from "vitest";
import { distantHolders, groupDependents, dependencyChoices, refusalWords, upstreamHolds, kindLabel } from "./dependencies";
import type { Component } from "./components";
import { uuidFor } from "./testids";
import type { Reachability } from "./reachability";

const c = (id: string, name: string, label?: string, product?: string): Component => ({ id, name, label, product, system_count: 0 });
const fleet = [c("1", "switch-1", "Core switch"), c("2", "bar-a", undefined, "kestrel"), c("3", "pdu-1", "Rack PDU")];

describe("dependencyChoices", () => {
  it("never offers the component itself, by uuid or by name", () => {
    expect(dependencyChoices(fleet, "1", "").map((x) => x.name)).toEqual(["bar-a", "pdu-1"]);
    expect(dependencyChoices(fleet, "switch-1", "").map((x) => x.name)).toEqual(["bar-a", "pdu-1"]);
  });
  it("filters on label, name or product, ignoring case", () => {
    expect(dependencyChoices(fleet, "x", "PDU").map((x) => x.name)).toEqual(["pdu-1"]);
    expect(dependencyChoices(fleet, "x", "kestrel").map((x) => x.name)).toEqual(["bar-a"]);
    expect(dependencyChoices(fleet, "x", "zzz")).toEqual([]);
  });
});

describe("refusalWords", () => {
  it("keeps the server's reason on a refusal", () => {
    expect(refusalWords({ status: 422, detail: "would close a cycle" })).toBe("Refused: would close a cycle");
    expect(refusalWords({ status: 409, detail: "already depends" })).toBe("already depends");
    expect(refusalWords({ status: 403, detail: "out of scope" })).toBe("Not allowed: out of scope");
    expect(refusalWords({ status: 404 })).toMatch(/^Not found/);
  });
});

describe("upstreamHolds", () => {
  it("collects the sources of upstream gate reasons only", () => {
    const reach = {
      component: "x",
      endpoints: [
        { gate_reasons: [{ kind: "unreachable", source: "t", since: "" }, { kind: "upstream", source: "u1", since: "", component: "switch-1" }] },
        { gate_reasons: null },
      ],
    } as unknown as Reachability;
    expect([...upstreamHolds(reach)]).toEqual([["u1", "switch-1"]]);
    expect(upstreamHolds(undefined).size).toBe(0);
  });
});

it("kindLabel falls back to the raw kind", () => {
  expect(kindLabel("power")).toBe("Power");
  expect(kindLabel("x")).toBe("x");
});

describe("groupDependents", () => {
  it("folds a dependent's edges into one row with each kind, ordered by name", () => {
    const e = (id: string, name: string, kind: "host" | "path" | "power") =>
      ({ component: name, component_id: id, upstream: "bar", upstream_id: "b", kind, created_at: "" }) as never;
    const rows = groupDependents([e("p", "panel-1", "host"), e("d", "display-1", "path"), e("p", "panel-1", "path")]);
    expect(rows.map((r) => [r.name, r.kinds])).toEqual([["display-1", ["path"]], ["panel-1", ["host", "path"]]]);
  });
});

describe("distantHolders", () => {
  it("names a holder that is not a direct upstream (the root of a chain)", () => {
    const core = uuidFor("core");
    const sw = uuidFor("sw");
    const holds = new Map([[core, "core-switch"], [sw, "switch-1"]]);
    expect(distantHolders(holds, [{ upstream_id: sw }])).toEqual([{ id: core, name: "core-switch" }]);
    expect(distantHolders(new Map(), [])).toEqual([]);
  });
});
