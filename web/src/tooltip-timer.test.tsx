import { render } from "@solidjs/testing-library";
import { describe, it, expect, type TestContext } from "vitest";
import Tooltip from "./components/ui/Tooltip";
import { tipOf } from "./test-tip";
import { pendingDelays, sweepTimeouts } from "./test-timers";

// Kobalte's tooltip keeps module-level timers (skip delay, warm-up, cool-down) on
// window.setTimeout. One still pending when a file ends fires after the file's
// window is torn down: "window is not defined", an unhandled error that fails
// the whole run (#2637). The setup sweeps pending timeouts as each test ends.

// Kobalte's skipDelayDuration default: the timer a closing tooltip starts, the
// one that fired after teardown in the sighting (globalSkipDelayTimeout).
const KOBALTE_SKIP_DELAY_MS = 300;

describe("a tooltip's timers", () => {
  let leftBehind = false;

  it("a closed tooltip leaves Kobalte's skip-delay timer pending, and the sweep clears it", async () => {
    const { getByRole } = render(() => <Tooltip content="help">open me</Tooltip>);
    expect(await tipOf(getByRole("button"))).toBe("help");
    expect(pendingDelays()).toContain(KOBALTE_SKIP_DELAY_MS);
    sweepTimeouts();
    expect(pendingDelays()).not.toContain(KOBALTE_SKIP_DELAY_MS);
    // Leave one behind for the next test to prove the setup's sweep removes it.
    const again = render(() => <Tooltip content="help again">open me too</Tooltip>);
    await tipOf(again.getByRole("button", { name: "open me too" }));
    expect(pendingDelays()).toContain(KOBALTE_SKIP_DELAY_MS);
    leftBehind = true;
  });

  // Depends on the test above having left a timer: skipped, never a vacuous
  // pass, when it did not run first (a filtered run).
  it("the setup's sweep removed it before this test started", (ctx: TestContext) => {
    if (!leftBehind) return ctx.skip();
    expect(pendingDelays()).not.toContain(KOBALTE_SKIP_DELAY_MS);
  });
});
