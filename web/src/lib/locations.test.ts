import { describe, it, expect, vi, beforeEach } from "vitest";
import { listLocations, createLocation, updateLocation, moveLocation, deleteLocation } from "./locations";
import { uuidFor } from "./testids";

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
      jsonResponse({ locations: [{ id: uuidFor("1"), name: "hq", location_type: "campus" }] }),
    );
    const locs = await listLocations();
    expect(locs).toHaveLength(1);
    expect(locs[0].name).toBe("hq");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/locations");
    // No bearer header is attached when no token is stored (the cookie path).
    expect(req.headers.get("Authorization")).toBeNull();
  });

  it("posts the create body", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("2"), name: "hq-b1", location_type: "building" }, 201),
    );
    const created = await createLocation({ name: "hq-b1", location_type: "building", parent: "hq" });
    expect(created.name).toBe("hq-b1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    const sent = await req.json();
    expect(sent).toMatchObject({ name: "hq-b1", location_type: "building", parent: "hq" });
  });

  it("patches label and location_type, never parent", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("4"), name: "hq-b1", location_type: "building", label: "Building 1" }),
    );
    await updateLocation("hq-b1", { label: "Building 1", location_type: "building" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    const sent = await req.json();
    expect(sent).not.toHaveProperty("parent");
  });

  it("posts :move to move a location, not PATCH", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("3"), name: "hq-b1", location_type: "building", parent: "lab" }),
    );
    const moved = await moveLocation("hq-b1", "lab");
    expect(moved.name).toBe("hq-b1");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/locations/hq-b1:move");
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
