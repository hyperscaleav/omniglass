import { afterEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Route, Router } from "@solidjs/router";
import Home from "./Home";

// The watch seam is stubbed: Home's liveness is useView's job (pinned in
// useview.test), and this file is about the page composing the right views
// into the right renderers.
const notifiers: Array<() => void> = [];
vi.mock("../lib/viewwatch", () => ({
  watchView: (_n: string, _p: string[] | undefined, onChange: () => void) => {
    notifiers.push(onChange);
    return { close() {} };
  },
}));

vi.mock("../lib/auth", () => ({
  useMe: () => ({ data: { principal: { id: "p", kind: "human" }, human: { username: "dev" }, permissions: [">"], grants: [] } }),
  can: () => true,
}));

// The three views Home reads, keyed by the name in the request path.
const counts = {
  columns: [
    { name: "tile", type: "string", role: "label" },
    { name: "count", type: "number", role: "value" },
  ],
  rows: [
    ["components", 4],
    ["interfaces", 6],
    ["reachable", 5],
    ["unreachable", 1],
  ],
};
const reachability = (state: string) => ({
  columns: [
    { name: "component", type: "string", role: "label" },
    { name: "interface", type: "string" },
    { name: "interface_type", type: "string" },
    { name: "state", type: "string", role: "value" },
    { name: "since", type: "time", role: "time" },
  ],
  rows: [["bar-1", "bar-1-icmp", "icmp", state, "2026-08-03T12:00:00Z"]],
});
const feed = {
  columns: [
    { name: "ts", type: "time", role: "time" },
    { name: "owner", type: "string", role: "label" },
    { name: "event_type", type: "string" },
    { name: "instance", type: "string" },
    { name: "origin", type: "string" },
    { name: "message", type: "string", role: "value" },
    { name: "source", type: "string" },
  ],
  rows: [["2026-08-03T12:00:00Z", "bar-1", "call.started", "", "caught", "a call started", "t"]],
};

let gridState = "up";
function stubApi() {
  const fetchMock = vi.fn().mockImplementation(async (input: Request | string) => {
    const url = typeof input === "string" ? input : input.url;
    const body = url.includes("estate-counts")
      ? counts
      : url.includes("component-reachability")
        ? reachability(gridState)
        : url.includes("event-feed")
          ? feed
          : { columns: [], rows: [] };
    return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const clients: QueryClient[] = [];
function mountHome() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(qc);
  window.history.pushState({}, "", "/");
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/" component={Home} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(() => {
  for (const qc of clients) {
    qc.unmount();
    qc.clear();
  }
  clients.length = 0;
  notifiers.length = 0;
  gridState = "up";
  vi.unstubAllGlobals();
});

describe("Home", () => {
  it("reads the estate through views, not through resource routes", async () => {
    const fetchMock = stubApi();
    mountHome();
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(3));
    const urls = fetchMock.mock.calls.map((c) => (typeof c[0] === "string" ? c[0] : (c[0] as Request).url));
    // Every read is a view run. A page that reached for /components or /events
    // directly would be the bespoke-endpoint-per-page shape views exist to end.
    for (const u of urls) {
      expect(u).toContain("/views/");
    }
    expect(urls.some((u) => u.includes("estate-counts:run"))).toBe(true);
    expect(urls.some((u) => u.includes("component-reachability:run"))).toBe(true);
    expect(urls.some((u) => u.includes("event-feed:run"))).toBe(true);
  });

  it("renders the tiles, the reachability grid, and the event feed", async () => {
    stubApi();
    const { container, findByText } = mountHome();
    // Tiles carry the counts.
    expect(await findByText("components")).toBeTruthy();
    expect(await findByText("4")).toBeTruthy();
    // The grid carries the verdict as a readable state, not only a colour.
    await waitFor(() => expect(container.querySelectorAll("[data-state]").length).toBe(1));
    expect(container.querySelector("[data-state]")?.getAttribute("data-state")).toBe("up");
    // The feed carries the occurrence.
    expect(await findByText("a call started")).toBeTruthy();
  });

  it("updates the grid when the watch stream reports a change, with no user action", async () => {
    stubApi();
    const { container } = mountHome();
    await waitFor(() => expect(container.querySelector("[data-state]")?.getAttribute("data-state")).toBe("up"));

    // The estate flips and the server notifies. Home refetches itself: this is
    // the whole point of the situation room being live.
    gridState = "down";
    for (const notify of [...notifiers]) notify();
    for (const notify of [...notifiers]) notify();
    await waitFor(() => expect(container.querySelector("[data-state]")?.getAttribute("data-state")).toBe("down"));
  });

  it("shows a view's failure in its own panel, leaving the rest of the page standing", async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: Request | string) => {
      const url = typeof input === "string" ? input : input.url;
      if (url.includes("event-feed")) {
        return new Response(JSON.stringify({ title: "Forbidden", detail: "running event-feed requires event:read" }), {
          status: 403,
          headers: { "Content-Type": "application/problem+json" },
        });
      }
      const body = url.includes("estate-counts") ? counts : reachability("up");
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    vi.stubGlobal("fetch", fetchMock);

    const { findByRole, findByText } = mountHome();
    // The refused panel says so...
    expect(await findByRole("alert")).toBeTruthy();
    // ...and the tiles beside it still render.
    expect(await findByText("components")).toBeTruthy();
  });
});
