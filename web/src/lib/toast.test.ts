import { describe, it, expect, vi, afterEach } from "vitest";
import { MAX_TOASTS, clearToasts, dismissToast, pushToast, toasts } from "./toast";

// The toast store (#1155): any surface raises a notice, the shell's Toaster
// draws it, and it goes away by itself or when dismissed; a burst keeps the
// newest few.
describe("the toast store", () => {
  afterEach(() => {
    clearToasts();
    vi.useRealTimers();
  });

  it("raises a toast and dismisses it by id", () => {
    const id = pushToast({ tone: "info", title: "set-volume issued", ttlMs: 0 });
    expect(toasts().map((t) => t.title)).toEqual(["set-volume issued"]);
    dismissToast(id);
    expect(toasts()).toEqual([]);
  });

  it("goes away by itself after its time, and stays when told to", () => {
    vi.useFakeTimers();
    pushToast({ tone: "success", title: "brief", ttlMs: 1_000 });
    pushToast({ tone: "info", title: "sticky", ttlMs: 0 });
    vi.advanceTimersByTime(1_001);
    expect(toasts().map((t) => t.title)).toEqual(["sticky"]);
  });

  it("keeps the newest when a burst outruns the screen", () => {
    for (let i = 0; i < MAX_TOASTS + 2; i++) pushToast({ tone: "info", title: `t${i}`, ttlMs: 0 });
    expect(toasts().map((t) => t.title)).toEqual(Array.from({ length: MAX_TOASTS }, (_, i) => `t${i + 2}`));
  });
});
