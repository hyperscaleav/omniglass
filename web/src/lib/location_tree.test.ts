import { describe, it, expect } from "vitest";
import { locationTreeNodes } from "./location_tree";
import { flattenTree } from "./treeselect";
import type { Location } from "./locations";

const types = [
  { name: "campus", allowed_parent_types: [] as string[] },
  { name: "building", allowed_parent_types: ["campus"] },
  { name: "room", allowed_parent_types: ["building"] },
];
const loc = (id: string, name: string, location_type: string, parent_id?: string, extra: Partial<Location> = {}): Location =>
  ({ id, name, location_type, parent_id, ...extra }) as Location;

const rows: Location[] = [
  loc("r1", "zeta-room", "room", "b1", { label: "Zeta Room", path: "hq.b1.zeta-room" }),
  loc("b1", "b1", "building", "c1", { label: "Building 1", path: "hq.b1" }),
  loc("c1", "hq", "campus", undefined, { label: "HQ", path: "hq" }),
  loc("r0", "alpha-room", "room", "c1", { label: "Alpha Room", path: "hq.alpha-room" }),
];

describe("locationTreeNodes", () => {
  it("nests by parent and orders siblings by location type rank before label", () => {
    const flat = flattenTree(locationTreeNodes(rows, types));
    // Building 1 (rank 1) sorts before Alpha Room (rank 2) under HQ, though "A" < "B".
    expect(flat.map((o) => [o.label, o.depth])).toEqual([
      ["HQ", 0],
      ["Building 1", 1],
      ["Zeta Room", 2],
      ["Alpha Room", 1],
    ]);
  });

  it("commits the uuid by default", () => {
    expect(flattenTree(locationTreeNodes(rows, types)).map((o) => o.value)).toEqual(["c1", "b1", "r1", "r0"]);
  });

  it("commits whatever address the caller asks for", () => {
    const byName = flattenTree(locationTreeNodes(rows, types, { addressOf: (l) => l.name }));
    expect(byName.map((o) => o.value)).toEqual(["hq", "b1", "zeta-room", "alpha-room"]);
    const byPath = flattenTree(locationTreeNodes(rows, types, { addressOf: (l) => l.path || l.name }));
    expect(byPath.map((o) => o.value)).toEqual(["hq", "hq.b1", "hq.b1.zeta-room", "hq.alpha-room"]);
  });

  it("sorts a type the catalog cannot rank after the ranked ones", () => {
    const odd = [...rows, loc("x1", "annex", "mystery", "c1", { label: "Annex" })];
    const under = flattenTree(locationTreeNodes(odd, types)).filter((o) => o.depth === 1).map((o) => o.label);
    expect(under).toEqual(["Building 1", "Alpha Room", "Annex"]);
  });

  it("keeps an unpickable location in the tree, disabled, so its children stay nested under it", () => {
    const flat = flattenTree(locationTreeNodes(rows, types, { pickable: (l) => l.location_type !== "campus" }));
    expect(flat.map((o) => [o.label, o.depth, !!o.disabled])).toEqual([
      ["HQ", 0, true],
      ["Building 1", 1, false],
      ["Zeta Room", 2, false],
      ["Alpha Room", 1, false],
    ]);
  });

  it("labels a location by its name when it has no label", () => {
    expect(flattenTree(locationTreeNodes([loc("c9", "bare", "campus")], types))[0].label).toBe("bare");
  });
});
