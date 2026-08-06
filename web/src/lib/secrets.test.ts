import { describe, it, expect, vi, beforeEach } from "vitest";
import { listSecrets, createSecret, updateSecret, deleteSecret, revealSecret, copySecret } from "./secrets";
import { uuidFor } from "./testids";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("secrets data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists secrets and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ secrets: [{ id: uuidFor("1"), name: "poll", secret_type: "snmp-community", owner_kind: "platform", fields: [{ name: "community", value: "••••••", secret: true }] }] }),
    );
    const secrets = await listSecrets();
    expect(secrets).toHaveLength(1);
    expect(secrets[0].name).toBe("poll");
    expect(secrets[0].fields[0].secret).toBe(true);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/secrets");
  });

  it("posts the create body with the field map", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("2"), name: "poll", secret_type: "snmp-community", owner_kind: "location", owner_name: "room", fields: [] }, 201),
    );
    const created = await createSecret({ name: "poll", secret_type: "snmp-community", owner_kind: "location", owner: "room", fields: { community: "public" } });
    expect(created.owner_name).toBe("room");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    const sent = await req.json();
    expect(sent).toMatchObject({ name: "poll", owner_kind: "location", owner: "room", fields: { community: "public" } });
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "unknown secret_type" }, 422));
    await expect(createSecret({ name: "x", secret_type: "nope", owner_kind: "platform", fields: {} })).rejects.toBeTruthy();
  });

  it("patches the field values on update", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("sec_123"), name: "poll", secret_type: "snmp-community", owner_kind: "platform", fields: [] }),
    );
    await updateSecret("sec_123", { community: "rotated" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/secrets/sec_123");
    const sent = await req.json();
    expect(sent).toMatchObject({ fields: { community: "rotated" } });
  });

  it("reveals a secret's plaintext by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ fields: { community: "public" } }),
    );
    const fields = await revealSecret("sec_123");
    expect(fields.community).toBe("public");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/secrets/sec_123:reveal");
  });

  it("copies a secret's plaintext by id (distinct verb endpoint)", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ fields: { community: "public" } }),
    );
    const fields = await copySecret("sec_123");
    expect(fields.community).toBe("public");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/secrets/sec_123:copy");
  });

  it("deletes by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteSecret("sec_123");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/secrets/sec_123");
  });
});
