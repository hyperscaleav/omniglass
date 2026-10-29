import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Inbox from "./Inbox";
import { ME_KEY, type Me } from "../lib/auth";
import { INBOX_COUNT_KEY, inboxListKey, type Inbox as InboxRead, type InboxEntry } from "../lib/inbox";

// The console inbox (#1355). The inbox reads are seeded into the query cache;
// fetch answers only the writes (and the refetches they cause) a test drives,
// and refuses anything else.
const viewer: Me = { principal: { id: "u-1", kind: "human" }, human: { username: "ada" }, permissions: ["*:read", "notification:subscribe"], grants: [] };
const readOnly: Me = { principal: { id: "u-2", kind: "human" }, human: { username: "bo" }, permissions: ["*:read"], grants: [] };
const noInbox: Me = { principal: { id: "u-3", kind: "human" }, human: { username: "cy" }, permissions: ["component:read"], grants: [] };

function entry(over: Partial<InboxEntry>): InboxEntry {
  return {
    id: "n-1", alarm: "a-1", component: "hq-boardroom-dsp", component_id: "c-dsp", transition: "raised", action_rule: "hq-outages",
    notification_type: "alarm-raised", target: "ada", channel: "inbox", recipient: "ada", subject: "Outage: hq-boardroom-dsp",
    text: "The DSP stopped answering.", status: "sent", attempts: 1, created_at: new Date(Date.now() - 120_000).toISOString(),
    ...over,
  } as InboxEntry;
}

const unreadOne = entry({ id: "n-1" });
const readOne = entry({ id: "n-2", subject: "Cleared: hq-lobby-display", component: "hq-lobby-display", component_id: "c-lobby", transition: "cleared", read_at: new Date().toISOString() });

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function mount(opts: { me?: Me; all?: InboxRead; unread?: InboxRead } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], opts.me ?? viewer);
  if (opts.all) qc.setQueryData([...inboxListKey(false)], opts.all);
  if (opts.unread) qc.setQueryData([...inboxListKey(true)], opts.unread);
  qc.setQueryData([...INBOX_COUNT_KEY], opts.all?.unread ?? 0);
  window.history.pushState({}, "", "/inbox");
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/inbox" component={Inbox} />
        <Route path="/profile/subscriptions" component={() => <div data-testid="subscriptions-page" />} />
        <Route path="/components/:id" component={() => <div data-testid="component-page" />} />
      </Router>
    </QueryClientProvider>
  ));
  return qc;
}

function stubFetch(handle: (req: Request) => Response | Promise<Response>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => handle(input as Request));
}

describe("Inbox", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    window.history.pushState({}, "", "/");
  });

  it("lists entries newest first, unread apart from read, each linking to its component", async () => {
    stubFetch((r) => { throw new Error(`unexpected ${r.method} ${r.url}`); });
    mount({ all: { notifications: [unreadOne, readOne], unread: 1 } });
    const rows = await screen.findAllByTestId("inbox-entry");
    expect(rows).toHaveLength(2);
    expect(rows[0].dataset.unread).toBe("true");
    expect(rows[1].dataset.unread).toBe("false");
    expect(within(rows[0]).getByText("Outage: hq-boardroom-dsp")).toBeTruthy();
    expect(within(rows[0]).getByText("raised")).toBeTruthy();
    expect(within(rows[0]).getByText("2m ago")).toBeTruthy();
    expect(within(rows[0]).getByText("hq-boardroom-dsp").closest("a")!.getAttribute("href")).toBe("/components/c-dsp?tab=activity");
    // Only the unread entry offers Mark read.
    expect(within(rows[0]).getByRole("button", { name: /Mark read/ })).toBeTruthy();
    expect(within(rows[1]).queryByRole("button", { name: /Mark read/ })).toBeNull();
    expect(screen.getByTestId("inbox-unread-count").textContent).toBe("1");
  });

  it("marks one entry read and refreshes the list", async () => {
    let marked = "";
    stubFetch((r) => {
      if (r.method === "POST" && r.url.includes(":markRead")) {
        marked = r.url;
        return json({ ...unreadOne, read_at: new Date().toISOString() });
      }
      if (r.method === "GET" && r.url.includes("/auth/me/notifications")) {
        return json({ notifications: [{ ...unreadOne, read_at: new Date().toISOString() }, readOne], unread: 0 });
      }
      throw new Error(`unexpected ${r.method} ${r.url}`);
    });
    mount({ all: { notifications: [unreadOne, readOne], unread: 1 } });
    fireEvent.click(await screen.findByRole("button", { name: /Mark read: Outage/ }));
    await waitFor(() => expect(marked).toContain("/auth/me/notifications/n-1:markRead"));
    await waitFor(() => expect(screen.getAllByTestId("inbox-entry")[0].dataset.unread).toBe("false"));
    expect(screen.getByTestId("inbox-unread-count").textContent).toBe("0");
  });

  it("marks an entry read when its component is opened", async () => {
    let marked = "";
    stubFetch((r) => {
      if (r.method === "POST" && r.url.includes(":markRead")) {
        marked = r.url;
        return json({ ...unreadOne, read_at: new Date().toISOString() });
      }
      if (r.method === "GET" && r.url.includes("/auth/me/notifications")) {
        return json({ notifications: [{ ...unreadOne, read_at: new Date().toISOString() }, readOne], unread: 0 });
      }
      return json({});
    });
    mount({ all: { notifications: [unreadOne, readOne], unread: 1 } });
    const rows = await screen.findAllByTestId("inbox-entry");
    fireEvent.click(within(rows[0]).getByText("hq-boardroom-dsp"));
    await waitFor(() => expect(marked).toContain("/auth/me/notifications/n-1:markRead"));
  });

  it("marks everything read", async () => {
    let all = false;
    stubFetch((r) => {
      if (r.method === "POST" && r.url.endsWith("/auth/me/notifications:markAllRead")) {
        all = true;
        return json({ marked: 1 });
      }
      if (r.method === "GET") return json({ notifications: [{ ...unreadOne, read_at: new Date().toISOString() }, readOne], unread: 0 });
      throw new Error(`unexpected ${r.method} ${r.url}`);
    });
    mount({ all: { notifications: [unreadOne, readOne], unread: 1 } });
    fireEvent.click(await screen.findByRole("button", { name: "Mark all read" }));
    await waitFor(() => expect(all).toBe(true));
    await waitFor(() => expect(screen.getAllByTestId("inbox-entry").every((r) => r.dataset.unread === "false")).toBe(true));
    expect((screen.getByRole("button", { name: "Mark all read" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("filters to the unread entries", async () => {
    stubFetch((r) => { throw new Error(`unexpected ${r.method} ${r.url}`); });
    mount({ all: { notifications: [unreadOne, readOne], unread: 1 }, unread: { notifications: [unreadOne], unread: 1 } });
    expect(await screen.findAllByTestId("inbox-entry")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Unread only" }));
    await waitFor(() => expect(screen.getAllByTestId("inbox-entry")).toHaveLength(1));
    expect(screen.getByRole("button", { name: "Unread only" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("says when nothing is unread, and offers everything back", async () => {
    stubFetch((r) => { throw new Error(`unexpected ${r.method} ${r.url}`); });
    mount({ all: { notifications: [readOne], unread: 0 }, unread: { notifications: [], unread: 0 } });
    fireEvent.click(await screen.findByRole("button", { name: "Unread only" }));
    expect(await screen.findByTestId("inbox-caught-up")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show everything" }));
    await waitFor(() => expect(screen.getAllByTestId("inbox-entry")).toHaveLength(1));
  });

  it("teaches the empty inbox: the two ways in, and the way to subscribe", async () => {
    stubFetch((r) => { throw new Error(`unexpected ${r.method} ${r.url}`); });
    mount({ all: { notifications: [], unread: 0 } });
    const empty = await screen.findByTestId("inbox-empty");
    expect(within(empty).getByRole("button", { name: "A group notification" })).toBeTruthy();
    expect(within(empty).getByRole("button", { name: "Your own subscription" })).toBeTruthy();
    fireEvent.click(within(empty).getByRole("button", { name: "Subscribe to alarms" }));
    expect(await screen.findByTestId("subscriptions-page")).toBeTruthy();
  });

  it("offers no subscribe to a person without notification:subscribe", async () => {
    stubFetch((r) => { throw new Error(`unexpected ${r.method} ${r.url}`); });
    mount({ me: readOnly, all: { notifications: [], unread: 0 } });
    const empty = await screen.findByTestId("inbox-empty");
    expect(within(empty).queryByRole("button", { name: "Subscribe to alarms" })).toBeNull();
  });

  it("alerts on a failed read and retries", async () => {
    let calls = 0;
    stubFetch((r) => {
      if (r.method === "GET" && r.url.includes("/auth/me/notifications")) {
        calls++;
        return calls === 1 ? json({ status: 500, detail: "the database is away" }, 500) : json({ notifications: [unreadOne], unread: 1 });
      }
      throw new Error(`unexpected ${r.method} ${r.url}`);
    });
    mount();
    const alert = await screen.findByTestId("inbox-error");
    expect(alert.textContent).toContain("the database is away");
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    expect(await screen.findAllByTestId("inbox-entry")).toHaveLength(1);
  });

  it("says so, and asks nothing, without notification:read", async () => {
    const fetch = stubFetch((r) => { throw new Error(`unexpected ${r.method} ${r.url}`); });
    mount({ me: noInbox });
    expect(await screen.findByTestId("inbox-denied")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Mark all read" })).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });
});
