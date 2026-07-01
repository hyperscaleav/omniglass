import { describe, it, expect, vi, beforeEach } from "vitest";
import { listPrincipals, createPrincipal, updatePrincipal, principalName, type Principal } from "./principals";

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

  it("principalName prefers display name, then username, then service label", () => {
    const human = (h: Partial<Principal["human"]>): Principal => ({ id: "x", kind: "human", human: h as never, grants: [] });
    expect(principalName(human({ username: "u", display_name: "Dee" }))).toBe("Dee");
    expect(principalName(human({ username: "u" }))).toBe("u");
    expect(principalName({ id: "s", kind: "service", service: { label: "svc" }, grants: [] })).toBe("svc");
  });
});
