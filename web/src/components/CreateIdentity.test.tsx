import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import CreateIdentity from "./CreateIdentity";
import { createPen, type NameMint } from "../lib/namegen";
import { type DraftLabel } from "../lib/labeldraft";

// The shared Identity section of the three estate create forms. What is asserted
// here is the CONTRACT the three pages share; each page's own test proves it is
// wired to this rather than to a copy of it.
//
// Both fields open LOCKED on the value the platform will use, and each unlocks
// on its own (#699). The two properties worth breaking a build over are that a
// locked field posts NOTHING (its pen holds no value at all, so the page's
// `value || undefined` omits it) and that unlocking one leaves the other
// exactly where it was.
//
// The lock is an INLINE ACTION inside the field, a square icon button in the
// field's join, matching the Settings row's "Restore to default" (#657). That
// makes `readonly` load-bearing rather than cosmetic: a DISABLED input fires no
// click, so click-to-override cannot work on one, and it is out of the tab
// order, so the locked value would have no keyboard path at all. Every
// assertion below that reads `readOnly` and `disabled` together is pinning that
// pair, not restating one fact twice.
function mount(mint: NameMint | null, label?: DraftLabel, bucket = "at HQ / Boardroom") {
  const namePen = createPen();
  const displayPen = createPen();
  const r = render(() => (
    <CreateIdentity
      kind="component"
      mint={() => mint}
      bucket={() => bucket}
      namePen={namePen}
      displayPen={displayPen}
      label={() => label}
      labelPending={() => false}
      namePlaceholder="mic-2"
      displayPlaceholder="Ceiling Mic 2"
    />
  ));
  const name = screen.getByPlaceholderText("mic-2") as HTMLInputElement;
  const display = screen.getByPlaceholderText("Ceiling Mic 2") as HTMLInputElement;
  // The lock actions sit in their field's join, one per field, in field order.
  const toggles = () => screen.getAllByRole("button") as HTMLButtonElement[];
  return {
    ...r,
    name,
    display,
    namePen,
    displayPen,
    toggles,
    unlockName: () => fireEvent.click(toggles()[0]),
    unlockDisplay: () => fireEvent.click(toggles()[toggles().length - 1]),
  };
}

const MINT: NameMint = { stem: "display", bareFirst: false };

// Everything an operator can reach with the Tab key, in document order. A
// locked field is readonly rather than disabled precisely so it stays in here.
function tabStops(): HTMLElement[] {
  return Array.from(document.querySelectorAll<HTMLElement>("input, button, select, textarea, a[href]")).filter(
    (el) => !(el as HTMLInputElement).disabled && el.tabIndex >= 0,
  );
}

describe("CreateIdentity", () => {
  it("opens locked on the shape the platform will mint, with the ordinal as a token", () => {
    const { name } = mount(MINT);
    expect(name.value).toBe("display-n");
    expect(name.readOnly).toBe(true);
    // NOT disabled: that is what keeps the value on the keyboard and lets the
    // field itself be clicked to take the pen.
    expect(name.disabled).toBe(false);
    // And never a number, which is the value that cannot be known before the
    // row exists.
    expect(name.value).not.toContain("display-1");
  });

  it("opens locked on the label the server rendered, and says which rule rendered it", () => {
    const { display } = mount(MINT, { label: "Display n", rule: "{{.TypeName}} {{.Ordinal}}" });
    expect(display.value).toBe("Display n");
    expect(display.readOnly).toBe(true);
    expect(display.disabled).toBe(false);
    expect(screen.getByText(/Rendered from \{\{\.TypeName\}\} \{\{\.Ordinal\}\}/)).toBeTruthy();
  });

  it("posts nothing from either locked field, so the platform keeps both pens", () => {
    const { namePen, displayPen } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    // The pens are what the page turns into the create body (`value ||
    // undefined`), and a locked one holds nothing at all. The shown text is a
    // rendering of the platform's answer, never a value staged for posting.
    expect(namePen.value()).toBe("");
    expect(displayPen.value()).toBe("");
  });

  it("shows the placement bucket the name has to be unique in, as text and not as a field", () => {
    const { name } = mount(MINT, undefined, "at HQ / West / Boardroom");
    const scope = screen.getByText(/Unique at HQ \/ West \/ Boardroom/);
    expect(scope).toBeTruthy();
    // Context, never part of the name: nothing put the path into the input.
    expect(name.value).toBe("display-n");
    expect(scope.querySelector("input")).toBeNull();
  });

  it("says a suppressed first ordinal reappears, so the shown value cannot silently differ", () => {
    const { name } = mount({ stem: "boardroom", bareFirst: true });
    expect(name.value).toBe("boardroom");
    expect(screen.getByText(/boardroom-2/)).toBeTruthy();
  });

  it("names the missing fact, and takes the lock away, when nothing will generate", () => {
    const { name } = mount(null);
    expect(name.readOnly).toBe(false);
    expect(name.value).toBe("");
    expect(screen.getByText(/This product's type carries no stem/)).toBeTruthy();
    // Nothing to lock means nothing to unlock: the only action left is the
    // label's.
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("falls back to the name, marked as such, where no label rule resolves", () => {
    // The state a location create reaches by clearing the rule at every tier: no
    // rule resolves, so the platform stores nothing and the surface reads the
    // name. A locked field showing nothing at all would be worse than the form
    // this replaces.
    const { display } = mount(MINT, { label: "", rule: "" });
    expect(display.value).toBe("display-n");
    expect(display.readOnly).toBe(true);
    expect(screen.getByText(/No label rule applies/)).toBeTruthy();
  });

  it("moves the fallback label with the name the operator types, where no rule resolves", () => {
    // The two states above, combined, which is the common location create: no
    // rule resolves, so the label IS the name, and the operator then names it
    // themselves. The locked label has to follow what they type or it promises
    // a label the created row will not land with. Held separately from the
    // locked-name fallback because the value comes from a different accessor
    // once the pen changes hands.
    const { name, display, unlockName } = mount(MINT, { label: "", rule: "" });
    expect(display.value).toBe("display-n");
    unlockName();
    fireEvent.input(name, { target: { value: "north-wing" } });
    expect(display.readOnly).toBe(true);
    expect(display.value).toBe("north-wing");
  });

  it("hands the name to the operator when it is unlocked, and takes the value when typed", () => {
    const { name, unlockName, namePen } = mount(MINT);
    unlockName();
    expect(name.readOnly).toBe(false);
    expect(name.value).toBe("");
    fireEvent.input(name, { target: { value: "front-mic" } });
    expect(namePen.value()).toBe("front-mic");
    expect(screen.getByText(/Named by you/)).toBeTruthy();
  });

  it("hands the name back, and clears it, when the lock closes again", () => {
    // The wire contract, structurally: re-locking cannot leave a value behind
    // for the create body to pick up, or the form would show a locked field
    // and post an override.
    const { name, unlockName, namePen } = mount(MINT);
    unlockName();
    fireEvent.input(name, { target: { value: "front-mic" } });
    unlockName();
    expect(namePen.value()).toBe("");
    expect(name.readOnly).toBe(true);
    expect(name.value).toBe("display-n");
  });

  it("leaves the label locked and unchanged when the name is unlocked", () => {
    const { display, unlockName, displayPen } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    unlockName();
    expect(display.readOnly).toBe(true);
    expect(display.value).toBe("Display n");
    expect(displayPen.value()).toBe("");
  });

  it("leaves the name locked and unchanged when the label is unlocked", () => {
    const { name, display, unlockDisplay, namePen } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    unlockDisplay();
    expect(display.readOnly).toBe(false);
    expect(name.readOnly).toBe(true);
    expect(name.value).toBe("display-n");
    expect(namePen.value()).toBe("");
  });

  it("never rewrites the name from the display name", () => {
    // The coupling lib/entities.ts's createIdentity owns for the registry
    // pages, which is exactly wrong here: a blank name is the request to
    // GENERATE one, so filling it from a typed label would claim the pen on the
    // operator's behalf the moment they typed a word.
    const { display, unlockDisplay, namePen, name } = mount(MINT);
    unlockDisplay();
    fireEvent.input(display, { target: { value: "Front Ceiling Mic" } });
    expect(namePen.value()).toBe("");
    expect(name.value).toBe("display-n");
  });

  it("leaves the display name alone when the name is overridden", () => {
    const { name, display, unlockName, unlockDisplay, displayPen } = mount(MINT);
    unlockDisplay();
    fireEvent.input(display, { target: { value: "Front Ceiling Mic" } });
    unlockName();
    fireEvent.input(name, { target: { value: "front-mic" } });
    expect(displayPen.value()).toBe("Front Ceiling Mic");
    expect(display.value).toBe("Front Ceiling Mic");
  });

  // # The affordance itself (#657)

  it("carries no text on either action, only an icon and a tooltip", () => {
    // The architect's constraint, and the reason the hints below have to stay
    // legible: the button no longer says what it does, so the tooltip and the
    // field's own copy carry it.
    const { toggles, unlockName } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    for (const b of toggles()) {
      expect(b.textContent).toBe("");
      expect(b.className).toContain("btn-square");
    }
    expect(toggles()[0].title).toBe("Override");
    // The accessible name says WHICH field, because two identically named
    // buttons in one section are two buttons a screen reader cannot tell apart.
    expect(toggles()[0].getAttribute("aria-label")).toBe("Override the name");
    unlockName();
    // Overridden reads as Settings' own restore action, the same idea and the
    // same words: handing a field back to the platform IS restoring its default.
    expect(toggles()[0].title).toBe("Restore to default");
    expect(toggles()[0].getAttribute("aria-label")).toBe("Restore the name to default");
  });

  it("puts each action inside its own field's join, not on the label row", () => {
    // "Inside the field boundary" is a daisyUI join, which is what makes the
    // input and the button read as one control. Asserted structurally because
    // the alternative (a button above the field) looks fine in a screenshot.
    const { name, display, toggles } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    const [nameBtn, displayBtn] = toggles();
    expect(nameBtn.closest(".join")).toBe(name.closest(".join"));
    expect(displayBtn.closest(".join")).toBe(display.closest(".join"));
    expect(nameBtn.closest(".join")).not.toBe(displayBtn.closest(".join"));
    expect(nameBtn.className).toContain("join-item");
    expect(name.className).toContain("join-item");
  });

  it("keeps the label pointing at the input once the field grew a join", () => {
    // The regression the join wrapper invites: a <label for> aimed at the join
    // DIV labels nothing at all, and the field silently loses its accessible
    // name. Page tests and the browser e2e both address these fields by label.
    const { name, display } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    const labels = Array.from(document.querySelectorAll("label"));
    expect(labels.map((l) => l.getAttribute("for"))).toEqual([name.id, display.id]);
    expect(name.id).toBeTruthy();
    expect(display.id).toBeTruthy();
  });

  it("keeps every state on the keyboard: both locked fields and both actions are tab stops", () => {
    // The acceptance this whole refactor turns on. `disabled` would have left
    // four controls as two, with the locked VALUES unreachable, and a hover-only
    // affordance would have left the override unreachable entirely.
    const { name, display, toggles } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    const stops = tabStops();
    expect(stops).toHaveLength(4);
    expect(stops[0]).toBe(name);
    expect(stops[1]).toBe(toggles()[0]);
    expect(stops[2]).toBe(display);
    expect(stops[3]).toBe(toggles()[1]);
  });

  it("keeps every state on the keyboard once a field is overridden", () => {
    // The other half: taking the pen must not cost a tab stop either, or the
    // way back to the platform would be reachable only by mouse.
    const { name, display, toggles, unlockName } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    unlockName();
    const stops = tabStops();
    expect(stops).toHaveLength(4);
    expect(stops[0]).toBe(name);
    expect(stops[1]).toBe(toggles()[0]);
    expect(stops[2]).toBe(display);
    expect(stops[3]).toBe(toggles()[1]);
  });

  it("takes the pen when the operator clicks the locked field", () => {
    // The accelerator on top of the button, and the reason `disabled` had to go:
    // a disabled input fires no click at all.
    const { name, namePen } = mount(MINT);
    fireEvent.click(name);
    expect(namePen.overridden()).toBe(true);
    expect(name.readOnly).toBe(false);
    expect(name.value).toBe("");
  });

  it("leaves the other field alone when a locked field is clicked", () => {
    const { name, display, displayPen } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    fireEvent.click(name);
    expect(display.readOnly).toBe(true);
    expect(display.value).toBe("Display n");
    expect(displayPen.value()).toBe("");
  });

  it("does not take the pen when focus merely passes through a locked field", () => {
    // Deliberately NOT the brief's "click or focus takes it over". A locked
    // field is a tab stop, so focus-to-override would mean tabbing from the
    // pickers to the Create button claimed BOTH pens and blanked both fields on
    // the way past, which is the exact state #699 exists to prevent. Clicking is
    // a deliberate act; passing through is not.
    const { name, display, namePen, displayPen } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    for (const el of tabStops()) el.focus();
    expect(namePen.overridden()).toBe(false);
    expect(displayPen.overridden()).toBe(false);
    expect(name.readOnly).toBe(true);
    expect(display.readOnly).toBe(true);
    expect(name.value).toBe("display-n");
    expect(display.value).toBe("Display n");
  });

  it("clicking an unlocked field changes nothing, so a second click cannot re-lock it", () => {
    // The click accelerator is one-way on purpose: the way back is the action
    // button, which is where "restore to default" reads as the deliberate act it
    // is. A toggling field would discard a typed name on a stray click.
    const { name, namePen } = mount(MINT);
    fireEvent.click(name);
    fireEvent.input(name, { target: { value: "front-mic" } });
    fireEvent.click(name);
    expect(namePen.value()).toBe("front-mic");
    expect(name.readOnly).toBe(false);
  });

  it("styles a locked field as locked without the attribute that would disable it", () => {
    // daisyUI 5 ships no `.input-disabled` class at all (only `:disabled` and
    // `[disabled]` selectors), so the locked look is app.css's `.input-locked`
    // and it has to travel with the readonly state or a locked field reads as an
    // ordinary empty-looking editable one.
    const { name, unlockName } = mount(MINT);
    expect(name.className).toContain("input-locked");
    unlockName();
    expect(name.className).not.toContain("input-locked");
  });
});
