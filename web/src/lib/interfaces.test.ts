import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listInterfaces,
  getInterface,
  createInterface,
  updateInterface,
  deleteInterface,
  interfaceTarget,
  interfaceFilterKeys,
  type Interface,
} from "./interfaces";
import { buildPredicate, type Chip } from "./predicate";

// The data layer is the unit under test; fetch is the seam we fake, so these assert
// the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("interfaces data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists interfaces and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ interfaces: [{ name: "disp-1-tcp", type: "tcp", component: "disp-1" }] }),
    );
    const ifaces = await listInterfaces();
    expect(ifaces).toHaveLength(1);
    expect(ifaces[0].name).toBe("disp-1-tcp");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/interfaces");
  });

  it("tolerates a null interfaces envelope (none yet)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ interfaces: null }));
    expect(await listInterfaces()).toEqual([]);
  });

  it("gets an interface by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ name: "disp-1-tcp", type: "tcp" }));
    const i = await getInterface("disp-1-tcp");
    expect(i.name).toBe("disp-1-tcp");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/interfaces/disp-1-tcp");
  });

  it("posts the create body (type, component, node, params.target) and returns the created interface", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ name: "disp-1-tcp", type: "tcp", component: "disp-1" }, 201),
    );
    const created = await createInterface({ name: "disp-1-tcp", type: "tcp", component: "disp-1", node: "edge-hq", params: { target: "10.0.0.1:22" } });
    expect(created.name).toBe("disp-1-tcp");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/interfaces");
    expect(await req.json()).toMatchObject({ name: "disp-1-tcp", type: "tcp", component: "disp-1", node: "edge-hq", params: { target: "10.0.0.1:22" } });
  });

  it("patches only the mutable fields (node, params) on update", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ name: "disp-1-tcp", type: "tcp" }));
    await updateInterface("disp-1-tcp", { node: "edge-east", params: { target: "9.9.9.9" } });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/interfaces/disp-1-tcp");
    expect(await req.json()).toEqual({ node: "edge-east", params: { target: "9.9.9.9" } });
  });

  it("deletes an interface by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteInterface("disp-1-tcp");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/interfaces/disp-1-tcp");
  });

  it("throws on an error status (e.g. a delete refused while a task references it)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "interface still referenced" }, 409));
    await expect(deleteInterface("disp-1-tcp")).rejects.toBeTruthy();
  });
});

// interfaceTarget renders the probed endpoint from the interface params, mirroring
// the read side: target, with :port appended only when a separate port is present.
describe("interfaceTarget", () => {
  const iface = (params?: Interface["params"]): Interface => ({ name: "i", type: "tcp", params });
  it("returns the bare target when there is no separate port", () => {
    expect(interfaceTarget(iface({ target: "10.0.0.1" }))).toBe("10.0.0.1");
  });
  it("passes an embedded host:port target through unchanged", () => {
    expect(interfaceTarget(iface({ target: "10.0.0.1:22" }))).toBe("10.0.0.1:22");
  });
  it("appends a separate port when params carry one", () => {
    expect(interfaceTarget(iface({ target: "10.0.0.1", port: 5000 }))).toBe("10.0.0.1:5000");
  });
  it("is empty when there is no target (real field only, never invented)", () => {
    expect(interfaceTarget(iface(undefined))).toBe("");
    expect(interfaceTarget(iface({}))).toBe("");
  });
});

// interfaceFilterKeys are the console's shared faceted search: name (substring), type
// (exact), and component (exact). Matching is client-side via lib/predicate.
const rows: Interface[] = [
  { name: "disp-1-tcp", type: "tcp", component: "disp-1", params: { target: "10.0.0.1:22" } },
  { name: "disp-1-icmp", type: "icmp", component: "disp-1", params: { target: "10.0.0.1" } },
  { name: "srv-tcp", type: "tcp", params: { target: "10.0.0.9:80" } },
];
const matched = (chips: Chip[]): string[] => rows.filter(buildPredicate(interfaceFilterKeys, chips)).map((r) => r.name);

describe("interfaceFilterKeys", () => {
  it("filters by name (substring)", () => {
    expect(matched([{ key: "name", op: "contains", values: ["icmp"] }])).toEqual(["disp-1-icmp"]);
  });
  it("filters by type (exact)", () => {
    expect(matched([{ key: "type", op: "eq", values: ["tcp"] }])).toEqual(["disp-1-tcp", "srv-tcp"]);
  });
  it("filters by component (exact), treating a server-hosted interface as component-less", () => {
    expect(matched([{ key: "component", op: "eq", values: ["disp-1"] }])).toEqual(["disp-1-tcp", "disp-1-icmp"]);
  });
  it("offers the type and component value catalogs", () => {
    const byKey = Object.fromEntries(interfaceFilterKeys.map((k) => [k.key, k]));
    expect(byKey.type.values!(rows)).toEqual(["icmp", "tcp"]);
    expect(byKey.component.values!(rows)).toEqual(["disp-1"]);
  });
});
