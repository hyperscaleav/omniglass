import { describe, expect, it } from "vitest";
import { fireEvent, screen, within } from "@solidjs/testing-library";
import { createLeaveGuard, LEAVE_PROMPT } from "./leave_guard";

const slotOf = (dirty: () => boolean) => ({ dirty });

// The guard asks in the one confirm dialog (#2463), never the browser's.
async function answer(button: "Discard changes" | "Cancel") {
  const d = await screen.findByRole("alertdialog", { name: LEAVE_PROMPT });
  fireEvent.click(within(d).getByRole("button", { name: button }));
}

describe("leave guard", () => {
  it("lets the operator leave without asking when nothing is dirty", async () => {
    const g = createLeaveGuard();
    g.track(slotOf(() => false));
    expect(g.confirmLeave()).toBe(true);
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("asks once when any tracked slot is dirty, and follows the answer", async () => {
    const g = createLeaveGuard();
    g.track(slotOf(() => false));
    g.track(slotOf(() => true));
    g.track(slotOf(() => true));
    const first = g.confirmLeave();
    await answer("Cancel");
    await expect(first).resolves.toBe(false);
    const second = g.confirmLeave();
    await answer("Discard changes");
    await expect(second).resolves.toBe(true);
  });

  it("stops asking about a slot once it is untracked", async () => {
    const g = createLeaveGuard();
    const untrack = g.track(slotOf(() => true));
    untrack();
    expect(g.confirmLeave()).toBe(true);
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });
});
