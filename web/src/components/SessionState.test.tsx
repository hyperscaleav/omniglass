import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SessionPill, { SESSION_LOOK } from "./SessionPill";
import ReachLadder from "./ReachLadder";
import SessionNotice from "./SessionNotice";
import { ME_KEY, type Me } from "../lib/auth";
import { REACHABILITY_KEY, STALENESS_WINDOW_MS, type ReachEndpoint } from "../lib/reachability";
import { ENDPOINTS_KEY } from "../lib/endpoints";
import { uuidFor } from "../lib/testids";

// The held session's surfaces (#1059): the pill (state, glyph, hue, age),
// the ladder (the layered gate, taught one rung at a time), and the notice
// (the reason, Retry gated on endpoint:retry, the path to fix the endpoint).
// Data is seeded into the query cache, the house shape.

const now = Date.now();
const ago = (ms: number) => new Date(now - ms).toISOString();
const compId = uuidFor("comp-panel");
const epId = uuidFor("ep-telnet");

const rejected = (over: Partial<ReachEndpoint> = {}): ReachEndpoint => ({
  id: epId,
  endpoint: "telnet",
  label: "Panel login",
  transport: "telnet",
  node: "edge-hq",
  last_seen: ago(2_000),
  verdict: { value: "up", ts: ago(60_000) },
  responsive: { value: "up", ts: ago(60_000) },
  authenticated: { value: "no", ts: ago(30_000) },
  session: { state: "rejected", ts: ago(30_000), reason: "auth: auth rejected" },
  rejection: { rung: "authenticated", class: "auth", ts: "2026-10-23T11:58:00.000Z" },
  layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: ago(2_000) }],
  history: [],
  ...over,
});

const operator: Me = { principal: { id: "p1", kind: "human" }, permissions: ["*:read", "endpoint:create", "endpoint:update", "endpoint:retry"], grants: [] };
const viewer: Me = { principal: { id: "p2", kind: "human" }, permissions: ["*:read"], grants: [] };

function client(me: Me | null = operator) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  return qc;
}

describe("SessionPill", () => {
  it("reads each state with its own word and glyph, rejected apart from down", () => {
    expect(SESSION_LOOK.rejected.badge).not.toBe(SESSION_LOOK.down.badge);
    expect(SESSION_LOOK.rejected.icon).not.toBe(SESSION_LOOK.down.icon);
    for (const look of Object.values(SESSION_LOOK)) expect(look.badge).not.toMatch(/badge-neutral/);
    const { getByTestId } = render(() => <SessionPill iface={rejected()} now={now} />);
    const pill = getByTestId("session-pill");
    expect(pill.dataset.state).toBe("rejected");
    expect(pill.textContent).toContain("rejected");
    expect(pill.className).toContain("badge-warning");
    expect(pill.querySelector("svg")).toBeTruthy();
  });

  it("carries its age: when the state last changed", () => {
    const { getByTestId } = render(() => <SessionPill iface={rejected()} now={now} />);
    expect(getByTestId("session-age").textContent).toMatch(/30s/);
  });

  it("goes grey and says what it last read once nothing has arrived for the window", () => {
    const stale = rejected({ session: { state: "up", ts: ago(86_400_000) }, last_seen: ago(STALENESS_WINDOW_MS + 5_000) });
    const { getByTestId } = render(() => <SessionPill iface={stale} now={now} />);
    const pill = getByTestId("session-pill");
    expect(pill.dataset.state).toBe("stale");
    expect(pill.className).toContain("badge-ghost");
    expect(pill.getAttribute("title")).toMatch(/last read up/i);
  });

  it("renders nothing for an endpoint that never held a session", () => {
    const { queryByTestId } = render(() => <SessionPill iface={rejected({ session: undefined })} now={now} />);
    expect(queryByTestId("session-pill")).toBeNull();
  });
});

describe("ReachLadder", () => {
  it("shows each rung bottom up, the held login marked, every rung named for a screen reader", () => {
    render(() => <ReachLadder iface={rejected()} />);
    const rungs = screen.getAllByTestId("ladder-rung");
    expect(rungs.map((r) => [r.dataset.rung, r.dataset.status])).toEqual([
      ["reachable", "pass"],
      ["responsive", "pass"],
      ["authenticated", "held"],
    ]);
    expect(screen.getByRole("button", { name: /authenticated: held/i })).toBeTruthy();
  });

  it("marks a failed ping as informational while the transport rung passes", () => {
    render(() => (
      <ReachLadder
        iface={rejected({
          session: { state: "up", ts: ago(1_000) },
          rejection: undefined,
          authenticated: { value: "yes", ts: ago(1_000) },
          layers: [{ layer: "ping", check: "icmp-reachable", value: 0, ts: ago(1_000) }],
        })}
      />
    ));
    const [reach, resp] = screen.getAllByTestId("ladder-rung");
    expect([reach.dataset.status, reach.dataset.role]).toEqual(["fail", "informational"]);
    expect([resp.dataset.status, resp.dataset.role]).toEqual(["pass", "gate"]);
  });

  it("teaches the rung on keyboard focus", async () => {
    render(() => <ReachLadder iface={rejected()} />);
    const btn = screen.getByRole("button", { name: /reachable: pass/i });
    fireEvent.keyDown(document.body, { key: "Tab" });
    btn.focus();
    expect(await screen.findByText(/informational only/i)).toBeTruthy();
  });
});

function mountNotice(iface: ReachEndpoint, me: Me | null = operator, onFix?: () => void) {
  const qc = client(me);
  qc.setQueryData([...REACHABILITY_KEY(compId)], { component: "panel", endpoints: [iface] });
  qc.setQueryData([...ENDPOINTS_KEY], []);
  const invalidate = vi.spyOn(qc, "invalidateQueries");
  const utils = render(() => (
    <QueryClientProvider client={qc}>
      <SessionNotice iface={iface} component={compId} now={now} onFix={onFix} fixLabel="Edit credential" />
    </QueryClientProvider>
  ));
  return { ...utils, invalidate };
}

describe("SessionNotice", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("says why a rejected session was refused and retries it through :retry", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: epId, name: "telnet", transport: "telnet" }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const { getByTestId, getByRole, invalidate } = mountNotice(rejected());
    expect(getByTestId("session-reason").textContent).toBe("auth: auth rejected");
    const retry = getByRole("button", { name: /retry/i }) as HTMLButtonElement;
    expect(retry.disabled).toBe(false);
    fireEvent.click(retry);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain(`/endpoints/${epId}:retry`);
    // The pill updates through the polled read: the retry invalidates it.
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: [...REACHABILITY_KEY(compId)] }));
    // Until the session moves, the button says the retry is in hand.
    await waitFor(() => expect((getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(true));
  });

  it("greys Retry with its reason for a caller without endpoint:retry, and never calls it", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    const { getByRole, getByLabelText } = mountNotice(rejected(), viewer);
    const retry = getByRole("button", { name: /retry/i }) as HTMLButtonElement;
    expect(retry.disabled).toBe(true);
    expect(getByLabelText(/needs the endpoint:retry permission/i)).toBeTruthy();
    fireEvent.click(retry);
    // The client sends from an async hook, so give a call time to land.
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // The component's view shows the session on its Collection card and, once
  // opened, on the endpoint's blade: two notices for one endpoint. A retry
  // from one is in hand for both, so the other cannot stack a second redial.
  it("holds a retry for every notice of the same endpoint", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: epId, name: "telnet", transport: "telnet" }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const qc = client(operator);
    render(() => (
      <QueryClientProvider client={qc}>
        <SessionNotice iface={rejected()} component={compId} now={now} />
        <SessionNotice iface={rejected()} component={compId} now={now} />
      </QueryClientProvider>
    ));
    const [first, second] = screen.getAllByRole("button", { name: /retry/i }) as HTMLButtonElement[];
    fireEvent.click(first);
    await waitFor(() => expect(second.disabled).toBe(true));
  });

  it("says why a retry was refused", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ title: "Forbidden", status: 403, detail: "endpoint:retry is not granted on this component" }), { status: 403, headers: { "Content-Type": "application/problem+json" } }),
    );
    const { getByRole, findByRole } = mountNotice(rejected());
    fireEvent.click(getByRole("button", { name: /retry/i }));
    expect((await findByRole("alert")).textContent).toMatch(/not granted/);
  });

  it("offers the fix path the host passes, for an auth refusal", () => {
    const onFix = vi.fn();
    const { getByRole } = mountNotice(rejected(), operator, onFix);
    fireEvent.click(getByRole("button", { name: "Edit credential" }));
    expect(onFix).toHaveBeenCalledOnce();
  });

  it("explains a down session without offering a retry the node is already making", () => {
    const down = rejected({ session: { state: "down", ts: ago(5_000), reason: "5 consecutive attempts failed, last: dial tcp 127.60.0.9:23: i/o timeout" }, rejection: undefined });
    const { getByTestId, queryByRole } = mountNotice(down);
    expect(getByTestId("session-reason").textContent).toMatch(/5 consecutive attempts failed/);
    expect(queryByRole("button", { name: /retry/i })).toBeNull();
  });

  it("renders nothing for an up session", () => {
    const { queryByTestId } = mountNotice(rejected({ session: { state: "up", ts: ago(1_000) }, rejection: undefined }));
    expect(queryByTestId("session-notice")).toBeNull();
  });
});

// #1110: an endpoint gated by its reachability probe alone holds no session,
// and its refusal reads the same as a rejected session's: the same notice, the
// gate's reason, Retry behind endpoint:retry, and the fix path.
describe("SessionNotice for a probe's gate, no session held", () => {
  beforeEach(() => vi.restoreAllMocks());

  const probeGated = (transport: string, rung: "responsive" | "authenticated", cls: string, reason?: string, over: Partial<ReachEndpoint> = {}): ReachEndpoint => ({
    id: epId,
    endpoint: transport,
    label: `Panel ${transport}`,
    transport,
    node: "edge-hq",
    last_seen: ago(2_000),
    verdict: { value: "down", ts: ago(60_000) },
    responsive: { value: rung === "responsive" ? "down" : "up", ts: ago(60_000) },
    rejection: { rung, class: cls, ts: ago(40_000), ...(reason ? { reason } : {}) },
    layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: ago(2_000) }],
    history: [],
    ...over,
  });

  it.each([
    ["tls", "responsive", "certificate", "certificate: the certificate failed verification: x509: certificate signed by unknown authority"],
    ["udp", "responsive", "auth", "auth: ERR 401 not authorized"],
    ["snmp", "authenticated", "auth", "auth: the agent answered authorizationError for the community"],
    ["ssh", "authenticated", "auth", "auth: the component refused the probe's credential"],
  ] as const)("says why a %s probe was refused and retries it through :retry", async (transport, rung, cls, reason) => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: epId, name: transport, transport }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const onFix = vi.fn();
    const { getByTestId, getByRole, invalidate } = mountNotice(probeGated(transport, rung, cls, reason), operator, onFix);
    expect(getByTestId("session-notice").dataset.state).toBe("rejected");
    expect(getByTestId("session-reason").textContent).toBe(reason);
    fireEvent.click(getByRole("button", { name: "Edit credential" }));
    expect(onFix).toHaveBeenCalledOnce();
    const retry = getByRole("button", { name: /retry/i }) as HTMLButtonElement;
    expect(retry.disabled).toBe(false);
    fireEvent.click(retry);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain(`/endpoints/${epId}:retry`);
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: [...REACHABILITY_KEY(compId)] }));
    // Until the gate moves, the button says the retry is in hand.
    await waitFor(() => expect((getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(true));
  });

  it("names the class when the gate carries no reason", () => {
    const { getByTestId } = mountNotice(probeGated("snmp", "authenticated", "auth"));
    expect(getByTestId("session-reason").textContent).toMatch(/^auth: /);
  });

  it("greys Retry with its reason for a caller without endpoint:retry, and never calls it", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    const { getByRole, getByLabelText, getByTestId } = mountNotice(probeGated("snmp", "authenticated", "auth", "auth: the agent answered authorizationError for the community"), viewer);
    // The refusal still reads, so a viewer can tell an operator what is wrong.
    expect(getByTestId("session-reason").textContent).toMatch(/authorizationError/);
    const retry = getByRole("button", { name: /retry/i }) as HTMLButtonElement;
    expect(retry.disabled).toBe(true);
    expect(getByLabelText(/needs the endpoint:retry permission/i)).toBeTruthy();
    fireEvent.click(retry);
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // A retry lifts the gate; a refusal met again sets it anew, at a new time,
  // and Retry is offered again at once rather than after the wait runs out.
  it("offers Retry again as soon as the refusal is met again", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: epId, name: "snmp", transport: "snmp" }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const qc = client(operator);
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    const [iface, setIface] = createSignal(probeGated("snmp", "authenticated", "auth", "auth: the agent answered authorizationError for the community"));
    render(() => (
      <QueryClientProvider client={qc}>
        <SessionNotice iface={iface()} component={compId} now={now} />
      </QueryClientProvider>
    ));
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    // The retry landed, and while the gate has not moved it is in hand.
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: [...REACHABILITY_KEY(compId)] }));
    await new Promise((r) => setTimeout(r, 10));
    expect((screen.getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(true);
    setIface(probeGated("snmp", "authenticated", "auth", "auth: the agent answered authorizationError for the community", { rejection: { rung: "authenticated", class: "auth", ts: ago(1_000), reason: "auth: the agent answered authorizationError for the community" } }));
    await waitFor(() => expect((screen.getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(false));
  });

  it("renders nothing for a probe endpoint with no gate", () => {
    const { queryByTestId } = mountNotice(probeGated("snmp", "authenticated", "auth", undefined, { rejection: undefined }));
    expect(queryByTestId("session-notice")).toBeNull();
  });
});
