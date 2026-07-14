import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import TreeSelect from "./TreeSelect";
import type { TreeNode } from "../lib/treeselect";

const items: TreeNode[] = [
  { id: "ca", value: "campus-a", label: "Campus A", parentId: null, rank: 0 },
  { id: "b1", value: "bldg-1", label: "Bldg 1", parentId: "ca", rank: 1 },
];

describe("TreeSelect", () => {
  it("renders the optional root then the tree in order, indenting children", () => {
    const { container } = render(() => (
      <TreeSelect items={items} value="" onChange={() => {}} rootLabel="Root (no parent)" />
    ));
    const opts = [...container.querySelectorAll("option")];
    expect(opts.map((o) => o.value)).toEqual(["", "campus-a", "bldg-1"]);
    expect(opts[1].textContent).toBe("Campus A"); // a root, no indent
    expect(opts[2].textContent).toContain("Bldg 1");
    expect(opts[2].textContent!.startsWith(" ")).toBe(true); // a child, nbsp-indented
  });

  it("reports the chosen value on change", () => {
    let picked = "";
    const { container } = render(() => (
      <TreeSelect items={items} value="" onChange={(v) => (picked = v)} rootLabel="Root (no parent)" />
    ));
    const select = container.querySelector("select")!;
    fireEvent.change(select, { target: { value: "bldg-1" } });
    expect(picked).toBe("bldg-1");
  });

  it("forwards an id to the underlying select", () => {
    const { container } = render(() => (
      <TreeSelect items={items} value="" onChange={() => {}} id="reparent-select" />
    ));
    expect(container.querySelector("select")!.id).toBe("reparent-select");
  });
});
