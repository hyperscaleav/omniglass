import { describe, it, expect, vi, beforeEach } from "vitest";
import { metricBuckets, metricBucketsKey, propertyBuckets } from "./series";

// The bucketed reads' request shape (#1485): a bucket is one instance, so the
// instance travels when the caller names one, the empty string included (the
// owner-level series), and is left off when it does not.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("bucketed series reads", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("sends a named instance with the resolution", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ buckets: [], covered_from: "2026-11-01T00:00:00Z" }));
    const got = await metricBuckets("systems", "room-1", "room-temperature", 24, 300, "icmp");
    expect(got.coveredFrom).toBe("2026-11-01T00:00:00Z");
    const url = new URL((fetchMock.mock.calls[0][0] as Request).url);
    expect(url.pathname).toContain("/systems/room-1/metrics/room-temperature/samples");
    expect(url.searchParams.get("resolution")).toBe("300");
    expect(url.searchParams.get("instance")).toBe("icmp");
  });

  it("sends the empty instance as asked", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ buckets: [] }));
    await metricBuckets("systems", "room-1", "room-temperature", 24, 300, "");
    const url = new URL((fetchMock.mock.calls[0][0] as Request).url);
    expect(url.searchParams.has("instance")).toBe(true);
    expect(url.searchParams.get("instance")).toBe("");
  });

  it("leaves the instance off when none is named, so the server picks the one measured", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ buckets: [] }));
    await propertyBuckets("components", "disp-1", "firmware-version", 24, 3600);
    const url = new URL((fetchMock.mock.calls[0][0] as Request).url);
    expect(url.searchParams.has("instance")).toBe(false);
  });

  it("keys two instances of one metric apart", () => {
    expect(metricBucketsKey("systems", "r", "m", 24, 300, "a")).not.toEqual(metricBucketsKey("systems", "r", "m", 24, 300, "b"));
    expect(metricBucketsKey("systems", "r", "m", 24, 300, "")).not.toEqual(metricBucketsKey("systems", "r", "m", 24, 300));
  });
});
