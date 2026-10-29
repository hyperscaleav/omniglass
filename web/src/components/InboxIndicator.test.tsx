import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import InboxIndicator from "./InboxIndicator";
import { ME_KEY, type Me } from "../lib/auth";
import { INBOX_COUNT_KEY, INBOX_PREFIX } from "../lib/inbox";

// The top bar's bell (#1355): the unread count from the seeded cache, a link
// to the inbox, and nothing at all for a principal without notification:read.
const viewer: Me = { principal: { id: "u-1", kind: "human" }, human: { username: "ada" }, permissions: ["*:read", "notification:subscribe"], grants: [] };
const noInbox: Me = { principal: { id: "u-2", kind: "human" }, human: { username: "bo" }, permissions: ["component:read"], grants: [] };

function mount(me: Me, unread?: number) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  if (unread !== undefined) qc.setQueryData([...INBOX_COUNT_KEY], unread);
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={InboxIndicator} />
      </Router>
    </QueryClientProvider>
  ));
  return qc;
}

describe("InboxIndicator", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the unread count and links to the inbox", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("no fetch expected"));
    mount(viewer, 3);
    const link = await screen.findByTestId("inbox-indicator");
    expect(link.getAttribute("href")).toBe("/inbox");
    expect(link.getAttribute("aria-label")).toBe("Inbox, 3 unread");
    expect(screen.getByTestId("inbox-unread").textContent).toBe("3");
  });

  it("shows the bell without a count when everything is read", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("no fetch expected"));
    mount(viewer, 0);
    const link = await screen.findByTestId("inbox-indicator");
    expect(link.getAttribute("aria-label")).toBe("Inbox");
    expect(screen.queryByTestId("inbox-unread")).toBeNull();
  });

  it("caps a long count at 99+", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("no fetch expected"));
    mount(viewer, 240);
    expect((await screen.findByTestId("inbox-unread")).textContent).toBe("99+");
  });

  it("renders nothing, and asks nothing, without notification:read", async () => {
    const fetch = vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("no fetch expected"));
    mount(noInbox);
    await Promise.resolve();
    expect(screen.queryByTestId("inbox-indicator")).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("follows a mark: invalidating the inbox prefix refetches the count", async () => {
    const fetch = vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      new Response(JSON.stringify({ notifications: [], unread: 1 }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const qc = mount(viewer, 5);
    expect((await screen.findByTestId("inbox-unread")).textContent).toBe("5");
    await qc.invalidateQueries({ queryKey: INBOX_PREFIX });
    await waitFor(() => expect(screen.getByTestId("inbox-unread").textContent).toBe("1"));
    const url = (fetch.mock.calls[0][0] as Request).url;
    expect(url).toContain("/auth/me/notifications");
    expect(url).toContain("unread=true");
  });
});
