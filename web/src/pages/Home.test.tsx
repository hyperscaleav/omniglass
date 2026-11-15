import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Home from "./Home";
import { PLATFORM_VIEW_KEY, type PlatformView } from "../lib/platformhealth";
import { LOCATIONS_KEY } from "../lib/locations";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { NODE_DOWN_AFTER_MS, NODES_KEY, type Node } from "../lib/nodes";
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

const fleet = (openAlarms?: number): FleetView => ({
  locations: [],
  systems: ["hq-huddle", "depot-huddle", "lab-rack"].map((name) => ({ id: uuidFor(name), name, label: "", verdict: "healthy", dots: [] })),
  ...(openAlarms === undefined ? {} : { open_alarms: openAlarms }),
});
const nodeRows: Node[] = ["edge-a", "edge-b"].map((name) => ({ name, enrolled: true, tags: {} }));

// A refused platform view (null) mounts on the production retry default, so
// only the tile's own retry policy can surface the refusal promptly.
function mount(me: Me, platform: PlatformView | null = view, fleetData: FleetView | null = fleet(4)) {
  const qc = new QueryClient({ defaultOptions: { queries: platform ? { staleTime: Infinity, retry: false } : { staleTime: Infinity } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...NODES_KEY], nodeRows);
  if (fleetData) qc.setQueryData([...FLEET_VIEW_KEY], fleetData);
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

// The count tiles (#983): Open alarms, Systems and Collectors read the scoped
// counts the console already has, and a tile that cannot say so reads an honest
// state, never a dash under "collection pending".
describe("Home count tiles", () => {
  it("show the seeded counts and none reads as collection pending", async () => {
    mount(meWith(["location:read", "node:read", "system:read"]));
    await waitFor(() => expect(screen.getByTestId("home-open-alarms")).toHaveTextContent("4"));
    expect(screen.getByTestId("home-systems")).toHaveTextContent("3");
    expect(screen.getByTestId("home-collectors")).toHaveTextContent("2");
    expect(screen.queryByText(/collection pending/)).toBeNull();
  });

  it("opens the Systems and Collectors lists on click", async () => {
    const { path } = mount(meWith(["location:read", "node:read"]));
    fireEvent.click(await screen.findByRole("button", { name: /Systems/ }));
    await waitFor(() => expect(path()).toBe("/systems"));
    fireEvent.click(await screen.findByRole("button", { name: /Collectors/ }));
    await waitFor(() => expect(path()).toBe("/nodes"));
  });

  // /alarms is a stub page until the alarm list lands, so the tile is not a link.
  it("leaves Open alarms a plain figure, not a button", async () => {
    mount(meWith(["location:read"]));
    await waitFor(() => expect(screen.getByTestId("home-open-alarms")).toHaveTextContent("4"));
    expect(screen.getByTestId("home-open-alarms").tagName).toBe("DIV");
  });

  it("says why the alarm count is missing when the view carries none", async () => {
    mount(meWith(["location:read"]), view, fleet());
    await waitFor(() => expect(screen.getByTestId("home-open-alarms")).toHaveTextContent("needs a component read"));
    expect(screen.getByTestId("home-open-alarms")).not.toHaveTextContent("0");
  });

  it("reads an error, not a dash, when the fleet view cannot be read", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({ title: "Boom", status: 500 }), {
      status: 500,
      headers: { "content-type": "application/json" },
    }))));
    mount(meWith(["location:read"]), view, null);
    await waitFor(() => expect(screen.getByTestId("home-systems")).toHaveTextContent("could not be read"));
    expect(screen.getByTestId("home-open-alarms")).toHaveTextContent("could not be read");
  });
});
