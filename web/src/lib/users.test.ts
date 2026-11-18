import { describe, it, expect, vi, beforeEach } from "vitest";
import { listUsers, createUser, updateUser, createGrant, revokeGrant, setUserActive, userName, kindLabel, roleFilterKeys, type User, type Role } from "./users";
import { buildPredicate, type Chip } from "./predicate";
import { uuidFor } from "./testids";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("users data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists users and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ users: [{ id: "1", kind: "person", person: { username: "ops" }, grants: [] }] }),
    );
    const ps = await listUsers();
    expect(ps).toHaveLength(1);
    expect(ps[0].person?.username).toBe("ops");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/users");
  });

  it("passes the kind filter as a query param", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ users: [] }));
    await listUsers("person");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("kind=person");
  });

  it("posts the create body and returns the created user", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "2", kind: "person", person: { username: "bob" }, grants: [] }, 201),
    );
    const created = await createUser({ username: "bob", password: "brand-new-pw" });
    expect(created.person?.username).toBe("bob");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(await req.json()).toMatchObject({ username: "bob" });
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "username already exists" }, 409));
    await expect(createUser({ username: "dup" })).rejects.toBeTruthy();
  });

  it("PATCHes the changed fields to the id path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "p1", kind: "person", person: { username: "renamed" }, grants: [] }, 200),
    );
    const out = await updateUser("p1", { username: "renamed", label: "New" });
    expect(out.person?.username).toBe("renamed");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/users/p1");
    expect(await req.json()).toMatchObject({ username: "renamed", label: "New" });
  });

  it("POSTs a grant to the user's grants path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "g1", role: "viewer", scope_kind: "all" }, 201),
    );
    const g = await createGrant("p1", { role: "viewer", scope_kind: "all" });
    expect(g.id).toBe("g1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/users/p1/grants");
    expect(await req.json()).toMatchObject({ role: "viewer", scope_kind: "all" });
  });

  it("DELETEs a grant by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await revokeGrant("p1", "g1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/users/p1/grants/g1");
  });

  it("disable and enable POST to the right custom method", async () => {
    const calls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      calls.push((input as Request).url);
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    await setUserActive("p1", false);
    await setUserActive("p1", true);
    expect(calls[0]).toContain("/api/v1/users/p1:disable");
    expect(calls[1]).toContain("/api/v1/users/p1:enable");
  });

  it("userName prefers label, then username, then the service name", () => {
    const human = (h: Partial<User["person"]>): User => ({ id: "x", kind: "person", active: true, person: h as never, grants: [] });
    expect(userName(human({ username: "u", label: "Dee" }))).toBe("Dee");
    expect(userName(human({ username: "u" }))).toBe("u");
    expect(userName({ id: uuidFor("p-svc"), kind: "machine", active: true, machine: { name: "svc" }, grants: [] })).toBe("svc");
  });
});

// The role facets are the console's shared faceted search applied to the roles
// catalog: name (label or id substring, the default), id (exact), and
// permission (a substring over the role's effective permission strings). These
// pin the filtering behavior the FilterBar/ListShell drives, independent of the
// page render.
const role = (p: Partial<Role>): Role => ({ id: p.id ?? "x", name: p.id ?? "x", official: true, permissions: [], inherits: [], ...p });

const roles: Role[] = [
  role({ id: "viewer", label: "Viewer", effective_permissions: ["*:read"] }),
  role({ id: "operator", label: "Operator", effective_permissions: ["*:read", "alarm:ack"] }),
  role({ id: "admin", label: "Administrator", effective_permissions: ["*:read", "user:*", "audit:read:admin"] }),
  role({ id: "owner", label: "Owner", effective_permissions: [">"] }),
];

const matched = (chips: Chip[]): string[] => roles.filter(buildPredicate(roleFilterKeys, chips)).map((r) => r.id);

describe("roleFilterKeys", () => {
  it("filters by name (label or id substring)", () => {
    expect(matched([{ key: "name", op: "contains", values: ["op"] }])).toEqual(["operator"]);
    // The label matches even when the id would not.
    expect(matched([{ key: "name", op: "contains", values: ["admini"] }])).toEqual(["admin"]);
  });
  it("filters by id (exact)", () => {
    expect(matched([{ key: "id", op: "eq", values: ["owner"] }])).toEqual(["owner"]);
    // Within one chip, values OR: viewer OR owner.
    expect(matched([{ key: "id", op: "eq", values: ["viewer", "owner"] }])).toEqual(["viewer", "owner"]);
  });
  it("filters by permission (substring over the effective set)", () => {
    // An admin can find every role that grants audit.
    expect(matched([{ key: "permission", op: "contains", values: ["audit"] }])).toEqual(["admin"]);
    expect(matched([{ key: "permission", op: "contains", values: ["alarm"] }])).toEqual(["operator"]);
    // The wildcard read floor is on every non-superuser role.
    expect(matched([{ key: "permission", op: "contains", values: ["*:read"] }])).toEqual(["viewer", "operator", "admin"]);
  });
  it("ANDs across chips", () => {
    expect(matched([
      { key: "permission", op: "contains", values: ["*:read"] },
      { key: "id", op: "eq", values: ["admin"] },
    ])).toEqual(["admin"]);
  });
  // The facet searches what the operator READS (#683). A row with no label falls
  // back to its name, and the haystack must not carry the word "undefined" for
  // it: several facets built their haystack by interpolating the raw column, so
  // typing "undefined" matched every unlabelled row and nothing else did.
  it("matches the rendered label, never the word undefined", () => {
    const unlabelled = [role({ id: "bare" })];
    const name = roleFilterKeys.find((k) => k.key === "name")!;
    expect(name.get(unlabelled[0])).not.toContain("undefined");
    expect(unlabelled.filter(buildPredicate(roleFilterKeys, [{ key: "name", op: "contains", values: ["bare"] }]))).toHaveLength(1);
    expect(unlabelled.filter(buildPredicate(roleFilterKeys, [{ key: "name", op: "contains", values: ["undefined"] }]))).toHaveLength(0);
  });

  it("offers a sorted, de-duplicated value catalog per facet", () => {
    const byKey = Object.fromEntries(roleFilterKeys.map((k) => [k.key, k]));
    expect(byKey.name.values!(roles)).toEqual(["Administrator", "Operator", "Owner", "Viewer"]);
    expect(byKey.id.values!(roles)).toEqual(["admin", "operator", "owner", "viewer"]);
    // The permission catalog is the distinct resource heads plus the superuser tail.
    expect(byKey.permission.values!(roles)).toEqual(["*", ">", "alarm", "audit", "user"]);
  });
});

// ADR-2036: a user's kind reads person or machine on every console surface,
// while the wire still carries human and service until #2075.
describe("kindLabel", () => {
  it("reads the wire kinds as person and machine", () => {
    expect(kindLabel("person")).toBe("person");
    expect(kindLabel("machine")).toBe("machine");
  });
  it("passes any other kind through", () => {
    expect(kindLabel("outpost")).toBe("outpost");
  });
});
