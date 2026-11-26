import { describe, it, expect } from "vitest";
import { uptimeOver, RANGE_30D_MS } from "./uptime";
import { fmtTime } from "./format";

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
      range: { kind: "preset", preset: "7d" },
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
      range: { kind: "preset", preset: "1h" },
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

// The window is the page's range (#2741): the label names it, so the number
// beside it is never read as another window's.
describe("uptimeOver names the range it measured (#2741)", () => {
  const old = { transitions: [], health: "healthy", firstRecordedAt: iso(NOW - 90 * 86_400_000), now: NOW };

  it("labels each preset by its window", () => {
    expect(uptimeOver({ ...old, range: { kind: "preset", preset: "1h" } }).label).toBe("of the last hour");
    expect(uptimeOver({ ...old, range: { kind: "preset", preset: "7d" } }).label).toBe("of the last 7 days");
    expect(uptimeOver({ ...old, range: { kind: "preset", preset: "30d" } }).label).toBe("of the last 30 days");
    expect(uptimeOver(old).label).toBe("of the last 30 days");
  });

  it("an hour's range measures that hour from the carried-in health", () => {
    const r = uptimeOver({
      transitions: [{ ts: iso(NOW - min(10)), health: "outage" }],
      carriedIn: "healthy",
      health: "outage",
      firstRecordedAt: iso(NOW - 5 * 86_400_000),
      range: { kind: "preset", preset: "1h" },
      now: NOW,
    });
    expect(r.from).toBe(NOW - min(60));
    expect(r.pct).toBe("83.3");
    expect(r.label).toBe("of the last hour");
  });

  it("a system younger than the range still reads since its first record", () => {
    const r = uptimeOver({ transitions: [], health: "healthy", firstRecordedAt: iso(NOW - min(20)), range: { kind: "preset", preset: "1h" }, now: NOW });
    expect(r.label).toBe(`since ${fmtTime(iso(NOW - min(20)))}`);
  });

  it("a custom range is measured to its own end and labelled by its dates", () => {
    const from = NOW - 3 * min(60);
    const to = NOW - min(60);
    const r = uptimeOver({
      transitions: [{ ts: iso(NOW - 2 * min(60)), health: "outage" }],
      carriedIn: "healthy",
      health: "healthy",
      firstRecordedAt: iso(NOW - 5 * 86_400_000),
      range: { kind: "absolute", from, to },
      now: NOW,
    });
    // Healthy the first hour, out the second: the hour after the range is not counted.
    expect(r.pct).toBe("50.0");
    expect(r.from).toBe(from);
    expect(r.label).toBe(`${fmtTime(iso(from))} → ${fmtTime(iso(to))}`);
  });
});
