import { describe, it, expect } from "vitest";
import {
  verdictWord,
  segments,
  uptime,
  reason,
  layerWord,
  STALENESS_WINDOW_MS,
  type ReachInterface,
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
  const base: ReachInterface = {
    interface: "disp-1-tcp",
    interface_type: "tcp",
    verdict: { value: "down", ts: ago(5_000) },
    layers: [],
    history: [],
  };
  it("explains ping-up port-down as a live box with a dead service", () => {
    const iface: ReachInterface = {
      ...base,
      layers: [
        { layer: "ping", check: "icmp.reachable", value: 1, ts: ago(5_000) },
        { layer: "port", check: "tcp.open", value: 0, ts: ago(5_000) },
      ],
    };
    expect(reason(iface, now)).toMatch(/service down, box up/i);
  });
  it("explains ping-down as unreachable", () => {
    const iface: ReachInterface = {
      ...base,
      layers: [{ layer: "ping", check: "icmp.reachable", value: 0, ts: ago(5_000) }],
    };
    expect(reason(iface, now)).toMatch(/unreachable/i);
  });
  it("is empty when the interface is not down", () => {
    expect(reason({ ...base, verdict: { value: "up", ts: ago(5_000) } }, now)).toBe("");
  });
});

describe("layerWord", () => {
  it("renders open/closed for the port layer", () => {
    expect(layerWord({ layer: "port", check: "tcp.open", value: 1, ts: ago(0) })).toBe("open");
    expect(layerWord({ layer: "port", check: "tcp.open", value: 0, ts: ago(0) })).toBe("closed");
  });
  it("renders up/down for the ping layer", () => {
    expect(layerWord({ layer: "ping", check: "icmp.reachable", value: 1, ts: ago(0) })).toBe("up");
    expect(layerWord({ layer: "ping", check: "icmp.reachable", value: 0, ts: ago(0) })).toBe("down");
  });
});
