import { describe, it, expect, vi, beforeEach } from "vitest";
import { listModels, createModel, updateModel, deleteModel } from "./component_models";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("component-models data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists models and unwraps the { models: [...] } envelope", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({
        models: [
          { id: "tsw-1070", display_name: "TSW-1070", make_id: "crestron", model_number: "TSW-1070-B-S", official: true },
          { id: "acme-123a", display_name: "Acme 123A", make_id: "acme", model_number: "123A", official: false },
        ],
      }),
    );
    const rows = await listModels();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("GET");
    expect(req.url).toContain("/api/v1/component-models");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ id: "tsw-1070", make_id: "crestron", model_number: "TSW-1070-B-S", official: true });
  });

  it("returns an empty list when the envelope has no models", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({}));
    const rows = await listModels();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(rows).toEqual([]);
  });

  it("throws when the list request errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "boom" }, 500));
    await expect(listModels()).rejects.toBeTruthy();
  });

  it("creates a model via POST with the body", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "acme-123a", display_name: "Acme 123A", make_id: "acme", model_number: "123A", official: false }, 201),
    );
    await createModel({ id: "acme-123a", display_name: "Acme 123A", make_id: "acme", model_number: "123A" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/component-models");
    const sent = await req.json();
    expect(sent).toMatchObject({ id: "acme-123a", make_id: "acme", model_number: "123A" });
  });

  it("throws when create errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "id already exists" }, 409));
    await expect(createModel({ id: "acme-123a", display_name: "Acme 123A", make_id: "acme", model_number: "123A" })).rejects.toBeTruthy();
  });

  it("updates a model via PATCH to the id path with the body, and never sends make_id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "acme-123a", display_name: "Acme 123A Rev B", make_id: "acme", model_number: "123A", official: false }),
    );
    await updateModel("acme-123a", { display_name: "Acme 123A Rev B" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/component-models/acme-123a");
    const sent = await req.json();
    expect(sent).toMatchObject({ display_name: "Acme 123A Rev B" });
    expect(sent).not.toHaveProperty("make_id");
  });

  it("throws when update errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "official model is read-only" }, 422));
    await expect(updateModel("tsw-1070", { display_name: "X" })).rejects.toBeTruthy();
  });

  it("deletes a model by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteModel("acme-123a");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/component-models/acme-123a");
  });

  it("throws when delete errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ title: "official model is read-only" }, 422));
    await expect(deleteModel("tsw-1070")).rejects.toBeTruthy();
  });
});
