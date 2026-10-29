import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SnoozeButton, { SnoozedBadge, SNOOZE_TIP } from "./SnoozeButton";
import { ME_KEY, type Me } from "../lib/auth";
import { snoozedUntilWords, timelineWhat } from "../lib/alarms";

// The snooze control (#1318): a menu of durations on an unsnoozed alarm, an
// Unsnooze on a snoozed one, nothing for a caller without alarm:snooze, and a
// badge every reader sees.
const operator: Me = { principal: { id: "p", kind: "human" }, permissions: ["alarm:snooze"], grants: [] };
const viewer: Me = { principal: { id: "v", kind: "human" }, permissions: ["alarm:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(me: Me, alarm: { id: string; snoozed_until?: string; snoozed_by?: string }) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <SnoozeButton component="disp-1" alarm={alarm} />
      <SnoozedBadge alarm={alarm} />
    </QueryClientProvider>
  ));
}

describe("SnoozeButton", () => {
  afterEach(() => vi.restoreAllMocks());

  it("snoozes the condition for the chosen duration, and teaches how a snooze differs", async () => {
    const calls: { url: string; body: unknown }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      calls.push({ url: req.url, body: req.method === "POST" ? await req.json() : undefined });
      return json({ id: "a-1" });
    });
    const { getByRole } = mount(operator, { id: "a-1" });
    const trigger = getByRole("button", { name: /Snooze/ });
    expect(trigger.getAttribute("title")).toBe(SNOOZE_TIP);
    expect(SNOOZE_TIP).toMatch(/acknowledging/);
    expect(SNOOZE_TIP).toMatch(/opting out/);
    expect(SNOOZE_TIP).toMatch(/pausing a rule/);
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByRole("button", { name: "1 hour" }));
    await waitFor(() => expect(calls.some((c) => c.url.includes("/components/disp-1/alarms/a-1:snooze"))).toBe(true));
    expect(calls.find((c) => c.url.includes(":snooze"))!.body).toEqual({ duration: "1h" });
  });

  it("ends a snooze early, and the badge names who set it", async () => {
    const urls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      urls.push((input as Request).url);
      return json({ id: "a-1" });
    });
    const until = new Date(Date.now() + 3_600_000).toISOString();
    const { getByRole, getByText } = mount(operator, { id: "a-1", snoozed_until: until, snoozed_by: "jordan" });
    expect(getByText(snoozedUntilWords(until)).getAttribute("title")).toMatch(/snoozed by jordan/);
    fireEvent.click(getByRole("button", { name: "Unsnooze" }));
    await waitFor(() => expect(urls.some((u) => u.includes("/components/disp-1/alarms/a-1:unsnooze"))).toBe(true));
  });

  it("offers a viewer no control, and still shows the snooze", () => {
    const until = new Date(Date.now() + 3_600_000).toISOString();
    const { queryByRole, getByText } = mount(viewer, { id: "a-1", snoozed_until: until });
    expect(queryByRole("button", { name: /snooze/i })).toBeNull();
    expect(getByText(snoozedUntilWords(until))).toBeTruthy();
  });
});

describe("snooze words", () => {
  it("words a deadline today as a time and a later one with its day", () => {
    const now = new Date(2026, 9, 29, 9, 0).getTime();
    expect(snoozedUntilWords(new Date(2026, 9, 29, 14, 30).toISOString(), now)).toMatch(/^snoozed until \d/);
    expect(snoozedUntilWords(new Date(2026, 10, 2, 14, 30).toISOString(), now)).toMatch(/^snoozed until \D/);
  });

  it("words a suppressed row and the snooze audit lines on the timeline", () => {
    expect(timelineWhat({ kind: "notification", what: "suppressed", target: "ops-chat" })).toBe("held back from ops-chat");
    expect(timelineWhat({ kind: "notification", what: "suppressed", target: "" })).toBe("held back");
    expect(timelineWhat({ kind: "audit", what: "snooze" })).toBe("snoozed");
    expect(timelineWhat({ kind: "audit", what: "unsnooze" })).toBe("unsnoozed");
    expect(timelineWhat({ kind: "audit", what: "snooze-end" })).toBe("snooze ended");
  });
});
