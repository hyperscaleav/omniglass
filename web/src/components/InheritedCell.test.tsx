import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import InheritedCell from "./InheritedCell";
import { EMPTY_VALUE } from "./BladeField";

// InheritedCell is the list-side render of a fact that inherits (#743). Its job
// is one fallback chain, in one place instead of at six cells across two
// registries: the row's own value, else what the server resolved for it, else
// the em dash.
//
// It says NOTHING about where the value came from, and every assertion about a
// mark below is an assertion of ABSENCE. That is the ruling, not an omission: a
// table is for scanning values, and the blade is where a value's origin is
// explained, so the provenance mark is a blade and detail affordance only.
describe("InheritedCell", () => {
  it("shows the row's own value", () => {
    const { container, queryByRole } = render(() => (
      <InheritedCell own="carray" resolved="from-above" />
    ));
    expect(container.textContent).toContain("carray");
    // The resolved value is what the cell would show if the row stated none, so
    // it must not leak into a cell whose row states one.
    expect(container.textContent).not.toContain("from-above");
    expect(queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  it("shows the value a stating-nothing row takes, rather than an em dash", () => {
    const { container, queryByRole } = render(() => (
      <InheritedCell own={undefined} resolved="from-above" />
    ));
    expect(container.textContent).toContain("from-above");
    expect(container.textContent).not.toContain(EMPTY_VALUE);
    // No mark, and no hover affordance of any kind: this is the whole of the
    // departure from the blade, which marks the same fact and names its
    // ancestor.
    expect(queryByRole("button", { name: /is inherited from/ })).toBeNull();
  });

  // The em dash keeps its one meaning. Removing it outright would be #743's
  // defect pointed the other way, a cell asserting a value that does not exist.
  it("reads an em dash when the row states nothing and nothing above it does", () => {
    const { container } = render(() => <InheritedCell own={undefined} resolved="" />);
    expect(container.textContent).toContain(EMPTY_VALUE);
  });

  // An empty string is the wire's spelling of "this node declares no fact of its
  // own" (#716), so it has to fall back exactly as an absent field does.
  it("treats an empty own value as stating nothing, the way the wire spells it", () => {
    const { container } = render(() => <InheritedCell own="" resolved="from-above" />);
    expect(container.textContent).toContain("from-above");
  });

  // The Icon column passes `resolved_icon` (what the row SHOWS) rather than
  // `inherited_icon` (what an emptied box would take), and renders its glyph
  // ahead of the text.
  it("shows the caller's resolved text behind an optional leading glyph", () => {
    const { container } = render(() => (
      <InheritedCell own={undefined} resolved="resolved-answer" leading={<svg data-testid="glyph" />} />
    ));
    expect(container.textContent).toContain("resolved-answer");
    expect(container.querySelector('[data-testid="glyph"]')).toBeTruthy();
  });

  // THE rule this component exists to hold, after the ruling that kept the mark
  // out of a table: a stated value and an inherited one are rendered
  // identically. It is not a consequence of dropping the mark, it is the
  // treatment, so it is asserted rather than left to be noticed.
  it("renders a stated and an inherited value identically", () => {
    const stated = render(() => <InheritedCell own="carray" resolved="from-above" />);
    const inherited = render(() => <InheritedCell own={undefined} resolved="from-above" />);
    const cellOf = (root: HTMLElement) =>
      Array.from(root.querySelectorAll("span")).find((s) => s.className.includes("font-data")) as HTMLElement;
    expect(cellOf(inherited.container).className).toBe(cellOf(stated.container).className);
    expect(cellOf(stated.container).className).toContain("text-base-content/60");
    // Nothing else separates them either: no mark on one and not the other, and
    // no extra node on either.
    expect(inherited.container.querySelectorAll("button")).toHaveLength(0);
    expect(stated.container.querySelectorAll("button")).toHaveLength(0);
  });
});
