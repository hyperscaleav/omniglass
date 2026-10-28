import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AlarmTimeline from "./AlarmTimeline";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// An alarm's timeline carries the notifications its transitions queued
// (#618): each after the edge that caused it, reading how its delivery
// stands, and a failed one offers Retry to a caller holding
// notification:retry, which re-queues it and refreshes the timeline.
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
const notified = uuidFor("notification-sent");
const failed = uuidFor("notification-failed");

const operator: Me = { principal: { id: "u-op", kind: "human" }, human: { username: "op" }, permissions: ["*:read", "notification:retry"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const entries = () => [
  { at: ago(60_000), kind: "audit", what: "raise", actor: "jordan", message: "no video" },
  { at: ago(60_000), kind: "notification", what: "sent", action_rule: "page-ops", target: "ops-chat", transition: "raised", notification: notified, attempts: 1, message: "[High] Lobby display: no video" },
  { at: ago(30_000), kind: "audit", what: "clear", actor: "jordan" },
  { at: ago(30_000), kind: "notification", what: "failed", action_rule: "page-ops", target: "ops-chat", transition: "cleared", notification: failed, attempts: 8, error: "the receiver answered 503", message: "[Cleared] Lobby display: no video" },
];

function mount(me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <AlarmTimeline component="disp-1" alarmId="a-1" label="Timeline of no video" />
    </QueryClientProvider>
  ));
}

describe("AlarmTimeline notifications", () => {
  afterEach(() => vi.restoreAllMocks());

  it("reads each notification after its edge, by its target and its action rule", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => json({ component: "disp-1", alarm_id: "a-1", entries: entries() }));
    const { getByRole } = mount(viewer);
    const list = await waitFor(() => getByRole("list", { name: "Timeline of no video" }));
    const items = within(list).getAllByRole("listitem").map((li) => li.textContent ?? "");
    expect(items[1]).toContain("notify");
    expect(items[1]).toContain("notified ops-chat");
    expect(items[1]).toContain("action rule page-ops");
    expect(items[3]).toContain("failed to notify ops-chat");
    expect(items[3]).toContain("the receiver answered 503 (8 attempts)");
    // A viewer may not retry.
    expect(within(list).queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("retries a failed notification for a caller who may, and refreshes", async () => {
    const calls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      calls.push(`${req.method} ${new URL(req.url).pathname}`);
      if (req.method === "POST") return json({ id: failed, status: "queued" });
      return json({ component: "disp-1", alarm_id: "a-1", entries: entries() });
    });
    const { getByRole } = mount(operator);
    const list = await waitFor(() => getByRole("list", { name: "Timeline of no video" }));
    fireEvent.click(within(list).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(calls).toContain(`POST /api/v1/notifications/${failed}:retry`));
    await waitFor(() => expect(calls.filter((c) => c.startsWith("GET")).length).toBe(2));
  });
});
