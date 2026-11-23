import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route, useParams } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AddComponentFlow from "./AddComponentFlow";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { COMPONENT_TYPES_KEY, type ComponentType } from "../lib/component_types";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { SYSTEMS_KEY, type System } from "../lib/systems";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { OUTPOSTS_KEY } from "../lib/outposts";
import { SECRETS_KEY } from "../lib/secrets";
import { SIMULATION_KEY } from "../lib/simulation";
import { METRICS_KEY } from "../lib/metric_types";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";
import { pickInPicker, typeInPicker } from "./ui/Picker.testkit";
import { pickTree } from "./TreeSelect.testkit";

// The add-a-component flow (#2460), walked as an operator walks it against a
// stubbed API: each step, the driver the product suggests, the simulated
// device that fills the address, the credential picked from a list, the test,
// and the promise that a cancel or a Back from the test leaves nothing behind.

const C_NEW = uuidFor("c-new");
const E_NEW = uuidFor("e-new");
const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };

const types: ComponentType[] = [
  { id: uuidFor("ct-device"), name: "device", label: "Device", official: true, forked: false, stem: "device" },
  { id: uuidFor("ct-display"), name: "display", label: "Display", official: true, forked: false, parent: "device", stem: "display" },
] as ComponentType[];
const products: Product[] = [
  { id: uuidFor("p-55"), name: "boreal-edge-55", label: "Boreal Edge 55", kind: "device", component_type: "display", component_type_id: uuidFor("ct-display"), vendor: "boreal", driver: "boreal-edge-snmp", official: true },
  { id: uuidFor("p-65"), name: "boreal-edge-65", label: "Boreal Edge 65", kind: "device", component_type: "display", component_type_id: uuidFor("ct-display"), vendor: "boreal", driver: "boreal-edge-http", official: true },
];
const poll = { name: "identity", schedule: { every: "30s" }, request: {}, emits: [] };
const drivers: Driver[] = [
  { id: uuidFor("d1"), name: "aa-other", label: "AA Other", official: true, spec: { version: 1, transport: "http", polls: [poll] } },
  { id: uuidFor("d2"), name: "boreal-edge-http", label: "Boreal Edge HTTP", official: true, spec: { version: 1, transport: "http", polls: [poll] } },
  {
    id: uuidFor("d3"),
    name: "boreal-edge-snmp",
    label: "Boreal Edge SNMP",
    official: true,
    spec: {
      version: 1,
      transport: "snmp",
      inputs: [
        { name: "host", kind: "string", required: true },
        { name: "port", kind: "number", default: "161" },
        { name: "community", kind: "secret", secret_type: "snmp-community", required: true },
      ],
      polls: [poll],
    },
  },
];
const room: Location = { id: uuidFor("l-room"), name: "lobby", label: "Lobby", location_type: "room", effective_tags: {} };
const systems: System[] = [{ id: uuidFor("s-lobby"), name: "lobby-av", label: "Lobby AV", location_id: room.id, member_count: 0, actions: ["update"] } as System];
const secrets = [
  { id: uuidFor("s1"), name: "sim-community", label: "Sim community", secret_type: "snmp-community", owner_kind: "platform", fields: [] },
  { id: uuidFor("s2"), name: "kestrel-login", secret_type: "http-basic", owner_kind: "platform", fields: [] },
];
const fleet = { connected: true, outposts: [{ outpost: "edge-hq", connected: true, last_seen: "", capabilities: {}, impairments: { available: true }, components: [{ component_id: "x", component: "display-1", product: "boreal-edge-55", address: "127.60.0.4", hits: 1, hits_by_protocol: {}, impaired: false, serving: true, status: "running" }] }] };
const plan = { outpost: "edge-hq", product: "boreal-edge-55", address: "127.60.0.9", endpoints: [{ driver: "boreal-edge-snmp", inputs: { host: "127.60.0.9", port: "161", community: "sim-community" } }] };
const slots = [
  { name: "display", label: "Display", accepted_types: ["display"], pinned_products: null, assigned: 0, assigned_to: [], from_standard: true, impact: "outage", quorum: 1 },
  { name: "codec", label: "Codec", accepted_types: ["codec"], pinned_products: null, assigned: 0, assigned_to: [], from_standard: true, impact: "outage", quorum: 1 },
];

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": status >= 400 ? "application/problem+json" : "application/json" } });

type Calls = string[];
function stub(opts: { pollFails?: { status: number; detail: string } } = {}) {
  const calls: Calls = [];
  let endpoints: unknown[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    const url = new URL(req.url);
    const path = decodeURIComponent(url.pathname.replace(/^.*\/api\/v1/, ""));
    const line = `${req.method} ${path}`;
    if (req.method === "POST" && path.endsWith(":renderLabel")) {
      const b = JSON.parse(await req.clone().text());
      return json(b.name ? { name: b.name, label: "", rule: "" } : { name: "display-3", ordinal: 3, label: "Display 3", rule: "" });
    }
    if (line === `GET /systems/${systems[0].id}/slots`) return json({ slots });
    if (line === "GET /outposts/edge-hq/simulation/components:plan") return json(plan);
    if (line === "GET /endpoints") return json({ endpoints });
    calls.push(line + (req.method === "POST" || req.method === "PUT" ? ` ${await req.clone().text()}` : ""));
    if (line === "POST /components") return json({ id: C_NEW, name: "display-3" }, 201);
    if (line.startsWith("PUT /systems/")) return new Response(null, { status: 204 });
    if (line === "POST /endpoints") {
      endpoints = [{ id: E_NEW, name: "snmp", transport: "snmp", driver: "boreal-edge-snmp", outpost: "edge-hq", component_id: C_NEW }];
      return json(endpoints[0], 201);
    }
    if (line === `POST /endpoints/${E_NEW}/polls/identity:run`) {
      if (opts.pollFails) return json({ title: "x", ...opts.pollFails }, opts.pollFails.status);
      return json({ endpoint: "snmp", outpost: "edge-hq", poll: "identity", samples: [{ name: "model-number", text: "Edge 55", ts: "2026-11-23T14:00:00Z" }], took_ms: 4 });
    }
    if (req.method === "DELETE") {
      endpoints = [];
      return new Response(null, { status: 204 });
    }
    throw new Error(`unexpected fetch: ${line}`);
  });
  return calls;
}

function mount(path = "/components/add") {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...PRODUCTS_KEY], products);
  qc.setQueryData([...COMPONENT_TYPES_KEY], types);
  qc.setQueryData([...DRIVERS_KEY], drivers);
  qc.setQueryData([...SYSTEMS_KEY], systems);
  qc.setQueryData([...LOCATIONS_KEY], [room]);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...OUTPOSTS_KEY], [{ name: "edge-hq", enrolled: true }]);
  qc.setQueryData([...SECRETS_KEY], secrets);
  qc.setQueryData([...SIMULATION_KEY], fleet);
  qc.setQueryData([...METRICS_KEY], []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/components/add" component={AddComponentFlow} />
        <Route path="/components/:id" component={() => <div data-testid="landed">{useParams().id}</div>} />
        <Route path="/explore" component={() => <div data-testid="explore" />} />
      </Router>
    </QueryClientProvider>
  ));
}

// The rail's primary button, pressed once it is enabled (the name draft, the
// slots and the plan answer on their own schedule).
async function next(name = "Next") {
  const b = await screen.findByRole("button", { name });
  await waitFor(() => expect((b as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(b);
}
const current = () => document.querySelector("[aria-current=step]")?.textContent;

// The happy path up to the commit, shared by every test that needs a created pair.
async function walkToCredentials() {
  await pickInPicker(screen.getByLabelText("Product"), "Boreal Edge 55");
  await next();
  await waitFor(() => expect(current()).toBe("Where"));
  await pickTree(screen.getByLabelText("System"), "Lobby AV");
  await waitFor(() => expect(screen.getByText(/Taken from Lobby AV/)).toBeTruthy());
  const slot = (await screen.findByLabelText("Slot")) as HTMLSelectElement;
  await waitFor(() => expect([...slot.options].map((o) => o.textContent)).toEqual(["No slot", "Display"]));
  fireEvent.change(slot, { target: { value: "display" } });
  await next();
  await waitFor(() => expect(current()).toBe("Endpoint"));
  fireEvent.click(await screen.findByRole("button", { name: "Use a simulated device" }));
  await waitFor(() => expect((screen.getByLabelText(/host/) as HTMLInputElement).value).toBe("127.60.0.9"));
  await next();
  await waitFor(() => expect(current()).toBe("Credentials"));
}

afterEach(() => vi.restoreAllMocks());

describe("AddComponentFlow", () => {
  it("offers products by name, type and vendor", async () => {
    stub();
    mount();
    const offered = await typeInPicker(screen.getByLabelText("Product"), "display");
    expect(offered).toEqual(["Boreal Edge 55Display, boreal", "Boreal Edge 65Display, boreal"]);
  });

  it("walks product, place, simulated endpoint and picked credential to a test that read values, then lands on the component", async () => {
    const calls = stub();
    mount();
    await walkToCredentials();
    // The credential is a picker, preset from the simulator's plan, never a typed name.
    expect((screen.getByLabelText("community") as HTMLInputElement).value).toBe("Sim community");
    expect(calls).toEqual([]);
    await next("Create and test");
    await waitFor(() => expect(current()).toBe("Test"));
    expect(calls[0]).toMatch(/^POST \/components .*"product":"boreal-edge-55".*/);
    expect(JSON.parse(calls[0].slice("POST /components ".length))).toMatchObject({ product: "boreal-edge-55", expected_name: "display-3", system: systems[0].id, location: room.id });
    expect(calls[1]).toBe(`PUT /systems/${systems[0].id}/slots/display/assignments/${C_NEW} `);
    expect(JSON.parse(calls[2].slice("POST /endpoints ".length))).toEqual({ driver: "boreal-edge-snmp", inputs: { host: "127.60.0.9", port: "161", community: "sim-community" }, component: C_NEW, outpost: "edge-hq" });
    // The test runs on its own and shows what it read.
    await screen.findByText("Edge 55");
    await next("Open the component");
    await waitFor(() => expect(screen.getByTestId("landed").textContent).toBe(C_NEW));
    expect(calls.some((c) => c.startsWith("DELETE"))).toBe(false);
  });

  it("preselects the product's own driver and lists its kin before the rest", async () => {
    stub();
    mount("/components/add?product=boreal-edge-55");
    await next();
    await next();
    await waitFor(() => expect(current()).toBe("Endpoint"));
    const driver = screen.getByLabelText("Driver") as HTMLInputElement;
    await waitFor(() => expect(driver.value).toBe("Boreal Edge SNMP"));
    const offered = await typeInPicker(driver, "");
    expect(offered).toEqual(["Boreal Edge SNMPthis product's driver", "Boreal Edge HTTPused by other Display products"]);
  });

  it("takes the component and its endpoint back out on a cancel from the test", async () => {
    const calls = stub();
    mount();
    await walkToCredentials();
    await next("Create and test");
    await screen.findByText("Edge 55");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await screen.findByTestId("explore");
    expect(calls.filter((c) => c.startsWith("DELETE"))).toEqual([`DELETE /endpoints/${E_NEW}`, `DELETE /components/${C_NEW}`]);
  });

  it("creates nothing when cancelled before the commit", async () => {
    const calls = stub();
    mount();
    await walkToCredentials();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await screen.findByTestId("explore");
    expect(calls).toEqual([]);
  });

  it("names a refused login and goes back to the credentials, taking the pair back out", async () => {
    const calls = stub({ pollFails: { status: 409, detail: "the endpoint is gated, so the poll was not sent: auth: the agent answered authorizationError for the community" } });
    mount();
    await walkToCredentials();
    await next("Create and test");
    const fix = await screen.findByTestId("flow-fix");
    expect(fix.textContent).toMatch(/refused the credential/);
    fireEvent.click(within(fix).getByRole("button", { name: "Fix the credentials" }));
    await waitFor(() => expect(current()).toBe("Credentials"));
    expect(calls.filter((c) => c.startsWith("DELETE"))).toEqual([`DELETE /endpoints/${E_NEW}`, `DELETE /components/${C_NEW}`]);
  });

  it("starts in the system it was opened from, by name, with the system's place", async () => {
    stub();
    mount("/components/add?system=lobby-av&product=boreal-edge-55");
    await next();
    await waitFor(() => expect(current()).toBe("Where"));
    expect(screen.getByLabelText("System").textContent).toBe("Lobby AV");
    expect(screen.getByText(/Taken from Lobby AV/)).toBeTruthy();
  });

  it("sends an address that did not answer back to the endpoint step", async () => {
    stub({ pollFails: { status: 504, detail: "no answer from 127.60.0.9:161" } });
    mount();
    await walkToCredentials();
    await next("Create and test");
    const fix = await screen.findByTestId("flow-fix");
    fireEvent.click(within(fix).getByRole("button", { name: "Fix the endpoint" }));
    await waitFor(() => expect(current()).toBe("Endpoint"));
  });
});
