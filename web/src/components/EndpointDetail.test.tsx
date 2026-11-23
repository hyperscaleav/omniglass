import { describe, it, expect, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import EndpointDetail from "./EndpointDetail";
import ReachabilityPanel from "./ReachabilityPanel";
import { endpointBlade } from "./endpointBlades";
import { BladeEditContext, BladesContext, createEditSlot, type BladeController } from "../lib/blades";
import { ENDPOINTS_KEY, type Endpoint } from "../lib/endpoints";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";
import { effectiveMetricsKey } from "../lib/component_metrics";
import { ME_KEY, type Me } from "../lib/auth";
import { effectivePropertiesKey } from "../lib/component_properties";
import { METRICS_KEY } from "../lib/metric_types";
import { ENDPOINT_FIELDS } from "../lib/endpoint_detail";
import { uuidFor } from "../lib/testids";

// EndpointDetail (#2436): the drawer never shows less than the page, because
// the page's expanded row and the drawer render this one body.

const cid = uuidFor("ed-comp");
const eid = uuidFor("ed-ssh");
const now = new Date().toISOString();
const me: Me = { user: { id: "op", kind: "person" }, permissions: ["*:read", "endpoint:run"], grants: [] };

const driver: Driver = {
  id: uuidFor("ed-driver"), name: "panel-ssh", label: "Panel SSH", official: true, version: "1.4.0",
  spec: {
    version: 1, transport: "ssh",
    inputs: [
      { name: "host", kind: "string", required: true },
      { name: "port", kind: "number", default: "22" },
      { name: "login", kind: "secret", secret_type: "ssh-login", required: true },
    ],
    polls: [{ name: "status", schedule: { every: "30s" }, request: {}, emits: [{ name: "cpu-load", extract: {} }] }],
  },
};
const ep: Endpoint = {
  id: eid, name: "ssh", label: "Panel shell", transport: "ssh", component: "panel-1", component_id: cid, outpost: "edge-hq",
  params: { target: "10.0.0.5", port: 22 }, driver: "panel-ssh", driver_id: driver.id,
  inputs: { host: "10.0.0.5", port: "22", login: "sim-panel-login" }, out_of_band: true,
};
const reach: Reachability = {
  component: "panel-1",
  endpoints: [{
    id: eid, endpoint: "ssh", label: "Panel shell", transport: "ssh", address: "10.0.0.5:22", outpost: "edge-hq", last_seen: now,
    reachability: { value: "up", ts: now },
    session: { state: "up", ts: now },
    gate_reasons: [{ kind: "disrupted", since: now, source: "command-7" }],
    layers: [
      { layer: "ping", check: "icmp-reachable", value: "up", ts: now },
      { layer: "port", check: "tcp-open", value: "open", ts: now },
    ],
    history: [{ ts: new Date(Date.now() - 600_000).toISOString(), value: "up" }],
  }],
};
const metrics = [
  { data_type: "float", from_contract: true, instance: "ssh", instance_count: 1, is_sampled: true, metric_type_id: "m1", metric_type_name: "cpu-load", required: false, unit: "percent", value: 12.5, sample_value: 12.5 },
];

function client() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...ENDPOINTS_KEY], [ep]);
  qc.setQueryData([...DRIVERS_KEY], [driver]);
  qc.setQueryData([...METRICS_KEY], []);
  qc.setQueryData([...REACHABILITY_KEY(cid)], reach);
  qc.setQueryData([...effectiveMetricsKey(cid)], metrics);
  qc.setQueryData([...effectivePropertiesKey(cid)], []);
  return qc;
}

function mountDrawer() {
  const slot = createEditSlot();
  const blades = { pop: () => {} } as unknown as BladeController;
  return render(() => (
    <QueryClientProvider client={client()}>
      <BladesContext.Provider value={blades}>
        <BladeEditContext.Provider value={slot}>
          <Router><Route path="/*" component={() => <endpointBlade.Body id={eid} />} /></Router>
        </BladeEditContext.Provider>
      </BladesContext.Provider>
    </QueryClientProvider>
  ));
}

function mountRow() {
  const r = render(() => (
    <QueryClientProvider client={client()}>
      <Router><Route path="/*" component={() => <ReachabilityPanel name={cid} />} /></Router>
    </QueryClientProvider>
  ));
  fireEvent.click(screen.getByRole("button", { expanded: false }));
  return r;
}

const fields = (root: ParentNode) => [...root.querySelectorAll("[data-field]")].map((n) => (n as HTMLElement).dataset.field!);

afterEach(cleanup);

describe("EndpointDetail", () => {
  it("renders every field id in the drawer and in the expanded row, the same ones", () => {
    const d = mountDrawer();
    const drawer = fields(d.container);
    d.unmount();
    const r = mountRow();
    const row = fields(r.container);
    expect(row).toEqual(drawer);
    expect([...row].sort()).toEqual([...ENDPOINT_FIELDS].sort());
  });

  it("states the driver as a link into its drawer, with its version", () => {
    mountDrawer();
    const link = within(document.querySelector('[data-field="driver"]') as HTMLElement).getByRole("link", { name: "panel-ssh" });
    expect(link.getAttribute("href")).toBe("/drivers?open=panel-ssh");
    expect(screen.getByText("v1.4.0")).toBeTruthy();
  });

  it("shows availability with the uptime figure and the rungs with their times", () => {
    mountDrawer();
    expect(within(document.querySelector('[data-field="availability"]') as HTMLElement).getByText(/% up/)).toBeTruthy();
    const ladder = document.querySelector('[data-field="ladder"]') as HTMLElement;
    expect(within(ladder).getByText("icmp-reachable")).toBeTruthy();
    expect(ladder.textContent).toMatch(/\d+s ago/);
  });

  it("lists held gate reasons beside the session", () => {
    mountDrawer();
    const reasons = screen.getByTestId("gate-reasons");
    expect(reasons.textContent).toMatch(/disrupted/);
    expect(reasons.textContent).toMatch(/held since/);
  });

  it("gives parameters and inputs their provenance, and the secret as a name only", () => {
    mountDrawer();
    const inputs = document.querySelector('[data-field="inputs"]') as HTMLElement;
    expect(inputs.textContent).toMatch(/port.*22.*driver default/);
    expect(inputs.textContent).toMatch(/host.*10\.0\.0\.5.*set on this endpoint/);
    expect((document.querySelector('[data-field="parameters"]') as HTMLElement).textContent).toMatch(/derived from inputs/);
    const secrets = document.querySelector('[data-field="secrets"]') as HTMLElement;
    expect(within(secrets).getByText("sim-panel-login")).toBeTruthy();
  });

  it("says the endpoint is out of band", () => {
    mountDrawer();
    expect((document.querySelector('[data-field="out-of-band"]') as HTMLElement).textContent).toMatch(/management path/);
  });

  it("draws each poll with its interval and last read, and the poll test beside it", () => {
    mountDrawer();
    const row = screen.getByTestId("poll-row");
    expect(row.textContent).toMatch(/status/);
    expect(row.textContent).toMatch(/every 30s/);
    expect(row.textContent).toMatch(/read 1 of 1 values/);
    expect(row.textContent).toMatch(/cpu-load 12\.5 percent/);
    expect(screen.getByTestId("poll-test")).toBeTruthy();
  });

  it("collapses a long section to its one-line summary and opens it again", () => {
    mountDrawer();
    const params = document.querySelector('[data-field="parameters"]') as HTMLElement;
    const trigger = within(params).getByRole("button");
    expect(trigger.textContent).toMatch(/Parameters/);
    expect(trigger.textContent).toMatch(/2 · target, port/);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(trigger.textContent).toMatch(/2 · target, port/);
  });

  it("says so when the endpoint is gone", () => {
    const qc = client();
    qc.setQueryData([...ENDPOINTS_KEY], []);
    render(() => (
      <QueryClientProvider client={qc}><Router><Route path="/*" component={() => <EndpointDetail id={eid} host="blade" />} /></Router></QueryClientProvider>
    ));
    expect(screen.getByText(/no longer available/)).toBeTruthy();
  });
});

describe("the endpoint row", () => {
  it("lights the whole row on hover, not the toggle alone", () => {
    render(() => (
      <QueryClientProvider client={client()}>
        <Router><Route path="/*" component={() => <ReachabilityPanel name={cid} />} /></Router>
      </QueryClientProvider>
    ));
    const toggle = screen.getByRole("button", { expanded: false });
    expect(toggle.className).not.toMatch(/hover:bg/);
    expect((toggle.parentElement as HTMLElement).className).toMatch(/hover:bg-base-content\/5/);
  });
});
