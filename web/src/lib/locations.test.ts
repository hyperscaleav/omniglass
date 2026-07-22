import { describe, it, expect, vi, beforeEach } from "vitest";
import { listLocations, listLocationTypes, createLocation, updateLocation, deleteLocation } from "./locations";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("locations data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists locations and unwraps the envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ locations: [{ id: "1", name: "hq", location_type: "campus" }] }),
    );
    const locs = await listLocations();
    expect(locs).toHaveLength(1);
    expect(locs[0].name).toBe("hq");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/locations");
    // No bearer header is attached when no token is stored (the cookie path).
    expect(req.headers.get("Authorization")).toBeNull();
  });

  it("lists location types and unwraps the registry envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ location_types: [{ id: "campus", display_name: "Campus", official: true, allowed_parent_types: ["root"] }] }),
    );
    const types = await listLocationTypes();
    expect(types).toHaveLength(1);
    expect(types[0]).toMatchObject({ id: "campus", display_name: "Campus", allowed_parent_types: ["root"] });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/types/location");
  });

  it("posts the create body", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "2", name: "hq-b1", location_type: "building" }, 201),
    );
    const created = await createLocation({ name: "hq-b1", location_type: "building", parent: "hq" });
    expect(created.name).toBe("hq-b1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    const sent = await req.json();
    expect(sent).toMatchObject({ name: "hq-b1", location_type: "building", parent: "hq" });
  });

  it("patches the parent to move a location", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "3", name: "hq-b1", location_type: "building", parent: "lab" }),
    );
    const moved = await updateLocation("hq-b1", { parent: "lab" });
    expect(moved.name).toBe("hq-b1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    const sent = await req.json();
    expect(sent).toMatchObject({ parent: "lab" });
  });

  it("throws on an error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "unknown location_type" }, 422));
    await expect(createLocation({ name: "x", location_type: "galaxy" })).rejects.toBeTruthy();
  });

  it("deletes by name", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteLocation("hq-r1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/locations/hq-r1");
  });
});
