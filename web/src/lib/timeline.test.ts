import { describe, it, expect } from "vitest";
import { spans, share, durationText, withCarriedIn } from "./timeline";

const now = Date.parse("2026-07-20T12:00:00Z");
const ago = (ms: number) => new Date(now - ms).toISOString();

describe("spans", () => {
  it("weights each edge by how long it held, and carries its wall-clock bounds", () => {
    const edges = [
      { ts: ago(100_000), value: "healthy" }, // held 60s
      { ts: ago(40_000), value: "outage" }, // held 40s, still running
    ];
    const out = spans(edges, "outage", now);
    expect(out.map((s) => s.value)).toEqual(["healthy", "outage"]);
    expect(out[0].weight).toBeCloseTo(0.6, 5);
    expect(out[1].weight).toBeCloseTo(0.4, 5);
    // The bounds are what "how long did this last" reads from: the last span runs
    // to now, not to some invented end.
    expect(out[0].to - out[0].from).toBe(60_000);
    expect(out[1].to).toBe(now);
  });

  it("fills the strip with the current value when nothing was recorded", () => {
    const out = spans([], "degraded", now);
    expect(out).toEqual([{ value: "degraded", weight: 1, from: now, to: now }]);
  });

  it("is empty when there is neither an edge nor a current value", () => {
    expect(spans([], null, now)).toEqual([]);
  });

  it("drops a zero-length span so a double edge at one instant is not a sliver", () => {
    const edges = [
      { ts: ago(60_000), value: "healthy" },
      { ts: ago(60_000), value: "degraded" }, // same instant: healthy never held
      { ts: ago(30_000), value: "outage" },
    ];
    expect(spans(edges, "outage", now).map((s) => s.value)).toEqual(["degraded", "outage"]);
  });
});

describe("share", () => {
  it("is the whole-number percent of the window matching the predicate", () => {
    const out = spans(
      [
        { ts: ago(100_000), value: "healthy" },
        { ts: ago(40_000), value: "outage" },
      ],
      "outage",
      now,
    );
    expect(share(out, (v) => v === "healthy")).toBe(60);
  });

  it("is null for an empty window, which is not the same as zero percent", () => {
    expect(share([], () => true)).toBeNull();
  });
});

describe("durationText", () => {
  it("reads in the coarsest useful unit, never more than two", () => {
    expect(durationText(45_000)).toBe("45s");
    expect(durationText(12 * 60_000)).toBe("12m");
    expect(durationText(3 * 3_600_000)).toBe("3h");
    expect(durationText(3 * 3_600_000 + 12 * 60_000)).toBe("3h 12m");
    expect(durationText(50 * 3_600_000)).toBe("2d 2h");
    expect(durationText(48 * 3_600_000)).toBe("2d");
  });
});

// A window opens at its start (#2741): the health in force then (a read's
// carried_in) is an edge at the window's left edge, so the strip begins where
// the range does rather than at the first change inside it.
describe("withCarriedIn", () => {
  const from = Date.parse("2026-08-20T15:00:00Z");
  const at = (m: number) => new Date(from + m * 60_000).toISOString();

  it("puts the carried-in value at the window's start, ahead of the first edge inside it", () => {
    expect(withCarriedIn([{ ts: at(30), value: "outage" }], "healthy", from)).toEqual([
      { ts: at(0), value: "healthy" },
      { ts: at(30), value: "outage" },
    ]);
  });

  it("fills a window with no edge inside it", () => {
    expect(withCarriedIn([], "degraded", from)).toEqual([{ ts: at(0), value: "degraded" }]);
  });

  it("leaves the edges alone with nothing carried in, or an edge already at the start", () => {
    const edges = [{ ts: at(30), value: "outage" }];
    expect(withCarriedIn(edges, undefined, from)).toBe(edges);
    expect(withCarriedIn(edges, null, from)).toBe(edges);
    const atStart = [{ ts: at(0), value: "outage" }];
    expect(withCarriedIn(atStart, "healthy", from)).toBe(atStart);
  });
});
