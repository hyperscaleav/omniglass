import { describe, it, expect } from "vitest";
import { flattenTree, type TreeNode } from "./treeselect";

// Deliberately unsorted input: the flattener owns ordering and nesting.
const nodes: TreeNode[] = [
  { id: "f2", value: "floor-2", label: "Floor 2", parentId: "b1", rank: 2 },
  { id: "cb", value: "campus-b", label: "Campus B", parentId: null, rank: 0 },
  { id: "ca", value: "campus-a", label: "Campus A", parentId: null, rank: 0 },
  { id: "b2", value: "bldg-2", label: "Bldg 2", parentId: "ca", rank: 1 },
  { id: "b1", value: "bldg-1", label: "Bldg 1", parentId: "ca", rank: 1 },
];

describe("flattenTree", () => {
  it("emits DFS pre-order with each node's depth, siblings by rank then label", () => {
    expect(flattenTree(nodes).map((o) => [o.value, o.depth])).toEqual([
      ["campus-a", 0],
      ["bldg-1", 1],
      ["floor-2", 2],
      ["bldg-2", 1],
      ["campus-b", 0],
    ]);
  });

  it("treats a node whose parent is missing as a root", () => {
    const orphan: TreeNode[] = [
      { id: "x", value: "x", label: "X", parentId: "ghost" },
      { id: "y", value: "y", label: "Y", parentId: null },
    ];
    expect(flattenTree(orphan).map((o) => o.value).sort()).toEqual(["x", "y"]);
    expect(flattenTree(orphan).every((o) => o.depth === 0)).toBe(true);
  });

  it("excludes a node and its whole subtree when excludeSubtreeOf is set (reparent self-guard)", () => {
    expect(flattenTree(nodes, "ca").map((o) => o.value)).toEqual(["campus-b"]);
  });
});
