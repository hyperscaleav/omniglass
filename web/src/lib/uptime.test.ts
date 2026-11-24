import { describe, it, expect } from "vitest";
import { uptimeOver, RANGE_30D_MS } from "./uptime";

const NOW = Date.parse("2026-11-23T14:00:00Z");
const min = (n: number) => n * 60_000;
const iso = (ms: number) => new Date(ms).toISOString();

describe("uptimeOver (#2435): the healthy share of the entity's recorded life inside the range", () => {
  it("first recorded 4 minutes ago and healthy since reads 100% over that window, named", () => {
    const r = uptimeOver({ transitions: [], health: "healthy", firstRecordedAt: iso(NOW - min(4)), now: NOW });
    expect(r.pct).toBe("100");
    expect(r.from).toBe(NOW - min(4));
    expect(r.shortened).toBe(true);
    expect(r.label).toMatch(/^since /);
  });

  it("an outage covering half of a short life reads 50.0%", () => {
    const r = uptimeOver({
      transitions: [
        { ts: iso(NOW - min(4)), health: "healthy" },
        { ts: iso(NOW - min(2)), health: "outage" },
      ],
      health: "outage",
      firstRecordedAt: iso(NOW - min(4)),
      now: NOW,
    });
    expect(r.pct).toBe("50.0");
    expect(r.shortened).toBe(true);
  });

  it("no first_recorded_at and no edges is no health recorded yet, never a percentage", () => {
    const r = uptimeOver({ transitions: [], health: undefined, now: NOW });
    expect(r.pct).toBeNull();
    expect(r.label).toBe("no health recorded yet");
  });

  it("an entity older than the range keeps the whole range and says so", () => {
    const r = uptimeOver({
      transitions: [{ ts: iso(NOW - 90 * 86_400_000), health: "healthy" }],
      health: "healthy",
      firstRecordedAt: iso(NOW - 90 * 86_400_000),
      now: NOW,
    });
    expect(r.from).toBe(NOW - RANGE_30D_MS);
    expect(r.shortened).toBe(false);
    expect(r.label).toBe("of the last 30 days");
    expect(r.pct).toBe("100");
  });

  it("a range older than the entity clips to the entity's life, not the range", () => {
    const r = uptimeOver({
      transitions: [{ ts: iso(NOW - min(10)), health: "degraded" }],
      health: "degraded",
      firstRecordedAt: iso(NOW - min(10)),
      rangeStart: NOW - 7 * 86_400_000,
      now: NOW,
    });
    expect(r.from).toBe(NOW - min(10));
    expect(r.pct).toBe("0.0");
  });

  it("carried_in fills the stretch before the first edge inside the range", () => {
    const r = uptimeOver({
      transitions: [{ ts: iso(NOW - min(30)), health: "outage" }],
      carriedIn: "healthy",
      health: "outage",
      firstRecordedAt: iso(NOW - 5 * 86_400_000),
      rangeStart: NOW - min(60),
      now: NOW,
    });
    expect(r.from).toBe(NOW - min(60));
    expect(r.pct).toBe("50.0");
    expect(r.shortened).toBe(false);
  });

  it("a zero-length life is no percentage", () => {
    const r = uptimeOver({ transitions: [], health: "healthy", firstRecordedAt: iso(NOW), now: NOW });
    expect(r.pct).toBeNull();
  });
});
