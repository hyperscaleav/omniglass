import { render } from "@solidjs/testing-library";
import { describe, it, expect } from "vitest";
import Tooltip from "./components/ui/Tooltip";
import { tipOf } from "./test-tip";
import { pendingTimeouts } from "./test-timers";

// Kobalte's tooltip keeps module-level timers (skip delay, warm-up, cool-down) on
// window.setTimeout. One still pending when a file ends fires after the file's
// window is torn down: "window is not defined", an unhandled error that fails
// the whole run (#2637). The setup sweeps pending timeouts as each test ends.
describe("a tooltip's timers", () => {
  it("are pending right after a tooltip opens and closes", async () => {
    const { getByRole } = render(() => <Tooltip content="help">open me</Tooltip>);
    expect(await tipOf(getByRole("button"))).toBe("help");
    expect(pendingTimeouts()).toBeGreaterThan(0);
  });

  it("do not outlive the test that made them", () => {
    expect(pendingTimeouts()).toBe(0);
  });
});
