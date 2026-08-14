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
// Either wording of the relation, so a query for "the sentence is gone" cannot
// pass by catching only the tense it was written in.
const anyRelation = /[Ii]nherit(ed|s) from/;
const baseHint = "The auto-generated component name's prefix.";

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

    // The edit-state twin of the assertion above, and the whole of #742's first
    // half. The hint used to append `Inherited from mic.` here, which is the
    // mark's sentence written out a second time in the same field at the same
    // moment: same fact, same instant, two tellings. The mark keeps it and the
    // hint drops back to describing the FACT, which is the only thing the mark
    // does not say.
    it("states the relation with the mark alone, not with a sentence beside it", () => {
      const { slot, ui } = setup();
      slot.begin();
      expect(ui.getByRole("button", { name: markName })).toBeTruthy();
      expect(ui.getByText(baseHint)).toBeTruthy();
      expect(ui.queryByText(anyRelation)).toBeNull();
    });
  });

  describe("the mark and the hint", () => {
    // The defect the spike photographed: the dot vanished on the keystroke while
    // the hint under the box still promised the inheritance in the present
    // tense. Both answers come from one predicate asked of one string (the
    // draft), so there is no moment at which they can disagree.
    // After #742 the two are not two tellings of one fact but a pair that takes
    // turns: exactly one of them is on screen in each state, and the predicate
    // that decides is the same one. The mark speaks while the box is empty; the
    // sentence speaks while the box is full, because that is the state with no
    // mark and no placeholder and therefore no other route back to inheriting.
    it("agree on the first keystroke, and again when the box is cleared", () => {
      const { slot, setDraft, ui } = setup();
      slot.begin();
      expect(ui.getByRole("button", { name: markName })).toBeTruthy();
      expect(ui.getByText(baseHint)).toBeTruthy();
      expect(ui.queryByText(anyRelation)).toBeNull();

      setDraft("c");
      expect(ui.queryByRole("button", { name: /is inherited from/ })).toBeNull();
      expect(ui.getByText(/prefix\. Empty inherits from mic\./)).toBeTruthy();

      setDraft("");
      expect(ui.getByRole("button", { name: markName })).toBeTruthy();
      expect(ui.getByText(baseHint)).toBeTruthy();
      expect(ui.queryByText(anyRelation)).toBeNull();
    });

    // KEPT deliberately by #742 while its present-tense twin was removed. This
    // sentence is not a second telling of the mark: there is no mark here (the
    // row states the value) and no placeholder either (the box is not empty), so
    // it is the ONLY thing on screen that says clearing the box returns the
    // field to inheriting, and the only thing that names what it would inherit.
    // Delete it and the UI offers no route back.
    it("says what a box with a value of its own would fall back to", () => {
      const { slot, ui } = setup({ value: "carray" });
      slot.begin();
      expect(ui.queryByRole("button", { name: /is inherited from/ })).toBeNull();
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
