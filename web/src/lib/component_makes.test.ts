import { describe, it, expect, vi, beforeEach } from "vitest";
import { listMakes, createMake, updateMake, deleteMake } from "./component_makes";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("component-makes data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists makes and unwraps the { makes: [...] } envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({
        makes: [
          { id: "crestron", display_name: "Crestron", official: true, icon: "crestron-logo" },
          { id: "acme", display_name: "Acme", official: false },
        ],
      }),
    );
    const rows = await listMakes();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("GET");
    expect(req.url).toContain("/api/v1/component-makes");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ id: "crestron", display_name: "Crestron", official: true, icon: "crestron-logo" });
  });

  it("returns an empty list when the envelope has no makes", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({}));
    const rows = await listMakes();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(rows).toEqual([]);
  });

  it("throws when the list request errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "boom" }, 500));
    await expect(listMakes()).rejects.toBeTruthy();
  });

  it("creates a make via POST with the body", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "acme", display_name: "Acme", official: false }, 201),
    );
    await createMake({ id: "acme", display_name: "Acme", website: "https://acme.example" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/component-makes");
    const sent = await req.json();
    expect(sent).toMatchObject({ id: "acme", display_name: "Acme", website: "https://acme.example" });
  });

  it("throws when create errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "id already exists" }, 409));
    await expect(createMake({ id: "acme", display_name: "Acme" })).rejects.toBeTruthy();
  });

  it("updates a make via PATCH to the id path with the body", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "acme", display_name: "Acme Corp", official: false }),
    );
    await updateMake("acme", { display_name: "Acme Corp" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/component-makes/acme");
    const sent = await req.json();
    expect(sent).toMatchObject({ display_name: "Acme Corp" });
  });

  it("throws when update errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "official make is read-only" }, 422));
    await expect(updateMake("crestron", { display_name: "X" })).rejects.toBeTruthy();
  });

  it("deletes a make by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteMake("acme");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/component-makes/acme");
  });

  it("throws when delete errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "official make is read-only" }, 422));
    await expect(deleteMake("crestron")).rejects.toBeTruthy();
  });
});
