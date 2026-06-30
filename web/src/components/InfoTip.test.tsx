import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import InfoTip from "./InfoTip";

describe("InfoTip", () => {
  it("renders an (i) trigger button labelled for its field", () => {
    const { getByRole } = render(() => <InfoTip text="A location_type id." label="Type" />);
    const btn = getByRole("button", { name: /more about Type/i }) as HTMLButtonElement;
    expect(btn.type).toBe("button"); // never submits the form
    expect(btn.querySelector("svg")).toBeTruthy(); // the (i) icon
  });

  it("reveals the help text on focus, portaled outside a clipping ancestor", async () => {
    const { getByRole, container } = render(() => (
      <div data-testid="clip" style={{ overflow: "hidden" }}>
        <InfoTip text="A location_type id." label="Type" />
      </div>
    ));
    const btn = getByRole("button", { name: /more about Type/i });
    fireEvent.keyDown(document.body, { key: "Tab" }); // Kobalte shows a tooltip on focus only under keyboard modality
    btn.focus();
    const tip = await screen.findByText("A location_type id.");
    const clip = container.querySelector('[data-testid="clip"]') as HTMLElement;
    expect(clip.contains(tip)).toBe(false); // tooltip portals out, never clipped by the drawer
  });
});
