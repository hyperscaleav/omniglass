import { afterEach, describe, expect, it, vi } from "vitest";
import { createSignal, type JSX } from "solid-js";
import { render, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { useView, type ViewResult } from "./views";

// A watch stub: the tests drive change notifications by hand, so invalidation
// is asserted without a live stream.
const notifiers: Array<() => void> = [];
const closed = { count: 0 };
vi.mock("./viewwatch", () => ({
  watchView: (_name: string, _params: string[] | undefined, onChange: () => void) => {
    notifiers.push(onChange);
    return {
      close() {
        closed.count += 1;
      },
    };
  },
}));

// openapi-fetch hands the global fetch a Request, not a URL string, so the
// tests read the url off it rather than stringifying the argument.
function requestUrl(mock: { mock: { calls: unknown[][] } }, i: number): string {
  const arg = mock.mock.calls[i][0];
  return typeof arg === "string" ? arg : (arg as Request).url;
}

const result = (state: string): ViewResult => ({
  columns: [{ name: "state", type: "string", role: "value" }],
  rows: [[state]],
});

// Every client the tests build, so afterEach can stop them. A query with a
// refetchInterval keeps a timer alive past its test, and vitest will not exit
// while one is pending.
const clients: QueryClient[] = [];

function withClient(children: () => JSX.Element) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(qc);
  return () => <QueryClientProvider client={qc}>{children()}</QueryClientProvider>;
}

afterEach(() => {
  for (const qc of clients) {
    qc.unmount();
    qc.clear();
  }
  clients.length = 0;
  notifiers.length = 0;
  closed.count = 0;
  vi.unstubAllGlobals();
});

describe("useView", () => {
  it("runs the view through the API and exposes the result", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(result("up")), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    let seen: ViewResult | undefined;
    render(
      withClient(() => {
        const q = useView("component-reachability");
        return <span>{(seen = q.data) ? "ready" : "loading"}</span>;
      }),
    );
    await waitFor(() => expect(seen?.rows?.[0]?.[0]).toBe("up"));
    const url = requestUrl(fetchMock, 0);
    expect(url).toContain("/views/component-reachability:run");
  });

  it("passes params into the request and re-runs when they change", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(result("up")), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const [owner, setOwner] = createSignal("disp-1");

    render(
      withClient(() => {
        const q = useView("sample-history", () => [`owner=${owner()}`]);
        return <span>{q.data ? "ready" : "loading"}</span>;
      }),
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(requestUrl(fetchMock, 0)).toContain("owner%3Ddisp-1");

    // A reactive param is the whole point of the accessor: changing it re-keys
    // the query, so the surface refetches with no imperative wiring.
    setOwner("cam-1");
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(requestUrl(fetchMock, 1)).toContain("owner%3Dcam-1");
  });

  it("ignores the connect baseline on a warm cache, then refetches on a real change", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify(result("up")), { status: 200, headers: { "Content-Type": "application/json" } }),
      )
      .mockResolvedValue(
        new Response(JSON.stringify(result("down")), { status: 200, headers: { "Content-Type": "application/json" } }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { unmount } = render(
      withClient(() => {
        const q = useView("component-reachability");
        return <span>{q.data ? String(q.data.rows?.[0]?.[0]) : "loading"}</span>;
      }),
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(notifiers.length).toBe(1);

    // The server opens every stream with a baseline change event. The query
    // already holds data newer than this connection, so acting on it would
    // double-fetch every view on mount and again on every stream renewal.
    notifiers[0]();
    await new Promise((r) => setTimeout(r, 30));
    expect(fetchMock).toHaveBeenCalledTimes(1);

    // A later notification is a real change, and the surface shows it with no
    // user action.
    notifiers[0]();
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(1));
    await waitFor(() => expect(document.body.textContent).toContain("down"));
    unmount();
  });

  it("shares one stream between surfaces watching the same view", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(result("up")), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    // Two widgets on one dashboard, same view and params. A browser allows only
    // a handful of connections per origin, so a stream each would starve the
    // page's ordinary requests.
    const { unmount } = render(
      withClient(() => {
        useView("component-reachability");
        useView("component-reachability");
        return <span>two widgets</span>;
      }),
    );
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(0));
    expect(notifiers.length).toBe(1);
    unmount();
    // The last watcher to leave closes it.
    expect(closed.count).toBe(1);
  });

  it("does not open a watch when watching is off, and polls instead", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(result("up")), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);
    render(
      withClient(() => {
        const q = useView("estate-counts", undefined, { watch: false, refetchIntervalMs: 20 });
        return <span>{q.data ? "ready" : "loading"}</span>;
      }),
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(notifiers.length).toBe(0);
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(1), { timeout: 1000 });
  });

  it("closes the watch when the surface unmounts, so a navigated-away page holds no stream", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(result("up")), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const { unmount } = render(
      withClient(() => {
        const q = useView("component-reachability");
        return <span>{q.data ? "ready" : "loading"}</span>;
      }),
    );
    await waitFor(() => expect(notifiers.length).toBe(1));
    unmount();
    expect(closed.count).toBe(1);
  });
});
