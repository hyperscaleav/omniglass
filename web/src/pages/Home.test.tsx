import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Home from "./Home";
import { PLATFORM_VIEW_KEY, type PlatformView } from "../lib/platformhealth";
import { LOCATIONS_KEY } from "../lib/locations";
import { NODE_DOWN_AFTER_MS } from "../lib/nodes";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Home's Platform tile (#1644): the platform system's verdict and how many
// nodes are not up, opening Platform health, and shown only to a caller who
// can read nodes (the page's own door).
const readAt = "2026-11-09T12:00:00Z";
const ago = (ms: number) => new Date(Date.parse(readAt) - ms).toISOString();
const view: PlatformView = {
  read_at: readAt,
  lab: false,
  figures_read: true,
  system: { id: uuidFor("platform"), name: "omniglass-platform", verdict: "degraded" },
  servers: [],
  groups: [],
  loss_hops: ["node"],
  nodes: [
    { name: "edge-a", label: "", enrolled: true, last_heartbeat_at: ago(1000) },
    { name: "edge-b", label: "", enrolled: true, last_heartbeat_at: ago(NODE_DOWN_AFTER_MS + 1) },
    { name: "edge-c", label: "", enrolled: false },
  ],
};
const meWith = (permissions: string[]): Me => ({ principal: { id: "p", kind: "human" }, human: { username: "ada" }, permissions, grants: [] });

// A refused platform view (null) mounts on the production retry default, so
// only the tile's own retry policy can surface the refusal promptly.
function mount(me: Me, platform: PlatformView | null = view) {
  const qc = new QueryClient({ defaultOptions: { queries: platform ? { staleTime: Infinity, retry: false } : { staleTime: Infinity } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...LOCATIONS_KEY], []);
  if (platform) qc.setQueryData([...PLATFORM_VIEW_KEY], platform);
  let path = () => "";
  const Probe = () => {
    const loc = useLocation();
    path = () => loc.pathname;
    return <Home />;
  };
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={Probe} />
      </Router>
    </QueryClientProvider>
  ));
  return { path: () => path() };
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  window.history.pushState({}, "", "/");
});

describe("Home Platform tile", () => {
  it("shows the platform verdict and the nodes not up, and opens Platform health", async () => {
    const { path } = mount(meWith(["node:read", "system:read"]));
    const tile = screen.getByTestId("home-platform");
    expect(tile).toHaveTextContent("degraded");
    expect(tile).toHaveTextContent("2 nodes not up");
    fireEvent.click(tile);
    await waitFor(() => expect(path()).toBe("/platform"));
  });

  it("is absent for a caller without node:read", () => {
    mount(meWith(["location:read"]));
    expect(screen.queryByTestId("home-platform")).toBeNull();
    expect(screen.queryByText("Collectors")).toBeNull();
  });

  // node:read held only for some places is refused by the view: the tile says
  // which read it needs rather than reading the platform forever.
  it("on a refusal reads that it needs a fleet-wide node read", async () => {
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : String(input);
      const refused = url.includes("/views/platform");
      return Promise.resolve(new Response(refused ? JSON.stringify({ title: "Forbidden", status: 403, detail: "forbidden" }) : "[]", {
        status: refused ? 403 : 200,
        headers: { "content-type": "application/json" },
      }));
    }));
    mount(meWith(["node:read"]), null);
    const tile = screen.getByTestId("home-platform");
    await waitFor(() => expect(tile).toHaveTextContent("needs a fleet-wide node read"));
    expect(tile).not.toHaveTextContent("reading the platform");
  });
});
