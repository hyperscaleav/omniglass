import "@testing-library/jest-dom/vitest";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import AlarmDuration from "./AlarmDuration";

// How long an alarm was raised (#2426): fixed for a cleared alarm, live for an
// open one, both worded by the timeline's one durationText.
describe("AlarmDuration", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-23T10:00:45Z"));
  });
  afterEach(() => vi.useRealTimers());

  it("reads raised to cleared for a cleared alarm and never moves", () => {
    render(() => <AlarmDuration alarm={{ raised_at: "2026-11-23T09:00:00Z", cleared_at: "2026-11-23T09:12:00Z" }} />);
    expect(screen.getByTestId("alarm-duration")).toHaveTextContent("12m");
    expect(screen.getByTestId("alarm-duration")).toHaveAttribute("data-open", "false");
    vi.advanceTimersByTime(3_600_000);
    expect(screen.getByTestId("alarm-duration")).toHaveTextContent("12m");
  });

  it("starts no timer for a cleared alarm, so a long history costs nothing per second", () => {
    render(() => <AlarmDuration alarm={{ raised_at: "2026-11-23T09:00:00Z", cleared_at: "2026-11-23T09:12:00Z" }} />);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("ticks for an open alarm", () => {
    render(() => <AlarmDuration alarm={{ raised_at: "2026-11-23T10:00:00Z" }} />);
    expect(screen.getByTestId("alarm-duration")).toHaveTextContent("45s");
    expect(screen.getByTestId("alarm-duration")).toHaveAttribute("data-open", "true");
    vi.advanceTimersByTime(20_000);
    expect(screen.getByTestId("alarm-duration")).toHaveTextContent("1m");
  });
});
