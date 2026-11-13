import { afterEach, describe, expect, it, vi } from "vitest";
import { createLeaveGuard } from "./leave_guard";

afterEach(() => vi.restoreAllMocks());

const slotOf = (dirty: () => boolean) => ({ dirty });

describe("leave guard", () => {
  it("lets the operator leave without asking when nothing is dirty", () => {
    const ask = vi.spyOn(window, "confirm");
    const g = createLeaveGuard();
    g.track(slotOf(() => false));
    expect(g.confirmLeave()).toBe(true);
    expect(ask).not.toHaveBeenCalled();
  });

  it("asks once when any tracked slot is dirty, and follows the answer", () => {
    const ask = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    const g = createLeaveGuard();
    g.track(slotOf(() => false));
    g.track(slotOf(() => true));
    g.track(slotOf(() => true));
    expect(g.confirmLeave()).toBe(false);
    expect(g.confirmLeave()).toBe(true);
    expect(ask).toHaveBeenCalledTimes(2);
  });

  it("stops asking about a slot once it is untracked", () => {
    const ask = vi.spyOn(window, "confirm").mockReturnValue(false);
    const g = createLeaveGuard();
    const untrack = g.track(slotOf(() => true));
    untrack();
    expect(g.confirmLeave()).toBe(true);
    expect(ask).not.toHaveBeenCalled();
  });
});
