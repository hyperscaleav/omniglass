import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import SimStatusBadge from "./SimStatusBadge";
import type { SimComponent } from "../lib/simulation";

// The status chip is HealthBadge's recipe: each state carries its word, its own
// glyph and its look, running stays grey, starting wears the incomplete look.
// The impairment is not a status: it is a second, warning chip beside it.
const sim = (state: SimComponent["state"], impairment?: SimComponent["impairment"], impaired = false) => ({ state, impairment, impaired });
const STATES = ["offline", "failed", "starting", "running"] as const;

describe("SimStatusBadge", () => {
  it("names each state in words, each with its own glyph", () => {
    const glyphs = new Set<string>();
    for (const s of STATES) {
      const { getByTestId, unmount } = render(() => <SimStatusBadge sim={sim(s)} />);
      const chip = getByTestId("sim-state");
      expect(chip.textContent).toBe(s);
      expect(chip.getAttribute("title")).toBeTruthy();
      glyphs.add(chip.querySelector("svg")!.innerHTML);
      unmount();
    }
    expect(glyphs.size).toBe(4); // offline and failed share a hue, never a glyph
  });

  it("draws offline and failed in the error hue, starting in the incomplete look, running grey", () => {
    const look = (s: (typeof STATES)[number]) => {
      const { getByTestId, unmount } = render(() => <SimStatusBadge sim={sim(s)} />);
      const cls = getByTestId("sim-state").className;
      unmount();
      return cls;
    };
    expect(look("offline")).toContain("badge-error");
    expect(look("failed")).toContain("badge-error");
    expect(look("starting")).toContain("badge-incomplete");
    expect(look("running")).toContain("badge-healthy");
    expect(look("running")).not.toMatch(/badge-(success|warning|error|incomplete)/);
  });

  it("explains a failed component in its hint", () => {
    const { getByTestId } = render(() => <SimStatusBadge sim={sim("failed")} />);
    expect(getByTestId("sim-state").getAttribute("title")).toMatch(/could not start it.*loopback/);
  });

  it("shows no impairment chip when none is set", () => {
    const { queryByTestId } = render(() => <SimStatusBadge sim={sim("running")} />);
    expect(queryByTestId("sim-impairment-chip")).toBeNull();
    expect(queryByTestId("sim-not-applied")).toBeNull();
  });

  it("puts an applied unplug beside running as a warning chip", () => {
    const { getByTestId, queryByTestId } = render(() => <SimStatusBadge sim={sim("running", { unplugged: true }, true)} />);
    expect(getByTestId("sim-state").textContent).toBe("running");
    const chip = getByTestId("sim-impairment-chip");
    expect(chip.textContent).toBe("unplugged");
    expect(chip.className).toContain("badge-warning");
    expect(queryByTestId("sim-not-applied")).toBeNull();
  });

  it("puts applied shaping beside running as its summary", () => {
    const { getByTestId } = render(() => <SimStatusBadge sim={sim("running", { latency_ms: 150, loss_pct: 2.5 }, true)} />);
    expect(getByTestId("sim-impairment-chip").textContent).toBe("150 ms, 2.5% loss");
    expect(getByTestId("sim-impairment-chip").className).toContain("badge-warning");
  });

  it("notes an impairment the simulator did not apply, and keeps the state", () => {
    const { getByTestId } = render(() => <SimStatusBadge sim={sim("running", { latency_ms: 150 }, false)} />);
    expect(getByTestId("sim-state").textContent).toBe("running");
    expect(getByTestId("sim-not-applied").textContent).toBe("impairment not applied");
  });

  it("keeps offline as the status when an impairment is set", () => {
    const { getByTestId } = render(() => <SimStatusBadge sim={sim("offline", { unplugged: true })} />);
    expect(getByTestId("sim-state").textContent).toBe("offline");
    expect(getByTestId("sim-state").className).toContain("badge-error");
  });
});
