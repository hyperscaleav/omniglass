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
      jsonResponse({ interfaces: [{ id: "if-1", name: "disp-1-tcp", interface_type: "tcp", component: "disp-1" }] }),
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

  it("gets an interface by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "if-1", name: "disp-1-tcp", interface_type: "tcp" }));
    const i = await getInterface("if-1");
    expect(i.name).toBe("disp-1-tcp");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/interfaces/if-1");
  });

  it("posts the create body (type, component, node, params.target) and returns the created interface", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "if-1", name: "disp-1-tcp", interface_type: "tcp", component: "disp-1" }, 201),
    );
    const created = await createInterface({ interface_type: "tcp", component: "disp-1", node: "edge-hq", params: { target: "10.0.0.1:22" } });
    expect(created.id).toBe("if-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/interfaces");
    expect(await req.json()).toMatchObject({ interface_type: "tcp", component: "disp-1", node: "edge-hq", params: { target: "10.0.0.1:22" } });
  });

  it("patches only the mutable fields (node, params) on update, addressed by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "if-1", name: "disp-1-tcp", interface_type: "tcp" }));
    await updateInterface("if-1", { node: "edge-east", params: { target: "9.9.9.9" } });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/interfaces/if-1");
    expect(await req.json()).toEqual({ node: "edge-east", params: { target: "9.9.9.9" } });
  });

  it("deletes an interface by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteInterface("if-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/interfaces/if-1");
  });

  it("throws on an error status (e.g. a delete refused while a task references it)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "interface still referenced" }, 409));
    await expect(deleteInterface("if-1")).rejects.toBeTruthy();
  });
});

// interfaceTarget renders the probed endpoint from the interface params, mirroring
// the read side: target, with :port appended only when a separate port is present.
describe("interfaceTarget", () => {
  const iface = (params?: Interface["params"]): Interface => ({ id: "if-i", name: "i", interface_type: "tcp", params });
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
  { id: "if-1", name: "disp-1-tcp", interface_type: "tcp", component: "disp-1", params: { target: "10.0.0.1:22" } },
  { id: "if-2", name: "disp-1-icmp", interface_type: "icmp", component: "disp-1", params: { target: "10.0.0.1" } },
  { id: "if-3", name: "srv-tcp", interface_type: "tcp", params: { target: "10.0.0.9:80" } },
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
