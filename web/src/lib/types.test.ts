import { describe, it, expect, vi, beforeEach } from "vitest";
import { listTypes, createType, updateType, deleteType } from "./types";

// The data layer is the unit under test; fetch is the seam we fake, so these
// assert the request shape and the response handling without a server.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("types data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists all four registries and tags each row's kind", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = (input as Request).url;
      if (url.includes("/types/location")) {
        return jsonResponse({
          location_types: [{ id: "campus", display_name: "Campus", rank: 10, icon: "building", official: true }],
        });
      }
      if (url.includes("/types/system")) {
        return jsonResponse({
          system_types: [{ id: "kiosk", display_name: "Kiosk", rank: 20, official: true }],
        });
      }
      if (url.includes("/types/component")) {
        return jsonResponse({
          component_types: [{ id: "relay", display_name: "Relay", rank: 30, official: false }],
        });
      }
      if (url.includes("/types/secret")) {
        return jsonResponse({
          secret_types: [
            {
              id: "credentials",
              display_name: "Credentials",
              official: true,
              fields: [{ name: "username", type: "string", secret: false, origin: "operator" }],
            },
          ],
        });
      }
      throw new Error(`unexpected url: ${url}`);
    });

    const rows = await listTypes();
    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(rows).toHaveLength(4);

    const location = rows.find((r) => r.kind === "location");
    expect(location).toMatchObject({ kind: "location", id: "campus", icon: "building", rank: 10 });

    const system = rows.find((r) => r.kind === "system");
    expect(system).toMatchObject({ kind: "system", id: "kiosk", rank: 20 });
    expect(system?.icon).toBeUndefined();
    expect(system?.fields).toBeUndefined();

    const component = rows.find((r) => r.kind === "component");
    expect(component).toMatchObject({ kind: "component", id: "relay", rank: 30 });

    const secret = rows.find((r) => r.kind === "secret");
    expect(secret).toMatchObject({ kind: "secret", id: "credentials" });
    expect(secret?.rank).toBeUndefined();
    expect(secret?.fields).toEqual([{ name: "username", type: "string", secret: false, origin: "operator" }]);
  });

  it("creates a location type via the typed literal path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "wing", display_name: "Wing", rank: 40, icon: "map-pin", official: false }, 201),
    );
    await createType("location", { id: "wing", display_name: "Wing", rank: 40, icon: "map-pin" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/types/location");
    const sent = await req.json();
    expect(sent).toMatchObject({ id: "wing", display_name: "Wing", rank: 40, icon: "map-pin" });
  });

  it("rejects creating a secret type without calling fetch", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    await expect(createType("secret", { id: "x", display_name: "X" })).rejects.toThrow(/read-only/);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("updates a system type by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: "kiosk", display_name: "Kiosk v2", rank: 20, official: true }),
    );
    await updateType("system", "kiosk", { display_name: "Kiosk v2" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain("/api/v1/types/system/kiosk");
    const sent = await req.json();
    expect(sent).toMatchObject({ display_name: "Kiosk v2" });
  });

  it("rejects updating a secret type without calling fetch", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    await expect(updateType("secret", "credentials", { display_name: "X" })).rejects.toThrow(/read-only/);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("deletes a component type by id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteType("component", "relay");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("DELETE");
    expect(req.url).toContain("/api/v1/types/component/relay");
  });

  it("rejects deleting a secret type without calling fetch", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    await expect(deleteType("secret", "credentials")).rejects.toThrow(/read-only/);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
