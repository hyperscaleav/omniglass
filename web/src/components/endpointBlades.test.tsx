import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { CreateEndpointForm } from "./endpointBlades";
import { BladeEditContext, createEditSlot } from "../lib/blades";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { TRANSPORTS_KEY, type Transport } from "../lib/endpoints";
import { COMPONENTS_KEY } from "../lib/components";
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
