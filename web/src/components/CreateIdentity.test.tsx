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
  // The lock toggles sit on the label row, one per field, in field order.
  const toggles = () => screen.getAllByRole("button") as HTMLButtonElement[];
  return {
    ...r,
    name,
    display,
    namePen,
    displayPen,
    unlockName: () => fireEvent.click(toggles()[0]),
    unlockDisplay: () => fireEvent.click(toggles()[toggles().length - 1]),
  };
}

const MINT: NameMint = { stem: "display", bareFirst: false };

describe("CreateIdentity", () => {
  it("opens locked on the shape the platform will mint, with the ordinal as a token", () => {
    const { name } = mount(MINT);
    expect(name.value).toBe("display-n");
    expect(name.disabled).toBe(true);
    // And never a number, which is the value that cannot be known before the
    // row exists.
    expect(name.value).not.toContain("display-1");
  });

  it("opens locked on the label the server rendered, and says which rule rendered it", () => {
    const { display } = mount(MINT, { label: "Display n", rule: "{{.TypeName}} {{.Ordinal}}" });
    expect(display.value).toBe("Display n");
    expect(display.disabled).toBe(true);
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
    expect(name.disabled).toBe(false);
    expect(name.value).toBe("");
    expect(screen.getByText(/This product's type carries no stem/)).toBeTruthy();
    // Nothing to lock means nothing to unlock: the only toggle left is the
    // label's.
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("falls back to the name, marked as such, where no label rule resolves", () => {
    // The state every location create form in a shipped estate opens in: no
    // rule at any tier, so the platform stores nothing and the surface reads
    // the name. A locked field showing nothing at all would be worse than the
    // form this replaces.
    const { display } = mount(MINT, { label: "", rule: "" });
    expect(display.value).toBe("display-n");
    expect(display.disabled).toBe(true);
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
    expect(display.disabled).toBe(true);
    expect(display.value).toBe("north-wing");
  });

  it("hands the name to the operator when it is unlocked, and takes the value when typed", () => {
    const { name, unlockName, namePen } = mount(MINT);
    unlockName();
    expect(name.disabled).toBe(false);
    expect(name.value).toBe("");
    fireEvent.input(name, { target: { value: "front-mic" } });
    expect(namePen.value()).toBe("front-mic");
    expect(screen.getByText(/You are naming this yourself/)).toBeTruthy();
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
    expect(name.disabled).toBe(true);
    expect(name.value).toBe("display-n");
  });

  it("leaves the label locked and unchanged when the name is unlocked", () => {
    const { display, unlockName, displayPen } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    unlockName();
    expect(display.disabled).toBe(true);
    expect(display.value).toBe("Display n");
    expect(displayPen.value()).toBe("");
  });

  it("leaves the name locked and unchanged when the label is unlocked", () => {
    const { name, display, unlockDisplay, namePen } = mount(MINT, { label: "Display n", rule: "{{.TypeName}}" });
    unlockDisplay();
    expect(display.disabled).toBe(false);
    expect(name.disabled).toBe(true);
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
});
