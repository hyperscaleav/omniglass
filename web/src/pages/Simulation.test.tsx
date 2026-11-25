import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Simulation from "./Simulation";
import Sidebar from "../components/Sidebar";
import SimComponentDrawer, { chooseDrawerTab } from "../components/SimComponentDrawer";
import SimulationSection from "../components/SimulationSection";
import { SimFaultChipFor } from "../components/SimFaultChip";
import { ME_KEY, type Me } from "../lib/auth";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { LOCATIONS_KEY } from "../lib/locations";
import { pickTree } from "../components/TreeSelect.testkit";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { SYSTEM_TYPES_KEY, type SystemType } from "../lib/system_types";
import {
  SIMULATION_KEY,
  SIM_PROFILES_KEY,
  componentSimKey,
  impairmentKey,
  simPlanKey,
  type ComponentSim,
  type SimSystemPlan,
  type SimComponent,
  type SimFleet,
} from "../lib/simulation";
import { uuidFor } from "../lib/testids";
import { pickInPicker } from "../components/ui/Picker.testkit";
import type { SimProfile } from "../lib/simulation";
import { answerConfirms } from "../lib/confirm.testkit";
import { tipOf } from "../test-tip";

// The simulation console (#890): the sidebar door that follows a connected
// simulator, the live table and its highlight, the simulated component drawer's fire and
// impairment paths, the component page's section, and what a reader without
// simulation:update is shown. The query cache is seeded directly; fetch is
// stubbed so the page's polls read the same fixtures back, and each write's
// request is recorded for the assertions.

const admin: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { user: { id: "u-view", kind: "person" }, person: { username: "viewer-hq" }, permissions: ["*:read"], grants: [] };

const DISPLAY = uuidFor("sim-display");
const EP1 = uuidFor("sim-ep-1");
const DSP = uuidFor("sim-dsp");

const simRow = (id: string, name: string, hits: number, extra: Partial<SimComponent> = {}): SimComponent => ({
  component_id: id, component: name, product: "boreal-edge-55", address: "127.60.0.2", serving: true, status: "running",
  hits, hits_by_protocol: hits ? { snmp: hits } : {}, impaired: false, last_protocol: hits ? "snmp" : undefined, ...extra,
});

const fleet = (hits: [number, number], connected = true): SimFleet => ({
  connected,
  outposts: [{
    outpost: "edge-hq", connected, last_seen: new Date().toISOString(),
    capabilities: { low_ports: false, net_admin: false, port_offset: 10000 },
    impairments: { available: false, reason: "impairments are off; run the simulator with --impairments" },
    components: [
      simRow(DISPLAY, "display-1", hits[0]),
      simRow(DSP, "dsp-1", hits[1], { address: "127.60.0.3", product: "polaris-dsp-8", impaired: true, impairment: { unplugged: true } }),
    ],
  }],
});

const components: Component[] = [
  { id: DISPLAY, name: "display-1", label: "Display 1", label_generated: true, location: "boardroom-a", system_count: 1, product: "boreal-edge-55" },
  { id: DSP, name: "dsp-1", label: "DSP 1", label_generated: true, location: "boardroom-a", system_count: 1, product: "polaris-dsp-8" },
];

const componentSim: ComponentSim = {
  simulated: true, outpost: "edge-hq", connected: true, address: "127.60.0.2", product: "boreal-edge-55", profile: "boreal-edge-86",
  summary: "Boreal Edge 86-inch 4K commercial display", variants: ["boreal-edge-86", "boreal-edge-55"],
  activity: simRow(DISPLAY, "display-1", 7),
  impairments: { available: false, reason: "impairments are off; run the simulator with --impairments" },
  surface: [
    { name: "web-admin", protocol: "http", port: 10080, summary: "Web management", fireable: true, routes: [{ method: "GET", path: "/api/status", summary: "Identity" }] },
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
      if (typeof a === "function") return (a as () => Promise<Response>)();
      if (a instanceof Answer) return json(a.body, a.status);
      return json(a);
    }
    if (key === "GET /simulation") return json(current.fleet);
    if (key === "GET /endpoints") return json({ endpoints: [] });
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

function mount(node: () => import("solid-js").JSX.Element, me: Me = admin, url = "/") {
  const qc = client(me);
  window.history.pushState({}, "", url);
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
  chooseDrawerTab("info");
  vi.restoreAllMocks();
  current = { fleet: fleet([0, 0]) };
});

describe("the Simulation sidebar door", () => {
  it("is hidden while no simulator is connected", async () => {
    current = { fleet: fleet([0, 0], false) };
    stubFetch();
    mount(() => <Sidebar collapsed={false} onToggle={() => {}} />);
    expect(await screen.findByText("Outposts")).toBeTruthy();
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
  it("renders the outpost card and a table row per simulated component, the impaired one marked", async () => {
    current = { fleet: fleet([3, 0]) };
    stubFetch();
    mount(() => <Simulation />);
    const rows = await screen.findAllByTestId("sim-row");
    expect(rows).toHaveLength(2);
    expect(rows[0].tagName).toBe("TR");
    expect(within(rows[0]).getByText("Display 1")).toBeTruthy();
    expect(within(rows[0]).getByText("boardroom-a")).toBeTruthy();
    expect(within(rows[0]).getByText("127.60.0.2")).toBeTruthy();
    expect(within(rows[0]).getByTestId("row-hits").textContent).toBe("3");
    // One Status column: each row's state, the impaired one with its impairment beside it.
    const headers = within(screen.getByTestId("sim-table")).getAllByRole("columnheader").map((h) => h.textContent);
    expect(headers).toContain("Status");
    expect(headers).not.toContain("Serving");
    expect(headers).not.toContain("Impairment");
    expect(within(rows[0]).getByTestId("sim-state").textContent).toBe("running");
    expect(within(rows[0]).queryByTestId("sim-fault-chip")).toBeNull();
    expect(within(rows[1]).getByTestId("sim-state").textContent).toBe("running");
    expect(within(rows[1]).getByTestId("sim-fault-label").textContent).toBe("unplugged");
    expect(screen.getByTestId("outpost-hits").textContent).toBe("3");
    expect(screen.getByText("unavailable")).toBeTruthy();
    // One outpost: no Outpost column.
    expect(within(screen.getByTestId("sim-table")).queryByRole("columnheader", { name: "Outpost" })).toBeNull();
    expect(screen.getByRole("button", { name: /Add simulated component/ })).toBeTruthy();
  });

  it("shows a failed and a starting component by their state, the impairment set on the starting one marked not applied", async () => {
    const f = fleet([0, 0]);
    f.outposts![0].components = [
      simRow(DISPLAY, "display-1", 0, { status: "failed", serving: false }),
      simRow(DSP, "dsp-1", 0, { status: "starting", serving: false, impairment: { latency_ms: 150, loss_pct: 2.5 } }),
    ];
    current = { fleet: f };
    stubFetch();
    mount(() => <Simulation />);
    const rows = await screen.findAllByTestId("sim-row");
    expect(within(rows[0]).getByTestId("sim-state").textContent).toBe("failed");
    expect(within(rows[1]).getByTestId("sim-state").textContent).toBe("starting");
    expect(within(rows[1]).getByTestId("sim-fault-label").textContent).toBe("150 ms, 2.5% loss");
    expect(within(rows[1]).getByTestId("sim-not-applied")).toBeTruthy();
  });

  it("filters by state, and by the impairment's words", async () => {
    const f = fleet([0, 0]);
    f.outposts![0].components = [
      simRow(DISPLAY, "display-1", 0, { status: "failed", serving: false }),
      simRow(DSP, "dsp-1", 0, { impaired: true, impairment: { latency_ms: 150, loss_pct: 2.5 } }),
    ];
    current = { fleet: f };
    stubFetch();
    mount(() => <Simulation />);
    await screen.findAllByTestId("sim-row");
    const filter = (text: string) => {
      const input = screen.getByRole("combobox") as HTMLInputElement;
      fireEvent.input(input, { target: { value: text } });
      fireEvent.keyDown(input, { key: "Enter" });
    };
    filter("status:failed");
    await waitFor(() => expect(screen.getAllByTestId("sim-row").map((r) => r.dataset.component)).toEqual([DISPLAY]));
    fireEvent.click(screen.getByRole("button", { name: "Clear" }));
    // The filter lives in the address now, which settles a tick after the click.
    await waitFor(() => expect(screen.getAllByTestId("sim-row")).toHaveLength(2));
    filter("impairment:loss");
    await waitFor(() => expect(screen.getAllByTestId("sim-row").map((r) => r.dataset.component)).toEqual([DSP]));
  });

  it("adds an Outpost column when more than one outpost serves", async () => {
    const west = { ...fleet([0, 0]).outposts![0], outpost: "edge-west", components: [simRow(uuidFor("sim-west"), "display-9", 0)] };
    current = { fleet: { connected: true, outposts: [fleet([0, 0]).outposts![0], west] } };
    stubFetch();
    mount(() => <Simulation />);
    const rows = await screen.findAllByTestId("sim-row");
    expect(rows).toHaveLength(3);
    expect(within(screen.getByTestId("sim-table")).getByRole("columnheader", { name: "Outpost" })).toBeTruthy();
    expect(screen.getAllByTestId("sim-outpost")).toHaveLength(2);
  });

  it("highlights a row when its hit count rises between reads, only that row, and not on first load", async () => {
    stubFetch();
    const { qc } = mount(() => <Simulation />);
    const rows = await screen.findAllByTestId("sim-row");
    expect(rows.some((t) => t.classList.contains("og-blink"))).toBe(false);
    current = { fleet: fleet([5, 0]) };
    qc.setQueryData([...SIMULATION_KEY], current.fleet);
    await waitFor(() => expect(screen.getAllByTestId("sim-row")[0].classList.contains("og-blink")).toBe(true));
    expect(screen.getAllByTestId("sim-row")[1].classList.contains("og-blink")).toBe(false);
  });

  it("keeps the same row elements across polls", async () => {
    stubFetch();
    const { qc } = mount(() => <Simulation />);
    const rows = await screen.findAllByTestId("sim-row");
    qc.setQueryData([...SIMULATION_KEY], fleet([5, 2]));
    await waitFor(() => expect(within(screen.getAllByTestId("sim-row")[1]).getByTestId("row-hits").textContent).toBe("2"));
    const after = screen.getAllByTestId("sim-row");
    expect(after[0]).toBe(rows[0]);
    expect(after[1]).toBe(rows[1]);
  });

  it("opens the simulated component drawer on a row click", async () => {
    stubFetch();
    mount(() => <Simulation />);
    const rows = await screen.findAllByTestId("sim-row");
    fireEvent.click(rows[0]);
    expect(await screen.findByTestId("sim-status")).toBeTruthy();
  });

  it("opens the drawer a link names in its ?c= parameter", async () => {
    stubFetch();
    mount(() => <Simulation />, admin, `/simulation?c=${DISPLAY}`);
    expect(await screen.findByTestId("sim-status")).toBeTruthy();
  });

  // Platform health's device link names the tab it is for (#2019): impairing a
  // device is what it opens the drawer to do, and the drawer opens on Info, so a
  // link that carries ?tab= lands on it with no extra step.
  it("lands on the tab a link names in its ?tab= parameter", async () => {
    sessionStorage.removeItem("og-sim-drawer-tab");
    stubFetch();
    mount(() => <Simulation />, admin, `/simulation?c=${DISPLAY}&tab=impairments`);
    const tab = await screen.findByRole("tab", { name: "Impairments" });
    expect(tab.getAttribute("aria-selected")).toBe("true");
  });

  // The tab is a one-shot hint: once applied it leaves the address, so a later
  // device opened by hand follows the user's own tab, not the stale link's (#2019).
  it("takes ?tab= out of the address once it has landed on that tab", async () => {
    sessionStorage.removeItem("og-sim-drawer-tab");
    stubFetch();
    mount(() => <Simulation />, admin, `/simulation?c=${DISPLAY}&tab=impairments`);
    const tab = await screen.findByRole("tab", { name: "Impairments" });
    expect(tab.getAttribute("aria-selected")).toBe("true");
    await waitFor(() => expect(window.location.search).not.toContain("tab="));
    expect(window.location.search).toContain(`c=${DISPLAY}`);
  });

  it("does not force the tab again when another device is opened afterwards", async () => {
    sessionStorage.removeItem("og-sim-drawer-tab");
    stubFetch();
    mount(() => <Simulation />, admin, `/simulation?c=${DISPLAY}&tab=impairments`);
    expect((await screen.findByRole("tab", { name: "Impairments" })).getAttribute("aria-selected")).toBe("true");
    fireEvent.click(screen.getByRole("tab", { name: "Info" }));
    expect(screen.getByRole("tab", { name: "Info" }).getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(document.body, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("tab", { name: "Info" })).toBeNull());
    const rows = await screen.findAllByTestId("sim-row");
    fireEvent.click(rows.find((r) => r.dataset.component === DSP)!);
    const info = await screen.findByRole("tab", { name: "Info" });
    expect(info.getAttribute("aria-selected")).toBe("true");
  });

  it("ignores a ?tab= that names no tab, and opens on the drawer's own", async () => {
    sessionStorage.removeItem("og-sim-drawer-tab");
    stubFetch();
    mount(() => <Simulation />, admin, `/simulation?c=${DISPLAY}&tab=nonsense`);
    const info = await screen.findByRole("tab", { name: "Info" });
    expect(info.getAttribute("aria-selected")).toBe("true");
  });

  it("opens the drawer from the keyboard: a focused row and Enter", async () => {
    stubFetch();
    mount(() => <Simulation />);
    const rows = await screen.findAllByTestId("sim-row");
    expect(rows[0].getAttribute("tabindex")).toBe("0");
    fireEvent.keyDown(rows[0], { key: "Enter" });
    expect(await screen.findByTestId("sim-status")).toBeTruthy();
  });

  it("teaches and offers the add path when a simulator serves nothing yet", async () => {
    current = { fleet: { connected: true, outposts: [{ ...fleet([0, 0]).outposts![0], components: [] }] } };
    stubFetch();
    mount(() => <Simulation />);
    const empty = await screen.findByTestId("sim-empty");
    expect(within(empty).getByText("No simulated components yet")).toBeTruthy();
    expect(within(empty).getByRole("button", { name: /Add simulated component/ })).toBeTruthy();
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
    await screen.findAllByTestId("sim-row");
    expect(screen.queryByRole("button", { name: /Add simulated component/ })).toBeNull();
  });
});

describe("the simulated component drawer", () => {
  it("fires a chosen operation and renders the component's answer", async () => {
    stubFetch({ [`POST /components/${DISPLAY}/simulation:fire`]: { status: 200, body: "{\"device\":{\"model\":\"Edge 55\"}}", took_ms: 3 } });
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    await openTab("Operations");
    fireEvent.click(await screen.findByRole("button", { name: /\/api\/status/ }));
    const panel = await screen.findByTestId("op-detail");
    fireEvent.click(within(panel).getByRole("button", { name: "Send" }));
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
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    await openTab("Impairments");
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
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    await openTab("Impairments");
    fireEvent.input(await screen.findByLabelText("Jitter (ms)"), { target: { value: "50" } });
    expect(await screen.findByText("At most the latency.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Save impairment" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("gives a reader the facts and no controls", async () => {
    stubFetch();
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />, viewer);
    await openTab("Impairments");
    await screen.findByTestId("sim-impairment");
    expect(screen.queryByRole("button", { name: "Save impairment" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Send/ })).toBeNull();
    expect(screen.queryByLabelText("Latency (ms)")).toBeNull();
    expect(screen.queryByRole("button", { name: "Change" })).toBeNull();
    // The surface still reads, as plain rows.
    await openTab("Operations");
    expect(await screen.findByText("/api/status")).toBeTruthy();
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

  // An outpost, an address or a hit count in monospace sits on its label's
  // baseline, not above it (#1379's review).
  it("aligns each simulation value on its label's baseline", async () => {
    stubFetch();
    mount(() => <SimulationSection componentId={DISPLAY} />);
    const section = await screen.findByTestId("leaf-simulation");
    expect(section.querySelector("dl")?.classList.contains("items-baseline")).toBe(true);
  });

  // A component aimed at a simulated device is served by that simulation, and
  // its card says so beside the device's own address and status (#2461).
  it("says which simulated component serves a component aimed at its address", async () => {
    stubFetch();
    const { qc } = mount(() => <SimulationSection componentId={DSP} />);
    qc.setQueryData([...componentSimKey(DSP)], { ...componentSim, served_by: "display-1", served_by_id: DISPLAY });
    const section = await screen.findByTestId("leaf-simulation");
    expect(section.textContent).toContain("Served by");
    expect(section.textContent).toContain("display-1");
    // The drawer opens by the owner's uuid, not its (repeatable) name.
    fireEvent.click(within(section).getByRole("button", { name: /Open simulation/ }));
    const drawer = await screen.findByRole("dialog");
    expect(drawer.textContent).toContain("127.60.0.2");
  });

  it("says what was already removed when the component delete fails after its endpoints went", async () => {
    stubFetch({
      "GET /endpoints": { endpoints: [{ id: EP1, name: "snmp", transport: "udp", component_id: DISPLAY }] },
      [`DELETE /endpoints/${EP1}`]: {},
      [`DELETE /components/${DISPLAY}`]: new Answer(409, { title: "Conflict", status: 409, detail: "still referenced" }),
    });
    answerConfirms(true);
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    expect((await screen.findByText(/already removed: its snmp endpoint/)).textContent).toContain("the component remains");
  });

  it("says where a simulated component is served and opens its drawer", async () => {
    stubFetch();
    mount(() => <SimulationSection componentId={DISPLAY} />);
    const section = await screen.findByTestId("leaf-simulation");
    expect(section.textContent).toContain("127.60.0.2");
    expect(section.textContent).toContain("edge-hq");
    // One Status row carries the state and any impairment; no Serving or Impairment rows.
    expect(within(section).getByTestId("sim-state").textContent).toBe("running");
    const terms = [...section.querySelectorAll("dt")].map((d) => d.textContent);
    expect(terms).toContain("Status");
    expect(terms).not.toContain("Serving");
    expect(terms).not.toContain("Impairment");
    fireEvent.click(within(section).getByRole("button", { name: /Open simulation/ }));
    const status = await screen.findByTestId("sim-status");
    expect(within(status).getByTestId("sim-state").textContent).toBe("running");
    expect(status.textContent).not.toContain("Serving");
  });
});

// A user who may remove a component but not impair one, and one who may add
// simulated components but not read locations.
const remover: Me = { user: { id: "u-rm", kind: "person" }, person: { username: "remover" }, permissions: ["*:read", "component:delete"], grants: [] };
const placeless: Me = {
  user: { id: "u-np", kind: "person" }, person: { username: "placeless" },
  permissions: ["simulation:read", "simulation:update", "component:read", "component:create", "endpoint:create"], grants: [],
};

const profiles: SimProfile[] = [
  { name: "boreal-edge-86", manufacturer: "Boreal", model: "Edge 86", device_type: "display", products: ["boreal-edge-86", "boreal-edge-55"], protocols: ["http"], summary: "a display" },
];

function failNext(detail: string) {
  vi.mocked(globalThis.fetch).mockImplementation(async () =>
    new Response(JSON.stringify({ title: "Internal Server Error", status: 500, detail }), { status: 500, headers: { "Content-Type": "application/json" } }));
}

describe("the simulated component drawer's writes", () => {
  it("keeps the operator's product pick when a poll lands with only hits changed", async () => {
    stubFetch();
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    await screen.findByTestId("sim-status");
    const sel = screen.getByLabelText("Product") as HTMLSelectElement;
    expect(sel.value).toBe("boreal-edge-55");
    fireEvent.change(sel, { target: { value: "boreal-edge-86" } });
    const change = screen.getByRole("button", { name: "Change" }) as HTMLButtonElement;
    await waitFor(() => expect(change.disabled).toBe(false));
    qc.setQueryData([...componentSimKey(DISPLAY)], { ...componentSim, activity: simRow(DISPLAY, "display-1", 9) });
    await waitFor(() => expect(screen.getByTestId("sim-status").textContent).toContain("9"));
    expect(sel.value).toBe("boreal-edge-86");
    expect(change.disabled).toBe(false);
  });

  it("follows a product the server changed", async () => {
    stubFetch();
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    await screen.findByTestId("sim-status");
    qc.setQueryData([...componentSimKey(DISPLAY)], { ...componentSim, product: "boreal-edge-86" });
    await waitFor(() => expect((screen.getByLabelText("Product") as HTMLSelectElement).value).toBe("boreal-edge-86"));
  });

  it("changes the product with a PATCH by uuid", async () => {
    stubFetch({ [`PATCH /components/${DISPLAY}`]: components[0] });
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
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
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
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
    const confirm = answerConfirms(true);
    const onClose = vi.fn();
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={onClose} />);
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

  // The server refuses to delete a component that still owns an endpoint, so
  // Remove deletes the endpoints first, after the one confirmation that lists
  // them (#2461).
  it("removes the endpoints with the component, after one confirm that lists them", async () => {
    stubFetch({
      "GET /endpoints": { endpoints: [
        { id: EP1, name: "tcp", transport: "tcp", component_id: DISPLAY, params: { target: "127.60.0.1:80" } },
        { id: uuidFor("sim-ep-other"), name: "tcp", transport: "tcp", component_id: uuidFor("sim-someone") },
      ] },
      [`DELETE /endpoints/${EP1}`]: {},
      [`DELETE /components/${DISPLAY}`]: {},
    });
    const confirm = answerConfirms(true);
    const onClose = vi.fn();
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={onClose} />);
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(confirm.mock.calls[0])).toContain("127.60.0.1");
    const dels = calls.filter((c) => c.method === "DELETE").map((c) => c.path);
    expect(dels).toEqual([`/endpoints/${EP1}`, `/components/${DISPLAY}`]);
  });

  it("sends nothing when the remove is not confirmed", async () => {
    stubFetch();
    answerConfirms(false);
    const onClose = vi.fn();
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={onClose} />);
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("gives a user who may remove but not impair a Remove-only rail", async () => {
    stubFetch();
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />, remover);
    expect(await screen.findByRole("button", { name: "Remove" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save impairment" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Repair" })).toBeNull();
    expect(screen.queryByLabelText("Latency (ms)")).toBeNull();
  });

  it("warns that the state is the last known one when a poll fails after a read", async () => {
    stubFetch();
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
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
  it("warns that the table is the last known state when a poll fails after a read", async () => {
    current = { fleet: fleet([3, 0]) };
    stubFetch();
    const { qc } = mount(() => <Simulation />);
    await screen.findAllByTestId("sim-row");
    failNext("connection refused");
    await qc.refetchQueries({ queryKey: SIMULATION_KEY, exact: true });
    const warn = await screen.findByTestId("sim-stale");
    expect(warn.textContent).toContain("connection refused");
    expect(warn.textContent).toContain("last known");
    expect(screen.getAllByTestId("sim-row")).toHaveLength(2);
  });

  it("restarts a row's highlight when a second rise lands before the first ends", async () => {
    stubFetch();
    const { qc } = mount(() => <Simulation />);
    await screen.findAllByTestId("sim-row");
    const reflow = vi.spyOn(HTMLElement.prototype, "offsetWidth", "get");
    qc.setQueryData([...SIMULATION_KEY], fleet([5, 0]));
    await waitFor(() => expect(screen.getAllByTestId("sim-row")[0].classList.contains("og-blink")).toBe(true));
    const before = reflow.mock.calls.length;
    qc.setQueryData([...SIMULATION_KEY], fleet([6, 0]));
    await waitFor(() => expect(reflow.mock.calls.length).toBeGreaterThan(before));
    expect(screen.getAllByTestId("sim-row")[0].classList.contains("og-blink")).toBe(true);
  });

  async function openAdd(me: Me = admin) {
    const r = mount(() => <Simulation />, me);
    r.qc.setQueryData([...SIM_PROFILES_KEY], profiles);
    r.qc.removeQueries({ queryKey: LOCATIONS_KEY });
    fireEvent.click(await screen.findByRole("button", { name: /Add simulated component/ }));
    return r;
  }

  it("adds a simulated component with a POST to the outpost, then closes", async () => {
    stubFetch({ "POST /outposts/edge-hq/simulation/components": { component: "lab-display-1", address: "127.60.0.4" } });
    await openAdd();
    await pickInPicker(await screen.findByLabelText("Product"), "boreal-edge-55");
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "lab-display-1" } });
    const add = screen.getByRole("button", { name: "Add component" }) as HTMLButtonElement;
    await waitFor(() => expect(add.disabled).toBe(false));
    fireEvent.click(add);
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    expect(calls.find((c) => c.method === "POST")!.body).toEqual({ name: "lab-display-1", product: "boreal-edge-55" });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Add component" })).toBeNull());
  });

  it("surfaces the server's refusal of an add and keeps the form open", async () => {
    stubFetch({ "POST /outposts/edge-hq/simulation/components": new Answer(409, { title: "Conflict", status: 409, detail: "component lab-display-1 already exists" }) });
    await openAdd();
    await pickInPicker(await screen.findByLabelText("Product"), "boreal-edge-55");
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "lab-display-1" } });
    const add = screen.getByRole("button", { name: "Add component" }) as HTMLButtonElement;
    await waitFor(() => expect(add.disabled).toBe(false));
    fireEvent.click(add);
    expect((await screen.findByText(/already exists/)).closest("[role=alert]")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add component" })).toBeTruthy();
  });

  it("shows every field's hint when an incomplete form is submitted", async () => {
    current = { fleet: { connected: true, outposts: [fleet([0, 0]).outposts![0], { ...fleet([0, 0]).outposts![0], outpost: "edge-west" }] } };
    stubFetch();
    await openAdd();
    const name = await screen.findByLabelText("Name");
    expect(screen.queryByText("Name the new component.")).toBeNull();
    fireEvent.submit(name.closest("form")!);
    expect(await screen.findByText("Name the new component.")).toBeTruthy();
    expect(screen.getByText("Choose a simulated product.")).toBeTruthy();
    expect(screen.getByText("Choose the outpost whose simulator serves it.")).toBeTruthy();
    expect(calls.some((c) => c.method === "POST")).toBe(false);
  });

  it("offers no Location to a user who cannot read locations, and never asks for them", async () => {
    stubFetch();
    await openAdd(placeless);
    await screen.findByLabelText("Name");
    expect(screen.queryByLabelText("Location")).toBeNull();
    expect(calls.some((c) => c.path.startsWith("/locations"))).toBe(false);
  });
});

// Adding a whole simulated system to a standard (#913).
const standards: Standard[] = [
  { id: uuidFor("std-meeting"), name: "meeting-room", label: "Meeting room", official: true },
  { id: uuidFor("std-huddle"), name: "huddle-space", label: "Huddle space", official: true },
];
const systemTypes: SystemType[] = [
  { id: uuidFor("st-room"), name: "room-system", label: "Room system", official: true } as SystemType,
];
const meetingPlan: SimSystemPlan = {
  standard: "meeting-room",
  alternates: { audio: "ceiling" },
  components: 3,
  slots: [
    { slot: "display", product: "boreal-edge-55", count: 2 },
    { slot: "ceiling-mic", alternate: "audio/ceiling", product: "polaris-mic-1", count: 1 },
    { slot: "camera", count: 0, missing: 1, reason: "no simulated product is a camera" },
  ],
};
const systemMaker: Me = {
  user: { id: "u-sm", kind: "person" }, person: { username: "system-maker" },
  permissions: ["simulation:read", "simulation:update", "system:create", "system:update", "component:read", "component:create", "endpoint:create", "standard:read", "system_type:read", "system:read", "outpost:read"], grants: [],
};
const noSystemUpdate: Me = {
  user: { id: "u-ns", kind: "person" }, person: { username: "no-system-update" },
  permissions: ["*:read", "simulation:update", "system:create", "component:create", "endpoint:create"], grants: [],
};

describe("adding a simulated system", () => {
  async function openAddSystem(me: Me = admin, plan: SimSystemPlan | null = meetingPlan) {
    const r = mount(() => <Simulation />, me);
    r.qc.setQueryData([...STANDARDS_KEY], standards);
    r.qc.setQueryData([...SYSTEM_TYPES_KEY], systemTypes);
    if (plan) r.qc.setQueryData([...simPlanKey(plan.standard)], plan);
    r.qc.removeQueries({ queryKey: LOCATIONS_KEY });
    fireEvent.click(await screen.findByRole("button", { name: /Add simulated system/ }));
    return r;
  }

  async function fill(standard = "meeting-room") {
    fireEvent.change(await screen.findByLabelText("Standard"), { target: { value: standard } });
    fireEvent.change(screen.getByLabelText("System type"), { target: { value: "room-system" } });
  }

  it("offers the action beside Add simulated component, quiet, to a user holding every permission it needs", async () => {
    stubFetch();
    mount(() => <Simulation />);
    const sys = await screen.findByRole("button", { name: /Add simulated system/ });
    expect(sys.classList.contains("btn-quiet")).toBe(true);
    expect(screen.getByRole("button", { name: /Add simulated component/ }).classList.contains("btn-action")).toBe(true);
  });

  it("hides the action from a user missing system:update, though it may add a component", async () => {
    stubFetch();
    mount(() => <Simulation />, noSystemUpdate);
    await screen.findAllByTestId("sim-row");
    expect(screen.getByRole("button", { name: /Add simulated component/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Add simulated system/ })).toBeNull();
  });

  it("offers the action to a user holding exactly the permissions it needs", async () => {
    stubFetch();
    mount(() => <Simulation />, systemMaker);
    expect(await screen.findByRole("button", { name: /Add simulated system/ })).toBeTruthy();
  });

  // Each permission the add needs, the writes and the reads its form and the
  // server depend on: without any one, the action is not offered. system:read is
  // gated too, but can() reads any system:<action> grant as including it, so a
  // user holding system:create is never without it and it has no row here.
  for (const perm of [
    "simulation:update", "system:create", "system:update", "component:create", "endpoint:create",
    "standard:read", "system_type:read", "outpost:read",
  ]) {
    it(`hides the action from a user missing ${perm}`, async () => {
      stubFetch();
      const me: Me = { ...systemMaker, permissions: systemMaker.permissions.filter((p) => p !== perm) };
      mount(() => <Simulation />, me);
      await screen.findAllByTestId("sim-row");
      expect(screen.queryByRole("button", { name: /Add simulated system/ })).toBeNull();
    });
  }

  it("disables the action, saying why, while no simulator is connected", async () => {
    current = { fleet: fleet([0, 0], false) };
    stubFetch();
    mount(() => <Simulation />);
    const sys = (await screen.findByRole("button", { name: /Add simulated system/ })) as HTMLButtonElement;
    expect(sys.getAttribute("aria-disabled")).toBe("true");
    expect(await tipOf(sys)).toBe("No simulator is connected");
  });

  it("hides the action from a reader", async () => {
    stubFetch();
    mount(() => <Simulation />, viewer);
    await screen.findAllByTestId("sim-row");
    expect(screen.queryByRole("button", { name: /Add simulated system/ })).toBeNull();
  });

  it("previews the plan once a standard is chosen: each slot's product and count, the short slot with its reason, the total", async () => {
    stubFetch();
    await openAddSystem();
    expect(screen.queryByTestId("sim-plan")).toBeNull();
    fireEvent.change(await screen.findByLabelText("Standard"), { target: { value: "meeting-room" } });
    const preview = await screen.findByTestId("sim-plan");
    const rows = within(preview).getAllByTestId("plan-slot");
    expect(rows.map((r) => r.getAttribute("data-slot"))).toEqual(["display", "ceiling-mic", "camera"]);
    expect(within(rows[0]).getByText("boreal-edge-55")).toBeTruthy();
    expect(rows[0].lastElementChild!.textContent).toBe("2");
    expect(within(rows[1]).getByText("audio/ceiling")).toBeTruthy();
    expect(rows[2].getAttribute("data-short")).toBe("true");
    expect(within(rows[2]).getByTestId("plan-short").textContent).toContain("no simulated product is a camera");
    expect(within(preview).getByTestId("plan-total").textContent).toBe("3");
    expect(within(preview).getByTestId("plan-short-count").textContent).toBe("1 short");
  });

  it("reads the plan from the API for the chosen standard", async () => {
    stubFetch({ "GET /simulation/standards/huddle-space:plan": { ...meetingPlan, standard: "huddle-space", components: 1, slots: [{ slot: "display", product: "boreal-edge-55", count: 1 }] } });
    await openAddSystem();
    fireEvent.change(await screen.findByLabelText("Standard"), { target: { value: "huddle-space" } });
    await waitFor(() => expect(screen.getByTestId("plan-total").textContent).toBe("1"));
    expect(calls.some((c) => c.method === "GET" && c.path === "/simulation/standards/huddle-space:plan")).toBe(true);
  });

  it("refuses to add when nothing in the standard can be simulated", async () => {
    stubFetch();
    const empty: SimSystemPlan = { standard: "meeting-room", alternates: {}, components: 0, slots: [{ slot: "camera", count: 0, missing: 1, reason: "no simulated product is a camera" }] };
    await openAddSystem(admin, empty);
    await fill();
    expect(await screen.findByTestId("plan-empty")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("sends the standard, the type and only what was filled, then closes, refreshes the fleet and names the system", async () => {
    stubFetch({
      "POST /outposts/edge-hq/simulation/systems": new Answer(201, {
        system: "room-system-7", system_id: uuidFor("sys-room-7"), outpost: "edge-hq",
        components: [
          { component: "display-1", slot: "display", product: "boreal-edge-55", address: "127.60.0.4" },
          { component: "display-2", slot: "display", product: "boreal-edge-55", address: "127.60.0.5" },
          { component: "mic-1", slot: "ceiling-mic", product: "polaris-mic-1", address: "127.60.0.6" },
        ],
        unstaffed: [{ slot: "camera", count: 0, missing: 1, reason: "no simulated product is a camera" }],
      }),
    });
    const { qc } = await openAddSystem();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    await fill();
    const add = screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement;
    await waitFor(() => expect(add.disabled).toBe(false));
    fireEvent.click(add);
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST")!;
    expect(post.path).toBe("/outposts/edge-hq/simulation/systems");
    expect(post.body).toEqual({ standard: "meeting-room", system_type: "room-system" });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Add system" })).toBeNull());
    const note = await screen.findByTestId("sim-system-added");
    expect(note.textContent).toContain("room-system-7");
    expect(note.textContent).toContain("3 components");
    expect(note.textContent).toContain("camera");
    // The short slot's reason reads as text, not only on hover.
    expect(note.textContent).toContain("no simulated product is a camera");
    // The system's name links to its page, by id.
    const link = within(note).getByRole("link", { name: "room-system-7" });
    expect(link.getAttribute("href")).toBe(`/systems/${uuidFor("sys-room-7")}`);
    const keys = invalidate.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    expect(keys).toContain(JSON.stringify(SIMULATION_KEY));
    expect(keys).toContain(JSON.stringify(COMPONENTS_KEY));
    fireEvent.click(within(note).getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByTestId("sim-system-added")).toBeNull();
  });

  it("sends a typed name and a chosen location", async () => {
    stubFetch({ "POST /outposts/edge-hq/simulation/systems": new Answer(201, { system: "lab-room", system_id: uuidFor("sys-lab"), outpost: "edge-hq", components: [], unstaffed: [] }) });
    const { qc } = await openAddSystem();
    qc.setQueryData([...LOCATIONS_KEY], [{ id: uuidFor("loc-a"), name: "boardroom-a", label: "Boardroom A", path: "hq.boardroom-a" }]);
    await fill();
    await pickTree(await screen.findByLabelText("Location"), "Boardroom A");
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "lab-room" } });
    const add = screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement;
    await waitFor(() => expect(add.disabled).toBe(false));
    fireEvent.click(add);
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    expect(calls.find((c) => c.method === "POST")!.body).toEqual({ standard: "meeting-room", system_type: "room-system", name: "lab-room", location: "hq.boardroom-a" });
  });

  for (const [status, title, detail] of [
    [409, "Conflict", "no simulator is connected to outpost edge-hq"],
    [422, "Unprocessable Entity", "nothing in standard meeting-room can be simulated"],
    [403, "Forbidden", "missing permission system:update"],
  ] as const) {
    it(`surfaces a ${status} in the form and keeps it open`, async () => {
      stubFetch({ "POST /outposts/edge-hq/simulation/systems": new Answer(status, { title, status, detail }) });
      await openAddSystem();
      await fill();
      const add = screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement;
      await waitFor(() => expect(add.disabled).toBe(false));
      fireEvent.click(add);
      const alert = await screen.findByTestId("add-system-error");
      expect(alert.textContent).toContain(detail);
      expect(screen.getByRole("button", { name: "Add system" })).toBeTruthy();
      expect(screen.queryByTestId("sim-system-added")).toBeNull();
    });
  }

  it("refuses a malformed name before it is sent", async () => {
    stubFetch();
    await openAddSystem();
    await fill();
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "Lab Room" } });
    expect(await screen.findByText(/Lowercase letters, digits and hyphens/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows the plan error with a retry, and keeps submit disabled, when the plan cannot be read", async () => {
    stubFetch();
    await openAddSystem();
    await fill("huddle-space");
    const preview = await screen.findByTestId("sim-plan");
    const alert = await within(preview).findByRole("alert");
    expect(alert.textContent).toContain("Could not plan the system");
    expect((screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement).disabled).toBe(true);
    const planGets = () => calls.filter((c) => c.method === "GET" && c.path === "/simulation/standards/huddle-space:plan").length;
    expect(planGets()).toBe(1);
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(planGets()).toBe(2));
  });

  it("keeps submit disabled while the plan is loading", async () => {
    stubFetch({ "GET /simulation/standards/huddle-space:plan": () => new Promise<Response>(() => {}) });
    await openAddSystem();
    await fill("huddle-space");
    const preview = await screen.findByTestId("sim-plan");
    expect(preview.querySelector("[aria-busy='true']")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("never lets a stale plan enable submit or fill the preview: A answered, B pending", async () => {
    stubFetch({ "GET /simulation/standards/huddle-space:plan": () => new Promise<Response>(() => {}) });
    await openAddSystem();
    await fill("meeting-room");
    const add = screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement;
    await waitFor(() => expect(add.disabled).toBe(false));
    fireEvent.change(screen.getByLabelText("Standard"), { target: { value: "huddle-space" } });
    await waitFor(() => expect(add.disabled).toBe(true));
    const preview = screen.getByTestId("sim-plan");
    expect(within(preview).queryAllByTestId("plan-slot")).toHaveLength(0);
    expect(within(preview).queryByTestId("plan-total")).toBeNull();
  });

  // The cache switches keys with the standard, so the A-then-B case above never
  // hands the form A's answer; the guard it relies on is that a plan counts only
  // when it answers for the chosen standard. Pin that directly: B's slot holding
  // an answer for A is neither previewed nor submittable.
  it("trusts a plan only when it answers for the chosen standard", async () => {
    stubFetch();
    const r = await openAddSystem();
    r.qc.setQueryData([...simPlanKey("huddle-space")], meetingPlan);
    await fill("huddle-space");
    const preview = await screen.findByTestId("sim-plan");
    expect(within(preview).queryAllByTestId("plan-slot")).toHaveLength(0);
    expect((screen.getByRole("button", { name: "Add system" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("names the plan table and announces the preview's updates", async () => {
    stubFetch();
    await openAddSystem();
    await fill();
    const preview = await screen.findByTestId("sim-plan");
    expect(preview.getAttribute("aria-live")).toBe("polite");
    expect(within(preview).getByRole("table", { name: /plan/i })).toBeTruthy();
  });

  it("alerts with a retry when the standards cannot be read", async () => {
    stubFetch();
    const r = mount(() => <Simulation />);
    r.qc.setQueryData([...SYSTEM_TYPES_KEY], systemTypes);
    fireEvent.click(await screen.findByRole("button", { name: /Add simulated system/ }));
    const alert = await screen.findByTestId("standards-error");
    expect(alert.getAttribute("role")).toBe("alert");
    const gets = () => calls.filter((c) => c.method === "GET" && c.path === "/standards").length;
    expect(gets()).toBe(1);
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(gets()).toBe(2));
  });

  it("alerts with a retry when the system types cannot be read", async () => {
    stubFetch();
    const r = mount(() => <Simulation />);
    r.qc.setQueryData([...STANDARDS_KEY], standards);
    fireEvent.click(await screen.findByRole("button", { name: /Add simulated system/ }));
    const alert = await screen.findByTestId("system-types-error");
    const gets = () => calls.filter((c) => c.method === "GET" && c.path === "/system-types").length;
    expect(gets()).toBe(1);
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(gets()).toBe(2));
  });

  it("alerts with a retry when the locations cannot be read, instead of offering only unplaced", async () => {
    stubFetch();
    await openAddSystem();
    const alert = await screen.findByTestId("locations-error");
    const gets = () => calls.filter((c) => c.method === "GET" && c.path === "/locations").length;
    expect(gets()).toBe(1);
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(gets()).toBe(2));
  });

  it("hints where to define a standard or a type when there are none", async () => {
    stubFetch();
    const r = mount(() => <Simulation />);
    r.qc.setQueryData([...STANDARDS_KEY], []);
    r.qc.setQueryData([...SYSTEM_TYPES_KEY], []);
    fireEvent.click(await screen.findByRole("button", { name: /Add simulated system/ }));
    expect(await screen.findByText(/No standards yet/)).toBeTruthy();
    expect(screen.getByText(/No system types yet/)).toBeTruthy();
  });

  it("offers no Location to a user who cannot read locations, and never asks for them", async () => {
    stubFetch();
    await openAddSystem(systemMaker);
    await screen.findByLabelText("Standard");
    expect(screen.queryByLabelText("Location")).toBeNull();
    expect(calls.some((c) => c.path.startsWith("/locations"))).toBe(false);
  });
});

describe("removing from the component page", () => {
  it("leaves for the Simulation page once the component is gone", async () => {
    stubFetch({ [`DELETE /components/${DISPLAY}`]: {} });
    answerConfirms(true);
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
    fireEvent.click(within(section).getByRole("button", { name: /Open simulation/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
    expect(await screen.findByTestId("sim-page")).toBeTruthy();
    expect(window.location.pathname).toBe("/simulation");
  });
});

// The drawer opens on Info; a test that wants another tab chooses it.
async function openTab(name: string) {
  fireEvent.click(await screen.findByRole("tab", { name }));
}

// One-click faults (#2462): the menu a profile generates, the chip the
// simulator's own read raises, and the list that filters as it is typed and
// keeps its view in the address.
const SINCE = new Date(Date.now() - 4 * 60_000).toISOString();
const powerSurface = [
  {
    name: "web-admin", protocol: "http", port: 10080, fireable: true,
    routes: [
      { method: "POST", path: "/api/display/power", summary: "Set display power", sets: [{ path: "status.device.power", field: "power", values: ["on", "off", "standby"], default: "on" }] },
      { method: "POST", path: "/api/display/input", sets: [{ path: "status.display.input", field: "input", values: ["hdmi1", "hdmi2"], default: "hdmi1" }] },
    ],
  },
];
// The harness seeds the cache with the plain component simulation; a test that
// wants the profile's surface puts it there (the read would otherwise be skipped).
const seedSurface = (qc: QueryClient, activity: SimComponent) => qc.setQueryData([...componentSimKey(DISPLAY)], withSurface(activity));
const withSurface = (activity: SimComponent): ComponentSim => ({ ...componentSim, activity, surface: powerSurface });
const poweredOff = { path: "status.device.power", value: "off", default: "on", since: SINCE };
const fireUrl = `POST /components/${DISPLAY}/simulation:fire`;
const fired = () => calls.filter((c) => c.path.endsWith("simulation:fire")).map((c) => c.body);

describe("one-click faults on the Simulation page", () => {
  it("powers a display off from the row's menu, with no JSON typed", async () => {
    stubFetch({ [`GET /components/${DISPLAY}/simulation`]: withSurface(simRow(DISPLAY, "display-1", 7)), [fireUrl]: { status: 200, body: "{}", took_ms: 2 } });
    const { qc } = mount(() => <Simulation />);
    seedSurface(qc, simRow(DISPLAY, "display-1", 7));
    const rows = await screen.findAllByTestId("sim-row");
    fireEvent.click(within(rows[0]).getByRole("button", { name: "Break display-1" }));
    // The menu is the profile's: power, the connection, the input; no Restore while healthy.
    const menu = await screen.findByRole("dialog", { name: "Break or restore" });
    await within(menu).findByRole("button", { name: "Power off" });
    expect(within(menu).getAllByRole("button").map((b) => b.textContent)).toEqual(["Power off", "Standby", "Unplug", "Input: hdmi2"]);
    fireEvent.click(within(menu).getByRole("button", { name: "Power off" }));
    await waitFor(() => expect(fired()).toHaveLength(1));
    expect(fired()[0]).toEqual({ surface: "web-admin", method: "POST", path: "/api/display/power", body: '{"power":"off"}' });
    // The click was the menu's, not the row's: no drawer opened.
    expect(screen.queryByTestId("sim-status")).toBeNull();
  });

  it("unplugs from the menu with an impairment write", async () => {
    stubFetch({ [`GET /components/${DISPLAY}/simulation`]: withSurface(simRow(DISPLAY, "display-1", 7)), [`PUT /components/${DISPLAY}/impairment`]: { unplugged: true } });
    const { qc } = mount(() => <Simulation />);
    seedSurface(qc, simRow(DISPLAY, "display-1", 7));
    const rows = await screen.findAllByTestId("sim-row");
    fireEvent.click(within(rows[0]).getByRole("button", { name: "Break display-1" }));
    fireEvent.click(await screen.findByRole("button", { name: "Unplug" }));
    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true));
    expect(calls.find((c) => c.method === "PUT")!.body).toEqual({ unplugged: true, latency_ms: 0, jitter_ms: 0, loss_pct: 0, rate_kbit: 0 });
  });

  it("shows a held fault as a chip with its age, from the simulator's read, and restores it in one click", async () => {
    const f = fleet([0, 0]);
    f.outposts![0].components = [simRow(DISPLAY, "display-1", 0, { faults: [poweredOff] }), simRow(DSP, "dsp-1", 0)];
    current = { fleet: f };
    stubFetch({
      [`GET /components/${DISPLAY}/simulation`]: withSurface(f.outposts![0].components![0]),
      [fireUrl]: { status: 200, body: "{}", took_ms: 2 },
    });
    const { qc } = mount(() => <Simulation />);
    seedSurface(qc, f.outposts![0].components![0]);
    const rows = await screen.findAllByTestId("sim-row");
    const chip = within(rows[0]).getByTestId("sim-fault-chip");
    expect(within(chip).getByTestId("sim-fault-label").textContent).toBe("powered off");
    expect(within(chip).getByTestId("sim-fault-age").textContent).toBe("4m");
    expect(within(rows[1]).queryByTestId("sim-fault-chip")).toBeNull();
    fireEvent.click(within(chip).getByRole("button", { name: /Clear the fault/ }));
    await waitFor(() => expect(fired()).toHaveLength(1));
    expect(fired()[0]).toEqual({ surface: "web-admin", method: "POST", path: "/api/display/power", body: '{"power":"on"}' });
    expect(screen.queryByTestId("sim-status")).toBeNull();
  });

  it("offers the way back, not the value already held: Power on and Restore while it is off", async () => {
    const f = fleet([0, 0]);
    f.outposts![0].components = [simRow(DISPLAY, "display-1", 0, { faults: [poweredOff] })];
    current = { fleet: f };
    stubFetch({ [`GET /components/${DISPLAY}/simulation`]: withSurface(f.outposts![0].components![0]) });
    const { qc } = mount(() => <Simulation />);
    seedSurface(qc, f.outposts![0].components![0]);
    const rows = await screen.findAllByTestId("sim-row");
    fireEvent.click(within(rows[0]).getByRole("button", { name: "Break display-1" }));
    const menu = await screen.findByRole("dialog", { name: "Break or restore" });
    await within(menu).findByRole("button", { name: "Power on" });
    expect(within(menu).getAllByRole("button").map((b) => b.textContent)).toEqual(["Power on", "Standby", "Unplug", "Input: hdmi2", "Restore"]);
  });

  it("gives a viewer the fault and no way to set or clear it", async () => {
    const f = fleet([0, 0]);
    f.outposts![0].components = [simRow(DISPLAY, "display-1", 0, { faults: [poweredOff] })];
    current = { fleet: f };
    stubFetch();
    mount(() => <Simulation />, viewer);
    const rows = await screen.findAllByTestId("sim-row");
    expect(within(rows[0]).getByTestId("sim-fault-label").textContent).toBe("powered off");
    expect(within(rows[0]).queryByRole("button", { name: /Clear the fault/ })).toBeNull();
    expect(within(rows[0]).queryByRole("button", { name: /^Break/ })).toBeNull();
  });

  it("lists the faults in the drawer with the profile's actions as buttons, and the operations as closed rows", async () => {
    const act = simRow(DISPLAY, "display-1", 7, { faults: [poweredOff] });
    stubFetch({ [`GET /components/${DISPLAY}/simulation`]: withSurface(act), [fireUrl]: { status: 200, body: "{}", took_ms: 2 } });
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    seedSurface(qc, act);
    const section = await screen.findByTestId("sim-faults");
    await waitFor(() => expect(within(section).getByTestId("sim-fault-label").textContent).toBe("powered off"));
    fireEvent.click(await within(section).findByRole("button", { name: "Power on" }));
    await waitFor(() => expect(fired()).toHaveLength(1));
    expect(fired()[0]).toMatchObject({ body: '{"power":"on"}' });
    // The operations are rows, closed until opened: no shared fire box.
    expect(screen.queryByTestId("op-detail")).toBeNull();
    expect(screen.queryByTestId("fire-panel")).toBeNull();
  });

  it("says nothing is broken for a healthy device, and shows no chip", async () => {
    stubFetch();
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    qc.setQueryData([...componentSimKey(DISPLAY)], withSurface(simRow(DISPLAY, "display-1", 7)));
    const section = await screen.findByTestId("sim-faults");
    expect(within(section).getByTestId("sim-no-faults")).toBeTruthy();
    expect(within(section).queryByTestId("sim-fault-chip")).toBeNull();
  });
});

describe("the fault chip on the component's page", () => {
  it("shows a held fault beside the component, clears from there, and is absent when healthy or not simulated", async () => {
    stubFetch({
      [`GET /components/${DISPLAY}/simulation`]: withSurface(simRow(DISPLAY, "display-1", 7, { faults: [poweredOff] })),
      [fireUrl]: { status: 200, body: "{}", took_ms: 2 },
    });
    const { qc } = mount(() => <SimFaultChipFor componentId={DISPLAY} name="display-1" />);
    seedSurface(qc, simRow(DISPLAY, "display-1", 7, { faults: [poweredOff] }));
    const chip = await screen.findByTestId("sim-fault-chip");
    expect(chip.textContent).toContain("powered off");
    fireEvent.click(within(chip).getByRole("button", { name: /Clear the fault/ }));
    await waitFor(() => expect(fired()).toHaveLength(1));
    cleanup();
    stubFetch();
    const healthy = mount(() => <SimFaultChipFor componentId={DISPLAY} />);
    seedSurface(healthy.qc, simRow(DISPLAY, "display-1", 7));
    expect(screen.queryByTestId("sim-fault-chip")).toBeNull();
    cleanup();
    stubFetch();
    const notSim = mount(() => <SimFaultChipFor componentId={DSP} />);
    notSim.qc.setQueryData([...componentSimKey(DSP)], { simulated: false, connected: false, impairments: { available: false } });
    expect(screen.queryByTestId("sim-fault-chip")).toBeNull();
  });

  it("shows a viewer the fault and no clear", async () => {
    stubFetch();
    const { qc } = mount(() => <SimFaultChipFor componentId={DISPLAY} />, viewer);
    seedSurface(qc, simRow(DISPLAY, "display-1", 7, { faults: [poweredOff] }));
    const chip = await screen.findByTestId("sim-fault-chip");
    expect(within(chip).queryByRole("button", { name: /Clear the fault/ })).toBeNull();
  });
});

describe("the Simulation list's filter and paging", () => {
  const many = (n: number): SimFleet => {
    const f = fleet([0, 0]);
    f.outposts![0].components = Array.from({ length: n }, (_, i) => simRow(uuidFor(`bulk-${i}`), `bulk-${String(i).padStart(3, "0")}`, 0, { product: i % 2 ? "polaris-dsp-8" : "boreal-edge-55" }));
    return f;
  };

  it("filters as it is typed, with no Enter, and Enter keeps the text as a chip in the address", async () => {
    current = { fleet: many(8) };
    stubFetch();
    mount(() => <Simulation />);
    await screen.findAllByTestId("sim-row");
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "bulk-00" } });
    await waitFor(() => expect(screen.getAllByTestId("sim-row")).toHaveLength(8));
    fireEvent.input(input, { target: { value: "bulk-003" } });
    await waitFor(() => expect(screen.getAllByTestId("sim-row")).toHaveLength(1));
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(window.location.search).toContain("chips="));
    expect(JSON.parse(new URLSearchParams(window.location.search).get("chips")!)).toEqual([{ key: "search", op: "contains", values: ["bulk-003"] }]);
    expect(screen.getAllByTestId("sim-row")).toHaveLength(1);
  });

  it("searches the product and the address too, not only the name", async () => {
    current = { fleet: many(6) };
    stubFetch();
    mount(() => <Simulation />);
    await screen.findAllByTestId("sim-row");
    fireEvent.input(screen.getByRole("combobox"), { target: { value: "polaris" } });
    await waitFor(() => expect(screen.getAllByTestId("sim-row")).toHaveLength(3));
  });

  it("a deep link reproduces the filter and the open device", async () => {
    current = { fleet: many(8) };
    stubFetch();
    const chips = encodeURIComponent(JSON.stringify([{ key: "search", op: "contains", values: ["bulk-00"] }, { key: "product", op: "eq", values: ["polaris-dsp-8"] }]));
    mount(() => <Simulation />, admin, `/simulation?chips=${chips}&c=${uuidFor("bulk-1")}`);
    await screen.findByTestId("sim-status");
    const rows = screen.getAllByTestId("sim-row").map((r) => r.dataset.component);
    expect(rows).toEqual([1, 3, 5, 7].map((i) => uuidFor(`bulk-${i}`)));
    // Closing the drawer leaves the filter.
    expect(window.location.search).toContain("chips=");
  });

  it("pages a long list: fifty rows, the count, and fifty more on request, starting over when the filter changes", async () => {
    current = { fleet: many(120) };
    stubFetch();
    mount(() => <Simulation />);
    await screen.findAllByTestId("sim-row");
    expect(screen.getAllByTestId("sim-row")).toHaveLength(50);
    const more = screen.getByTestId("list-more");
    expect(more.textContent).toContain("Showing 50 of 120");
    fireEvent.click(within(more).getByRole("button", { name: /Show 50 more/ }));
    await waitFor(() => expect(screen.getAllByTestId("sim-row")).toHaveLength(100));
    fireEvent.click(within(screen.getByTestId("list-more")).getByRole("button", { name: /Show 20 more/ }));
    await waitFor(() => expect(screen.getAllByTestId("sim-row")).toHaveLength(120));
    expect(screen.queryByTestId("list-more")).toBeNull();
    fireEvent.input(screen.getByRole("combobox"), { target: { value: "bulk-0" } });
    await waitFor(() => expect(screen.getByTestId("list-more").textContent).toContain("Showing 50 of 100"));
    expect(screen.getAllByTestId("sim-row")).toHaveLength(50);
  });
});

describe("in-place operation rows", () => {
  const twoRoutes = (): ComponentSim => ({
    ...componentSim,
    surface: [{
      name: "web-admin", protocol: "http", port: 10080, fireable: true,
      routes: [
        { method: "GET", path: "/api/status", summary: "Identity" },
        { method: "POST", path: "/api/display/power", sets: [{ path: "status.device.power", field: "power", values: ["on", "off"], default: "on" }], example: '{"power":"on"}' },
      ],
    }],
  });

  it("expands a row in place with its form and answer inside it, closes the other, and scrolls nothing", async () => {
    const scroll = vi.fn();
    (Element.prototype as unknown as { scrollIntoView: unknown }).scrollIntoView = scroll;
    stubFetch({ [fireUrl]: { status: 200, body: '{"ok":true}', took_ms: 4 }, [`GET /components/${DISPLAY}/simulation`]: twoRoutes() });
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    qc.setQueryData([...componentSimKey(DISPLAY)], twoRoutes());
    await openTab("Operations");
    const status = await screen.findByRole("button", { name: /\/api\/status/ });
    const power = await screen.findByRole("button", { name: /\/api\/display\/power/ });
    expect(status.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(status);
    const first = status.closest("li")!;
    expect(status.getAttribute("aria-expanded")).toBe("true");
    expect(within(first).getByTestId("op-detail")).toBeTruthy();
    fireEvent.click(within(first).getByRole("button", { name: "Send" }));
    await waitFor(() => expect(within(first).getByTestId("fire-answer").textContent).toContain('"ok":true'));
    // Another row opens: the first collapses, taking its form and answer.
    fireEvent.click(power);
    const second = power.closest("li")!;
    expect(within(second).getByTestId("op-detail")).toBeTruthy();
    expect(within(second).getByTestId("op-writes").textContent).toContain("status.device.power");
    expect(status.getAttribute("aria-expanded")).toBe("false");
    expect(within(first).queryByTestId("op-detail")).toBeNull();
    expect(screen.getAllByTestId("op-detail")).toHaveLength(1);
    expect(scroll).not.toHaveBeenCalled();
    // Clicking the open row closes it.
    fireEvent.click(power);
    expect(screen.queryByTestId("op-detail")).toBeNull();
  });
});

describe("the drawer's tabs", () => {
  it("opens on Info, offers the five tabs, remembers the chosen one, and a state form sets a path through the named-fault request", async () => {
    const act = simRow(DISPLAY, "display-1", 7);
    const full = { ...withSurface(act), behaviors: [{ name: "temperature_drift", interval: "5s", writes: ["status.display.temperature"] }] };
    stubFetch({ [fireUrl]: { status: 200, body: "{}", took_ms: 2 }, [`GET /components/${DISPLAY}/simulation`]: full });
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    qc.setQueryData([...componentSimKey(DISPLAY)], full);
    expect((await screen.findAllByRole("tab")).map((t) => t.textContent)).toEqual(["Info", "Impairments", "Operations", "Behaviors", "Events"]);
    expect(screen.getByRole("tab", { name: "Info" }).getAttribute("aria-selected")).toBe("true");
    const state = await screen.findByTestId("sim-state-form");
    fireEvent.change(within(state).getAllByRole("combobox")[0], { target: { value: "off" } });
    await waitFor(() => expect(fired()).toHaveLength(1));
    expect(fired()[0]).toEqual({ surface: "web-admin", method: "POST", path: "/api/display/power", body: '{"power":"off"}' });
    await openTab("Behaviors");
    expect((await screen.findByTestId("sim-behaviors")).textContent).toContain("temperature_drift");
    await openTab("Events");
    expect((await screen.findByTestId("sim-events-gap")).textContent).toContain("#2524");
    cleanup();
    mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />);
    await waitFor(() => expect(screen.getByRole("tab", { name: "Events" }).getAttribute("aria-selected")).toBe("true"));
  });

  it("says no behaviors are declared when the profile has none, and leaves a viewer's state read-only", async () => {
    stubFetch();
    const { qc } = mount(() => <SimComponentDrawer id={DISPLAY} onClose={() => {}} />, viewer);
    qc.setQueryData([...componentSimKey(DISPLAY)], withSurface(simRow(DISPLAY, "display-1", 7)));
    const state = await screen.findByTestId("sim-state-form");
    expect((within(state).getAllByRole("combobox")[0] as HTMLSelectElement).disabled).toBe(true);
    await openTab("Behaviors");
    expect((await screen.findByTestId("sim-behaviors")).textContent).toContain("No behaviors declared");
  });
});
