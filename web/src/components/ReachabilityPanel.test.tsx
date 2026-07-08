import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ReachabilityPanel from "./ReachabilityPanel";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";

// The panel is read-only and derives verdict/strip/reason client-side from the
// real API fields. Data is seeded into the query cache so no server is needed.
const nowIso = new Date().toISOString();
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const seed: Reachability = {
  component: "disp-1",
  interfaces: [
    {
      interface: "disp-1-tcp",
      type: "tcp",
      endpoint: "10.20.4.11:5000",
      node: "node-a",
      verdict: { value: "up", ts: nowIso },
      layers: [
        { layer: "ping", check: "icmp.reachable", value: 1, detail: "12.0 ms", ts: nowIso },
        { layer: "port", check: "tcp.open", value: 1, detail: "3.1 ms", ts: nowIso },
      ],
      history: [{ ts: ago(120_000), value: "up" }],
    },
    {
      interface: "disp-1-icmp",
      type: "icmp",
      endpoint: "10.20.4.11",
      node: "node-a",
      verdict: { value: "down", ts: nowIso },
      layers: [
        { layer: "ping", check: "icmp.reachable", value: 1, ts: nowIso },
        { layer: "port", check: "tcp.open", value: 0, ts: nowIso },
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
    expect(getByText("icmp.reachable")).toBeTruthy();
    expect(getByText("tcp.open")).toBeTruthy();
    expect(getByText(/probed by/)).toBeTruthy();
  });

  it("derives stale and unknown verdicts client-side", () => {
    const stale: Reachability = {
      component: "c2",
      interfaces: [
        { interface: "i-stale", type: "tcp", verdict: { value: "up", ts: ago(600_000) }, layers: [], history: [] },
        { interface: "i-unknown", type: "tcp", verdict: null, layers: [], history: [] },
      ],
    };
    const { getByText } = mount(stale);
    expect(getByText("stale")).toBeTruthy();
    expect(getByText("unknown")).toBeTruthy();
  });

  it("shows the empty state when a component has no interfaces", () => {
    const { getByText } = mount({ component: "c3", interfaces: [] });
    expect(getByText(/no reachability checks/i)).toBeTruthy();
  });
});
