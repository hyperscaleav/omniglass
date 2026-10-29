import { describe, expect, it } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Route, Router } from "@solidjs/router";
import RecentNotifications from "./RecentNotifications";
import { recentNotificationsKey, type Notification } from "../lib/action_rules";
import { PILL_NEUTRAL } from "../lib/subscriptions";

// A blade's recent notifications (#1346) say how each delivery stands. A
// notification an alarm's snooze held back (#1318) was deliberately not sent,
// so it reads as held back in the neutral pill, never as a failure, and the
// status table covers every status the API can answer (typecheck holds that).

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
  it("reads a notification a snooze held back as held back, in the neutral pill", async () => {
    mount([base]);
    const row = await screen.findByTestId("recent-notification");
    expect(row.getAttribute("data-status")).toBe("suppressed");
    const pill = Array.from(row.querySelectorAll("span")).find((s) => s.textContent === "held back");
    expect(pill, "no 'held back' pill").toBeTruthy();
    for (const c of PILL_NEUTRAL.split(" ")) expect(pill!.classList.contains(c)).toBe(true);
    expect(row.textContent).not.toContain("failed");
  });
});
