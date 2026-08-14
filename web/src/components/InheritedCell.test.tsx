import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import InheritedCell from "./InheritedCell";
import { EMPTY_VALUE } from "./BladeField";

// InheritedCell is the list-side half of the console's inheritance vocabulary
// (#743). Its whole job is to answer, per cell, which of three states a fact is
// in: stated here, taken from somewhere above, or absent everywhere. The three
// have to be told apart in a table cell with no room for a hint line and no
// per-row label to hang a mark beside.
describe("InheritedCell", () => {
  it("shows the row's own value, unmarked", () => {
    const { container, queryByRole } = render(() => (
      <InheritedCell label="Stem" own="carray" inherited={{ value: "from-above", from: "mic" }} />
    ));
    expect(container.textContent).toContain("carray");
    // The inherited value is what the cell would show if the row stated none, so
    // it must not leak into a cell whose row states one.
    expect(container.textContent).not.toContain("from-above");
    expect(queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  it("shows the value a stating-nothing row takes, marked with the ancestor it came from", () => {
    const { container, getByRole } = render(() => (
      <InheritedCell label="Stem" own={undefined} inherited={{ value: "from-above", from: "mic" }} />
    ));
    expect(container.textContent).toContain("from-above");
    expect(container.textContent).not.toContain(EMPTY_VALUE);
    // The mark states the whole fact in its accessible NAME, not only on hover:
    // a column of dots readable only with a mouse is a column of noise.
    expect(getByRole("button", { name: "Stem is inherited from mic" })).toBeTruthy();
  });

  // The em dash keeps its one meaning. Removing it outright would be #743's
  // defect pointed the other way, a cell claiming a value that does not exist.
  it("reads an em dash when the row states nothing and nothing above it does", () => {
    const { container, queryByRole } = render(() => (
      <InheritedCell label="Abbrev" own={undefined} inherited={{ value: "", from: "" }} />
    ));
    expect(container.textContent).toContain(EMPTY_VALUE);
    expect(queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  // An empty string is the wire's spelling of "this node declares no fact of its
  // own" (#716), so it has to read as inheriting exactly as an absent field does.
  it("treats an empty own value as stating nothing, the way the wire spells it", () => {
    const { getByRole } = render(() => (
      <InheritedCell label="Abbrev" own="" inherited={{ value: "from-above", from: "ceiling-mic" }} />
    ));
    expect(getByRole("button", { name: "Abbrev is inherited from ceiling-mic" })).toBeTruthy();
  });

  // The Icon column is the one fact the server answers twice: `resolved_icon` is
  // what the row SHOWS and `inherited_icon` is what an emptied box would take.
  // The cell renders the first and marks from the second.
  it("shows the caller's resolved text while marking from the inherited answer", () => {
    const { container, getByRole } = render(() => (
      <InheritedCell
        label="Icon"
        own={undefined}
        inherited={{ value: "inherited-answer", from: "av" }}
        shown="resolved-answer"
        leading={<svg data-testid="glyph" />}
      />
    ));
    expect(container.textContent).toContain("resolved-answer");
    expect(container.textContent).not.toContain("inherited-answer");
    expect(getByRole("button", { name: "Icon is inherited from av" })).toBeTruthy();
    expect(container.querySelector('[data-testid="glyph"]')).toBeTruthy();
  });

  // The console's DEFAULT glyph is not an inherited value: a chain that states
  // no icon anywhere resolves to "box", which came from nowhere and must carry
  // no mark pointing at a row that never supplied it.
  it("marks nothing when the caller's resolved text came from no ancestor", () => {
    const { container, queryByRole } = render(() => (
      <InheritedCell label="Icon" own={undefined} inherited={{ value: "", from: "" }} shown="box" />
    ));
    expect(container.textContent).toContain("box");
    expect(queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  // Every value in these columns is muted already, so the mark is the whole
  // distinction: an inherited value is not dimmed further, because `/40` is what
  // this console gives an ABSENT value and borrowing it would make "comes from
  // elsewhere" look like "nothing here".
  it("gives a stated and an inherited value the same muted treatment", () => {
    const stated = render(() => (
      <InheritedCell label="Stem" own="carray" inherited={{ value: "from-above", from: "mic" }} />
    ));
    const inherited = render(() => (
      <InheritedCell label="Stem" own={undefined} inherited={{ value: "from-above", from: "mic" }} />
    ));
    const classOf = (root: HTMLElement) =>
      (Array.from(root.querySelectorAll("span")).find((s) => s.className.includes("font-data")) as HTMLElement).className;
    expect(classOf(inherited.container)).toBe(classOf(stated.container));
    expect(classOf(stated.container)).toContain("text-base-content/60");
  });
});
