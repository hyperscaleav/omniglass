import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import Audit from "./Audit";
import { AUDIT_PAGE, type AuditEvent } from "../lib/audit";

// The Audit page renders the trail newest-first (resolving the actor to a name and
// marking an impersonated action), and wears the list-view standard: the shared
// FilterBar narrows the loaded rows, and "load older" pages back via the server
// `before` cursor. The network is the seam we fake (fetch), so the page is driven
// exactly as a user would without a server.
const seed: AuditEvent[] = [
  { id: "1", ts: "2026-07-07T10:02:00Z", actor: "u-alice", actor_name: "alice", verb: "login", resource: "auth" },
  { id: "2", ts: "2026-07-07T10:01:00Z", actor: "u-alice", actor_name: "alice", real_actor: "u-root", real_actor_name: "root", verb: "update", resource: "principal", resource_id: "u-alice" },
  { id: "3", ts: "2026-07-07T10:00:00Z", actor: "u-bob", actor_name: "bob", verb: "delete", resource: "component", resource_id: "cmp_9f2" },
];

const beforeParams: (string | null)[] = [];

beforeEach(() => {
  vi.restoreAllMocks();
  beforeParams.length = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = typeof input === "string" ? input : (input as Request).url;
    const before = new URL(url, "http://local").searchParams.get("before");
    beforeParams.push(before);
    // A page older than the newest page is empty (nothing further back).
    const events = before ? [] : seed;
    return new Response(JSON.stringify({ events }), { status: 200, headers: { "Content-Type": "application/json" } });
  });
});

describe("Audit page", () => {
  it("renders events with actor, verb, and resource", async () => {
    const { findByText, getByText } = render(() => <Audit />);
    expect(await findByText("login")).toBeTruthy();
    expect(getByText("auth")).toBeTruthy();
    expect(getByText("delete")).toBeTruthy();
  });

  it("shows the impersonator as the actor, tagged with the identity they assumed", async () => {
    const { findByText, getByText } = render(() => <Audit />);
    // Row 2 was root acting as alice: "root" (the real human) is the accountable
    // who, with an "as alice" tag naming whose identity was assumed.
    expect(await findByText(/as alice/)).toBeTruthy();
    expect(getByText("root")).toBeTruthy();
  });

  it("narrows the loaded rows through the FilterBar (a bare term filters the actor)", async () => {
    const { findByText, getByRole, queryByText, getAllByText } = render(() => <Audit />);
    await findByText("login");
    const input = getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "alice" } });
    fireEvent.keyDown(input, { key: "Enter" });
    // Only the two alice rows remain; bob's row is filtered out.
    expect(getAllByText("alice").length).toBeGreaterThan(0);
    expect(queryByText("bob")).toBeNull();
    expect(queryByText("delete")).toBeNull();
  });

  it("pages older through the server `before` cursor", async () => {
    // A full first page (== AUDIT_PAGE) means there may be more, so load-older is
    // live; the oldest row carries a known timestamp the cursor must use.
    const oldestTs = "2026-07-07T09:00:00Z";
    const full: AuditEvent[] = Array.from({ length: AUDIT_PAGE }, (_, i) => ({
      id: `f${i}`,
      ts: i === AUDIT_PAGE - 1 ? oldestTs : "2026-07-07T10:00:00Z",
      actor_name: "alice", verb: "login", resource: "auth",
    }));
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      const before = new URL(url, "http://local").searchParams.get("before");
      beforeParams.push(before);
      return new Response(JSON.stringify({ events: before ? [] : full }), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    const { findByText, getByText } = render(() => <Audit />);
    await findByText("Load older");
    // Re-query each poll: the footer re-renders (replacing the button node) as the
    // loaded-count updates, so a captured reference would go stale. Wait for the
    // first load to finish so the button is live (not the no-op it is in flight).
    const btn = () => getByText("Load older") as HTMLButtonElement;
    await waitFor(() => expect(btn().disabled).toBe(false));
    expect(beforeParams).toEqual([null]); // first page has no cursor
    fireEvent.click(btn());
    // The load-older page asks for events strictly older than the oldest loaded row.
    await waitFor(() => expect(beforeParams).toContain(oldestTs));
  });
});
