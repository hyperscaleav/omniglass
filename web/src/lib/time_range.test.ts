import { describe, expect, it } from "vitest";
import {
  DEFAULT_RANGE,
  MAX_CUSTOM_MS,
  PRESETS,
  parseRange,
  rangeHours,
  rangeLabel,
  rangeStepSeconds,
  resolveRange,
  serializeRange,
  type TimeRange,
} from "./time_range";

// The page's one time range (#2304): parse the URL, serialize it back, and
// resolve it to [from, to] at a clock the caller injects.

const asParams = (patch: Record<string, string | undefined>) =>
  Object.fromEntries(Object.entries(patch).filter(([, v]) => v !== undefined)) as Record<string, string>;

describe("parseRange and serializeRange", () => {
  it("round-trip every preset", () => {
    for (const p of PRESETS) {
      const r: TimeRange = { kind: "preset", preset: p.key };
      expect(parseRange(asParams(serializeRange(r)))).toEqual(r);
    }
  });

  it("round-trip a custom absolute range as RFC 3339 from and to", () => {
    const r: TimeRange = { kind: "absolute", from: Date.parse("2026-11-01T08:00:00Z"), to: Date.parse("2026-11-02T20:30:00Z") };
    const params = serializeRange(r);
    expect(params).toEqual({ range: undefined, from: "2026-11-01T08:00:00.000Z", to: "2026-11-02T20:30:00.000Z" });
    expect(parseRange(asParams(params))).toEqual(r);
  });

  it("writes a preset as ?range= and clears a custom range's params", () => {
    expect(serializeRange({ kind: "preset", preset: "7d" })).toEqual({ range: "7d", from: undefined, to: undefined });
  });

  it("leaves the default out of the address, so the canonical link stays clean", () => {
    expect(serializeRange(DEFAULT_RANGE)).toEqual({ range: undefined, from: undefined, to: undefined });
    expect(parseRange({})).toEqual(DEFAULT_RANGE);
    expect(DEFAULT_RANGE).toEqual({ kind: "preset", preset: "24h" });
  });

  it("reads URLSearchParams and router params alike, first value of a repeated key", () => {
    expect(parseRange(new URLSearchParams("range=6h"))).toEqual({ kind: "preset", preset: "6h" });
    expect(parseRange({ range: ["1h", "30d"] })).toEqual({ kind: "preset", preset: "1h" });
  });

  it("accepts an RFC 3339 offset, not only Z", () => {
    expect(parseRange({ from: "2026-11-01T01:00:00-07:00", to: "2026-11-01T02:00:00-07:00" })).toEqual({
      kind: "absolute",
      from: Date.parse("2026-11-01T08:00:00Z"),
      to: Date.parse("2026-11-01T09:00:00Z"),
    });
  });

  it("an absolute range wins over a stale ?range= beside it", () => {
    expect(parseRange({ range: "1h", from: "2026-11-01T08:00:00Z", to: "2026-11-01T09:00:00Z" }).kind).toBe("absolute");
  });
});

describe("unparseable input falls back to the default without throwing", () => {
  const bad: Record<string, string>[] = [
    { range: "banana" },
    { range: "" },
    { range: "24H" },
    { range: "2h" },
    { from: "yesterday", to: "today" },
    { from: "2026-11-01" , to: "2026-11-02" },
    { from: "2026-11-01T08:00:00Z" },
    { to: "2026-11-01T08:00:00Z" },
    { from: "2026-11-02T08:00:00Z", to: "2026-11-01T08:00:00Z" },
    { from: "2026-11-01T08:00:00Z", to: "2026-11-01T08:00:00Z" },
    { from: "2026-13-45T99:00:00Z", to: "2026-11-01T08:00:00Z" },
  ];
  for (const p of bad) {
    it(JSON.stringify(p), () => {
      expect(() => parseRange(p)).not.toThrow();
      expect(parseRange(p)).toEqual(DEFAULT_RANGE);
    });
  }

  it("a broken custom range beside a good preset keeps the preset", () => {
    expect(parseRange({ range: "7d", from: "nope", to: "2026-11-01T08:00:00Z" })).toEqual({ kind: "preset", preset: "7d" });
  });
});

describe("resolveRange", () => {
  const now = Date.parse("2026-11-23T12:00:00Z");

  it("slides a preset to the injected now", () => {
    expect(resolveRange({ kind: "preset", preset: "1h" }, now)).toEqual({ from: now - 3600_000, to: now });
    expect(resolveRange({ kind: "preset", preset: "30d" }, now)).toEqual({ from: now - 30 * 86400_000, to: now });
  });

  it("holds an absolute range still whatever the clock says", () => {
    const r: TimeRange = { kind: "absolute", from: 1, to: 2 };
    expect(resolveRange(r, now)).toEqual({ from: 1, to: 2 });
    expect(resolveRange(r, now + 1e9)).toEqual({ from: 1, to: 2 });
  });
});

describe("rangeHours", () => {
  it("is the preset's width in hours, the series routes' window", () => {
    expect(PRESETS.map((p) => rangeHours({ kind: "preset", preset: p.key }))).toEqual([1, 6, 24, 168, 720]);
  });
  it("is undefined for an absolute range, which an hours-back read cannot honor", () => {
    expect(rangeHours({ kind: "absolute", from: 0, to: 3600_000 })).toBeUndefined();
  });
});

describe("rangeStepSeconds", () => {
  it("keeps a day at five-minute buckets, the read the page always made", () => {
    expect(rangeStepSeconds(24 * 3600_000)).toBe(300);
  });
  it("picks a step that keeps every preset's chart a few hundred buckets wide", () => {
    expect(PRESETS.map((p) => rangeStepSeconds(p.ms))).toEqual([60, 60, 300, 3600, 3600]);
  });
  it("uses whole hours past two days, as the series route raises it to", () => {
    expect(rangeStepSeconds(49 * 3600_000) % 3600).toBe(0);
  });
  it("uses whole hours when asked, for a range reaching back past the raw samples", () => {
    expect(rangeStepSeconds(24 * 3600_000, true)).toBe(3600);
    expect(rangeStepSeconds(3600_000, true)).toBe(3600);
  });
  it("stays under the server's 5000-bucket cap for a long custom range", () => {
    const twoYears = 2 * 365 * 86400_000;
    expect(twoYears / 1000 / rangeStepSeconds(twoYears)).toBeLessThanOrEqual(5000);
  });
});

describe("rangeLabel", () => {
  it("names a preset in words", () => {
    expect(rangeLabel({ kind: "preset", preset: "24h" })).toBe("Last 24 hours");
    expect(rangeLabel({ kind: "preset", preset: "7d" })).toBe("Last 7 days");
  });
  it("names an absolute range by its two ends", () => {
    const label = rangeLabel({ kind: "absolute", from: Date.parse("2026-11-01T08:00:00Z"), to: Date.parse("2026-11-02T08:00:00Z") });
    expect(label).toContain("→");
  });
});

describe("MAX_CUSTOM_MS", () => {
  it("is the views read's two-year ceiling", () => {
    expect(MAX_CUSTOM_MS).toBe(2 * 365 * 86400_000);
  });
});
