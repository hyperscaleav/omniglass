import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import TreeSelect from "./TreeSelect";
import Drawer from "./Drawer";
import type { TreeNode } from "../lib/treeselect";

const items: TreeNode[] = [
  { id: "ca", value: "campus-a", label: "Campus A", parentId: null, rank: 0 },
  { id: "b1", value: "bldg-1", label: "Bldg 1", parentId: "ca", rank: 1 },
  { id: "f2", value: "floor-2", label: "Floor 2", parentId: "b1", rank: 0 },
  { id: "rm", value: "boardroom", label: "Boardroom", parentId: "f2", rank: 0 },
];

const trigger = () => screen.getByRole("button") as HTMLElement;
const open = () => fireEvent.keyDown(trigger(), { key: "Enter" });
const optionFor = (label: string) => screen.getByRole("option", { name: new RegExp(label) });

describe("TreeSelect", () => {
  it("shows a deep selection as its bare label when closed, with no indent or glue in the text", () => {
    render(() => <TreeSelect items={items} value="boardroom" onChange={() => {}} rootLabel="None" />);
    // The text content is the label and nothing else: the depth glyph and the
    // ancestor path are drawn by CSS, so they cannot leak into a copied value, a
    // test, or a screen reader's read of the label.
    expect(trigger().textContent).toBe("Boardroom");
    expect(trigger().querySelector("[data-path]")?.getAttribute("data-path")).toBe(
      "Campus A / Bldg 1 / Floor 2",
    );
  });

  it("indents each open item by its depth with inline padding, not text", async () => {
    render(() => <TreeSelect items={items} value="" onChange={() => {}} rootLabel="Root (no parent)" />);
    open();
    const pad = (label: string) => parseFloat(optionFor(label).style.paddingLeft || "0");
    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(5));
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
      "Root (no parent)",
      "Campus A",
      "Bldg 1",
      "Floor 2",
      "Boardroom",
    ]);
    expect(pad("Campus A")).toBeLessThan(pad("Bldg 1"));
    expect(pad("Bldg 1")).toBeLessThan(pad("Floor 2"));
    expect(pad("Floor 2")).toBeLessThan(pad("Boardroom"));
    expect(optionFor("Boardroom").style.paddingLeft).not.toBe("");
  });

  it("reports the chosen value, and the empty value for the root option", async () => {
    const picked: string[] = [];
    render(() => (
      <TreeSelect items={items} value="campus-a" onChange={(v) => picked.push(v)} rootLabel="Root (no parent)" />
    ));
    open();
    fireEvent.click(await screen.findByRole("option", { name: /Bldg 1/ }));
    expect(picked).toEqual(["bldg-1"]);
  });

  it("takes its value when the items land after it is rendered", () => {
    // Every caller feeds this from a query, so the picker is routinely rendered
    // with an empty tree and filled a tick later. The value it was given must
    // survive that, or a deep-linked edit face shows the root option in place of
    // the stored parent (#772). Driven by a signal, not a timer: the gap is the
    // subject of the test, so it is opened and closed by hand.
    const [tree, setTree] = createSignal<TreeNode[]>([]);
    render(() => <TreeSelect items={tree()} value="boardroom" onChange={() => {}} rootLabel="Root (no parent)" />);
    expect(trigger().textContent).not.toBe("Boardroom");

    setTree(items);

    expect(trigger().textContent).toBe("Boardroom");
  });

  it("follows a value changed from outside", () => {
    const [value, setValue] = createSignal("campus-a");
    render(() => <TreeSelect items={items} value={value()} onChange={() => {}} />);
    expect(trigger().textContent).toBe("Campus A");
    setValue("bldg-1");
    expect(trigger().textContent).toBe("Bldg 1");
  });

  it("forwards an id to the trigger, so an external label can point at it", () => {
    render(() => <TreeSelect items={items} value="" onChange={() => {}} id="reparent-select" />);
    expect(trigger().id).toBe("reparent-select");
  });

  it("omits the node and subtree named by excludeSubtreeOf", async () => {
    render(() => <TreeSelect items={items} value="" onChange={() => {}} excludeSubtreeOf="b1" />);
    open();
    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(1));
    expect(screen.getByRole("option").textContent).toBe("Campus A");
  });

  it("keeps its open list readable to assistive tech inside a drawer", async () => {
    // A drawer is a modal dialog, and a modal dialog marks every node added
    // outside it aria-hidden, a beat later. The list is portaled to the body, so
    // unless it claims the screen itself it lands outside the drawer and a screen
    // reader cannot reach a single option (the Secrets owner picker sits in one).
    render(() => (
      <Drawer open onClose={() => {}} title="New secret">
        <TreeSelect items={items} value="" onChange={() => {}} rootLabel="Choose…" />
      </Drawer>
    ));
    fireEvent.keyDown(screen.getByRole("button", { name: "Choose…" }), { key: "Enter" });
    await screen.findByRole("listbox");
    // The dialog hides on a timeout and then a frame; outlast both.
    await new Promise((r) => setTimeout(r, 100));
    expect(screen.getByRole("listbox")).toBeTruthy();
    expect(screen.getAllByRole("option")).toHaveLength(5);
  });

  describe("keyboard", () => {
    it.each([
      ["Enter", { key: "Enter" }],
      ["Space", { key: " " }],
      ["ArrowDown", { key: "ArrowDown" }],
    ])("opens on %s", async (_name, ev) => {
      render(() => <TreeSelect items={items} value="" onChange={() => {}} />);
      expect(screen.queryByRole("listbox")).toBeNull();
      fireEvent.keyDown(trigger(), ev);
      expect(await screen.findByRole("listbox")).toBeTruthy();
    });

    it("closes on Escape and returns focus to the trigger", async () => {
      render(() => <TreeSelect items={items} value="" onChange={() => {}} />);
      trigger().focus();
      open();
      const listbox = await screen.findByRole("listbox");
      fireEvent.keyDown(listbox, { key: "Escape" });
      await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
      // Kobalte hands focus back on a later frame, once the listbox is unmounted.
      await waitFor(() => expect(document.activeElement).toBe(trigger()));
    });

    it("jumps to a label by typing its first letters", async () => {
      const picked: string[] = [];
      render(() => <TreeSelect items={items} value="" onChange={(v) => picked.push(v)} />);
      trigger().focus();
      // Type-ahead on the closed trigger selects the matching label directly.
      fireEvent.keyDown(trigger(), { key: "B" });
      fireEvent.keyDown(trigger(), { key: "o" });
      fireEvent.keyDown(trigger(), { key: "a" });
      await waitFor(() => expect(picked).toContain("boardroom"));
    });
  });
});
