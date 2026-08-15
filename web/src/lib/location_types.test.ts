import { describe, it, expect, vi, beforeEach } from "vitest";
import { listLocationTypes, createLocationType, updateLocationType, deleteLocationType } from "./location_types";
import { uuidFor } from "./testids";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("location_types data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists the registry and normalizes a missing allowed_parent_types to empty", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({
        location_types: [{ id: uuidFor("lt-campus"), name: "campus", label: "Campus", icon: "landmark", official: false }],
      }),
    );
    const rows = await listLocationTypes();
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ name: "campus", icon: "landmark", allowed_parent_types: [] });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/location-types");
  });

  it("creates a location type via the typed literal path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("lt-wing"), name: "wing", label: "Wing", icon: "map-pin", official: false }, 201),
    );
    await createLocationType({ name: "wing", label: "Wing", icon: "map-pin" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/location-types");
    const sent = await req.json();
    expect(sent).toMatchObject({ name: "wing", label: "Wing", icon: "map-pin" });
  });

  it("creates a location type with allowed_parent_types", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("lt-wing"), name: "wing", label: "Wing", icon: "map-pin", official: false, allowed_parent_types: ["campus"] }, 201),
    );
    await createLocationType({ name: "wing", label: "Wing", icon: "map-pin", allowed_parent_types: ["campus"] });
    const req = fetchMock.mock.calls[0][0] as Request;
    const sent = await req.json();
    expect(sent).toMatchObject({ allowed_parent_types: ["campus"] });
  });

  it("updates a location type by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("lt-wing"), name: "wing", label: "Wing v2", official: false }),
    );
    await updateLocationType("wing", { label: "Wing v2" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/location-types/wing");
    const sent = await req.json();
    expect(sent).toMatchObject({ label: "Wing v2" });
  });

  it("sends an explicit empty allowed_parent_types to clear the constraint", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("lt-wing"), name: "wing", label: "Wing", official: false, allowed_parent_types: [] }),
    );
    await updateLocationType("wing", { allowed_parent_types: [] });
    const req = fetchMock.mock.calls[0][0] as Request;
    const sent = await req.json();
    expect(sent).toHaveProperty("allowed_parent_types");
    expect(sent.allowed_parent_types).toEqual([]);
  });

  it("omits allowed_parent_types entirely when not touching the constraint", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("lt-wing"), name: "wing", label: "X", official: false, allowed_parent_types: ["campus"] }),
    );
    await updateLocationType("wing", { label: "X" });
    const req = fetchMock.mock.calls[0][0] as Request;
    const sent = await req.json();
    expect(sent).not.toHaveProperty("allowed_parent_types");
  });

  it("deletes a location type by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteLocationType("wing");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/location-types/wing");
  });
});
