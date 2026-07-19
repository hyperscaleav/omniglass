import { describe, it, expect, vi, beforeEach } from "vitest";
import { listNodes, getNode, createNode, enrollNode, nodeStatus, NODE_DOWN_AFTER_MS, nodeFilterKeys, type Node } from "./nodes";
import { buildPredicate, type Chip } from "./predicate";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("nodes data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists nodes and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ nodes: [{ name: "edge-1", enrolled: true }] }),
    );
    const ns = await listNodes();
    expect(ns).toHaveLength(1);
    expect(ns[0].name).toBe("edge-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/nodes");
  });

  it("tolerates a null nodes envelope (no nodes yet)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ nodes: null }));
    expect(await listNodes()).toEqual([]);
  });

  it("gets a node by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ name: "edge-1", enrolled: false }));
    const n = await getNode("edge-1");
    expect(n.name).toBe("edge-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/nodes/edge-1");
  });

  it("posts the create body and returns the created node", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ name: "edge-2", description: "lab", enrolled: false }, 201),
    );
    const created = await createNode({ name: "edge-2", description: "lab" });
    expect(created.name).toBe("edge-2");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/nodes");
    expect(await req.json()).toMatchObject({ name: "edge-2", description: "lab" });
  });

  it("enrolls a node: POSTs to the :enroll custom method and returns the once-shown token", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ name: "edge-1", token: "og_secret_abc123" }),
    );
    const out = await enrollNode("edge-1");
    expect(out).toEqual({ name: "edge-1", token: "og_secret_abc123" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/nodes/edge-1:enroll");
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "node name already exists" }, 409));
    await expect(createNode({ name: "dup" })).rejects.toBeTruthy();
  });
});

// nodeStatus derives the liveness pill client-side from last_heartbeat_at against
// the server's down window (OMNIGLASS_NODE_DOWN_AFTER, default 90s). It is pure, so
// a fixed `now` pins the three outcomes and the exact boundary.
describe("nodeStatus", () => {
  const node = (last?: string): Node => ({ name: "n", enrolled: true, last_heartbeat_at: last });
  const now = Date.parse("2026-07-07T12:00:00Z");

  it("is `never` when the node has never heartbeated", () => {
    expect(nodeStatus(node(undefined), now)).toBe("never");
  });
  it("is `up` when the last heartbeat is within the down window", () => {
    const recent = new Date(now - (NODE_DOWN_AFTER_MS - 1_000)).toISOString();
    expect(nodeStatus(node(recent), now)).toBe("up");
  });
  it("is `down` when the last heartbeat is older than the down window", () => {
    const stale = new Date(now - (NODE_DOWN_AFTER_MS + 1_000)).toISOString();
    expect(nodeStatus(node(stale), now)).toBe("down");
  });
  it("treats the exact window boundary as still up", () => {
    const edge = new Date(now - NODE_DOWN_AFTER_MS).toISOString();
    expect(nodeStatus(node(edge), now)).toBe("up");
  });
});

// nodeFilterKeys are the console's shared faceted search over the nodes list: name
// (substring, the default) and status (exact, derived). Matching is client-side via
// lib/predicate, exactly as the other lists drive it.
const nodes: Node[] = [
  { name: "edge-hq", enrolled: true, last_heartbeat_at: new Date().toISOString() }, // up
  { name: "edge-east", enrolled: true, last_heartbeat_at: new Date(Date.now() - 10 * 60_000).toISOString() }, // down
  { name: "edge-new", enrolled: false }, // never
];
const matched = (chips: Chip[]): string[] => nodes.filter(buildPredicate(nodeFilterKeys, chips)).map((n) => n.name);

describe("nodeFilterKeys", () => {
  it("filters by name (substring)", () => {
    expect(matched([{ key: "name", op: "contains", values: ["east"] }])).toEqual(["edge-east"]);
  });
  it("filters by status (exact, derived)", () => {
    expect(matched([{ key: "status", op: "eq", values: ["up"] }])).toEqual(["edge-hq"]);
    expect(matched([{ key: "status", op: "eq", values: ["down"] }])).toEqual(["edge-east"]);
    expect(matched([{ key: "status", op: "eq", values: ["never"] }])).toEqual(["edge-new"]);
  });
  it("offers the status value catalog", () => {
    const byKey = Object.fromEntries(nodeFilterKeys.map((k) => [k.key, k]));
    expect(byKey.status.values!(nodes)).toEqual(["down", "never", "up"]);
  });
});
