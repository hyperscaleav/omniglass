import { describe, it, expect } from "vitest";
import {
  buildIndex,
  pathOf,
  flattenRows,
  treeRows,
  parsePref,
  toggleItem,
  moveItem,
  allExpanded,
} from "./listmodel";
import type { FilterKey } from "./predicate";

// A node shape like the inventory pages build (id, display, children + a domain
// field). The place tree: hq > [b1 > f1 > [r1, r2]].
type N = { id: string; display: string; children: N[]; type: string };
const node = (id: string, display: string, type: string, children: N[] = []): N => ({ id, display, type, children });

function estate(): N[] {
  return [
    node("hq", "HQ", "campus", [
      node("b1", "Building 1", "building", [
        node("f1", "Floor 1", "floor", [node("r1", "Room A", "room"), node("r2", "Room B", "room")]),
      ]),
    ]),
  ];
}

const keys: FilterKey<N>[] = [
  { key: "name", type: "string", hint: "substring", get: (n) => n.display },
  { key: "type", type: "string", hint: "exact", get: (n) => n.type },
];
const sortVal = (n: N, key: string) => (key === "type" ? n.type : n.display.toLowerCase());

describe("buildIndex", () => {
  it("flattens the forest depth-first into the in-order node list", () => {
    const idx = buildIndex(estate());
    expect(idx.all.map((n) => n.id)).toEqual(["hq", "b1", "f1", "r1", "r2"]);
  });

  it("maps each child to its parent", () => {
    const idx = buildIndex(estate());
    expect(idx.parentOf.get("r1")?.id).toBe("f1");
    expect(idx.parentOf.get("f1")?.id).toBe("b1");
    expect(idx.parentOf.has("hq")).toBe(false);
  });

  it("records only the containers (nodes with children)", () => {
    const idx = buildIndex(estate());
    expect([...idx.containerIds].sort()).toEqual(["b1", "f1", "hq"]);
  });
});

describe("pathOf", () => {
  it("returns ancestors root-first", () => {
    const idx = buildIndex(estate());
    expect(pathOf(idx, idx.byId.get("r1")!).map((c) => c.id)).toEqual(["hq", "b1", "f1"]);
  });

  it("is empty for a root", () => {
    const idx = buildIndex(estate());
    expect(pathOf(idx, idx.byId.get("hq")!)).toEqual([]);
  });
});

describe("flattenRows", () => {
  it("with no sort keeps index (tree depth-first) order, not alphabetical", () => {
    const idx = buildIndex(estate());
    const rows = flattenRows(idx, keys, [], null, sortVal);
    expect(rows.map((r) => r.n.id)).toEqual(["hq", "b1", "f1", "r1", "r2"]);
  });

  it("a column sort overrides the default order", () => {
    const idx = buildIndex(estate());
    const rows = flattenRows(idx, keys, [], { key: "name", dir: 1 }, sortVal);
    expect(rows.map((r) => r.n.display)).toEqual(["Building 1", "Floor 1", "HQ", "Room A", "Room B"]);
    const desc = flattenRows(idx, keys, [], { key: "name", dir: -1 }, sortVal);
    expect(desc.map((r) => r.n.display)[0]).toBe("Room B");
  });

  it("filters by a chip and carries each row's ancestor path", () => {
    const idx = buildIndex(estate());
    const rows = flattenRows(idx, keys, [{ key: "type", op: "eq", values: ["room"] }], null, sortVal);
    expect(rows.map((r) => r.n.id)).toEqual(["r1", "r2"]);
    expect(rows[0].path?.map((c) => c.id)).toEqual(["hq", "b1", "f1"]);
  });

  // pathOf walks the PAGE'S OWN tree (parentOf, built from this same forest),
  // so it cannot cross the component-tree-to-location-tree boundary: a
  // component with no component parent sitting directly in a room has no
  // ancestor in the component forest at all, even though it plainly sits
  // "under" that room. pathRender is the server's own dotted-path render
  // (#627 Task 15, storage.PathOf on the Go side), fed straight through
  // rather than recomputed from local tree structure, so a row like that
  // still shows where it lives.
  it("carries a node's own pathRender through to the row, unlike the tree-local path", () => {
    type R = N & { pathRender?: string };
    const rootWithNoLocalAncestor: R[] = [
      { id: "c1", display: "Display 1", type: "component", children: [], pathRender: "boi-17c-216b-display-1" },
    ];
    const idx = buildIndex(rootWithNoLocalAncestor);
    const rows = flattenRows(idx, keys, [], null, sortVal);
    expect(rows[0].path).toEqual([]); // the tree-local walk finds no ancestor
    expect(rows[0].pathRender).toBe("boi-17c-216b-display-1"); // the server's render still does
  });

  it("omits pathRender when the node carries none", () => {
    const idx = buildIndex(estate());
    const rows = flattenRows(idx, keys, [], null, sortVal);
    expect(rows[0].pathRender).toBeUndefined();
  });
});

describe("treeRows", () => {
  it("shows only roots when nothing is expanded", () => {
    const rows = treeRows(estate(), new Set());
    expect(rows.map((r) => r.n.id)).toEqual(["hq"]);
  });

  it("descends only into expanded containers, with depth", () => {
    const rows = treeRows(estate(), new Set(["hq", "b1"]));
    expect(rows.map((r) => [r.n.id, r.depth])).toEqual([
      ["hq", 0],
      ["b1", 1],
      ["f1", 2],
    ]);
  });
});

describe("parsePref", () => {
  const valid = ["type", "parent", "tech"];
  it("returns null when absent so the caller uses its default", () => {
    expect(parsePref(null, valid)).toBeNull();
  });
  it("keeps only valid keys in stored order", () => {
    expect(parsePref(JSON.stringify(["tech", "type"]), valid)).toEqual(["tech", "type"]);
  });
  it("de-dupes a corrupt value, preserving first-seen order", () => {
    expect(parsePref(JSON.stringify(["type", "type", "parent"]), valid)).toEqual(["type", "parent"]);
  });
  it("honors an explicit empty array (operator hid everything)", () => {
    expect(parsePref(JSON.stringify([]), valid)).toEqual([]);
  });
  it("returns null on garbage or all-invalid keys", () => {
    expect(parsePref("not json", valid)).toBeNull();
    expect(parsePref(JSON.stringify(["nope"]), valid)).toBeNull();
    expect(parsePref(JSON.stringify({ a: 1 }), valid)).toBeNull();
  });
});

describe("toggleItem / moveItem / allExpanded", () => {
  it("toggleItem adds at the end and removes", () => {
    expect(toggleItem(["a", "b"], "c")).toEqual(["a", "b", "c"]);
    expect(toggleItem(["a", "b", "c"], "b")).toEqual(["a", "c"]);
  });
  it("moveItem reorders down and up", () => {
    expect(moveItem(["a", "b", "c"], 0, 1)).toEqual(["b", "a", "c"]);
    expect(moveItem(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"]);
  });
  it("allExpanded is true only when every container is open", () => {
    const c = new Set(["hq", "b1", "f1"]);
    expect(allExpanded(c, new Set(["hq", "b1"]))).toBe(false);
    expect(allExpanded(c, new Set(["hq", "b1", "f1"]))).toBe(true);
    expect(allExpanded(new Set(), new Set())).toBe(false);
  });
});
