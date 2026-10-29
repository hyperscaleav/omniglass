import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import NotificationStatusPill from "./NotificationStatusPill";
import { PILL_NEUTRAL } from "../lib/subscriptions";

// The one pill a notification's delivery wears (#1413), on the alarm timeline
// and the Recent notifications blade alike: one word per status, a snoozed row
// in the neutral pill (deliberately not sent, never a failure).
const pill = (status: string, size?: "xs" | "sm") => {
  const { container } = render(() => <NotificationStatusPill status={status} size={size} />);
  return container.querySelector("[data-testid=notification-status]") as HTMLElement;
};
const classes = (el: HTMLElement) => new Set(el.className.split(/\s+/).filter(Boolean));

describe("NotificationStatusPill", () => {
  it("words each status the API answers", () => {
    expect(pill("queued").textContent).toBe("on its way");
    expect(pill("sent").textContent).toBe("sent");
    expect(pill("failed").textContent).toBe("failed");
    expect(pill("suppressed").textContent).toBe("snoozed");
  });

  it("wears each status's color, a snoozed row the neutral pill", () => {
    expect(classes(pill("queued"))).toEqual(new Set("badge badge-sm badge-soft badge-info".split(" ")));
    expect(classes(pill("sent"))).toEqual(new Set("badge badge-sm badge-soft badge-success".split(" ")));
    expect(classes(pill("failed"))).toEqual(new Set("badge badge-sm badge-soft badge-error".split(" ")));
    expect(classes(pill("suppressed"))).toEqual(new Set(PILL_NEUTRAL.split(" ")));
  });

  it("carries its status and a hint that teaches it", () => {
    const p = pill("suppressed");
    expect(p.dataset.status).toBe("suppressed");
    expect(p.title).toMatch(/snooze/i);
  });

  it("sizes down for a dense row", () => {
    const c = classes(pill("sent", "xs"));
    expect(c.has("badge-xs")).toBe(true);
    expect(c.has("badge-sm")).toBe(false);
  });

  it("renders an unknown status as itself, neutral", () => {
    const p = pill("parked");
    expect(p.textContent).toBe("parked");
    expect(classes(p)).toEqual(new Set(PILL_NEUTRAL.split(" ")));
  });
});
