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

  // A chip can be its own trigger (#1059: a ladder rung teaches itself on
  // hover), so the console keeps one tooltip primitive rather than a second
  // hand-rolled Kobalte tooltip per shape.
  it("takes its trigger's content as children, named by the label in full", async () => {
    const { getByRole } = render(() => (
      <InfoTip text="The login, tried only once the transport answers." label="authenticated: held" class="chip">
        <span>authenticated</span>
      </InfoTip>
    ));
    const btn = getByRole("button", { name: "authenticated: held" }) as HTMLButtonElement;
    expect(btn.type).toBe("button");
    expect(btn.classList.contains("chip")).toBe(true);
    expect(btn.querySelector("svg")).toBeNull(); // the chip, not the (i)
    fireEvent.keyDown(document.body, { key: "Tab" });
    btn.focus();
    expect(await screen.findByText("The login, tried only once the transport answers.")).toBeTruthy();
  });
});
