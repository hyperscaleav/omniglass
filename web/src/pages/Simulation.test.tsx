import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Simulation from "./Simulation";
import Sidebar from "../components/Sidebar";
import SimDeviceDrawer from "../components/SimDeviceDrawer";
import SimulationSection from "../components/SimulationSection";
import { ME_KEY, type Me } from "../lib/auth";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { LOCATIONS_KEY } from "../lib/locations";
import {
  SIMULATION_KEY,
  SIM_PROFILES_KEY,
  componentSimKey,
  impairmentKey,
  type ComponentSim,
  type SimDevice,
  type SimFleet,
} from "../lib/simulation";
import { uuidFor } from "../lib/testids";

// The simulation console (#890): the sidebar door that follows a connected
// simulator, the live grid and its flash, the device drawer's fire and
// impairment paths, the component page's section, and what a reader without
// simulation:update is shown. The query cache is seeded directly; fetch is
// stubbed so the page's polls read the same fixtures back, and each write's
// request is recorded for the assertions.

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer-hq" }, permissions: ["*:read"], grants: [] };

const DISPLAY = uuidFor("sim-display");
const DSP = uuidFor("sim-dsp");

const device = (id: string, name: string, hits: number, extra: Partial<SimDevice> = {}): SimDevice => ({
  component_id: id, component: name, product: "boreal-edge-55", address: "127.60.0.2", serving: true,
  hits, hits_by_protocol: hits ? { snmp: hits } : {}, impaired: false, last_protocol: hits ? "snmp" : undefined, ...extra,
});

const fleet = (hits: [number, number], connected = true): SimFleet => ({
  connected,
  nodes: [{
    node: "edge-hq", connected, last_seen: new Date().toISOString(),
    capabilities: { low_ports: false, net_admin: false, port_offset: 10000 },
    impairments: { available: false, reason: "impairments are off; run the simulator with --impairments" },
    devices: [
      device(DISPLAY, "display-1", hits[0]),
      device(DSP, "dsp-1", hits[1], { address: "127.60.0.3", product: "polaris-dsp-8", impaired: true, impairment: { unplugged: true } }),
    ],
  }],
});

const components: Component[] = [
  { id: DISPLAY, name: "display-1", label: "Display 1", label_generated: true, location: "boardroom-a", system_count: 1, product: "boreal-edge-55" },
  { id: DSP, name: "dsp-1", label: "DSP 1", label_generated: true, location: "boardroom-a", system_count: 1, product: "polaris-dsp-8" },
];

const componentSim: ComponentSim = {
  simulated: true, node: "edge-hq", connected: true, address: "127.60.0.2", product: "boreal-edge-55", profile: "boreal-edge-86",
  summary: "Boreal Edge 86-inch 4K commercial display", variants: ["boreal-edge-86", "boreal-edge-55"],
  device: device(DISPLAY, "display-1", 7),
  impairments: { available: false, reason: "impairments are off; run the simulator with --impairments" },
  surface: [
    { name: "web-admin", protocol: "http", port: 10080, summary: "Web management", fireable: true, routes: [{ method: "GET", path: "/api/status", summary: "Device identity" }] },
    { name: "snmp", protocol: "snmp", port: 10161, fireable: true, oids: [{ oid: "1.3.6.1.2.1.1.3.0", path: "status.device.uptime" }] },
    { name: "icmp", protocol: "icmp", port: 0, fireable: false },
  ],
};

type Call = { method: string; path: string; body: unknown };
let calls: Call[] = [];
let current: { fleet: SimFleet } = { fleet: fleet([0, 0]) };

function stubFetch(answers: Record<string, unknown> = {}) {
  calls = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    const url = new URL(req.url);
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const text = req.method === "GET" ? "" : await req.text();
    calls.push({ method: req.method, path, body: text ? JSON.parse(text) : undefined });
    const key = `${req.method} ${path}`;
    const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
    if (key in answers) return json(answers[key]);
    if (key === "GET /simulation") return json(current.fleet);
    if (path.endsWith("/simulation") && req.method === "GET") return json(componentSim);
    return json({ title: "Not Found", status: 404, detail: "unexpected request" }, 404);
  });
}

function client(me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SIMULATION_KEY], current.fleet);
  qc.setQueryData([...COMPONENTS_KEY], components);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...SIM_PROFILES_KEY], []);
  qc.setQueryData([...componentSimKey(DISPLAY)], componentSim);
  qc.setQueryData([...impairmentKey(DISPLAY)], { unplugged: false, latency_ms: 0, jitter_ms: 0, loss_pct: 0, rate_kbit: 0 });
  return qc;
}

function mount(node: () => import("solid-js").JSX.Element, me: Me = admin) {
  const qc = client(me);
  window.history.pushState({}, "", "/");
  const r = render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={node} />
      </Router>
    </QueryClientProvider>
  ));
  return { qc, ...r };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  current = { fleet: fleet([0, 0]) };
});

describe("the Simulation sidebar door", () => {
  it("is hidden while no simulator is connected", async () => {
    current = { fleet: fleet([0, 0], false) };
    stubFetch();
    mount(() => <Sidebar collapsed={false} onToggle={() => {}} />);
    expect(await screen.findByText("Nodes")).toBeTruthy();
    expect(screen.queryByText("Simulation")).toBeNull();
  });

  it("shows once a simulator is connected", async () => {
    stubFetch();
    mount(() => <Sidebar collapsed={false} onToggle={() => {}} />);
    const link = (await screen.findByText("Simulation")).closest("a")!;
    expect(link.getAttribute("href")).toContain("/simulation");
  });
});

describe("the Simulation page", () => {
  it("renders the node card and a tile per device, the impaired one marked", async () => {
    current = { fleet: fleet([3, 0]) };
    stubFetch();
    mount(() => <Simulation />);
    const tiles = await screen.findAllByTestId("sim-tile");
    expect(tiles).toHaveLength(2);
    expect(within(tiles[0]).getByText("Display 1")).toBeTruthy();
    expect(within(tiles[0]).getByTestId("tile-hits").textContent).toBe("3");
    expect(within(tiles[1]).getByTestId("impaired-marker").textContent).toContain("unplugged");
    expect(screen.getByTestId("node-hits").textContent).toBe("3");
    expect(screen.getByText("unavailable")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Add simulated device/ })).toBeTruthy();
  });

  it("flashes a tile when its hit count rises between reads, and only that tile", async () => {
    stubFetch();
    const { qc } = mount(() => <Simulation />);
    const tiles = await screen.findAllByTestId("sim-tile");
    expect(tiles.some((t) => t.classList.contains("og-blink"))).toBe(false);
    current = { fleet: fleet([5, 0]) };
    qc.setQueryData([...SIMULATION_KEY], current.fleet);
    await waitFor(() => expect(screen.getAllByTestId("sim-tile")[0].classList.contains("og-blink")).toBe(true));
    expect(screen.getAllByTestId("sim-tile")[1].classList.contains("og-blink")).toBe(false);
    // The same element, not a rebuilt one: a poll must not recreate the tiles.
    expect(screen.getAllByTestId("sim-tile")[0]).toBe(tiles[0]);
  });

  it("teaches and offers the add path when a simulator serves nothing yet", async () => {
    current = { fleet: { connected: true, nodes: [{ ...fleet([0, 0]).nodes![0], devices: [] }] } };
    stubFetch();
    mount(() => <Simulation />);
    const empty = await screen.findByTestId("sim-empty");
    expect(within(empty).getByText("No simulated devices yet")).toBeTruthy();
    expect(within(empty).getByRole("button", { name: /Add simulated device/ })).toBeTruthy();
  });

  it("alerts with a retry when the fleet cannot be read", async () => {
    stubFetch({});
    vi.mocked(globalThis.fetch).mockImplementation(async () => new Response(JSON.stringify({ title: "Internal Server Error", status: 500, detail: "boom" }), { status: 500, headers: { "Content-Type": "application/json" } }));
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], admin);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router><Route path="*" component={Simulation} /></Router>
      </QueryClientProvider>
    ));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("boom");
    expect(within(alert).getByRole("button", { name: "Retry" })).toBeTruthy();
  });

  it("hides Add from a reader without simulation:update", async () => {
    stubFetch();
    mount(() => <Simulation />, viewer);
    await screen.findAllByTestId("sim-tile");
    expect(screen.queryByRole("button", { name: /Add simulated device/ })).toBeNull();
  });
});

describe("the simulated device drawer", () => {
  it("fires a chosen operation and renders the device's answer", async () => {
    stubFetch({ [`POST /components/${DISPLAY}/simulation:fire`]: { status: 200, body: "{\"device\":{\"model\":\"Edge 55\"}}", took_ms: 3 } });
    mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: /\/api\/status/ }));
    const panel = await screen.findByTestId("fire-panel");
    fireEvent.click(within(panel).getByRole("button", { name: "Fire" }));
    const answer = await screen.findByTestId("fire-answer");
    expect(answer.textContent).toContain("Edge 55");
    expect(answer.textContent).toContain("200");
    expect(answer.textContent).toContain("3 ms");
    const fired = calls.find((c) => c.path.endsWith("simulation:fire"));
    expect(fired?.body).toEqual({ surface: "web-admin", method: "GET", path: "/api/status" });
  });

  it("saves the whole impairment, the fields left alone sent as zero", async () => {
    const saved = { unplugged: false, latency_ms: 120, jitter_ms: 10, loss_pct: 0, rate_kbit: 0 };
    stubFetch({ [`PUT /components/${DISPLAY}/impairment`]: saved });
    mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    const latency = await screen.findByLabelText("Latency (ms)");
    fireEvent.input(latency, { target: { value: "120" } });
    fireEvent.input(screen.getByLabelText("Jitter (ms)"), { target: { value: "10" } });
    // Impairments are off on this simulator: the setting saves, it just will not apply.
    expect(screen.getByTestId("impairments-unavailable").textContent).toContain("do not apply");
    const save = screen.getByRole("button", { name: "Save impairment" });
    await waitFor(() => expect((save as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(save);
    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true));
    expect(calls.find((c) => c.method === "PUT")!.body).toEqual(saved);
  });

  it("refuses jitter above latency before it is sent", async () => {
    stubFetch();
    mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    fireEvent.input(await screen.findByLabelText("Jitter (ms)"), { target: { value: "50" } });
    expect(await screen.findByText("At most the latency.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Save impairment" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("gives a reader the facts and no controls", async () => {
    stubFetch();
    mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />, viewer);
    await screen.findByTestId("sim-impairment");
    expect(screen.queryByRole("button", { name: "Save impairment" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Fire/ })).toBeNull();
    expect(screen.queryByLabelText("Latency (ms)")).toBeNull();
    expect(screen.queryByRole("button", { name: "Change" })).toBeNull();
    // The surface still reads, as plain rows.
    expect(screen.getByText("/api/status")).toBeTruthy();
  });
});

describe("the component page's Simulation section", () => {
  it("renders nothing for a component that is not simulated", async () => {
    stubFetch();
    const { qc } = mount(() => <SimulationSection componentId={DSP} />);
    qc.setQueryData([...componentSimKey(DSP)], { simulated: false, connected: false, impairments: { available: false } });
    await waitFor(() => expect(qc.getQueryData([...componentSimKey(DSP)])).toBeTruthy());
    expect(screen.queryByTestId("leaf-simulation")).toBeNull();
  });

  it("says where a simulated component is served and opens its drawer", async () => {
    stubFetch();
    mount(() => <SimulationSection componentId={DISPLAY} />);
    const section = await screen.findByTestId("leaf-simulation");
    expect(section.textContent).toContain("127.60.0.2");
    expect(section.textContent).toContain("edge-hq");
    fireEvent.click(within(section).getByRole("button", { name: /Open device/ }));
    expect(await screen.findByTestId("sim-status")).toBeTruthy();
  });
});
