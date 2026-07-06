import { describe, it, expect, vi, beforeEach } from "vitest";
import { listPrincipals, createPrincipal, updatePrincipal, createGrant, revokeGrant, setPrincipalActive, principalName, type Principal } from "./principals";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("principals data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists principals and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ principals: [{ id: "1", kind: "human", human: { username: "ops" }, grants: [] }] }),
    );
    const ps = await listPrincipals();
    expect(ps).toHaveLength(1);
    expect(ps[0].human?.username).toBe("ops");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/principals");
  });

  it("passes the kind filter as a query param", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ principals: [] }));
    await listPrincipals("human");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("kind=human");
  });

  it("posts the create body and returns the created principal", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "2", kind: "human", human: { username: "bob" }, grants: [] }, 201),
    );
    const created = await createPrincipal({ username: "bob", password: "brand-new-pw" });
    expect(created.human?.username).toBe("bob");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(await req.json()).toMatchObject({ username: "bob" });
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "username already exists" }, 409));
    await expect(createPrincipal({ username: "dup" })).rejects.toBeTruthy();
  });

  it("PATCHes the changed fields to the id path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "p1", kind: "human", human: { username: "renamed" }, grants: [] }, 200),
    );
    const out = await updatePrincipal("p1", { username: "renamed", display_name: "New" });
    expect(out.human?.username).toBe("renamed");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/principals/p1");
    expect(await req.json()).toMatchObject({ username: "renamed", display_name: "New" });
  });

  it("POSTs a grant to the principal's grants path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "g1", role: "viewer", scope_kind: "all" }, 201),
    );
    const g = await createGrant("p1", { role: "viewer", scope_kind: "all" });
    expect(g.id).toBe("g1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/principals/p1/grants");
    expect(await req.json()).toMatchObject({ role: "viewer", scope_kind: "all" });
  });

  it("DELETEs a grant by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await revokeGrant("p1", "g1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/principals/p1/grants/g1");
  });

  it("disable and enable POST to the right custom method", async () => {
    const calls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      calls.push((input as Request).url);
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    await setPrincipalActive("p1", false);
    await setPrincipalActive("p1", true);
    expect(calls[0]).toContain("/api/v1/principals/p1:disable");
    expect(calls[1]).toContain("/api/v1/principals/p1:enable");
  });

  it("principalName prefers display name, then username, then service label", () => {
    const human = (h: Partial<Principal["human"]>): Principal => ({ id: "x", kind: "human", active: true, human: h as never, grants: [] });
    expect(principalName(human({ username: "u", display_name: "Dee" }))).toBe("Dee");
    expect(principalName(human({ username: "u" }))).toBe("u");
    expect(principalName({ id: "s", kind: "service", active: true, service: { label: "svc" }, grants: [] })).toBe("svc");
  });
});
