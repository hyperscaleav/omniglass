import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { createSignal, type Accessor, type Setter } from "solid-js";
import InheritedField from "./InheritedField";
import { BladeEditContext, createEditSlot, type BladeEdit } from "../lib/blades";
import type { InheritedFact } from "../lib/catalog";

// A field wired the way a registry blade wires it: a persisted value, a draft
// the box binds to, and the server's answer to "clear this box and you get
// what?". `inherited` is a SIGNAL here rather than a constant, because the real
// one is derived from a TanStack query that hands back a fresh object on every
// refetch, and that churn is what detached the mark in the spike.
function setup(opts: { value?: string; inherited?: InheritedFact } = {}): {
  slot: BladeEdit;
  draft: Accessor<string>;
  setDraft: Setter<string>;
  setInherited: Setter<InheritedFact>;
  ui: ReturnType<typeof render>;
} {
  const slot = createEditSlot();
  const value = () => opts.value ?? "";
  const [draft, setDraft] = createSignal(opts.value ?? "");
  const [inherited, setInherited] = createSignal<InheritedFact>(
    opts.inherited ?? { value: "from-the-server", from: "mic" },
  );
  const ui = render(() => (
    <BladeEditContext.Provider value={slot}>
      <InheritedField
        label="Stem"
        hint="The auto-generated component name's prefix."
        value={value}
        draft={draft}
        onInput={setDraft}
        inherited={inherited}
      />
    </BladeEditContext.Provider>
  ));
  return { slot, draft, setDraft, setInherited, ui };
}

const markName = /Stem is inherited from mic/;

describe("InheritedField", () => {
  describe("the mark", () => {
    it("marks a fact that comes from elsewhere, in read and in edit alike", () => {
      const { slot, ui } = setup();
      expect(ui.getByRole("button", { name: markName })).toBeTruthy();
      slot.begin();
      expect(ui.getByRole("button", { name: markName })).toBeTruthy();
    });

    it("marks nothing on a fact the row states itself", () => {
      const { slot, ui } = setup({ value: "carray" });
      expect(ui.queryByRole("button", { name: /is inherited from/ })).toBeNull();
      slot.begin();
      expect(ui.queryByRole("button", { name: /is inherited from/ })).toBeNull();
    });

    it("marks nothing when nothing above states the fact", () => {
      const { slot, ui } = setup({ inherited: { value: "", from: "" } });
      expect(ui.queryByRole("button", { name: /is inherited from/ })).toBeNull();
      slot.begin();
      expect(ui.queryByRole("button", { name: /is inherited from/ })).toBeNull();
    });

    // The sentence under the value is gone: the mark and its hover carry the
    // relation now, and the value line carries the value.
    it("reads as the inherited value with no attribution sentence under it", () => {
      const { ui } = setup();
      expect(ui.container.textContent).toContain("from-the-server");
      expect(ui.container.textContent).not.toContain("inherited from");
      // Nor the em dash it replaced, written as an escape so the repo's em-dash
      // scan over changed files stays a plain grep.
      expect(ui.container.textContent).not.toContain("\u2014");
    });
  });

  describe("the mark and the hint", () => {
    // The defect the spike photographed: the dot vanished on the keystroke while
    // the hint under the box still promised the inheritance in the present
    // tense. Both answers come from one predicate asked of one string (the
    // draft), so there is no moment at which they can disagree.
    it("agree on the first keystroke, and again when the box is cleared", () => {
      const { slot, setDraft, ui } = setup();
      slot.begin();
      expect(ui.getByRole("button", { name: markName })).toBeTruthy();
      expect(ui.getByText(/prefix\. Inherited from mic\./)).toBeTruthy();

      setDraft("c");
      expect(ui.queryByRole("button", { name: /is inherited from/ })).toBeNull();
      expect(ui.queryByText(/Inherited from mic\./)).toBeNull();
      expect(ui.getByText(/prefix\. Empty inherits from mic\./)).toBeTruthy();

      setDraft("");
      expect(ui.getByRole("button", { name: markName })).toBeTruthy();
      expect(ui.getByText(/prefix\. Inherited from mic\./)).toBeTruthy();
    });

    it("says what a box with a value of its own would fall back to", () => {
      const { slot, ui } = setup({ value: "carray" });
      slot.begin();
      expect(ui.getByText(/prefix\. Empty inherits from mic\./)).toBeTruthy();
    });

    it("promises nothing on a row with nothing above it", () => {
      const { slot, ui } = setup({ inherited: { value: "", from: "" } });
      slot.begin();
      expect(ui.getByText(/prefix\. Nothing above this states one\./)).toBeTruthy();
      expect(ui.queryByText(/inherits from/)).toBeNull();
    });
  });

  // The trap spike 2 corrected spike 1 about: an element threaded through a
  // forwarding primitive is rebuilt whenever anything the prop getter reads
  // notifies, and the row this field's data is derived from is refetched with
  // equal contents in a new object. The mark is threaded as a STRING for
  // exactly this reason, so the mounted node survives the churn; a hover that
  // is halfway open is not interrupted, and a click does not land on a node
  // that has just been replaced.
  it("keeps the mounted mark through a refetch that hands back an equal row", () => {
    const { setInherited, ui } = setup();
    const dot = ui.getByRole("button", { name: markName });
    setInherited({ value: "from-the-server", from: "mic" });
    expect(ui.getByRole("button", { name: markName })).toBe(dot);
    expect(document.body.contains(dot)).toBe(true);
  });

  it("follows the source when the served answer actually changes", () => {
    const { setInherited, ui } = setup();
    setInherited({ value: "from-the-server", from: "ceiling-mic" });
    expect(ui.getByRole("button", { name: /Stem is inherited from ceiling-mic/ })).toBeTruthy();
  });

  it("still offers the inherited value as the placeholder", () => {
    const { slot, ui } = setup({ value: "carray" });
    slot.begin();
    expect((ui.container.querySelector("input") as HTMLInputElement).placeholder).toBe("from-the-server");
  });
});
