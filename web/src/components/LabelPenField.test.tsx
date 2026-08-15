import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import LabelPenField, { seedLabelPen } from "./LabelPenField";
import { createPen } from "../lib/namegen";
import { type Labelled } from "../lib/entities";

// The label pen on the EDIT BLADE (#693). The fact used to be a "Generated" chip
// in every list cell, which an operator could read and not act on; it is now the
// same lock the create form carries, beside the field it belongs to.
//
// What is asserted here is the field's own contract. Each page's test proves it
// is wired to this rather than to a copy of it, and proves the half no component
// test can see: what a Save actually posts.
function mount(e: Labelled, placeholder = "Executive Boardroom") {
  const pen = createPen();
  seedLabelPen(pen, e);
  const r = render(() => <LabelPenField pen={pen} entity={() => e} placeholder={placeholder} />);
  return {
    ...r,
    pen,
    input: screen.getByPlaceholderText(placeholder) as HTMLInputElement,
    toggle: () => screen.getByRole("button") as HTMLButtonElement,
  };
}

const platformLabelled: Labelled = { name: "boardroom-2", label: "Boardroom 2", label_generated: true };
const operatorLabelled: Labelled = { name: "boardroom-2", label: "The East Half", label_generated: false };
// The rule resolved nowhere, so the platform stores nothing and the surface
// reads the name. Every location in an fleet that predates a location name rule.
const noRule: Labelled = { name: "north-wing", label: "", label_generated: true };

describe("LabelPenField", () => {
  it("opens locked on the label the rule rendered, and says a rule edit rewrites it", () => {
    const { input } = mount(platformLabelled);
    expect(input.value).toBe("Boardroom 2");
    expect(input.readOnly).toBe(true);
    // NOT disabled: that is what keeps the value on the keyboard and lets the
    // field itself be clicked to take the pen (#657).
    expect(input.disabled).toBe(false);
    expect(input.className).toContain("input-locked");
    expect(screen.getByText(/Rendered from a label rule/)).toBeTruthy();
  });

  it("holds no value of its own while it is locked, so a save posts the platform's pen back", () => {
    // The wire contract, structurally. The blade posts pen.value(), and the API
    // reads an empty label as "the platform's": that is the whole of how
    // a locked field leaves the pen where it was.
    const { pen } = mount(platformLabelled);
    expect(pen.value()).toBe("");
  });

  it("opens editable on a label the operator typed, and says whose it is", () => {
    const { input, pen } = mount(operatorLabelled);
    expect(input.value).toBe("The East Half");
    expect(input.readOnly).toBe(false);
    expect(input.className).not.toContain("input-locked");
    expect(pen.value()).toBe("The East Half");
    expect(screen.getByText("Labelled by you.")).toBeTruthy();
  });

  it("shows the name, marked as standing in, where no rule rendered a label", () => {
    // A lock over an EMPTY field would be worse than no lock at all, so the
    // locked field falls back to the row's own name, in the same italic the
    // create form gives the same fact.
    const { input } = mount(noRule);
    expect(input.value).toBe("north-wing");
    expect(input.readOnly).toBe(true);
    expect(input.className).toContain("italic");
    expect(screen.getByText(/No label rule applies/)).toBeTruthy();
  });

  it("takes the pen when the lock is clicked, seeded with the label it was showing", () => {
    // The one place this diverges from the create form, and the reason: a blade
    // has a label on screen already, so taking the pen means amending it. An
    // empty box here would silently discard the label the operator meant to edit.
    const { input, toggle, pen } = mount(platformLabelled);
    expect(toggle().getAttribute("aria-label")).toBe("Override the label");
    fireEvent.click(toggle());
    expect(input.readOnly).toBe(false);
    expect(input.value).toBe("Boardroom 2");
    expect(pen.value()).toBe("Boardroom 2");
    expect(screen.getByText("Labelled by you.")).toBeTruthy();
  });

  it("takes the pen when the operator clicks the locked field itself", () => {
    // The accelerator, and the reason `readonly` rather than `disabled` is
    // load-bearing: a disabled input fires no click at all.
    const { input, pen } = mount(platformLabelled);
    fireEvent.click(input);
    expect(pen.overridden()).toBe(true);
    expect(input.readOnly).toBe(false);
  });

  it("does not re-lock a field the operator is typing in, however it is clicked", () => {
    // One-way on purpose: the way back discards typed words and belongs on the
    // button, where it reads as the deliberate act it is.
    const { input, pen } = mount(operatorLabelled);
    fireEvent.input(input, { target: { value: "The West Half" } });
    fireEvent.click(input);
    expect(pen.value()).toBe("The West Half");
    expect(input.readOnly).toBe(false);
  });

  it("hands the pen back on restore, and says the platform relabels on save", () => {
    // The state a create form does not have, and the only one showing a value
    // that is about to change. Nothing knows what the rule will render for an
    // EXISTING row (:renderLabel previews the next free ordinal, not this row's),
    // so the words carry it rather than a guessed value.
    const { input, toggle, pen } = mount(operatorLabelled);
    expect(toggle().getAttribute("aria-label")).toBe("Restore the label to default");
    fireEvent.click(toggle());
    expect(pen.value()).toBe("");
    expect(input.readOnly).toBe(true);
    expect(screen.getByText(/Handed back, so the platform renders this/)).toBeTruthy();
  });

  it("says what clearing the field does, rather than leaving it to be discovered", () => {
    const { input } = mount(operatorLabelled);
    fireEvent.input(input, { target: { value: "" } });
    expect(screen.getByText(/Empty hands the label back/)).toBeTruthy();
    expect(input.readOnly).toBe(false);
  });

  it("carries no text on its action, only an icon and a tooltip", () => {
    const { toggle } = mount(platformLabelled);
    expect(toggle().textContent).toBe("");
    expect(toggle().className).toContain("btn-square");
    expect(toggle().title).toBe("Override");
  });

  it("puts the action inside the field's join, not on the label row", () => {
    const { input, toggle } = mount(platformLabelled);
    expect(toggle().closest(".join")).toBe(input.closest(".join"));
    expect(input.className).toContain("join-item");
  });

  it("keeps the label pointing at the input once the field grew a join", () => {
    // The regression a join wrapper invites: a <label for> aimed at the join DIV
    // labels nothing, and the field silently loses its accessible name. Page
    // tests and the browser e2e both address this field by its label.
    const { input } = mount(platformLabelled);
    const label = document.querySelector("label")!;
    expect(label.getAttribute("for")).toBe(input.id);
    expect(input.id).toBeTruthy();
    expect(screen.getByLabelText("Label")).toBe(input);
  });

  it("keeps the locked value and its action on the keyboard", () => {
    // `disabled` would have left the locked value unreachable and dropped the
    // action out of the tab order with it.
    const { input, toggle } = mount(platformLabelled);
    const stops = Array.from(document.querySelectorAll<HTMLElement>("input, button")).filter(
      (el) => !(el as HTMLInputElement).disabled && el.tabIndex >= 0,
    );
    expect(stops).toEqual([input, toggle()]);
  });
});

describe("seedLabelPen", () => {
  it("opens a platform-labelled row locked and holding nothing", () => {
    const pen = createPen();
    seedLabelPen(pen, platformLabelled);
    expect(pen.overridden()).toBe(false);
    expect(pen.value()).toBe("");
  });

  it("opens an operator-labelled row overridden and holding their words", () => {
    const pen = createPen();
    seedLabelPen(pen, operatorLabelled);
    expect(pen.overridden()).toBe(true);
    expect(pen.value()).toBe("The East Half");
  });

  it("discards what a cancelled edit left behind", () => {
    // Cancel exits edit and the next begin re-seeds, which is the only thing
    // that reverts a blade. A seed that merged rather than replaced would carry
    // a discarded label into the next edit and post it.
    const pen = createPen();
    seedLabelPen(pen, operatorLabelled);
    pen.setValue("half typed");
    seedLabelPen(pen, platformLabelled);
    expect(pen.overridden()).toBe(false);
    expect(pen.value()).toBe("");
  });
});
