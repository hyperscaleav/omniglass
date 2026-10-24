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
  // An endpoint answering every probe since an old transition (#931): the
  // pill reads responding and "last checked" is its newest probe sample.
  it("ages a long-up endpoint by its newest sample, not its last transition", () => {
    const old = ago(21 * 60_000);
    const { getByText, queryByText } = mount({
      component: "disp-2",
      endpoints: [{
        endpoint: "disp-2-tcp", transport: "tcp", node: "node-a",
        verdict: { value: "up", ts: old },
        last_seen: ago(5_000),
        layers: [{ layer: "port", check: "tcp-open", value: 1, ts: ago(5_000) }],
        history: [{ ts: old, value: "up" }],
      }],
    });
    expect(getByText("responding")).toBeTruthy();
    expect(queryByText("stale")).toBeNull();
    fireEvent.click(getByText("disp-2-tcp"));
    expect(getByText(/last checked/).textContent).not.toMatch(/21 ?m/);
  });

  it("renders the endpoint count and a verdict pill per endpoint", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText("2 endpoints")).toBeTruthy();
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

  it("expands a row to the gate breakdown and the reason line for a down endpoint", () => {
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

  it("shows the empty state when a component has no endpoints", () => {
    const { getByText } = mount({ component: "c3", endpoints: [] });
    expect(getByText(/no endpoints on this component/i)).toBeTruthy();
  });
});

// The panel doubles as the component's Interfaces management surface: it shows an
// "Add endpoint" header affordance and a per-row "Manage" affordance ONLY when the
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
  it("shows the Add endpoint affordance only when onAdd is provided", () => {
    const { queryByText, unmount } = mountManaged({});
    expect(queryByText("Add endpoint")).toBeNull();
    unmount();
    const onAdd = vi.fn();
    mountManaged({ onAdd });
    fireEvent.click(screen.getByText("Add endpoint"));
    expect(onAdd).toHaveBeenCalledOnce();
  });

  it("surfaces a per-row Manage affordance that opens the endpoint by id", () => {
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

// The layer-7 rungs (#812) read as the ladder (#1059, ADR-0158): the ping,
// the transport rung, and the login only once a credential was tried, so a
// plain tcp endpoint's row carries no login rung.
describe("the layer-7 rungs", () => {
  it("reads the ladder from the read, the login rung only where a credential was tried", () => {
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
    const groups = screen.getAllByRole("group", { name: /reachability ladder/i });
    const rungsOf = (g: HTMLElement) => [...g.querySelectorAll<HTMLElement>("[data-testid=ladder-rung]")].map((r) => `${r.dataset.rung}:${r.dataset.status}`);
    expect(rungsOf(groups[0])).toEqual(["reachable:unknown", "responsive:pass", "authenticated:fail"]);
    expect(rungsOf(groups[1])).toEqual(["reachable:unknown", "responsive:unknown"]);
  });

  // The pill ages with a clock of its own, not with the data (#934): a poll
  // returning the same answer must still let it go stale.
  it("lets the pill go stale as time passes with the data unchanged", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval", "Date"] });
    try {
      const t0 = new Date().toISOString();
      const { getByText, queryByText } = mount({
        component: "disp-3",
        endpoints: [{ endpoint: "disp-3-tcp", transport: "tcp", node: "node-a", verdict: { value: "up", ts: t0 }, last_seen: t0, layers: [], history: [] }],
      } as unknown as Reachability);
      expect(getByText("responding")).toBeTruthy();
      await vi.advanceTimersByTimeAsync(200_000);
      expect(queryByText("responding")).toBeNull();
      expect(getByText("stale")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });
});

// A held session on the Endpoints panel (#1059): the pill on the row, and for
// a rejected or down session the reason beneath it without expanding, since a
// session the node will not leave on its own is the row's whole story.
describe("a held session on the row", () => {
  const telnetRow = (state: "up" | "rejected", lastSeen = new Date().toISOString()) => ({
    id: uuidFor("if-telnet"), endpoint: "telnet", transport: "telnet", node: "node-a", last_seen: lastSeen,
    verdict: { value: "up", ts: lastSeen }, responsive: { value: "up", ts: lastSeen },
    ...(state === "rejected"
      ? { authenticated: { value: "no", ts: lastSeen }, session: { state, ts: lastSeen, reason: "auth: auth rejected" }, rejection: { rung: "authenticated" as const, class: "auth" } }
      : { session: { state, ts: lastSeen } }),
    layers: [], history: [],
  });

  it("wears the session pill and shows a rejected session's reason and Retry, unexpanded", () => {
    mount({ component: compId, endpoints: [telnetRow("rejected")] });
    expect(screen.getByTestId("session-pill").dataset.state).toBe("rejected");
    expect(screen.getByTestId("session-reason").textContent).toBe("auth: auth rejected");
    expect(screen.getByRole("button", { name: /retry/i })).toBeTruthy();
  });

  it("shows no notice for an up session", () => {
    mount({ component: compId, endpoints: [telnetRow("up")] });
    expect(screen.getByTestId("session-pill").dataset.state).toBe("up");
    expect(screen.queryByTestId("session-notice")).toBeNull();
  });

  // The component's view polls this read every five seconds and every poll
  // hands back fresh objects (last_seen moves), so rows are kept by endpoint,
  // not by object: an expanded row, and a retry waiting on its session, must
  // survive the next poll.
  it("keeps an expanded row open across a poll that hands back fresh objects", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...REACHABILITY_KEY(compId)], { component: compId, endpoints: [telnetRow("up")] });
    render(() => (
      <QueryClientProvider client={qc}>
        <ReachabilityPanel name={compId} />
      </QueryClientProvider>
    ));
    const toggle = screen.getByRole("button", { expanded: false });
    fireEvent.click(toggle);
    expect(screen.getByText(/probed by/)).toBeTruthy();
    qc.setQueryData([...REACHABILITY_KEY(compId)], { component: compId, endpoints: [telnetRow("up", new Date(Date.now() + 5_000).toISOString())] });
    // The query notifies its observers asynchronously: let the poll land.
    await new Promise((r) => setTimeout(r, 20));
    expect(document.body.contains(toggle)).toBe(true);
    expect(screen.getByText(/probed by/)).toBeTruthy();
  });
});

// A tls endpoint (#1035): its breakdown teaches the handshake, the
// certificate's days left and how the endpoint verifies, and a skipped
// verification is visible on the row without expanding it.
describe("a tls endpoint", () => {
  const key = "SHA256:" + "Q".repeat(43);
  const tlsRow = (mode: string, daysLeft?: number) => ({
    endpoint: "tls",
    label: "Panel over TLS",
    transport: "tls",
    address: "127.60.0.9:51326",
    node: "node-a",
    last_seen: nowIso,
    verdict: { value: "up", ts: nowIso },
    responsive: { value: "up", ts: nowIso },
    layers: [{ layer: "port", check: "tcp-open", value: 1, detail: "0.4 ms", ts: nowIso }],
    history: [{ ts: ago(120_000), value: "up" }],
    tls: { mode, key: mode === "pin" ? key : undefined, handshake_ms: 12.5, expires: "2026-11-03T12:00:00Z", days_left: daysLeft },
  });

  it("lists the handshake, the days left and the pinned key in the breakdown", () => {
    const { getByText, getByTitle } = mount({ component: "panel-1", endpoints: [tlsRow("pin", 10.02)] });
    fireEvent.click(getByText("Panel over TLS"));
    expect(getByText("handshake")).toBeTruthy();
    expect(getByText(/12\.5 ms/)).toBeTruthy();
    expect(getByText("10 days left")).toBeTruthy();
    expect(getByText("pinned on first use")).toBeTruthy();
    expect(getByTitle(key)).toBeTruthy();
  });

  // The handshake row follows the latest probe: a handshake time from an
  // earlier answer is not this probe's, so a transport that stopped
  // answering reads not answered, never "answered" beside a red dot.
  it("reads a handshake the latest probe did not get as not answered", () => {
    const row = { ...tlsRow("pin", 10), responsive: { value: "down", ts: nowIso } };
    const { getByText, queryByText } = mount({ component: "panel-1", endpoints: [row] });
    fireEvent.click(getByText("Panel over TLS"));
    expect(getByText("not answered")).toBeTruthy();
    expect(queryByText("answered")).toBeNull();
    expect(queryByText(/12\.5 ms/)).toBeNull();
  });

  it("marks a certificate inside a month amber", () => {
    const { getByText, getByTestId } = mount({ component: "panel-1", endpoints: [tlsRow("pin", 12)] });
    fireEvent.click(getByText("Panel over TLS"));
    expect(getByTestId("tls-cert").dataset.tone).toBe("warn");
  });

  it("shows a skipped verification on the row, unexpanded", () => {
    const { getByText, queryByText } = mount({ component: "panel-1", endpoints: [tlsRow("skip", 300)] });
    expect(getByText("unverified")).toBeTruthy();
    expect(queryByText("handshake")).toBeNull();
    fireEvent.click(getByText("Panel over TLS"));
    expect(getByText("not verified")).toBeTruthy();
  });

  // Review of #1035, finding 4: under a certificate gate the probe connects
  // only, so the read withholds the last accepted certificate's facts; the
  // row says the offered certificate is refused rather than "none yet".
  it("reads a refused certificate under a certificate gate", () => {
    const row = { ...tlsRow("pin", undefined), rejection: { rung: "responsive" as const, class: "certificate", pinned_key: key, offered_key: "SHA256:" + "Z".repeat(43) } };
    const { getByText, getByTestId, queryByText } = mount({ component: "panel-1", endpoints: [row] });
    fireEvent.click(getByText("Panel over TLS"));
    expect(getByText("the offered certificate is refused")).toBeTruthy();
    expect(queryByText("no certificate accepted yet")).toBeNull();
    expect(getByTestId("tls-cert").dataset.tone).toBe("bad");
    // Beneath the gate only the TCP connect runs: the handshake row says so,
    // rather than an earlier probe's answer or its absence.
    expect(getByText("not tried beneath the gate")).toBeTruthy();
    expect(queryByText(/12\.5 ms/)).toBeNull();
  });

  it("says nothing of tls on a row that is not tls", () => {
    const { queryByText, getByText } = mount();
    fireEvent.click(getByText("disp-1-tcp"));
    expect(queryByText("handshake")).toBeNull();
    expect(queryByText("unverified")).toBeNull();
  });
});

// A udp endpoint has no port to open (#1037): its transport rung is the
// driver's request answered, so the breakdown lists that rung (the layer the
// verdict follows), and a down one explains itself.
describe("a udp endpoint", () => {
  const udpRow = (answered: boolean) => ({
    endpoint: "udp",
    label: "Panel over UDP",
    transport: "udp",
    address: "127.60.0.9:51327",
    node: "node-a",
    last_seen: nowIso,
    verdict: { value: answered ? "up" : "down", ts: nowIso },
    responsive: { value: answered ? "up" : "down", ts: nowIso },
    layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: nowIso }],
    history: [{ ts: ago(120_000), value: answered ? "up" : "down" }],
  });

  it("lists the request rung in the breakdown", () => {
    const { getByText, getByTestId } = mount({ component: "panel-1", endpoints: [udpRow(true)] });
    fireEvent.click(getByText("Panel over UDP"));
    const row = getByTestId("request-rung");
    expect(row.textContent).toMatch(/request/);
    expect(row.textContent).toMatch(/answered/);
    expect(row.textContent).not.toMatch(/not answered/);
  });

  it("reads an unanswered request down and says why", () => {
    const { getByText, getByTestId } = mount({ component: "panel-1", endpoints: [udpRow(false)] });
    fireEvent.click(getByText("Panel over UDP"));
    expect(getByTestId("request-rung").textContent).toMatch(/not answered/);
    expect(getByText(/request goes unanswered/)).toBeTruthy();
  });

  it("reads a gated request rung as refused beneath the gate", () => {
    const row = { ...udpRow(false), rejection: { rung: "responsive" as const, class: "auth" } };
    const { getByText, getByTestId } = mount({ component: "panel-1", endpoints: [row] });
    fireEvent.click(getByText("Panel over UDP"));
    expect(getByTestId("request-rung").textContent).toMatch(/refused/);
    expect(getByTestId("request-rung").textContent).not.toMatch(/not answered/);
  });

  it("lists no request rung on a row with a port", () => {
    const { getByText, queryByTestId } = mount();
    fireEvent.click(getByText("disp-1-tcp"));
    expect(queryByTestId("request-rung")).toBeNull();
  });
});
