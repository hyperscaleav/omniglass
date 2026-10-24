import { describe, it, expect } from "vitest";
import {
  verdictWord,
  endpointWord,
  lastSeenAt,
  segments,
  uptime,
  reason,
  layerWord,
  requestRung,
  STALENESS_WINDOW_MS,
  type ReachEndpoint,
} from "./reachability";

const now = Date.parse("2026-07-07T12:00:00Z");
const ago = (ms: number) => new Date(now - ms).toISOString();

describe("verdictWord", () => {
  it("is responding for a fresh up verdict", () => {
    expect(verdictWord({ value: "up", ts: ago(10_000) }, now)).toBe("responding");
  });
  it("is down for a fresh down verdict", () => {
    expect(verdictWord({ value: "down", ts: ago(10_000) }, now)).toBe("down");
  });
  it("is stale for a verdict older than the staleness window", () => {
    expect(verdictWord({ value: "up", ts: ago(STALENESS_WINDOW_MS + 1_000) }, now)).toBe("stale");
  });
  it("is unknown when there is no verdict", () => {
    expect(verdictWord(null, now)).toBe("unknown");
  });
});

// The verdict is stored on change only, so its ts is the last TRANSITION.
// An endpoint's freshness is the server's own stamp, last_seen: when ingest
// last landed a batch it delivered, whatever the batch carried and whatever
// time a node wrote on its samples (#931, #933).
describe("an endpoint's freshness is the server's last_seen (#931, #933)", () => {
  const ep = (over: Partial<ReachEndpoint>): ReachEndpoint =>
    ({ endpoint: "tcp", transport: "tcp", verdict: null, layers: [], history: [], ...over }) as ReachEndpoint;
  const layer = (msAgo: number) => ({ layer: "port", check: "tcp-open", value: 1, ts: ago(msAgo) });

  it("ages an old transition by last_seen, not the transition", () => {
    const e = ep({ verdict: { value: "up", ts: ago(21 * 60_000) }, last_seen: ago(5_000) });
    expect(lastSeenAt(e)).toBe(ago(5_000));
    expect(endpointWord(e, now)).toBe("responding");
  });
  it("keeps a down verdict's word while it is fresh", () => {
    expect(endpointWord(ep({ verdict: { value: "down", ts: ago(400_000) }, last_seen: ago(2_000) }), now)).toBe("down");
  });
  it("is stale once last_seen is past the window, however new a node dated its samples", () => {
    const e = ep({ verdict: { value: "up", ts: ago(-3_600_000) }, layers: [layer(-3_600_000)], last_seen: ago(STALENESS_WINDOW_MS + 1_000) });
    expect(lastSeenAt(e)).toBe(ago(STALENESS_WINDOW_MS + 1_000));
    expect(endpointWord(e, now)).toBe("stale");
  });
  it("falls back to the verdict's own time before any stamp, which can only read older", () => {
    expect(endpointWord(ep({ verdict: { value: "up", ts: ago(900_000) } }), now)).toBe("stale");
  });
  it("is unknown with no verdict, and has no time with no stamp", () => {
    expect(endpointWord(ep({ last_seen: ago(1_000) }), now)).toBe("unknown");
    expect(lastSeenAt(ep({}))).toBeNull();
  });
});

describe("segments and uptime", () => {
  it("weights each transition by its duration", () => {
    const history = [
      { ts: ago(100_000), value: "up" }, // up for 60s (100k -> 40k)
      { ts: ago(40_000), value: "down" }, // down for 40s (40k -> now)
    ];
    const segs = segments(history, { value: "down", ts: ago(40_000) }, now);
    expect(segs.map((s) => s.value)).toEqual(["up", "down"]);
    // up ~60%, down ~40%
    expect(segs[0].weight).toBeGreaterThan(segs[1].weight);
    expect(uptime(history, null, now)).toBe(60);
  });
  it("fills the strip with the current verdict when there is no history", () => {
    const segs = segments([], { value: "up", ts: ago(5_000) }, now);
    expect(segs).toEqual([{ value: "up", weight: 1 }]);
    expect(uptime([], { value: "up", ts: ago(5_000) }, now)).toBe(100);
  });
  it("returns an empty strip when there is neither history nor verdict", () => {
    expect(segments([], null, now)).toEqual([]);
    expect(uptime([], null, now)).toBeNull();
  });
});

describe("reason", () => {
  const base: ReachEndpoint = {
    endpoint: "disp-1-tcp",
    transport: "tcp",
    verdict: { value: "down", ts: ago(5_000) },
    layers: [],
    history: [],
  };
  it("explains ping-up port-down as a live box with a dead service", () => {
    const iface: ReachEndpoint = {
      ...base,
      layers: [
        { layer: "ping", check: "icmp-reachable", value: 1, ts: ago(5_000) },
        { layer: "port", check: "tcp-open", value: 0, ts: ago(5_000) },
      ],
    };
    expect(reason(iface, now)).toMatch(/service down, box up/i);
  });
  it("explains ping-down as unreachable", () => {
    const iface: ReachEndpoint = {
      ...base,
      layers: [{ layer: "ping", check: "icmp-reachable", value: 0, ts: ago(5_000) }],
    };
    expect(reason(iface, now)).toMatch(/unreachable/i);
  });
  it("is empty when the interface is not down", () => {
    expect(reason({ ...base, verdict: { value: "up", ts: ago(5_000) } }, now)).toBe("");
  });
  // A udp endpoint has no port to open (#1037): its transport rung is the
  // driver's request answered, so ping-up request-unanswered names that.
  it("explains a ping-up endpoint whose request goes unanswered", () => {
    const iface: ReachEndpoint = {
      ...base,
      endpoint: "udp",
      transport: "udp",
      responsive: { value: "down", ts: ago(5_000) },
      layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: ago(5_000) }],
    };
    expect(reason(iface, now)).toMatch(/request goes unanswered/i);
    expect(reason(iface, now)).toMatch(/cannot be detected/i);
  });
  // A gated udp endpoint's request was answered with a refusal retrying
  // cannot fix, and the node sends nothing more (review of #1037, finding 5):
  // the why says that, never "unanswered".
  it("explains a gated request rung as refused, not unanswered", () => {
    const iface: ReachEndpoint = {
      ...base,
      endpoint: "udp",
      transport: "udp",
      responsive: { value: "down", ts: ago(5_000) },
      rejection: { rung: "responsive", class: "auth" },
      layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: ago(5_000) }],
    };
    expect(reason(iface, now)).not.toMatch(/unanswered/i);
    expect(reason(iface, now)).toMatch(/refused/i);
    expect(reason(iface, now)).toMatch(/retry/i);
  });
});

describe("layerWord", () => {
  it("renders open/closed for the port layer", () => {
    expect(layerWord({ layer: "port", check: "tcp-open", value: 1, ts: ago(0) })).toBe("open");
    expect(layerWord({ layer: "port", check: "tcp-open", value: 0, ts: ago(0) })).toBe("closed");
  });
  it("renders up/down for the ping layer", () => {
    expect(layerWord({ layer: "ping", check: "icmp-reachable", value: 1, ts: ago(0) })).toBe("up");
    expect(layerWord({ layer: "ping", check: "icmp-reachable", value: 0, ts: ago(0) })).toBe("down");
  });
});

describe("requestRung", () => {
  const ping = { layer: "ping", check: "icmp-reachable", value: 1, ts: ago(0) };
  const port = { layer: "port", check: "tcp-open", value: 1, ts: ago(0) };
  const base: ReachEndpoint = { endpoint: "udp", transport: "udp", verdict: { value: "up", ts: ago(0) }, layers: [ping], history: [] };
  it("is the responsive rung where there is no port to open", () => {
    expect(requestRung({ ...base, responsive: { value: "up", ts: ago(0) } })).toBe(true);
    expect(requestRung({ ...base, responsive: { value: "down", ts: ago(0) } })).toBe(false);
  });
  it("reads the request layer the read serves first", () => {
    const layer = { layer: "request", check: "endpoint-responsive", value: 0, ts: ago(0) };
    expect(requestRung({ ...base, layers: [ping, layer], responsive: { value: "up", ts: ago(0) } })).toBe(false);
  });
  it("is undefined beside a port, before the rung lands, and for icmp", () => {
    expect(requestRung({ ...base, transport: "tls", layers: [ping, port], responsive: { value: "up", ts: ago(0) } })).toBeUndefined();
    expect(requestRung(base)).toBeUndefined();
    expect(requestRung({ ...base, transport: "icmp", responsive: { value: "up", ts: ago(0) } })).toBeUndefined();
  });
});
