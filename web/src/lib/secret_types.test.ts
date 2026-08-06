import { describe, it, expect, vi, beforeEach } from "vitest";
import { listSecretTypes } from "./secret_types";
import { uuidFor } from "./testids";

// The registry read is the module's whole surface: secret types are seed-owned
// reference data with no write routes, so there are no write wrappers to test.
function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("secret_types data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists the registry with each type's declared fields", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({
        secret_types: [
          {
            id: uuidFor("st-snmp-community"),
            name: "snmp-community",
            display_name: "SNMP Community",
            official: true,
            fields: [{ name: "community", type: "string", secret: true, origin: "operator" }],
          },
        ],
      }),
    );
    const rows = await listSecretTypes();
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ name: "snmp-community", official: true });
    expect(rows[0].fields).toEqual([{ name: "community", type: "string", secret: true, origin: "operator" }]);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/secret-types");
  });

  it("normalizes a missing fields list to empty", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ secret_types: [{ id: uuidFor("st-bare"), name: "bare", display_name: "Bare", official: true }] }),
    );
    const rows = await listSecretTypes();
    expect(rows[0].fields).toEqual([]);
  });
});
