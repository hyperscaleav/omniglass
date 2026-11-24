import { describe, it, expect } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import Tooltip from "./Tooltip";

// jsdom hover is unreliable; Kobalte opens a tooltip on focus under keyboard
// modality, so a Tab keydown first, then the focus (the kobalte skill).
const focusIn = (el: HTMLElement) => {
  fireEvent.keyDown(document.body, { key: "Tab" });
  el.focus();
};

describe("Tooltip keyboard contract", () => {
  it("is a focusable button that shows its help on focus, described by it, portalled", async () => {
    const { container } = render(() => (
      <div data-testid="clip" style={{ overflow: "hidden" }}>
        <Tooltip aria-label="More about Cadence" content="How often the outpost polls.">
          i
        </Tooltip>
      </div>
    ));
    const trigger = screen.getByRole("button", { name: "More about Cadence" });
    expect(trigger.getAttribute("type")).toBe("button");
    focusIn(trigger);
    const tip = await screen.findByRole("tooltip");
    expect(tip.textContent).toContain("How often the outpost polls.");
    expect(trigger.getAttribute("aria-describedby")).toBe(tip.id);
    expect(container.querySelector("[data-testid=clip]")!.contains(tip)).toBe(false);
  });

  it("dismisses on Esc and keeps the focus on the trigger", async () => {
    render(() => <Tooltip aria-label="Help" content="Body">i</Tooltip>);
    const trigger = screen.getByRole("button", { name: "Help" });
    focusIn(trigger);
    await screen.findByRole("tooltip");
    fireEvent.keyDown(trigger, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    expect(document.activeElement).toBe(trigger);
  });

  it("renders a non-button trigger with as, every other attribute on it", () => {
    render(() => (
      <Tooltip as="span" tabIndex={0} data-testid="chip" class="chip" content="Full list">
        three tags
      </Tooltip>
    ));
    const chip = screen.getByTestId("chip");
    expect(chip.tagName).toBe("SPAN");
    expect(chip.className).toContain("chip");
    expect(chip.tabIndex).toBe(0);
  });

  it("is held shut when controlled closed", async () => {
    render(() => <Tooltip open={false} aria-label="Held" content="Never">i</Tooltip>);
    focusIn(screen.getByRole("button", { name: "Held" }));
    await new Promise((r) => setTimeout(r, 250));
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
});
