import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import Time from "./Time";
import { fmtTime } from "../lib/format";

const focusIn = (el: HTMLElement) => {
  fireEvent.keyDown(document.body, { key: "Tab" });
  el.focus();
};
const iso = "2026-11-23T13:56:36.123Z";
const now = new Date("2026-11-23T14:00:36Z").getTime();

// An instant reads relative in a list ("4m ago") and absolute on hover, from the
// one formatter module (#2447).
describe("Time", () => {
  it("shows the relative age and the absolute time plus the exact instant on hover", async () => {
    render(() => <Time iso={iso} now={now} />);
    const el = screen.getByText("4m ago");
    expect(el.tagName).toBe("SPAN");
    focusIn(el);
    const tip = await screen.findByRole("tooltip");
    expect(tip.textContent).toContain(fmtTime(iso));
    expect(tip.textContent).toContain(iso);
  });

  it("shows the absolute time as its text when asked, for a place where relative reads poorly", () => {
    render(() => <Time iso={iso} absolute />);
    expect(screen.getByText(fmtTime(iso))).toBeTruthy();
  });

  it("falls back to the raw text for a value that is not an instant", () => {
    render(() => <Time iso="soon" now={now} />);
    expect(screen.getByText("soon")).toBeTruthy();
  });
});
