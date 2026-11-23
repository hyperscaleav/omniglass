import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import PollTest from "./PollTest";
import { ME_KEY, type Me } from "../lib/auth";
import { ENDPOINTS_KEY, type Endpoint } from "../lib/endpoints";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { METRICS_KEY } from "../lib/metric_types";
import { uuidFor } from "../lib/testids";

// An operator tests an endpoint's poll from the console (#2427): one button runs
// a poll now and the result lands in place, whichever way it went. Data sits in
// the query cache (the house shape); the run itself is a mocked fetch.

const epId = uuidFor("ep-pdu");
const endpoint = (over: Partial<Endpoint> = {}): Endpoint => ({ id: epId, name: "http", transport: "http", component: "power-1", outpost: "edge-hq", driver: "ferrite-pdu-api", ...over });
const driver = (polls: string[]): Driver =>
  ({ id: uuidFor("driver-pdu"), name: "ferrite-pdu-api", label: "Ferrite PDU", official: true, spec: { version: 1, transport: "http", polls: polls.map((name) => ({ name, schedule: { every: "30s" }, request: {}, emits: [] })) } }) as Driver;

const operator: Me = { user: { id: "p1", kind: "person" }, permissions: ["*:read", "endpoint:run"], grants: [] };
const viewer: Me = { user: { id: "p2", kind: "person" }, permissions: ["*:read"], grants: [] };

function mount(opts: { me?: Me | null; ep?: Endpoint; polls?: string[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], opts.me === undefined ? operator : opts.me);
  qc.setQueryData([...ENDPOINTS_KEY], [opts.ep ?? endpoint()]);
  qc.setQueryData([...DRIVERS_KEY], [driver(opts.polls ?? ["identity"])]);
  qc.setQueryData([...METRICS_KEY], [{ name: "uptime", data_type: "int", unit: "s", precision: 0, official: true }]);
  return render(() => (
    <QueryClientProvider client={qc}>
      <PollTest endpoint={epId} />
    </QueryClientProvider>
  ));
}

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": status === 200 ? "application/json" : "application/problem+json" } });
const run = (poll: string, over: Record<string, unknown> = {}) => ({
  endpoint: "http", component: "power-1", outpost: "edge-hq", stored: true, poll, took_ms: 3,
  samples: [
    { name: "model-number", text: "PDU 4C", ts: "2026-11-23T14:00:00Z" },
    { name: "uptime", value: 538, ts: "2026-11-23T14:00:00Z" },
  ],
  ...over,
});

beforeEach(() => vi.restoreAllMocks());

describe("PollTest", () => {
  it("offers a Test per poll, and no Test all for a single poll", () => {
    mount();
    expect(screen.getByRole("button", { name: "Test identity" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Test all" })).toBeNull();
  });

  it("offers Test all beside several polls", () => {
    mount({ polls: ["identity", "status"] });
    expect(screen.getByRole("button", { name: "Test identity" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Test status" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Test all" })).toBeTruthy();
  });

  it("shows nothing to a person without endpoint:run", () => {
    mount({ me: viewer });
    expect(screen.queryByTestId("poll-test")).toBeNull();
    expect(screen.queryByRole("button", { name: /Test/ })).toBeNull();
  });

  it("shows nothing for an endpoint with no driver, or a driver with no polls", () => {
    mount({ ep: endpoint({ driver: undefined }) });
    expect(screen.queryByTestId("poll-test")).toBeNull();
  });

  it("shows nothing for a driver with no polls", () => {
    mount({ polls: [] });
    expect(screen.queryByTestId("poll-test")).toBeNull();
  });

  it("is pending while the outpost runs it, then shows each sample read, its unit and the round trip", async () => {
    let release!: (r: Response) => void;
    const fetchMock = vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise((res) => (release = res)));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Test identity" }));
    const card = await screen.findByTestId("poll-result");
    expect(card.getAttribute("data-state")).toBe("pending");
    expect((fetchMock.mock.calls[0][0] as Request).url).toContain(`/endpoints/${epId}/polls/identity:run`);
    expect((screen.getByRole("button", { name: "Test identity" }) as HTMLButtonElement).disabled).toBe(true);

    release(json(200, run("identity")));
    await waitFor(() => expect(screen.getByTestId("poll-result").getAttribute("data-state")).toBe("ok"));
    const ok = screen.getByTestId("poll-result");
    expect(within(ok).getByText("model-number")).toBeTruthy();
    expect(within(ok).getByText("PDU 4C")).toBeTruthy();
    expect(within(ok).getByText("538")).toBeTruthy();
    expect(within(ok).getByText("s")).toBeTruthy();
    expect(ok.textContent).toMatch(/2 samples/);
    expect(ok.textContent).toMatch(/3 ms/);
    expect(ok.textContent).toMatch(/edge-hq/);
  });

  it("says a poll that answered but read nothing, and lists the emits that did not locate", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(json(200, run("identity", { samples: [], faults: ["model-number: not found at $.model"] })));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Test identity" }));
    const card = await screen.findByTestId("poll-result");
    await waitFor(() => expect(card.getAttribute("data-state")).toBe("ok"));
    expect(card.textContent).toMatch(/no samples/i);
    expect(card.textContent).toMatch(/model-number: not found at \$\.model/);
  });

  it("shows the gate's own reason when the endpoint is gated", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      json(409, { status: 409, title: "Conflict", detail: "the endpoint is gated, so the poll was not sent: auth: the agent answered authorizationError for the community; fix what refused it, or retry the endpoint (retry-endpoint)" }),
    );
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Test identity" }));
    const card = await screen.findByTestId("poll-result");
    await waitFor(() => expect(card.getAttribute("data-state")).toBe("gated"));
    expect(card.textContent).toMatch(/authorizationError/);
  });

  it("names a failure with its cause", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(json(502, { status: 502, title: "Bad Gateway", detail: "the outpost could not run the poll: dial tcp 127.60.0.19:443: connection refused" }));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Test identity" }));
    const card = await screen.findByTestId("poll-result");
    await waitFor(() => expect(card.getAttribute("data-state")).toBe("failed"));
    expect(card.textContent).toMatch(/connection refused/);
  });

  it("says no outpost when the outpost is not connected", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(json(503, { status: 503, title: "Service Unavailable", detail: "the outpost is not connected, so it cannot run this now" }));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Test identity" }));
    const card = await screen.findByTestId("poll-result");
    await waitFor(() => expect(card.getAttribute("data-state")).toBe("no-outpost"));
    expect(card.textContent).toMatch(/not connected/);
  });

  it("says no outpost, up front, for an endpoint placed on none", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    mount({ ep: endpoint({ outpost: undefined }) });
    fireEvent.click(screen.getByRole("button", { name: "Test identity" }));
    const card = await screen.findByTestId("poll-result");
    expect(card.getAttribute("data-state")).toBe("no-outpost");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("Test all runs each poll in turn and shows each result", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (r) => {
      const poll = /polls\/([^:]+):run/.exec((r as Request).url)![1];
      return poll === "status" ? json(409, { status: 409, title: "Conflict", detail: "the endpoint is held, so the poll was not sent: unreachable" }) : json(200, run(poll));
    });
    mount({ polls: ["identity", "status"] });
    fireEvent.click(screen.getByRole("button", { name: "Test all" }));
    await waitFor(() => expect(screen.getAllByTestId("poll-result")).toHaveLength(2));
    await waitFor(() => expect(screen.getAllByTestId("poll-result").map((c) => c.getAttribute("data-state"))).toEqual(["ok", "gated"]));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
