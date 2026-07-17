import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import KVRow from "./KVRow";

describe("KVRow", () => {
  it("read mode is a slim inline value: no input, no bordered box, no join", () => {
    const { getByText, container } = render(() => (
      <KVRow label="Gain" value="-6 dB" input={<input data-testid="inp" />} editing={false} />
    ));
    expect(getByText("-6 dB")).toBeTruthy(); // value rendered inline
    expect(container.querySelector("input, textarea")).toBeNull(); // no editable control
    expect(container.querySelector(".join")).toBeNull(); // no join wrapper
    expect(container.querySelector(".input-bordered")).toBeNull(); // no bordered box
  });

  it("edit mode renders the input inside a join box, not the read value", () => {
    const { getByTestId, queryByText, container } = render(() => (
      <KVRow label="Gain" value="-6 dB" input={<input data-testid="inp" />} editing={true} />
    ));
    expect(getByTestId("inp")).toBeTruthy();
    expect(queryByText("-6 dB")).toBeNull(); // read value not shown in edit mode
    expect(container.querySelector(".join")).toBeTruthy(); // the bordered join appears only in edit
  });

  it("an override reads with weight and keeps its origin badge", () => {
    const { getByText, container } = render(() => (
      <KVRow label="Gain" value={<span>x</span>} origin="override" emphasize />
    ));
    expect(getByText("Gain").className).toContain("font-medium");
    const badge = container.querySelector(".badge");
    expect(badge?.textContent).toBe("override");
  });

  it("a default is unweighted and shows no origin badge", () => {
    const { getByText, container } = render(() => <KVRow label="Gain" value={<span>x</span>} origin="" />);
    expect(getByText("Gain").className).not.toContain("font-medium");
    expect(container.querySelector(".badge")).toBeNull();
  });

  it('suppresses the badge for an explicit "default" origin', () => {
    const { container } = render(() => <KVRow label="Gain" value={<span>x</span>} origin="default" />);
    expect(container.querySelector(".badge")).toBeNull();
  });

  it("renders the type badge when given and omits it when not", () => {
    const withBadge = render(() => <KVRow label="host" value={<span>x</span>} typeBadge="string" />);
    expect(withBadge.getByText("string").className).toContain("badge");
    withBadge.unmount();

    const noBadge = render(() => <KVRow label="host" value={<span>x</span>} />);
    expect(noBadge.container.querySelector(".badge")).toBeNull();
  });

  it("the drill-in chevron calls onDrillIn", () => {
    const onDrillIn = vi.fn();
    const { getByLabelText } = render(() => <KVRow label="Gain" value={<span>x</span>} onDrillIn={onDrillIn} />);
    fireEvent.click(getByLabelText("Show resolution"));
    expect(onDrillIn).toHaveBeenCalledTimes(1);
  });

  it("a read-mode drill-in row is whole-row clickable (a click on the label opens it)", () => {
    const onDrillIn = vi.fn();
    const { getByText } = render(() => <KVRow label="Gain" value={<span>x</span>} onDrillIn={onDrillIn} />);
    fireEvent.click(getByText("Gain")); // the label bubbles to the row
    expect(onDrillIn).toHaveBeenCalledTimes(1);
  });

  it("a click on an inline action does not open the drill-in", () => {
    const onDrillIn = vi.fn();
    const { getByLabelText } = render(() => (
      <KVRow label="Secret" value={<span>x</span>} onDrillIn={onDrillIn} actions={<button aria-label="reveal">o</button>} />
    ));
    fireEvent.click(getByLabelText("reveal")); // stopped, never bubbles to the row
    expect(onDrillIn).not.toHaveBeenCalled();
  });

  it("an edit-mode row is not whole-row clickable", () => {
    const onDrillIn = vi.fn();
    const { getByText } = render(() => (
      <KVRow label="Gain" value={<span>x</span>} input={<input />} editing onDrillIn={onDrillIn} />
    ));
    fireEvent.click(getByText("Gain")); // no row handler in edit mode
    expect(onDrillIn).not.toHaveBeenCalled();
  });
});
