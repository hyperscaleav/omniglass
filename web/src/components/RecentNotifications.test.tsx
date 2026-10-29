import { describe, expect, it } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Route, Router } from "@solidjs/router";
import RecentNotifications from "./RecentNotifications";
import { recentNotificationsKey, type Notification } from "../lib/action_rules";
import { PILL_NEUTRAL } from "../lib/subscriptions";

// A blade's recent notifications (#1346) say how each delivery stands, in the
// one status pill the alarm timeline wears too (#1413). A notification an
// alarm's snooze suppressed (#1318) was deliberately not sent, so it reads
// "snoozed" in the neutral pill, never as a failure, and its reason is not a
// retry.

const at = "2026-10-29T04:00:00Z";
const base: Notification = {
  id: "00000000-0000-7000-8000-000000000001", alarm: "a-1", attempts: 0, channel: "webhook", component: "display-1", component_id: "c-1",
  created_at: at, notification_type: "alarm-raised", status: "suppressed", subject: "s", target: "ops-chat", text: "t", transition: "raised",
  action_rule: "page-ops",
};

function mount(rows: Notification[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...recentNotificationsKey({ target: "ops-chat" })], rows);
  window.history.pushState({}, "", "/x");
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/x" component={() => <RecentNotifications target="ops-chat" hint="h" />} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("RecentNotifications", () => {
  it("reads a notification a snooze suppressed as snoozed, in the shared neutral pill", async () => {
    mount([{ ...base, last_error: "the condition is snoozed until 2026-10-29 14:30 UTC by root" }]);
    const row = await screen.findByTestId("recent-notification");
    expect(row.getAttribute("data-status")).toBe("suppressed");
    const pill = row.querySelector("[data-testid=notification-status]") as HTMLElement;
    expect(pill, "no shared status pill").toBeTruthy();
    expect(pill.textContent).toBe("snoozed");
    expect(new Set(pill.className.split(/\s+/).filter(Boolean))).toEqual(new Set(PILL_NEUTRAL.split(" ")));
    expect(row.textContent).not.toContain("failed");
    expect(row.textContent).not.toContain("held back");
    const why = await screen.findByTestId("recent-notification-error");
    expect(why.textContent).toBe("the condition is snoozed until 2026-10-29 14:30 UTC by root");
    expect(why.classList.contains("text-error")).toBe(false);
  });

  it("says a queued row's last error is being retried, and a failed row's as it stands", async () => {
    mount([
      { ...base, id: "00000000-0000-7000-8000-000000000002", status: "queued", last_error: "the receiver answered 503" },
      { ...base, id: "00000000-0000-7000-8000-000000000003", status: "failed", last_error: "the receiver answered 503" },
    ]);
    const errs = await screen.findAllByTestId("recent-notification-error");
    expect(errs.map((e) => e.textContent)).toEqual(["Retrying: the receiver answered 503", "the receiver answered 503"]);
    const pills = Array.from(document.querySelectorAll("[data-testid=notification-status]")).map((p) => p.textContent);
    expect(pills).toEqual(["on its way", "failed"]);
  });
});
