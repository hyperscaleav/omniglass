import { describe, it, expect, vi, beforeEach } from "vitest";
import { listTags, createTag, updateTag, deleteTag, appliesToLabel } from "./tags";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("tags data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists tags and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ tags: [{ id: "1", name: "environment", applies_to: [], propagates: true }] }),
    );
    const tags = await listTags();
    expect(tags).toHaveLength(1);
    expect(tags[0].name).toBe("environment");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/tags");
  });

  it("posts the create body with applies_to and propagates", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "2", name: "rack_position", applies_to: ["location"], propagates: false }, 201),
    );
    const created = await createTag({ name: "rack_position", applies_to: ["location"], propagates: false });
    expect(created.propagates).toBe(false);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    const sent = await req.json();
    expect(sent).toMatchObject({ name: "rack_position", applies_to: ["location"], propagates: false });
  });

  it("patches the governance fields on update, addressing by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "1", name: "environment", applies_to: ["component"], propagates: true }),
    );
    await updateTag("environment", { applies_to: ["component"], propagates: true });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/tags/environment");
    const sent = await req.json();
    expect(sent).toMatchObject({ applies_to: ["component"], propagates: true });
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "tag key invalid" }, 422));
    await expect(createTag({ name: "Bad Key" })).rejects.toBeTruthy();
  });

  it("deletes by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteTag("environment");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/tags/environment");
  });
});

describe("tag helpers", () => {
  it("labels an applies_to set", () => {
    expect(appliesToLabel([])).toBe("Any");
    expect(appliesToLabel(["component"])).toBe("component");
    expect(appliesToLabel(["system", "location"])).toBe("system, location");
  });
});
