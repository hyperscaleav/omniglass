import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { CreateEndpointForm, endpointBlade } from "./endpointBlades";
import { BladeEditContext, BladesContext, createEditSlot, type BladeController } from "../lib/blades";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { ENDPOINTS_KEY, TRANSPORTS_KEY, type Transport } from "../lib/endpoints";
import { COMPONENTS_KEY } from "../lib/components";
import { ME_KEY, type Me } from "../lib/auth";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";
import { uuidFor } from "../lib/testids";

// The endpoint create form's two faces (#813): Probe posts a transport, Attach
// posts a driver plus its inputs. These pin the mode-dependent payload branch
// (the review's finding lived here) and the driver-switch input reset.

const transports: Transport[] = [
  { name: "icmp", description: "", held: false, built: true },
  { name: "tcp", description: "", held: false, built: true },
  { name: "snmp", description: "", held: true, built: false },
];
const drivers: Driver[] = [
  {
    id: uuidFor("snmp-generic"), name: "snmp-generic", label: "Generic SNMP", official: true,
    spec: { version: 1, transport: "snmp", inputs: [
      { name: "host", kind: "string", required: true },
      { name: "community", kind: "secret", secret_type: "snmp-community", required: true },
    ], polls: [] },
  },
  {
    id: uuidFor("line-proto"), name: "line-proto", label: "Line Proto", official: true,
    spec: { version: 1, transport: "tcp", inputs: [
      { name: "host", kind: "string", required: true },
      { name: "channel", kind: "string" },
    ], polls: [] },
  },
];

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TRANSPORTS_KEY], transports);
  qc.setQueryData([...DRIVERS_KEY], drivers);
  qc.setQueryData([...COMPONENTS_KEY], [{ id: uuidFor("amp-1"), name: "amp-1", label: "Amp 1" }]);
  const slot = createEditSlot();
  render(() => (
    <QueryClientProvider client={qc}>
      <BladeEditContext.Provider value={slot}>
        <CreateEndpointForm component="amp-1" onCreated={() => {}} />
      </BladeEditContext.Provider>
    </QueryClientProvider>
  ));
  // The form binds its primary action on the blade footer slot; invoke it the
  // way the footer button would.
  return { submit: () => slot.primary?.()?.onClick() };
}

afterEach(() => vi.restoreAllMocks());

describe("CreateEndpointForm", () => {
  it("probe mode posts a transport and no driver/inputs", async () => {
    const bodies: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      bodies.push(await (input as Request).json());
      return json({ id: uuidFor("e1"), name: "tcp", transport: "tcp" });
    });
    const { submit } = mount();
    // Probe is the default face; a target makes it a full create.
    const target = screen.getByPlaceholderText("10.0.0.1:22") as HTMLInputElement;
    fireEvent.input(target, { target: { value: "10.0.0.9:22" } });
    submit();
    await waitFor(() => expect(bodies.length).toBe(1));
    const body = bodies[0] as Record<string, unknown>;
    expect(body.transport).toBe("icmp"); // the built-transport default
    expect(body.driver).toBeUndefined();
    expect(body.inputs).toBeUndefined();
    expect(body.params).toEqual({ target: "10.0.0.9:22" });
  });

  it("attach mode posts a driver and its inputs, no transport/params", async () => {
    const bodies: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      bodies.push(await (input as Request).json());
      return json({ id: uuidFor("e2"), name: "snmp", transport: "snmp", driver: "snmp-generic" });
    });
    const { submit } = mount();
    fireEvent.click(screen.getByText("Attach a driver"));
    // Pick the SNMP driver, fill its two inputs.
    const driverSel = screen.getByLabelText("Driver") as HTMLSelectElement;
    fireEvent.change(driverSel, { target: { value: "snmp-generic" } });
    await waitFor(() => screen.getByLabelText(/community/i));
    fireEvent.input(screen.getByLabelText(/^host/i), { target: { value: "10.0.0.5" } });
    fireEvent.input(screen.getByLabelText(/community/i), { target: { value: "lab-community" } });
    submit();
    await waitFor(() => expect(bodies.length).toBe(1));
    const body = bodies[0] as Record<string, unknown>;
    expect(body.driver).toBe("snmp-generic");
    expect(body.inputs).toEqual({ host: "10.0.0.5", community: "lab-community" });
    expect(body.transport).toBeUndefined();
    expect(body.params).toBeUndefined();
  });

  it("switching drivers clears an input the new driver does not declare", async () => {
    const bodies: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      bodies.push(await (input as Request).json());
      return json({ id: uuidFor("e3"), name: "tcp", transport: "tcp", driver: "line-proto" });
    });
    const { submit } = mount();
    fireEvent.click(screen.getByText("Attach a driver"));
    const driverSel = screen.getByLabelText("Driver") as HTMLSelectElement;
    // Pick SNMP, type its community, then switch to the line driver (no community).
    fireEvent.change(driverSel, { target: { value: "snmp-generic" } });
    await waitFor(() => screen.getByLabelText(/community/i));
    fireEvent.input(screen.getByLabelText(/community/i), { target: { value: "secret-ref" } });
    fireEvent.change(driverSel, { target: { value: "line-proto" } });
    await waitFor(() => screen.getByLabelText(/channel/i));
    fireEvent.input(screen.getByLabelText(/^host/i), { target: { value: "10.0.0.7" } });
    submit();
    await waitFor(() => expect(bodies.length).toBe(1));
    const body = bodies[0] as Record<string, unknown>;
    // No leftover community key from the first driver.
    expect(body.inputs).toEqual({ host: "10.0.0.7" });
  });
});

// The edit blade on a driver-attached endpoint (#1061 ruling): its inputs (the
// host, the port, an attached secret's reference) edit in place, and only the
// ones changed are sent; no re-attach.
describe("endpoint edit blade, driver-attached", () => {
  const telnet: Driver = {
    id: uuidFor("newtron-nvp-telnet"), name: "newtron-nvp-telnet", label: "Newtron NVP over Telnet", official: true,
    spec: { version: 1, transport: "telnet", inputs: [
      { name: "host", kind: "string", required: true },
      { name: "port", kind: "number", default: "23" },
      { name: "login", kind: "secret", secret_type: "basic-auth", required: true },
    ], polls: [] },
  };
  const row = {
    id: uuidFor("ep-t"), name: "telnet", transport: "telnet", component: "panel-1", component_id: uuidFor("panel-1"),
    params: { target: "10.0.0.5:2399" }, driver: "newtron-nvp-telnet", driver_id: uuidFor("newtron-nvp-telnet"),
    inputs: { host: "10.0.0.5", port: "2399", login: "panel-login" },
  };

  function mountEdit() {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ENDPOINTS_KEY], [row]);
    qc.setQueryData([...DRIVERS_KEY], [telnet]);
    const slot = createEditSlot();
    const blades = { pop: () => {} } as unknown as BladeController;
    render(() => (
      <QueryClientProvider client={qc}>
        <BladesContext.Provider value={blades}>
          <BladeEditContext.Provider value={slot}>
            <endpointBlade.Body id={row.id} />
          </BladeEditContext.Provider>
        </BladesContext.Provider>
      </QueryClientProvider>
    ));
    slot.begin();
    return slot;
  }

  it("sends only the inputs changed, in place", async () => {
    const bodies: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH") bodies.push(await req.json());
      return json(row);
    });
    const slot = mountEdit();
    const port = (await screen.findByLabelText("port")) as HTMLInputElement;
    expect(port.value).toBe("2399");
    fireEvent.input(port, { target: { value: "23" } });
    await slot.save();
    await waitFor(() => expect(bodies.length).toBe(1));
    expect(bodies[0]).toEqual({ inputs: { port: "23" } });
    expect(screen.queryByText(/re-attach/i)).toBeNull();
  });

  it("a cleared input is sent empty, so the server clears it (or refuses a required one)", async () => {
    const bodies: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH") bodies.push(await req.json());
      return json(row);
    });
    const slot = mountEdit();
    fireEvent.input(await screen.findByLabelText("port"), { target: { value: "" } });
    await slot.save();
    await waitFor(() => expect(bodies.length).toBe(1));
    expect(bodies[0]).toEqual({ inputs: { port: "" } });
  });

  it("a label-only edit sends no inputs", async () => {
    const bodies: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH") bodies.push(await req.json());
      return json(row);
    });
    const slot = mountEdit();
    fireEvent.input(await screen.findByLabelText("Label"), { target: { value: "Lobby panel" } });
    await slot.save();
    await waitFor(() => expect(bodies.length).toBe(1));
    expect(bodies[0]).toEqual({ label: "Lobby panel" });
  });
});

// The blade reads the endpoint's held session (#1059): the pill, the ladder,
// the reason, Retry gated on endpoint:retry, and the path to fix a refused
// credential in place, which is this blade's own edit.
describe("endpoint blade, held session", () => {
  const telnet: Driver = {
    id: uuidFor("newtron-nvp-telnet"), name: "newtron-nvp-telnet", label: "Newtron NVP over Telnet", official: true,
    spec: { version: 1, transport: "telnet", inputs: [
      { name: "host", kind: "string", required: true },
      { name: "port", kind: "number", default: "23" },
      { name: "login", kind: "secret", secret_type: "basic-auth", required: true },
    ], polls: [] },
  };
  const compId = uuidFor("panel-2");
  const row = {
    id: uuidFor("ep-rejected"), name: "telnet", transport: "telnet", component: "panel-2", component_id: compId,
    params: { target: "127.60.0.9:23" }, driver: "newtron-nvp-telnet", driver_id: uuidFor("newtron-nvp-telnet"),
    inputs: { host: "127.60.0.9", port: "23", login: "panel-login-old" },
  };
  const now = new Date().toISOString();
  const reach: Reachability = {
    component: "panel-2",
    endpoints: [{
      id: row.id, endpoint: "telnet", transport: "telnet", node: "edge-hq", last_seen: now,
      verdict: { value: "up", ts: now }, responsive: { value: "up", ts: now }, authenticated: { value: "no", ts: now },
      session: { state: "rejected", ts: now, reason: "auth: auth rejected" },
      rejection: { rung: "authenticated", class: "auth", ts: "2026-10-23T11:58:00.000Z" },
      layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: now }], history: [],
    }],
  };
  const operator: Me = { principal: { id: "op", kind: "human" }, permissions: ["*:read", "endpoint:update", "endpoint:retry"], grants: [] };
  const viewer: Me = { principal: { id: "vw", kind: "human" }, permissions: ["*:read"], grants: [] };

  function mountHeld(me: Me) {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ENDPOINTS_KEY], [row]);
    qc.setQueryData([...DRIVERS_KEY], [telnet]);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...REACHABILITY_KEY(compId)], reach);
    const slot = createEditSlot();
    const blades = { pop: () => {} } as unknown as BladeController;
    render(() => (
      <QueryClientProvider client={qc}>
        <BladesContext.Provider value={blades}>
          <BladeEditContext.Provider value={slot}>
            <endpointBlade.Body id={row.id} />
          </BladeEditContext.Provider>
        </BladesContext.Provider>
      </QueryClientProvider>
    ));
    return slot;
  }

  it("reads the session rejected, with its reason and its ladder", () => {
    mountHeld(operator);
    expect(screen.getByTestId("session-pill").dataset.state).toBe("rejected");
    expect(screen.getByTestId("session-reason").textContent).toBe("auth: auth rejected");
    expect(screen.getAllByTestId("ladder-rung").map((r) => r.dataset.status)).toEqual(["pass", "pass", "held"]);
    expect((screen.getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("fixes the refused credential in place: the blade's own edit, the secret input focused", async () => {
    const slot = mountHeld(operator);
    fireEvent.click(screen.getByRole("button", { name: "Edit credential" }));
    expect(slot.editing()).toBe(true);
    const login = (await screen.findByLabelText("login")) as HTMLInputElement;
    expect(login.value).toBe("panel-login-old");
    await waitFor(() => expect(document.activeElement).toBe(login));
  });

  it("greys Retry for a viewer and offers no edit", () => {
    mountHeld(viewer);
    expect((screen.getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByLabelText(/needs the endpoint:retry permission/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit credential" })).toBeNull();
  });
});

// #1110: an endpoint gated by its reachability probe alone (an snmp community
// the agent refuses) holds no session, and its blade reads the refusal the
// same way: the reason, Retry, and Edit credential into the community input.
describe("endpoint blade, a probe's gate with no held session", () => {
  const snmp: Driver = {
    id: uuidFor("snmp-generic"), name: "snmp-generic", label: "Generic SNMP", official: true,
    spec: { version: 1, transport: "snmp", inputs: [
      { name: "host", kind: "string", required: true },
      { name: "port", kind: "number", default: "161" },
      { name: "community", kind: "secret", secret_type: "snmp-community", required: true },
    ], polls: [] },
  };
  const compId = uuidFor("panel-3");
  const row = {
    id: uuidFor("ep-snmp-gated"), name: "snmp", transport: "snmp", component: "panel-3", component_id: compId,
    params: { target: "127.60.0.9:10161" }, driver: "snmp-generic", driver_id: uuidFor("snmp-generic"),
    inputs: { host: "127.60.0.9", port: "10161", community: "snmp-revoked" },
  };
  const now = new Date().toISOString();
  const reach: Reachability = {
    component: "panel-3",
    endpoints: [{
      id: row.id, endpoint: "snmp", transport: "snmp", node: "edge-hq", last_seen: now,
      verdict: { value: "down", ts: now }, responsive: { value: "down", ts: now }, authenticated: { value: "no", ts: now },
      rejection: { rung: "authenticated", class: "auth", ts: now, reason: "auth: the agent answered authorizationError for the community" },
      layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: now }], history: [],
    }],
  };
  const operator: Me = { principal: { id: "op", kind: "human" }, permissions: ["*:read", "endpoint:update", "endpoint:retry"], grants: [] };

  it("reads the refusal and fixes the community in place", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ENDPOINTS_KEY], [row]);
    qc.setQueryData([...DRIVERS_KEY], [snmp]);
    qc.setQueryData([...ME_KEY], operator);
    qc.setQueryData([...REACHABILITY_KEY(compId)], reach);
    const slot = createEditSlot();
    const blades = { pop: () => {} } as unknown as BladeController;
    render(() => (
      <QueryClientProvider client={qc}>
        <BladesContext.Provider value={blades}>
          <BladeEditContext.Provider value={slot}>
            <endpointBlade.Body id={row.id} />
          </BladeEditContext.Provider>
        </BladesContext.Provider>
      </QueryClientProvider>
    ));
    expect(screen.queryByTestId("session-pill")).toBeNull();
    expect(screen.getByTestId("session-reason").textContent).toBe("auth: the agent answered authorizationError for the community");
    expect((screen.getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Edit credential" }));
    expect(slot.editing()).toBe(true);
    const community = (await screen.findByLabelText("community")) as HTMLInputElement;
    expect(community.value).toBe("snmp-revoked");
    await waitFor(() => expect(document.activeElement).toBe(community));
  });
});
