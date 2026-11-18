import { describe, it, expect, vi, beforeEach } from "vitest";
import { listOutposts, getOutpost, createOutpost, enrollOutpost, outpostStatus, OUTPOST_DOWN_AFTER_MS, outpostFilterKeys, type Outpost } from "./outposts";
import { buildPredicate, type Chip } from "./predicate";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("outposts data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists outposts and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ outposts: [{ name: "edge-1", enrolled: true }] }),
    );
    const ns = await listOutposts();
    expect(ns).toHaveLength(1);
    expect(ns[0].name).toBe("edge-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/outposts");
  });

  it("tolerates a null outposts envelope (no outposts yet)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ outposts: null }));
    expect(await listOutposts()).toEqual([]);
  });

  it("gets an outpost by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ name: "edge-1", enrolled: false }));
    const n = await getOutpost("edge-1");
    expect(n.name).toBe("edge-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/outposts/edge-1");
  });

  it("posts the create body and returns the created outpost", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ name: "edge-2", description: "lab", enrolled: false }, 201),
    );
    const created = await createOutpost({ name: "edge-2", description: "lab" });
    expect(created.name).toBe("edge-2");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/outposts");
    expect(await req.json()).toMatchObject({ name: "edge-2", description: "lab" });
  });

  it("enrolls an outpost: POSTs to the :enroll custom method and returns the once-shown token", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ name: "edge-1", token: "og_secret_abc123" }),
    );
    const out = await enrollOutpost("edge-1");
    expect(out).toEqual({ name: "edge-1", token: "og_secret_abc123" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/outposts/edge-1:enroll");
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "outpost name already exists" }, 409));
    await expect(createOutpost({ name: "dup" })).rejects.toBeTruthy();
  });
});

// outpostStatus derives the liveness pill client-side from last_heartbeat_at against
// the outpost-silent rule's 90s window. It is pure, so
// a fixed `now` pins the three outcomes and the exact boundary.
describe("outpostStatus", () => {
  const outpost = (last?: string): Outpost => ({ name: "n", enrolled: true, last_heartbeat_at: last, tags: {} });
  const now = Date.parse("2026-07-07T12:00:00Z");

  it("is `never` when the outpost has never heartbeated", () => {
    expect(outpostStatus(outpost(undefined), now)).toBe("never");
  });
  it("is `up` when the last heartbeat is within the down window", () => {
    const recent = new Date(now - (OUTPOST_DOWN_AFTER_MS - 1_000)).toISOString();
    expect(outpostStatus(outpost(recent), now)).toBe("up");
  });
  it("is `down` when the last heartbeat is older than the down window", () => {
    const stale = new Date(now - (OUTPOST_DOWN_AFTER_MS + 1_000)).toISOString();
    expect(outpostStatus(outpost(stale), now)).toBe("down");
  });
  it("treats the exact window boundary as still up", () => {
    const edge = new Date(now - OUTPOST_DOWN_AFTER_MS).toISOString();
    expect(outpostStatus(outpost(edge), now)).toBe("up");
  });
});

// outpostFilterKeys are the console's shared faceted search over the outposts list: name
// (substring, the default) and status (exact, derived). Matching is client-side via
// lib/predicate, exactly as the other lists drive it.
const outposts: Outpost[] = [
  { name: "edge-hq", enrolled: true, last_heartbeat_at: new Date().toISOString(), tags: {} }, // up
  { name: "edge-east", enrolled: true, last_heartbeat_at: new Date(Date.now() - 10 * 60_000).toISOString(), tags: {} }, // down
  { name: "edge-new", enrolled: false, tags: {} }, // never
];
const matched = (chips: Chip[]): string[] => outposts.filter(buildPredicate(outpostFilterKeys, chips)).map((n) => n.name);

describe("outpostFilterKeys", () => {
  it("filters by name (substring)", () => {
    expect(matched([{ key: "name", op: "contains", values: ["east"] }])).toEqual(["edge-east"]);
  });
  it("filters by status (exact, derived)", () => {
    expect(matched([{ key: "status", op: "eq", values: ["up"] }])).toEqual(["edge-hq"]);
    expect(matched([{ key: "status", op: "eq", values: ["down"] }])).toEqual(["edge-east"]);
    expect(matched([{ key: "status", op: "eq", values: ["never"] }])).toEqual(["edge-new"]);
  });
  it("offers the status value catalog", () => {
    const byKey = Object.fromEntries(outpostFilterKeys.map((k) => [k.key, k]));
    expect(byKey.status.values!(outposts)).toEqual(["down", "never", "up"]);
  });
});
