import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ReachabilityPanel from "./ReachabilityPanel";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";
import { ENDPOINTS_KEY, type Endpoint } from "../lib/endpoints";
import { uuidFor } from "../lib/testids";

// The panel is read-only and derives verdict/strip/reason client-side from the
// real API fields. Data is seeded into the query cache so no server is needed.
const nowIso = new Date().toISOString();
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const seed: Reachability = {
  component: "disp-1",
  endpoints: [
    {
      endpoint: "disp-1-tcp",
      transport: "tcp",
      address: "10.20.4.11:5000",
      node: "node-a",
      verdict: { value: "up", ts: nowIso },
      layers: [
        { layer: "ping", check: "icmp-reachable", value: 1, detail: "12.0 ms", ts: nowIso },
        { layer: "port", check: "tcp-open", value: 1, detail: "3.1 ms", ts: nowIso },
      ],
      history: [{ ts: ago(120_000), value: "up" }],
    },
    {
      endpoint: "disp-1-icmp",
      transport: "icmp",
      address: "10.20.4.11",
      node: "node-a",
      verdict: { value: "down", ts: nowIso },
      layers: [
        { layer: "ping", check: "icmp-reachable", value: 1, ts: nowIso },
        { layer: "port", check: "tcp-open", value: 0, ts: nowIso },
      ],
      history: [
        { ts: ago(120_000), value: "up" },
        { ts: ago(30_000), value: "down" },
      ],
    },
  ],
};

function mount(data: Reachability = seed) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...REACHABILITY_KEY(data.component)], data);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ReachabilityPanel name={data.component} />
    </QueryClientProvider>
  ));
}

describe("ReachabilityPanel", () => {
  it("renders the interface count and a verdict pill per interface", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText("2 interfaces")).toBeTruthy();
    expect(getByText("responding")).toBeTruthy();
    expect(getByText("down")).toBeTruthy();
    // both endpoints render as type · endpoint fragments
    expect(getAllByText(/10\.20\.4\.11/).length).toBeGreaterThan(0);
  });

  it("shows an availability strip with an uptime hint", () => {
    const { getAllByText } = mount();
    // the icmp interface (up 120s->30s, down 30s->now) is ~75% up
    expect(getAllByText(/% up/).length).toBe(2);
  });

  it("expands a row to the gate breakdown and the reason line for a down interface", () => {
    const { getByText, queryByText } = mount();
    // reason line hidden until expanded
    expect(queryByText(/service down, box up/i)).toBeNull();
    // expand the down (icmp) interface row via its name button
    fireEvent.click(getByText("disp-1-icmp"));
    expect(getByText(/service down, box up/i)).toBeTruthy();
    // the gate breakdown lists the layer checks and the verdict line
    expect(getByText("icmp-reachable")).toBeTruthy();
    expect(getByText("tcp-open")).toBeTruthy();
    expect(getByText(/probed by/)).toBeTruthy();
  });

  it("derives stale and unknown verdicts client-side", () => {
    const stale: Reachability = {
      component: "c2",
      endpoints: [
        { endpoint: "i-stale", transport: "tcp", verdict: { value: "up", ts: ago(600_000) }, layers: [], history: [] },
        { endpoint: "i-unknown", transport: "tcp", verdict: null, layers: [], history: [] },
      ],
    };
    const { getByText } = mount(stale);
    expect(getByText("stale")).toBeTruthy();
    expect(getByText("unknown")).toBeTruthy();
  });

  it("shows the empty state when a component has no interfaces", () => {
    const { getByText } = mount({ component: "c3", endpoints: [] });
    expect(getByText(/no interfaces on this component/i)).toBeTruthy();
  });
});

// The panel doubles as the component's Interfaces management surface: it shows an
// "Add interface" header affordance and a per-row "Manage" affordance ONLY when the
// component detail passes their callbacks (which it gates on interface:create /
// interface:read). A row maps to its interface id via the seeded interfaces list,
// matched by component_id (#627): the console addresses this panel by the
// component's uuid now (two components can share a name, ADR-0062), so the
// interfaces list is matched on component_id, not the component name.
const compId = uuidFor("comp-disp-1");
const ifaceSeed: Endpoint[] = [
  { id: uuidFor("if-1"), name: "disp-1-tcp", transport: "tcp", component: "disp-1", component_id: compId },
  { id: uuidFor("if-2"), name: "disp-1-icmp", transport: "icmp", component: "disp-1", component_id: compId },
];

function mountManaged(opts: { onAdd?: () => void; onOpenEndpoint?: (id: string) => void }) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...REACHABILITY_KEY(compId)], seed);
  qc.setQueryData([...ENDPOINTS_KEY], ifaceSeed);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ReachabilityPanel name={compId} onAdd={opts.onAdd} onOpenEndpoint={opts.onOpenEndpoint} />
    </QueryClientProvider>
  ));
}

describe("ReachabilityPanel management affordances", () => {
  it("shows the Add interface affordance only when onAdd is provided", () => {
    const { queryByText, unmount } = mountManaged({});
    expect(queryByText("Add interface")).toBeNull();
    unmount();
    const onAdd = vi.fn();
    mountManaged({ onAdd });
    fireEvent.click(screen.getByText("Add interface"));
    expect(onAdd).toHaveBeenCalledOnce();
  });

  it("surfaces a per-row Manage affordance that opens the interface by id", () => {
    const opened: string[] = [];
    mountManaged({ onOpenEndpoint: (id) => opened.push(id) });
    // No Manage affordance without the callback.
    fireEvent.click(screen.getByLabelText("Manage disp-1-tcp"));
    expect(opened).toEqual([uuidFor("if-1")]);
  });

  it("omits the Manage affordance when onOpenEndpoint is absent", () => {
    mountManaged({});
    expect(screen.queryByLabelText("Manage disp-1-tcp")).toBeNull();
  });
});

// Two interfaces of ONE protocol on one component: their derived names are the
// only thing the platform gives them, and it gives them the same one. The label
// is what tells them apart, and an interface without one still reads its derived
// name verbatim rather than a prettified version of it (#613).
describe("ReachabilityPanel identity", () => {
  const twoOfAKind: Reachability = {
    component: "codec-1",
    endpoints: [
      { endpoint: "ssh", label: "Control processor", transport: "ssh", address: "10.0.0.9:22", node: "node-a", verdict: null, layers: [], history: [] },
      { endpoint: "ssh", transport: "ssh", address: "10.0.0.10:22", node: "node-a", verdict: null, layers: [], history: [] },
    ],
  };

  it("reads the label where there is one and the derived name where there is not", () => {
    mount(twoOfAKind);
    expect(screen.getByText("Control processor")).toBeTruthy();
    // The unlabelled row still says `ssh`, exactly, in its identity line.
    expect(screen.getAllByText("ssh").length).toBeGreaterThan(0);
    expect(screen.queryByText("Ssh")).toBeNull();
  });
});

// The layer-7 rungs (#812): the responds and auth chips render only once a
// probe has climbed them, so a plain tcp endpoint's row is untouched.
describe("the layer-7 rungs", () => {
  it("renders responds and auth chips from the read, and omits them when absent", () => {
    mount({
      component: compId,
      endpoints: [
        {
          endpoint: "ssh", transport: "ssh", address: "10.0.0.9:22",
          verdict: { value: "up", ts: new Date().toISOString() },
          responsive: { value: "up", ts: new Date().toISOString() },
          authenticated: { value: "no", ts: new Date().toISOString() },
          layers: [], history: [],
        },
        { endpoint: "disp-1-tcp", transport: "tcp", verdict: null, layers: [], history: [] },
      ],
    });
    expect(screen.getByText("responds")).toBeTruthy();
    expect(screen.getByText("auth failed")).toBeTruthy();
    expect(screen.queryByText("no response")).toBeNull();
    expect(screen.queryByText("auth ok")).toBeNull();
  });
});
