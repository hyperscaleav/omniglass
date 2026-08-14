import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import ProvenanceDot from "./ProvenanceDot";

describe("ProvenanceDot", () => {
  // The whole fact lives in the accessible NAME, not only in the hover. A hover
  // has no keyboard and no touch equivalent, so a mark whose only statement is
  // the tooltip is unreadable for anyone not using a mouse.
  it("carries the field and the row the value came from in its accessible name", () => {
    const { getByRole } = render(() => <ProvenanceDot label="Stem" from="ceiling-mic" />);
    const btn = getByRole("button", { name: "Stem is inherited from ceiling-mic" }) as HTMLButtonElement;
    expect(btn.type).toBe("button"); // never submits the form it may sit in
  });

  it("is a tab stop, so the mark is reachable without a mouse", () => {
    const { getByRole } = render(() => <ProvenanceDot label="Icon" from="av" />);
    const btn = getByRole("button", { name: /inherited from av/ }) as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    expect(btn.tabIndex).toBe(0);
  });

  it("names the source on keyboard focus, portaled outside a clipping ancestor", async () => {
    const { getByRole, container } = render(() => (
      <div data-testid="clip" style={{ overflow: "hidden" }}>
        <ProvenanceDot label="Abbrev" from="ceiling-mic" />
      </div>
    ));
    const btn = getByRole("button", { name: /inherited from ceiling-mic/ });
    fireEvent.keyDown(document.body, { key: "Tab" }); // Kobalte opens on focus under keyboard modality
    btn.focus();
    const tip = await screen.findByText("ceiling-mic");
    const clip = container.querySelector('[data-testid="clip"]') as HTMLElement;
    expect(clip.contains(tip)).toBe(false); // portaled, so a blade's overflow cannot clip it
  });

  // Teal deliberately: the mark is emphasis, and the console's one teal for a
  // small glyph is `text-primary`, which app.css darkens on the light ground so
  // the bright brand fill never becomes an illegible 1.9:1 dot.
  it("draws the mark in the brand teal", () => {
    const { getByRole } = render(() => <ProvenanceDot label="Stem" from="mic" />);
    const btn = getByRole("button", { name: /inherited from mic/ });
    expect(btn.className).toContain("text-primary");
    expect((btn.firstElementChild as HTMLElement).className).toContain("bg-current");
  });
});
