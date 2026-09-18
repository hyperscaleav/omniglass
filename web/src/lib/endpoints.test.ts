import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listEndpoints,
  getEndpoint,
  createEndpoint,
  updateEndpoint,
  deleteEndpoint,
  endpointTarget,
  endpointFilterKeys,
  type Endpoint,
} from "./endpoints";
import { buildPredicate, type Chip } from "./predicate";
import { uuidFor } from "./testids";

// The data layer is the unit under test; fetch is the seam we fake, so these assert
// the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("endpoints data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists endpoints and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ endpoints: [{ id: uuidFor("if-1"), name: "disp-1-tcp", transport: "tcp", component: "disp-1" }] }),
    );
    const ifaces = await listEndpoints();
    expect(ifaces).toHaveLength(1);
    expect(ifaces[0].name).toBe("disp-1-tcp");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/endpoints");
  });

  it("tolerates a null endpoints envelope (none yet)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ endpoints: null }));
    expect(await listEndpoints()).toEqual([]);
  });

  it("gets an endpoint by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: uuidFor("if-1"), name: "disp-1-tcp", transport: "tcp" }));
    const i = await getEndpoint("if-1");
    expect(i.name).toBe("disp-1-tcp");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/endpoints/if-1");
  });

  it("posts the create body (transport, component, node, params.target) and returns the created endpoint", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("if-1"), name: "disp-1-tcp", transport: "tcp", component: "disp-1" }, 201),
    );
    const created = await createEndpoint({ transport: "tcp", component: "disp-1", node: "edge-hq", params: { target: "10.0.0.1:22" } });
    expect(created.id).toBe(uuidFor("if-1"));
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/endpoints");
    expect(await req.json()).toMatchObject({ transport: "tcp", component: "disp-1", node: "edge-hq", params: { target: "10.0.0.1:22" } });
  });

  it("patches only the mutable fields (node, params) on update, addressed by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: uuidFor("if-1"), name: "disp-1-tcp", transport: "tcp" }));
    await updateEndpoint("if-1", { node: "edge-east", params: { target: "9.9.9.9" } });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/endpoints/if-1");
    expect(await req.json()).toEqual({ node: "edge-east", params: { target: "9.9.9.9" } });
  });

  it("deletes an endpoint by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteEndpoint("if-1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/endpoints/if-1");
  });

  it("throws on an error status (e.g. a delete refused while a task references it)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "endpoint still referenced" }, 409));
    await expect(deleteEndpoint("if-1")).rejects.toBeTruthy();
  });
});

// endpointTarget renders the probed address from the endpoint params, mirroring
// the read side: target, with :port appended only when a separate port is present.
describe("endpointTarget", () => {
  const iface = (params?: Endpoint["params"]): Endpoint => ({ id: uuidFor("if-i"), name: "i", transport: "tcp", params });
  it("returns the bare target when there is no separate port", () => {
    expect(endpointTarget(iface({ target: "10.0.0.1" }))).toBe("10.0.0.1");
  });
  it("passes an embedded host:port target through unchanged", () => {
    expect(endpointTarget(iface({ target: "10.0.0.1:22" }))).toBe("10.0.0.1:22");
  });
  it("appends a separate port when params carry one", () => {
    expect(endpointTarget(iface({ target: "10.0.0.1", port: 5000 }))).toBe("10.0.0.1:5000");
  });
  it("is empty when there is no target (real field only, never invented)", () => {
    expect(endpointTarget(iface(undefined))).toBe("");
    expect(endpointTarget(iface({}))).toBe("");
  });
});

// endpointFilterKeys are the console's shared faceted search: name (substring),
// transport (exact), and component (exact). Matching is client-side via lib/predicate.
const rows: Endpoint[] = [
  { id: uuidFor("if-1"), name: "disp-1-tcp", transport: "tcp", component: "disp-1", params: { target: "10.0.0.1:22" } },
  { id: uuidFor("if-2"), name: "disp-1-icmp", transport: "icmp", component: "disp-1", params: { target: "10.0.0.1" } },
  { id: uuidFor("if-3"), name: "srv-tcp", transport: "tcp", params: { target: "10.0.0.9:80" } },
];
const matched = (chips: Chip[]): string[] => rows.filter(buildPredicate(endpointFilterKeys, chips)).map((r) => r.name);

describe("endpointFilterKeys", () => {
  it("filters by name (substring)", () => {
    expect(matched([{ key: "name", op: "contains", values: ["icmp"] }])).toEqual(["disp-1-icmp"]);
  });
  it("filters by transport (exact)", () => {
    expect(matched([{ key: "transport", op: "eq", values: ["tcp"] }])).toEqual(["disp-1-tcp", "srv-tcp"]);
  });
  it("filters by component (exact), treating a server-hosted endpoint as component-less", () => {
    expect(matched([{ key: "component", op: "eq", values: ["disp-1"] }])).toEqual(["disp-1-tcp", "disp-1-icmp"]);
  });
  it("offers the transport and component value catalogs", () => {
    const byKey = Object.fromEntries(endpointFilterKeys.map((k) => [k.key, k]));
    expect(byKey.transport.values!(rows)).toEqual(["icmp", "tcp"]);
    expect(byKey.component.values!(rows)).toEqual(["disp-1"]);
  });
});
