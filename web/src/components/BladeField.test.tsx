import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import BladeField from "./BladeField";
import { BladeEditContext, createEditSlot, type BladeEdit } from "../lib/blades";

// An edit slot already in edit mode, for the tests that assert the edit state.
function editingSlot(): BladeEdit {
  const slot = createEditSlot();
  slot.begin();
  return slot;
}

describe("BladeField", () => {
  describe("read state", () => {
    it("renders the value as a fact, with no control", () => {
      const { getByText, container } = render(() => (
        <BladeField label="Support phone" value={() => "+1 800 555 0100"} onInput={() => {}} />
      ));
      expect(getByText("Support phone").className).toContain("eyebrow");
      expect(getByText("+1 800 555 0100")).toBeTruthy();
      // The defect this component exists for: a blade nobody can edit rendered
      // five bordered input boxes, so it read as a disabled form.
      expect(container.querySelector("input")).toBeNull();
      expect(container.querySelector("textarea")).toBeNull();
      expect(container.querySelector(".input")).toBeNull();
    });

    it("renders the empty placeholder when the value is blank", () => {
      const { container } = render(() => (
        <BladeField label="Icon" value={() => ""} onInput={() => {}} />
      ));
      expect(container.textContent).toContain("\u2014");
    });

    it("renders a custom read node when one is given", () => {
      const { getByText, container } = render(() => (
        <BladeField label="Kind" read={<span class="badge">manufacturer</span>}>
          <select />
        </BladeField>
      ));
      expect(getByText("manufacturer").className).toContain("badge");
      expect(container.querySelector("select")).toBeNull();
    });
  });

  describe("edit state", () => {
    it("renders an input for a single-line field while the blade is editing", () => {
      const slot = editingSlot();
      const { container } = render(() => (
        <BladeEditContext.Provider value={slot}>
          <BladeField label="Support phone" value={() => "+1 800 555 0100"} onInput={() => {}} />
        </BladeEditContext.Provider>
      ));
      const input = container.querySelector("input")!;
      expect(input).toBeTruthy();
      expect(input.value).toBe("+1 800 555 0100");
      expect(container.querySelector("textarea")).toBeNull();
    });

    it("renders the caller's own control when children are given", () => {
      const slot = editingSlot();
      const { container } = render(() => (
        <BladeEditContext.Provider value={slot}>
          <BladeField label="Kind" read={<span>manufacturer</span>}>
            <select><option>manufacturer</option></select>
          </BladeField>
        </BladeEditContext.Provider>
      ));
      expect(container.querySelector("select")).toBeTruthy();
    });

    it("associates the label with the control it renders", () => {
      const slot = editingSlot();
      const { getByText, container } = render(() => (
        <BladeEditContext.Provider value={slot}>
          <BladeField label="Website" value={() => "https://example.com"} onInput={() => {}} />
        </BladeEditContext.Provider>
      ));
      const label = getByText("Website") as HTMLLabelElement;
      const input = container.querySelector("input")!;
      expect(label.tagName).toBe("LABEL");
      expect(label.getAttribute("for")).toBe(input.id);
      expect(input.id).toBeTruthy();
    });

    it("reports what the operator types", () => {
      const slot = editingSlot();
      let typed = "";
      const { container } = render(() => (
        <BladeEditContext.Provider value={slot}>
          <BladeField label="Icon" value={() => ""} onInput={(v) => (typed = v)} />
        </BladeEditContext.Provider>
      ));
      const input = container.querySelector("input")!;
      input.value = "crestron-logo";
      input.dispatchEvent(new Event("input", { bubbles: true }));
      expect(typed).toBe("crestron-logo");
    });
  });

  describe("free text", () => {
    // #573: a long description ran off the right edge of the blade in read mode
    // (the single-line `input` box cannot wrap) and scrolled horizontally in
    // edit mode (bound to an `input`). Both halves are fixed here, once.
    it("wraps its read value instead of running off the blade", () => {
      const { container } = render(() => (
        <BladeField label="Description" multiline value={() => "a".repeat(400)} onInput={() => {}} />
      ));
      const box = container.querySelector(".whitespace-pre-wrap")!;
      expect(box).toBeTruthy();
      expect(box.className).toContain("wrap-break-word");
    });

    it("preserves newlines in its read value", () => {
      const { container } = render(() => (
        <BladeField label="Description" multiline value={() => "first\nsecond"} onInput={() => {}} />
      ));
      const box = container.querySelector(".whitespace-pre-wrap")!;
      expect(box.textContent).toBe("first\nsecond");
    });

    it("edits in a textarea rather than a single-line input", () => {
      const slot = editingSlot();
      const { container } = render(() => (
        <BladeEditContext.Provider value={slot}>
          <BladeField label="Description" multiline value={() => "long"} onInput={() => {}} />
        </BladeEditContext.Provider>
      ));
      expect(container.querySelector("textarea")).toBeTruthy();
      expect(container.querySelector("input")).toBeNull();
    });
  });

  describe("the identity pairing", () => {
    // The rule that used to be a regex over source with an eight-line lookahead:
    // a field bound to display_name reads "Display name", one bound to name reads
    // "Name". It is now derived from the binding, so a caller cannot pair them
    // wrongly (and `label` is not accepted alongside `bind` at the type level).
    it("labels a display_name field 'Display name'", () => {
      const { getByText } = render(() => (
        <BladeField bind="display_name" value={() => "Crestron"} onInput={() => {}} />
      ));
      expect(getByText("Display name")).toBeTruthy();
    });

    it("labels a name field 'Name'", () => {
      const { getByText } = render(() => (
        <BladeField bind="name" value={() => "crestron"} onInput={() => {}} />
      ));
      expect(getByText("Name")).toBeTruthy();
    });

    it("rejects a label alongside a binding", () => {
      // @ts-expect-error a bound field takes its label from the binding
      const bad = () => <BladeField bind="name" label="Key" value={() => "x"} onInput={() => {}} />;
      expect(bad).toBeTruthy();
    });
  });

  describe("the edit slot", () => {
    it("renders read-only outside a BladeEditContext rather than throwing", () => {
      // Systems, Components, and Locations render one renderDetail both inside a
      // blade (in a provider) and on the full page (outside one). Outside, there
      // is nothing to edit with, and read-only is the correct answer.
      const { container, getByText } = render(() => (
        <BladeField bind="display_name" value={() => "Executive Boardroom"} onInput={() => {}} />
      ));
      expect(getByText("Executive Boardroom")).toBeTruthy();
      expect(container.querySelector("input")).toBeNull();
    });

    it("takes an explicit edit slot in preference to the context", () => {
      const slot = editingSlot();
      const { container } = render(() => (
        <BladeField bind="display_name" edit={slot} value={() => "Executive Boardroom"} onInput={() => {}} />
      ));
      expect(container.querySelector("input")).toBeTruthy();
    });

    it("follows the slot as it enters and leaves edit mode", () => {
      const slot = createEditSlot();
      const { container } = render(() => (
        <BladeEditContext.Provider value={slot}>
          <BladeField label="Icon" value={() => "crestron-logo"} onInput={() => {}} />
        </BladeEditContext.Provider>
      ));
      expect(container.querySelector("input")).toBeNull();
      slot.begin();
      expect(container.querySelector("input")).toBeTruthy();
      slot.cancel();
      expect(container.querySelector("input")).toBeNull();
    });
  });
});
