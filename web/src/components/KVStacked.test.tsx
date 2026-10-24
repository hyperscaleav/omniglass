import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import KVStacked from "./KVStacked";

describe("KVStacked", () => {
  it("renders an eyebrow label above the value", () => {
    const { getByText } = render(() => <KVStacked label="Type" value={<span>mic</span>} />);
    const label = getByText("Type");
    expect(label.className).toContain("eyebrow"); // the small-caps label
    expect(getByText("mic")).toBeTruthy(); // the value
  });

  it("wraps the value in a plain text-sm box (no mono) by default", () => {
    const { container } = render(() => <KVStacked label="Type" value="mic" />);
    const box = container.querySelector(".text-sm")!;
    expect(box.className).toContain("text-sm");
    expect(box.className).not.toContain("font-data"); // matches the legacy fact markup
  });

  it("renders the value font-data when mono is set", () => {
    const { container } = render(() => <KVStacked label="Name" value="og-1" mono />);
    expect(container.querySelector(".text-sm")!.className).toContain("font-data");
  });

  it("labels a bound fact from the identity map rather than from a prop", () => {
    // A fact carries the same two words a field does, so it derives them the
    // same way. Otherwise "Name" stays hand-typed on 20 surfaces and the rule
    // holds in only half the places it has to.
    const { getByText } = render(() => (
      <>
        <KVStacked bind="name" value="crestron" />
        <KVStacked bind="label" value="Crestron" />
      </>
    ));
    expect(getByText("Name")).toBeTruthy();
    expect(getByText("Label")).toBeTruthy();
  });

  it("renders the label even when no value is given", () => {
    const { getByText } = render(() => <KVStacked label="Parent" />);
    expect(getByText("Parent")).toBeTruthy();
  });

  // The read half of the provenance mark. It lands in the eyebrow, which is the
  // same slot FieldRow's label occupies, so one treatment covers both states and
  // the dot does not move when the pencil is clicked.
  it("marks a fact whose value comes from elsewhere, in the eyebrow beside the label", () => {
    const { getByText, getByRole } = render(() => (
      <KVStacked label="Stem" value="mic" provenance="ceiling-mic" />
    ));
    const eyebrow = getByText("Stem");
    const dot = getByRole("button", { name: /Stem is inherited from ceiling-mic/ });
    expect(eyebrow.className).toContain("eyebrow");
    expect(eyebrow.contains(dot)).toBe(true);
  });

  it("marks nothing when the fact is the row's own", () => {
    const { queryByRole } = render(() => <KVStacked label="Stem" value="carray" provenance="" />);
    expect(queryByRole("button")).toBeNull();
  });

  // A fact teaches what it is the way a field does: an (i) beside the label,
  // opening on keyboard focus, so a detail surface carries no explanatory
  // prose (a pinned key's fingerprint, #1078).
  it("teaches the fact through an (i) beside its label when given a hint", async () => {
    const { getByText, getByRole } = render(() => (
      <KVStacked label="Pinned key" value="SHA256:abc" hint="The key every connection must present." />
    ));
    const tip = getByRole("button", { name: "More about Pinned key" });
    expect(getByText("Pinned key").contains(tip)).toBe(true);
    fireEvent.keyDown(document.body, { key: "Tab" });
    tip.focus();
    expect(await screen.findByText("The key every connection must present.")).toBeTruthy();
  });

  it("draws no (i) without a hint", () => {
    const { queryByRole } = render(() => <KVStacked label="Pinned key" value="SHA256:abc" />);
    expect(queryByRole("button")).toBeNull();
  });
});
