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
import type { SimProfile } from "../lib/simulation";

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

// Answer: a stubbed reply with a status other than 200.
class Answer {
  constructor(public status: number, public body: unknown) {}
}

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
    if (key in answers) {
      const a = answers[key];
      if (a instanceof Answer) return json(a.body, a.status);
      return json(a);
    }
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

// A principal who may remove a component but not impair one, and one who may add
// devices but not read places.
const remover: Me = { principal: { id: "u-rm", kind: "human" }, human: { username: "remover" }, permissions: ["*:read", "component:delete"], grants: [] };
const placeless: Me = {
  principal: { id: "u-np", kind: "human" }, human: { username: "placeless" },
  permissions: ["simulation:read", "simulation:update", "component:read", "component:create", "endpoint:create"], grants: [],
};

const profiles: SimProfile[] = [
  { name: "boreal-edge-86", manufacturer: "Boreal", model: "Edge 86", device_type: "display", products: ["boreal-edge-86", "boreal-edge-55"], protocols: ["http"], summary: "a display" },
];

function failNext(detail: string) {
  vi.mocked(globalThis.fetch).mockImplementation(async () =>
    new Response(JSON.stringify({ title: "Internal Server Error", status: 500, detail }), { status: 500, headers: { "Content-Type": "application/json" } }));
}

describe("the device drawer's writes", () => {
  it("keeps the operator's product pick when a poll lands with only hits changed", async () => {
    stubFetch();
    const { qc } = mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    await screen.findByTestId("sim-status");
    const sel = screen.getByLabelText("Product") as HTMLSelectElement;
    expect(sel.value).toBe("boreal-edge-55");
    fireEvent.change(sel, { target: { value: "boreal-edge-86" } });
    const change = screen.getByRole("button", { name: "Change" }) as HTMLButtonElement;
    await waitFor(() => expect(change.disabled).toBe(false));
    qc.setQueryData([...componentSimKey(DISPLAY)], { ...componentSim, device: device(DISPLAY, "display-1", 9) });
    await waitFor(() => expect(screen.getByTestId("sim-status").textContent).toContain("9"));
    expect(sel.value).toBe("boreal-edge-86");
    expect(change.disabled).toBe(false);
  });

  it("follows a product the server changed", async () => {
    stubFetch();
    const { qc } = mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    await screen.findByTestId("sim-status");
    qc.setQueryData([...componentSimKey(DISPLAY)], { ...componentSim, product: "boreal-edge-86" });
    await waitFor(() => expect((screen.getByLabelText("Product") as HTMLSelectElement).value).toBe("boreal-edge-86"));
  });

  it("changes the product with a PATCH by uuid", async () => {
    stubFetch({ [`PATCH /components/${DISPLAY}`]: components[0] });
    mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    await screen.findByTestId("sim-status");
    fireEvent.change(screen.getByLabelText("Product"), { target: { value: "boreal-edge-86" } });
    fireEvent.click(screen.getByRole("button", { name: "Change" }));
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true));
    const patch = calls.find((c) => c.method === "PATCH")!;
    expect(patch.path).toBe(`/components/${DISPLAY}`);
    expect(patch.body).toEqual({ product: "boreal-edge-86" });
  });

  it("repairs with a PUT of the all-zero impairment", async () => {
    stubFetch({ [`PUT /components/${DISPLAY}/impairment`]: { unplugged: false, latency_ms: 0, jitter_ms: 0, loss_pct: 0, rate_kbit: 0 } });
    const { qc } = mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    qc.setQueryData([...impairmentKey(DISPLAY)], { unplugged: true, latency_ms: 120, jitter_ms: 0, loss_pct: 0, rate_kbit: 0 });
    const repair = (await screen.findByRole("button", { name: "Repair" })) as HTMLButtonElement;
    await waitFor(() => expect(repair.disabled).toBe(false));
    fireEvent.click(repair);
    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true));
    const put = calls.find((c) => c.method === "PUT")!;
    expect(put.path).toBe(`/components/${DISPLAY}/impairment`);
    expect(put.body).toEqual({ unplugged: false, latency_ms: 0, jitter_ms: 0, loss_pct: 0, rate_kbit: 0 });
  });

  it("removes after a confirm: DELETE by uuid, the fleet invalidated, the drawer closed", async () => {
    stubFetch({ [`DELETE /components/${DISPLAY}`]: {} });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const onClose = vi.fn();
    const { qc } = mount(() => <SimDeviceDrawer id={DISPLAY} onClose={onClose} />);
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(confirm).toHaveBeenCalled();
    const del = calls.find((c) => c.method === "DELETE")!;
    expect(del.path).toBe(`/components/${DISPLAY}`);
    const keys = invalidate.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    expect(keys).toContain(JSON.stringify(SIMULATION_KEY));
    expect(keys).toContain(JSON.stringify(COMPONENTS_KEY));
    expect(keys).toContain(JSON.stringify(componentSimKey(DISPLAY)));
  });

  it("sends nothing when the remove is not confirmed", async () => {
    stubFetch();
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const onClose = vi.fn();
    mount(() => <SimDeviceDrawer id={DISPLAY} onClose={onClose} />);
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("gives a user who may remove but not impair a Remove-only rail", async () => {
    stubFetch();
    mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />, remover);
    expect(await screen.findByRole("button", { name: "Remove" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save impairment" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Repair" })).toBeNull();
    expect(screen.queryByLabelText("Latency (ms)")).toBeNull();
  });

  it("warns that the state is the last known one when a poll fails after a read", async () => {
    stubFetch();
    const { qc } = mount(() => <SimDeviceDrawer id={DISPLAY} onClose={() => {}} />);
    await screen.findByTestId("sim-status");
    failNext("simulator gone");
    await qc.refetchQueries({ queryKey: componentSimKey(DISPLAY) });
    const warn = await screen.findByTestId("sim-stale");
    expect(warn.textContent).toContain("simulator gone");
    expect(warn.textContent).toContain("last known");
    expect(screen.getByTestId("sim-status")).toBeTruthy();
  });
});

describe("the Simulation page's writes and stale state", () => {
  it("warns that the grid is the last known state when a poll fails after a read", async () => {
    current = { fleet: fleet([3, 0]) };
    stubFetch();
    const { qc } = mount(() => <Simulation />);
    await screen.findAllByTestId("sim-tile");
    failNext("connection refused");
    await qc.refetchQueries({ queryKey: SIMULATION_KEY, exact: true });
    const warn = await screen.findByTestId("sim-stale");
    expect(warn.textContent).toContain("connection refused");
    expect(warn.textContent).toContain("last known");
    expect(screen.getAllByTestId("sim-tile")).toHaveLength(2);
  });

  it("restarts a tile's flash when a second rise lands before the first ends", async () => {
    stubFetch();
    const { qc } = mount(() => <Simulation />);
    await screen.findAllByTestId("sim-tile");
    const reflow = vi.spyOn(HTMLElement.prototype, "offsetWidth", "get");
    qc.setQueryData([...SIMULATION_KEY], fleet([5, 0]));
    await waitFor(() => expect(screen.getAllByTestId("sim-tile")[0].classList.contains("og-blink")).toBe(true));
    const before = reflow.mock.calls.length;
    qc.setQueryData([...SIMULATION_KEY], fleet([6, 0]));
    await waitFor(() => expect(reflow.mock.calls.length).toBeGreaterThan(before));
    expect(screen.getAllByTestId("sim-tile")[0].classList.contains("og-blink")).toBe(true);
  });

  async function openAdd(me: Me = admin) {
    const r = mount(() => <Simulation />, me);
    r.qc.setQueryData([...SIM_PROFILES_KEY], profiles);
    r.qc.removeQueries({ queryKey: LOCATIONS_KEY });
    fireEvent.click(await screen.findByRole("button", { name: /Add simulated device/ }));
    return r;
  }

  it("adds a device with a POST to the node, then closes", async () => {
    stubFetch({ "POST /nodes/edge-hq/simulation/devices": { component: "lab-display-1", address: "127.60.0.4" } });
    await openAdd();
    fireEvent.change(await screen.findByLabelText("Product"), { target: { value: "boreal-edge-55" } });
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "lab-display-1" } });
    const add = screen.getByRole("button", { name: "Add device" }) as HTMLButtonElement;
    await waitFor(() => expect(add.disabled).toBe(false));
    fireEvent.click(add);
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    expect(calls.find((c) => c.method === "POST")!.body).toEqual({ name: "lab-display-1", product: "boreal-edge-55" });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Add device" })).toBeNull());
  });

  it("surfaces the server's refusal of an add and keeps the form open", async () => {
    stubFetch({ "POST /nodes/edge-hq/simulation/devices": new Answer(409, { title: "Conflict", status: 409, detail: "component lab-display-1 already exists" }) });
    await openAdd();
    fireEvent.change(await screen.findByLabelText("Product"), { target: { value: "boreal-edge-55" } });
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "lab-display-1" } });
    const add = screen.getByRole("button", { name: "Add device" }) as HTMLButtonElement;
    await waitFor(() => expect(add.disabled).toBe(false));
    fireEvent.click(add);
    expect((await screen.findByText(/already exists/)).closest("[role=alert]")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add device" })).toBeTruthy();
  });

  it("shows every field's hint when an incomplete form is submitted", async () => {
    current = { fleet: { connected: true, nodes: [fleet([0, 0]).nodes![0], { ...fleet([0, 0]).nodes![0], node: "edge-west" }] } };
    stubFetch();
    await openAdd();
    const name = await screen.findByLabelText("Name");
    expect(screen.queryByText("Name the new component.")).toBeNull();
    fireEvent.submit(name.closest("form")!);
    expect(await screen.findByText("Name the new component.")).toBeTruthy();
    expect(screen.getByText("Choose a simulated product.")).toBeTruthy();
    expect(screen.getByText("Choose the node whose simulator serves it.")).toBeTruthy();
    expect(calls.some((c) => c.method === "POST")).toBe(false);
  });

  it("offers no Location to a user who cannot read places, and never asks for them", async () => {
    stubFetch();
    await openAdd(placeless);
    await screen.findByLabelText("Name");
    expect(screen.queryByLabelText("Location")).toBeNull();
    expect(calls.some((c) => c.path.startsWith("/locations"))).toBe(false);
  });
});

describe("removing from the component page", () => {
  it("leaves for the Simulation page once the component is gone", async () => {
    stubFetch({ [`DELETE /components/${DISPLAY}`]: {} });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const qc = client(admin);
    window.history.pushState({}, "", `/components/${DISPLAY}`);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components/:id" component={() => <SimulationSection componentId={DISPLAY} />} />
          <Route path="/simulation" component={() => <p data-testid="sim-page">simulation page</p>} />
        </Router>
      </QueryClientProvider>
    ));
    const section = await screen.findByTestId("leaf-simulation");
    fireEvent.click(within(section).getByRole("button", { name: /Open device/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    expect(await screen.findByTestId("sim-page")).toBeTruthy();
    expect(window.location.pathname).toBe("/simulation");
  });
});
