import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AlarmTimeline from "./AlarmTimeline";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// An alarm's timeline carries the notifications its transitions queued
// (#618): each after the edge that caused it, reading how its delivery
// stands, and a failed one offers Retry to a caller holding
// notification:retry, which re-queues it and refreshes the timeline. A
// notification line is a status row, not a sentence (#1413): its target, the
// shared status pill and the action rule that queued it, labelled as such, so
// a snoozed row never reads as the rule having snoozed anything. Event and
// audit lines keep their sentences ("snoozed by alice").
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
const notified = uuidFor("notification-sent");
const failed = uuidFor("notification-failed");
const queued = uuidFor("notification-queued");
const snoozed = uuidFor("notification-suppressed");

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

// Every status a notification can stand at, beside an event and an audit
// line, as the architect's mock lays them out.
const allStatuses = () => [
  { at: ago(90_000), kind: "event", what: "condition-started", rule: "lobby-video", message: "no video" },
  { at: ago(90_000), kind: "notification", what: "sent", action_rule: "page-ops", target: "ops-chat", transition: "raised", notification: notified, attempts: 1, message: "[High] Lobby display: no video" },
  { at: ago(80_000), kind: "notification", what: "failed", action_rule: "page-ops", target: "ops-chat", transition: "raised", notification: failed, attempts: 3, error: "the receiver answered 503", message: "[High] Lobby display: no video" },
  { at: ago(70_000), kind: "audit", what: "snooze", actor: "alice" },
  { at: ago(60_000), kind: "notification", what: "suppressed", action_rule: "page-ops", target: "ops-chat", transition: "cleared", notification: snoozed, error: "the condition is snoozed until 2026-10-29 14:30 UTC by root", message: "[Cleared] Lobby display: no video" },
  { at: ago(50_000), kind: "notification", what: "queued", action_rule: "page-ops", target: "ops-chat", transition: "cleared", notification: queued, attempts: 2, message: "[Cleared] Lobby display: no video" },
];

async function rows(me: Me, body: unknown[]) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => json({ component: "disp-1", alarm_id: "a-1", entries: body }));
  const { getByRole } = mount(me);
  const list = await waitFor(() => getByRole("list", { name: "Timeline of no video" }));
  return within(list).getAllByRole("listitem");
}

const pillOf = (li: HTMLElement) => li.querySelector("[data-testid=notification-status]") as HTMLElement | null;
// The sentence's "by <who>" span, the slot a notify row no longer has.
const bySpan = (li: HTMLElement) => Array.from(li.querySelectorAll("span")).find((s) => /^by /.test(s.textContent ?? ""));

describe("AlarmTimeline notifications", () => {
  afterEach(() => vi.restoreAllMocks());

  it("reads each notification as a status row: target, the shared pill, its action rule", async () => {
    const lis = await rows(viewer, allStatuses());
    const want: [number, string][] = [[1, "sent"], [2, "failed"], [4, "snoozed"], [5, "on its way"]];
    for (const [i, word] of want) {
      const li = lis[i];
      const text = li.textContent ?? "";
      expect(text).toContain("notify");
      expect(pillOf(li)?.textContent, `row ${i}`).toBe(word);
      expect(text).toContain("action rule page-ops");
      expect(text.indexOf("ops-chat")).toBeLessThan(text.indexOf(word));
      expect(text.indexOf(word)).toBeLessThan(text.indexOf("action rule page-ops"));
      expect(bySpan(li), `row ${i} still reads "by"`).toBeUndefined();
      expect(text).not.toContain("by action rule");
      expect(text).not.toMatch(/notified|notifying|failed to notify|held back/);
    }
  });

  it("gives a failed row its error and attempts, and a snoozed row its reason, muted", async () => {
    const lis = await rows(viewer, allStatuses());
    expect(lis[2].textContent).toContain("the receiver answered 503 (3 attempts)");
    const why = Array.from(lis[4].querySelectorAll("span")).find((s) => s.textContent === "the condition is snoozed until 2026-10-29 14:30 UTC by root");
    expect(why, "no snooze reason").toBeTruthy();
    expect(why!.className).toContain("text-base-content/45");
    // Neither a sent nor a queued row (even one on its second attempt) carries a reason.
    expect(lis[1].textContent).not.toContain("attempts");
    expect(lis[5].textContent).not.toContain("attempts");
    // A viewer may not retry.
    expect(within(lis[2]).queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("offers Retry on a failed row only, to a caller who may", async () => {
    const lis = await rows(operator, allStatuses());
    expect(within(lis[2]).getByRole("button", { name: "Retry" })).toBeTruthy();
    for (const i of [1, 4, 5]) expect(within(lis[i]).queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("keeps event and audit lines as sentences", async () => {
    const lis = await rows(viewer, allStatuses());
    expect(lis[0].textContent).toContain("condition-started");
    expect(bySpan(lis[0])?.textContent).toBe("by rule lobby-video");
    expect(pillOf(lis[0])).toBeNull();
    expect(lis[3].textContent).toContain("snoozed");
    expect(bySpan(lis[3])?.textContent).toBe("by alice");
    expect(pillOf(lis[3])).toBeNull();
  });

  it("names a person's row by its recipient and channel, and leaves out what it withholds", async () => {
    const lis = await rows(viewer, [
      { at: ago(20_000), kind: "notification", what: "sent", target: "", channel: "email", recipient: "alice", action_rule: "my-pages", transition: "raised", notification: notified, attempts: 1, message: "[High] Lobby display: no video" },
      { at: ago(10_000), kind: "notification", what: "sent", target: "", channel: "inbox", recipient_withheld: true, transition: "raised", notification: queued, attempts: 1, message: "[High] Lobby display: no video" },
    ]);
    const [seen, withheld] = lis;
    expect(seen.textContent).toContain("alice (email)");
    expect(seen.textContent).toContain("action rule my-pages");
    expect(withheld.textContent).toContain("a person (inbox)");
    expect(withheld.textContent).not.toContain("action rule");
    for (const li of lis) {
      expect(pillOf(li)?.textContent).toBe("sent");
      expect(bySpan(li)).toBeUndefined();
      // The rendered subject rides as the addressee's hint.
      expect(li.querySelector('[title="[High] Lobby display: no video"]')).toBeTruthy();
    }
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
