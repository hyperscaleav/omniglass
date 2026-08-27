import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@solidjs/testing-library";
import Eyebrow from "./Eyebrow";

// The section-label primitive (#790): an operator page carries no inline
// explanatory prose, so every section label can carry its explainer as a
// tooltip instead. One component, so the rule has one implementation.
afterEach(cleanup);

describe("Eyebrow", () => {
  it("renders the label in the eyebrow style with no inline hint text", () => {
    render(() => <Eyebrow label="History" hint="One entry per change, never a sample." />);
    const label = screen.getByText("History");
    expect(label.className).toContain("eyebrow");
    // The explainer is NOT in the flow.
    expect(screen.queryByText(/One entry per change/)).toBeNull();
  });

  it("reveals the hint from the info trigger on keyboard focus, portalled to the body", async () => {
    const r = render(() => (
      <div style={{ overflow: "hidden" }}>
        <Eyebrow label="History" hint="One entry per change, never a sample." />
      </div>
    ));
    fireEvent.keyDown(document.body, { key: "Tab" });
    screen.getByRole("button", { name: /More about History/ }).focus();
    const tip = await screen.findByText(/One entry per change/);
    // Portalled: mounted outside the overflow wrapper, so a card can never clip it.
    expect(r.container.contains(tip)).toBe(false);
  });

  it("renders no trigger at all without a hint: the affordance never dangles", () => {
    render(() => <Eyebrow label="Vitals" />);
    expect(screen.getByText("Vitals")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
